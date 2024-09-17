package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	lru "github.com/hashicorp/golang-lru"
)

type FileStore struct {
	cache       *lru.Cache
	cacheDir    string
	mu          sync.Mutex
	maxSize     int64
	currentSize int64
}

func NewFileStore(cacheSize int, cacheDir string) (*FileStore, error) {
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
	}, nil
}

func (fs *FileStore) Store(key string, reader io.Reader, size int64) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.currentSize+size > fs.maxSize {
		if err := fs.evict(size); err != nil {
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

func (fs *FileStore) Cleanup() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for fs.currentSize > fs.maxSize {
		key, filePath, ok := fs.cache.RemoveOldest()
		if !ok {
			break
		}

		info, err := os.Stat(filePath.(string))
		if err != nil {
			return fmt.Errorf("failed to get file info during cleanup: %w", err)
		}

		if err := os.Remove(filePath.(string)); err != nil {
			return fmt.Errorf("failed to remove file during cleanup: %w", err)
		}

		fs.currentSize -= info.Size()
		fs.cache.Remove(key)
	}

	return nil
}
func (fs *FileStore) Get(key string) (io.ReadCloser, error) {
	if filePath, ok := fs.cache.Get(key); ok {
		file, err := os.Open(filePath.(string))
		if err != nil {
			return nil, fmt.Errorf("failed to open file: %w", err)
		}
		return file, nil
	}
	return nil, fmt.Errorf("file not found in cache")
}

func (fs *FileStore) evict(sizeNeeded int64) error {
	for fs.currentSize+sizeNeeded > fs.maxSize {
		key, filePath, ok := fs.cache.RemoveOldest()
		if !ok {
			return fmt.Errorf("failed to evict file from cache")
		}

		info, err := os.Stat(filePath.(string))
		if err != nil {
			return fmt.Errorf("failed to get file info: %w", err)
		}

		if err := os.Remove(filePath.(string)); err != nil {
			return fmt.Errorf("failed to remove file: %w", err)
		}

		fs.currentSize -= info.Size()
		fs.cache.Remove(key)
	}
	return nil
}

func (fs *FileStore) Close() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for _, key := range fs.cache.Keys() {
		filePath, ok := fs.cache.Get(key)
		if ok {
			if err := os.Remove(filePath.(string)); err != nil {
				return fmt.Errorf("failed to remove file during cleanup: %w", err)
			}
		}
	}

	return nil
}
func (fs *FileStore) CloseFile(key string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if filePath, ok := fs.cache.Get(key); ok {
		file, err := os.Open(filePath.(string))
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
		defer file.Close()

		if err := file.Close(); err != nil {
			return fmt.Errorf("failed to close file: %w", err)
		}

		fs.cache.Remove(key)
		return nil
	}
	return fmt.Errorf("file not found in cache")
}
