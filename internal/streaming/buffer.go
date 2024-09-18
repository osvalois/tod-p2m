// internal/streaming/buffer.go

package streaming

import (
	"container/ring"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
)

const (
	defaultBufferSize     = 20 * 1024 * 1024 // 20MB
	defaultPrefetchPieces = 5
	maxConcurrentReads    = 100
)

// Buffer represents a circular buffer for efficient streaming
type Buffer struct {
	data           []byte
	readIndex      int
	writeIndex     int
	size           int
	pieceLength    int64
	prefetchBuffer *ring.Ring
	file           *torrent.File
	prefetchSize   int
	readSemaphore  chan struct{}
	mu             sync.RWMutex
	prefetchMu     sync.Mutex
	isClosed       bool
	metrics        *BufferMetrics
	reader         io.ReadSeeker
}

// BufferMetrics holds performance metrics for the buffer
type BufferMetrics struct {
	CacheHits     int64
	CacheMisses   int64
	BytesRead     int64
	BytesWritten  int64
	PrefetchCount int64
}

// NewBuffer creates a new Buffer instance
func NewBuffer(file *torrent.File, size int, prefetchSize int) *Buffer {
	if size <= 0 {
		size = defaultBufferSize
	}
	if prefetchSize <= 0 {
		prefetchSize = defaultPrefetchPieces
	}

	b := &Buffer{
		data:           make([]byte, size),
		size:           size,
		pieceLength:    file.Torrent().Info().PieceLength,
		file:           file,
		prefetchSize:   prefetchSize,
		prefetchBuffer: ring.New(prefetchSize),
		readSemaphore:  make(chan struct{}, maxConcurrentReads),
		metrics:        &BufferMetrics{},
		reader:         file.NewReader(),
	}

	go b.prefetchRoutine()

	return b
}

// Read reads data from the buffer
func (b *Buffer) Read(p []byte) (n int, err error) {
	b.readSemaphore <- struct{}{}
	defer func() { <-b.readSemaphore }()

	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.isClosed {
		return 0, io.EOF
	}

	if b.readIndex == b.writeIndex {
		return 0, io.EOF
	}

	if b.readIndex < b.writeIndex {
		n = copy(p, b.data[b.readIndex:b.writeIndex])
	} else {
		n = copy(p, b.data[b.readIndex:])
		if n < len(p) {
			n += copy(p[n:], b.data[:b.writeIndex])
		}
	}

	b.readIndex = (b.readIndex + n) % b.size
	b.metrics.BytesRead += int64(n)

	return n, nil
}

// Write writes data to the buffer
func (b *Buffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.isClosed {
		return 0, errors.New("buffer is closed")
	}

	available := b.availableSpace()
	if len(p) > available {
		p = p[:available]
	}

	n = len(p)

	if b.writeIndex+n <= b.size {
		copy(b.data[b.writeIndex:], p)
	} else {
		copy(b.data[b.writeIndex:], p[:b.size-b.writeIndex])
		copy(b.data, p[b.size-b.writeIndex:])
	}

	b.writeIndex = (b.writeIndex + n) % b.size
	b.metrics.BytesWritten += int64(n)

	return n, nil
}

// Close closes the buffer
func (b *Buffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.isClosed {
		return nil
	}

	b.isClosed = true
	return nil
}

// Reset resets the buffer
func (b *Buffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.readIndex = 0
	b.writeIndex = 0
}

// availableSpace returns the available space in the buffer
func (b *Buffer) availableSpace() int {
	if b.writeIndex >= b.readIndex {
		return b.size - b.writeIndex + b.readIndex
	}
	return b.readIndex - b.writeIndex
}

// prefetchRoutine continuously prefetches data
func (b *Buffer) prefetchRoutine() {
	for {
		if b.isClosed {
			return
		}

		b.prefetchMu.Lock()
		nextPiece := b.prefetchBuffer.Next()
		b.prefetchMu.Unlock()

		if nextPiece.Value == nil {
			pieceIndex := b.file.Offset() / b.pieceLength
			b.file.Torrent().Piece(int(pieceIndex)).SetPriority(torrent.PiecePriorityNow)

			buffer := make([]byte, b.pieceLength)
			n, err := b.reader.Read(buffer)
			if err != nil && err != io.EOF {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			b.prefetchMu.Lock()
			nextPiece.Value = buffer[:n]
			b.prefetchMu.Unlock()

			b.metrics.PrefetchCount++
		}

		time.Sleep(50 * time.Millisecond)
	}
}

// ReadAt reads data from a specific offset
func (b *Buffer) ReadAt(p []byte, off int64) (n int, err error) {
	b.readSemaphore <- struct{}{}
	defer func() { <-b.readSemaphore }()

	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.isClosed {
		return 0, io.EOF
	}

	// Check if the requested data is in the buffer
	bufferStart := b.file.Offset()
	bufferEnd := bufferStart + int64(b.writeIndex-b.readIndex)
	if off >= bufferStart && off < bufferEnd {
		// Data is in the buffer
		relativeOffset := int(off - bufferStart)
		n = copy(p, b.data[relativeOffset:])
		b.metrics.CacheHits++
		b.metrics.BytesRead += int64(n)
		return n, nil
	}

	// Data is not in the buffer, read from the file
	b.metrics.CacheMisses++
	_, err = b.reader.Seek(off, io.SeekStart)
	if err != nil {
		return 0, err
	}
	n, err = b.reader.Read(p)
	b.metrics.BytesRead += int64(n)
	return n, err
}

// GetMetrics returns the current buffer metrics
func (b *Buffer) GetMetrics() BufferMetrics {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return *b.metrics
}

// ResetMetrics resets the buffer metrics
func (b *Buffer) ResetMetrics() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.metrics = &BufferMetrics{}
}
