// Package storage provides utilities for efficient file caching and management.
package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

// FileInfo represents metadata about a cached file.
type FileInfo struct {
	Path    string    // Absolute path to the file
	Size    int64     // File size in bytes
	ModTime time.Time // Last modification time
}

// FileStore manages a cache of files with concurrent access control.
type FileStore struct {
	cache       *lru.Cache
	cacheDir    string
	mu          sync.RWMutex
	maxSize     int64
	currentSize int64
	sem         *semaphore.Weighted
}

// NewFileStore creates a new FileStore instance.
//
// Parameters:
//   - cacheSize: Maximum number of files to cache
//   - cacheDir: Directory to store cached files
//   - maxConcurrency: Maximum number of concurrent operations
//
// Returns:
//   - *FileStore: Newly created FileStore
//   - error: Any error encountered during creation
func NewFileStore(cacheSize int, cacheDir string, maxConcurrency int64) (*FileStore, error) {
	cache, err := lru.New(cacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache: %w", err)
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	return &FileStore{
		cache:    cache,
		cacheDir: cacheDir,
		maxSize:  int64(cacheSize) * 1024 * 1024, // Convert MB to bytes
		sem:      semaphore.NewWeighted(maxConcurrency),
	}, nil
}

// Store caches a file with the given key.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - key: Unique identifier for the file
//   - reader: Source of file content
//   - size: Expected size of the file
//
// Returns:
//   - error: Any error encountered during storage
func (fs *FileStore) Store(ctx context.Context, key string, reader io.Reader, size int64) error {
	if err := fs.sem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer fs.sem.Release(1)

	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.currentSize+size > fs.maxSize {
		if err := fs.evict(ctx, size); err != nil {
			return fmt.Errorf("failed to evict files: %w", err)
		}
	}

	filePath := filepath.Join(fs.cacheDir, key)
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	written, err := io.Copy(file, reader)
	if err != nil {
		os.Remove(filePath)
		return fmt.Errorf("failed to write file: %w", err)
	}

	fs.cache.Add(key, filePath)
	fs.currentSize += written

	return nil
}

// Get retrieves a cached file.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - key: Unique identifier for the file
//
// Returns:
//   - io.ReadCloser: Reader for the file content
//   - error: Any error encountered during retrieval
func (fs *FileStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := fs.sem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to acquire semaphore: %w", err)
	}

	fs.mu.RLock()
	filePathInterface, ok := fs.cache.Get(key)
	fs.mu.RUnlock()

	if !ok {
		fs.sem.Release(1)
		return nil, fmt.Errorf("file not found in cache")
	}

	filePath, ok := filePathInterface.(string)
	if !ok {
		fs.sem.Release(1)
		return nil, fmt.Errorf("invalid cache entry")
	}

	file, err := os.Open(filePath)
	if err != nil {
		fs.sem.Release(1)
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	return &semaphoreCloser{
		ReadCloser: file,
		sem:        fs.sem,
	}, nil
}

// evict removes files from the cache to free up space.
func (fs *FileStore) evict(ctx context.Context, sizeNeeded int64) error {
	for fs.currentSize+sizeNeeded > fs.maxSize {
		key, filePathInterface, ok := fs.cache.RemoveOldest()
		if !ok {
			return fmt.Errorf("failed to evict file from cache")
		}

		filePath, ok := filePathInterface.(string)
		if !ok {
			return fmt.Errorf("invalid cache entry")
		}

		info, err := os.Stat(filePath)
		if err != nil {
			return fmt.Errorf("failed to get file info: %w", err)
		}

		if err := os.Remove(filePath); err != nil {
			return fmt.Errorf("failed to remove file: %w", err)
		}

		fs.currentSize -= info.Size()
		fs.cache.Remove(key)
	}
	return nil
}

// Close cleans up resources used by the FileStore.
func (fs *FileStore) Close() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	var g errgroup.Group
	for _, key := range fs.cache.Keys() {
		key := key
		g.Go(func() error {
			filePathInterface, ok := fs.cache.Get(key)
			if !ok {
				return nil
			}
			filePath, ok := filePathInterface.(string)
			if !ok {
				return fmt.Errorf("invalid cache entry for key %v", key)
			}
			return os.Remove(filePath)
		})
	}

	return g.Wait()
}

// CloseFile removes a specific file from the cache.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - key: Unique identifier for the file
//
// Returns:
//   - error: Any error encountered during the operation
func (fs *FileStore) CloseFile(ctx context.Context, key string) error {
	if err := fs.sem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer fs.sem.Release(1)

	fs.mu.Lock()
	defer fs.mu.Unlock()

	filePathInterface, ok := fs.cache.Get(key)
	if !ok {
		return fmt.Errorf("file not found in cache")
	}

	filePath, ok := filePathInterface.(string)
	if !ok {
		return fmt.Errorf("invalid cache entry")
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to remove file: %w", err)
	}

	fs.cache.Remove(key)
	return nil
}

// semaphoreCloser wraps an io.ReadCloser with a semaphore.
type semaphoreCloser struct {
	io.ReadCloser
	sem *semaphore.Weighted
}

// Close closes the underlying ReadCloser and releases the semaphore.
func (sc *semaphoreCloser) Close() error {
	err := sc.ReadCloser.Close()
	sc.sem.Release(1)
	return err
}

// GetFileInfo retrieves metadata about a cached file.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - key: Unique identifier for the file
//
// Returns:
//   - *FileInfo: Metadata about the file
//   - error: Any error encountered during the operation
func (fs *FileStore) GetFileInfo(ctx context.Context, key string) (*FileInfo, error) {
	if err := fs.sem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer fs.sem.Release(1)

	fs.mu.RLock()
	filePathInterface, ok := fs.cache.Get(key)
	fs.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("file not found in cache")
	}

	filePath, ok := filePathInterface.(string)
	if !ok {
		return nil, fmt.Errorf("invalid cache entry")
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	return &FileInfo{
		Path:    filePath,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}, nil
}

// ListFiles returns metadata for all cached files.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//
// Returns:
//   - []*FileInfo: Slice of file metadata
//   - error: Any error encountered during the operation
func (fs *FileStore) ListFiles(ctx context.Context) ([]*FileInfo, error) {
	if err := fs.sem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer fs.sem.Release(1)

	fs.mu.RLock()
	defer fs.mu.RUnlock()

	files := make([]*FileInfo, 0, fs.cache.Len())
	for _, key := range fs.cache.Keys() {
		filePathInterface, ok := fs.cache.Get(key)
		if !ok {
			continue
		}

		filePath, ok := filePathInterface.(string)
		if !ok {
			return nil, fmt.Errorf("invalid cache entry for key %v", key)
		}

		info, err := os.Stat(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to get file info: %w", err)
		}

		files = append(files, &FileInfo{
			Path:    filePath,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}

	return files, nil
}
