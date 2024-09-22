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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

// FileInfo represents metadata about a cached file.
type FileInfo struct {
	Path     string    // Absolute path to the file
	Size     int64     // File size in bytes
	ModTime  time.Time // Last modification time
	UseCount int64     // Number of times the file has been accessed
}

// FileStore manages a cache of files with concurrent access control.
type FileStore struct {
	cache       *lru.Cache
	cacheDir    string
	mu          sync.RWMutex
	maxSize     int64
	currentSize int64
	sem         *semaphore.Weighted
	metrics     *fileStoreMetrics
}

type fileStoreMetrics struct {
	cacheHits   prometheus.Counter
	cacheMisses prometheus.Counter
	cacheSize   prometheus.Gauge
	evictions   prometheus.Counter
}

func newFileStoreMetrics() *fileStoreMetrics {
	return &fileStoreMetrics{
		cacheHits: promauto.NewCounter(prometheus.CounterOpts{
			Name: "file_store_cache_hits_total",
			Help: "The total number of cache hits",
		}),
		cacheMisses: promauto.NewCounter(prometheus.CounterOpts{
			Name: "file_store_cache_misses_total",
			Help: "The total number of cache misses",
		}),
		cacheSize: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "file_store_cache_size_bytes",
			Help: "The current size of the cache in bytes",
		}),
		evictions: promauto.NewCounter(prometheus.CounterOpts{
			Name: "file_store_evictions_total",
			Help: "The total number of cache evictions",
		}),
	}
}

// NewFileStore creates a new FileStore instance.
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
		metrics:  newFileStoreMetrics(),
	}, nil
}

// Store caches a file with the given key.
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

	fileInfo := &FileInfo{
		Path:     filePath,
		Size:     written,
		ModTime:  time.Now(),
		UseCount: 0,
	}

	fs.cache.Add(key, fileInfo)
	fs.currentSize += written
	fs.metrics.cacheSize.Set(float64(fs.currentSize))

	return nil
}

// Get retrieves a cached file.
func (fs *FileStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := fs.sem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to acquire semaphore: %w", err)
	}

	fs.mu.RLock()
	fileInfoInterface, ok := fs.cache.Get(key)
	fs.mu.RUnlock()

	if !ok {
		fs.sem.Release(1)
		fs.metrics.cacheMisses.Inc()
		return nil, fmt.Errorf("file not found in cache")
	}

	fileInfo, ok := fileInfoInterface.(*FileInfo)
	if !ok {
		fs.sem.Release(1)
		return nil, fmt.Errorf("invalid cache entry")
	}

	fs.metrics.cacheHits.Inc()
	fileInfo.UseCount++

	file, err := os.Open(fileInfo.Path)
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
		_, fileInfoInterface, ok := fs.cache.RemoveOldest()
		if !ok {
			return fmt.Errorf("failed to evict file from cache")
		}

		fileInfo, ok := fileInfoInterface.(*FileInfo)
		if !ok {
			return fmt.Errorf("invalid cache entry")
		}

		if err := os.Remove(fileInfo.Path); err != nil {
			return fmt.Errorf("failed to remove file: %w", err)
		}

		fs.currentSize -= fileInfo.Size
		fs.metrics.cacheSize.Set(float64(fs.currentSize))
		fs.metrics.evictions.Inc()
	}
	return nil
}

// Close cleans up resources used by the FileStore.
func (fs *FileStore) Close() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	var g errgroup.Group
	for _, keyInterface := range fs.cache.Keys() {
		key, ok := keyInterface.(string)
		if !ok {
			continue
		}
		g.Go(func() error {
			fileInfoInterface, ok := fs.cache.Get(key)
			if !ok {
				return nil
			}
			fileInfo, ok := fileInfoInterface.(*FileInfo)
			if !ok {
				return fmt.Errorf("invalid cache entry")
			}
			return os.Remove(fileInfo.Path)
		})
	}

	return g.Wait()
}

// CloseFile removes a specific file from the cache.
func (fs *FileStore) CloseFile(ctx context.Context, key string) error {
	if err := fs.sem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer fs.sem.Release(1)

	fs.mu.Lock()
	defer fs.mu.Unlock()

	fileInfoInterface, ok := fs.cache.Get(key)
	if !ok {
		return fmt.Errorf("file not found in cache")
	}

	fileInfo, ok := fileInfoInterface.(*FileInfo)
	if !ok {
		return fmt.Errorf("invalid cache entry")
	}

	if err := os.Remove(fileInfo.Path); err != nil {
		return fmt.Errorf("failed to remove file: %w", err)
	}

	fs.cache.Remove(key)
	fs.currentSize -= fileInfo.Size
	fs.metrics.cacheSize.Set(float64(fs.currentSize))
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
func (fs *FileStore) GetFileInfo(ctx context.Context, key string) (*FileInfo, error) {
	if err := fs.sem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer fs.sem.Release(1)

	fs.mu.RLock()
	fileInfoInterface, ok := fs.cache.Get(key)
	fs.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("file not found in cache")
	}

	fileInfo, ok := fileInfoInterface.(*FileInfo)
	if !ok {
		return nil, fmt.Errorf("invalid cache entry")
	}

	return fileInfo, nil
}

// ListFiles returns metadata for all cached files.
func (fs *FileStore) ListFiles(ctx context.Context) ([]*FileInfo, error) {
	if err := fs.sem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer fs.sem.Release(1)

	fs.mu.RLock()
	defer fs.mu.RUnlock()

	files := make([]*FileInfo, 0, fs.cache.Len())
	for _, keyInterface := range fs.cache.Keys() {
		key, ok := keyInterface.(string)
		if !ok {
			continue
		}
		fileInfoInterface, ok := fs.cache.Get(key)
		if !ok {
			continue
		}
		fileInfo, ok := fileInfoInterface.(*FileInfo)
		if !ok {
			continue
		}
		files = append(files, fileInfo)
	}

	return files, nil
}
