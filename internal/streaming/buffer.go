// Package streaming provides high-performance functionality for efficient streaming of torrent data.
// It is designed to support world-class streaming applications with optimal performance and reliability.
package streaming

import (
	"container/ring"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent"
	"golang.org/x/sync/errgroup"
)

// Buffer configuration constants
const (
	defaultBufferSize       = 50 * 1024 * 1024 // 50MB for improved caching
	defaultPrefetchPieces   = 10               // Increased prefetch for smoother playback
	maxConcurrentReads      = 200              // Increased concurrent reads for better parallelism
	prefetchInterval        = 20 * time.Millisecond
	readRetryInterval       = 50 * time.Millisecond
	maxReadRetries          = 5
	bandwidthSampleInterval = time.Second
	qualityAdjustInterval   = 10 * time.Second
	adaptiveCacheInterval   = 30 * time.Second
)

// QualityLevel represents different quality levels for adaptive streaming
type QualityLevel int

const (
	QualityLow QualityLevel = iota
	QualityMedium
	QualityHigh
)

// AdaptiveCacheConfig holds configuration for adaptive caching
type AdaptiveCacheConfig struct {
	MinCacheSize int64
	MaxCacheSize int64
	GrowthFactor float64
	ShrinkFactor float64
}

// Buffer represents a high-performance circular buffer for efficient streaming of torrent data.
// It manages data reading, writing, and prefetching to optimize performance for world-class streaming applications.
type Buffer struct {
	data              []byte
	readIndex         int64 // Using int64 for atomic operations
	writeIndex        int64 // Using int64 for atomic operations
	size              int64
	pieceLength       int64
	prefetchBuffer    *ring.Ring
	file              *torrent.File
	prefetchSize      int
	readSemaphore     chan struct{}
	mu                sync.RWMutex
	prefetchMu        sync.Mutex
	isClosed          int32 // Using int32 for atomic operations
	metrics           *BufferMetrics
	reader            io.ReadSeeker
	ctx               context.Context
	cancel            context.CancelFunc
	adaptiveCache     AdaptiveCacheConfig
	bandwidthMonitor  *BandwidthMonitor
	qualityAdjuster   *QualityAdjuster
	backgroundTasks   *errgroup.Group
	backgroundContext context.Context
	backgroundCancel  context.CancelFunc
}

// BufferMetrics holds detailed performance metrics for the buffer.
// It provides comprehensive statistics to measure and optimize buffer efficiency.
type BufferMetrics struct {
	CacheHits        int64 // Number of successful reads from the buffer cache
	CacheMisses      int64 // Number of reads that had to fetch data from the underlying file
	BytesRead        int64 // Total number of bytes read from the buffer
	BytesWritten     int64 // Total number of bytes written to the buffer
	PrefetchCount    int64 // Number of pieces prefetched
	AvgPrefetchTime  int64 // Average time taken to prefetch a piece (in microseconds)
	ReadRetries      int64 // Number of read retries due to temporary failures
	PrefetchFailures int64 // Number of prefetch operations that failed
}

// BandwidthMonitor tracks and analyzes bandwidth usage
type BandwidthMonitor struct {
	mu             sync.Mutex
	samples        []float64
	sampleInterval time.Duration
}

// QualityAdjuster manages adaptive quality changes
type QualityAdjuster struct {
	mu           sync.Mutex
	currentLevel QualityLevel
}

// NewBuffer creates a new high-performance Buffer instance with the specified file, size, and prefetch size.
// It initializes the buffer with optimal settings for world-class streaming performance.
func NewBuffer(file *torrent.File, size int, prefetchSize int) *Buffer {
	if size <= 0 {
		size = defaultBufferSize
	}
	if prefetchSize <= 0 {
		prefetchSize = defaultPrefetchPieces
	}

	ctx, cancel := context.WithCancel(context.Background())
	backgroundCtx, backgroundCancel := context.WithCancel(context.Background())

	b := &Buffer{
		data:           make([]byte, size),
		size:           int64(size),
		pieceLength:    file.Torrent().Info().PieceLength,
		file:           file,
		prefetchSize:   prefetchSize,
		prefetchBuffer: ring.New(prefetchSize),
		readSemaphore:  make(chan struct{}, maxConcurrentReads),
		metrics:        &BufferMetrics{},
		reader:         file.NewReader(),
		ctx:            ctx,
		cancel:         cancel,
		adaptiveCache: AdaptiveCacheConfig{
			MinCacheSize: 10 * 1024 * 1024,  // 10MB
			MaxCacheSize: 100 * 1024 * 1024, // 100MB
			GrowthFactor: 1.5,
			ShrinkFactor: 0.8,
		},
		bandwidthMonitor: &BandwidthMonitor{
			samples:        make([]float64, 0, 100),
			sampleInterval: bandwidthSampleInterval,
		},
		qualityAdjuster: &QualityAdjuster{
			currentLevel: QualityMedium,
		},
		backgroundTasks:   &errgroup.Group{},
		backgroundContext: backgroundCtx,
		backgroundCancel:  backgroundCancel,
	}

	return b
}

// Read reads data from the buffer into p.
// It implements the io.Reader interface with high-performance optimizations.
func (b *Buffer) Read(p []byte) (n int, err error) {
	select {
	case b.readSemaphore <- struct{}{}:
		defer func() { <-b.readSemaphore }()
	case <-b.ctx.Done():
		return 0, io.EOF
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	if atomic.LoadInt32(&b.isClosed) == 1 {
		return 0, io.EOF
	}

	readIndex := atomic.LoadInt64(&b.readIndex)
	writeIndex := atomic.LoadInt64(&b.writeIndex)

	if readIndex == writeIndex {
		return 0, io.EOF
	}

	// Handle wrap-around case in the circular buffer
	if readIndex < writeIndex {
		n = copy(p, b.data[readIndex:writeIndex])
	} else {
		n = copy(p, b.data[readIndex:])
		if n < len(p) {
			n += copy(p[n:], b.data[:writeIndex])
		}
	}

	atomic.AddInt64(&b.readIndex, int64(n))
	atomic.AddInt64(&b.metrics.BytesRead, int64(n))

	return n, nil
}

// Write writes data from p into the buffer.
// It implements the io.Writer interface with optimizations for high-throughput scenarios.
func (b *Buffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if atomic.LoadInt32(&b.isClosed) == 1 {
		return 0, errors.New("buffer is closed")
	}

	available := b.availableSpace()
	if int64(len(p)) > available {
		p = p[:available]
	}

	n = len(p)
	writeIndex := atomic.LoadInt64(&b.writeIndex)

	// Handle wrap-around case in the circular buffer
	if writeIndex+int64(n) <= b.size {
		copy(b.data[writeIndex:], p)
	} else {
		copy(b.data[writeIndex:], p[:b.size-writeIndex])
		copy(b.data, p[b.size-writeIndex:])
	}

	atomic.AddInt64(&b.writeIndex, int64(n))
	atomic.AddInt64(&b.metrics.BytesWritten, int64(n))

	return n, nil
}

// Close gracefully shuts down the buffer, preventing further reads and writes.
func (b *Buffer) Close() error {
	if !atomic.CompareAndSwapInt32(&b.isClosed, 0, 1) {
		return nil // Already closed
	}

	b.cancel() // Cancel ongoing operations
	b.StopBackgroundTasks()

	b.mu.Lock()
	defer b.mu.Unlock()

	// Perform any necessary cleanup here

	return nil
}

// Reset resets the buffer's read and write indices to 0 and clears metrics.
func (b *Buffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	atomic.StoreInt64(&b.readIndex, 0)
	atomic.StoreInt64(&b.writeIndex, 0)
	b.ResetMetrics()
}

// availableSpace returns the number of bytes available for writing in the buffer.
func (b *Buffer) availableSpace() int64 {
	readIndex := atomic.LoadInt64(&b.readIndex)
	writeIndex := atomic.LoadInt64(&b.writeIndex)

	if writeIndex >= readIndex {
		return b.size - writeIndex + readIndex
	}
	return readIndex - writeIndex
}

// prefetchRoutine continuously prefetches data in the background to improve read performance.
func (b *Buffer) prefetchRoutine() error {
	for {
		select {
		case <-b.backgroundContext.Done():
			return nil
		default:
			b.prefetchPiece()
			time.Sleep(prefetchInterval)
		}
	}
}

// prefetchPiece attempts to prefetch a single piece of data.
func (b *Buffer) prefetchPiece() {
	b.prefetchMu.Lock()
	defer b.prefetchMu.Unlock()

	nextPiece := b.prefetchBuffer.Next()
	if nextPiece.Value != nil {
		return // Already prefetched
	}

	pieceIndex := b.file.Offset() / b.pieceLength
	b.file.Torrent().Piece(int(pieceIndex)).SetPriority(torrent.PiecePriorityNow)

	buffer := make([]byte, b.pieceLength)
	startTime := time.Now()

	n, err := b.retryRead(buffer)
	if err != nil {
		atomic.AddInt64(&b.metrics.PrefetchFailures, 1)
		return
	}

	nextPiece.Value = buffer[:n]
	atomic.AddInt64(&b.metrics.PrefetchCount, 1)
	atomic.AddInt64(&b.metrics.AvgPrefetchTime, int64(time.Since(startTime).Microseconds()))
}

// retryRead attempts to read data with retries in case of temporary failures.
func (b *Buffer) retryRead(buffer []byte) (int, error) {
	var n int
	var err error
	for i := 0; i < maxReadRetries; i++ {
		n, err = b.reader.Read(buffer)
		if err == nil || err == io.EOF {
			return n, err
		}
		atomic.AddInt64(&b.metrics.ReadRetries, 1)
		time.Sleep(readRetryInterval)
	}
	return n, err
}

// ReadAt reads len(p) bytes into p starting at offset off in the underlying file.
func (b *Buffer) ReadAt(p []byte, off int64) (n int, err error) {
	select {
	case b.readSemaphore <- struct{}{}:
		defer func() { <-b.readSemaphore }()
	case <-b.ctx.Done():
		return 0, io.EOF
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	if atomic.LoadInt32(&b.isClosed) == 1 {
		return 0, io.EOF
	}

	// Check if the requested data is in the buffer
	bufferStart := b.file.Offset()
	bufferEnd := bufferStart + atomic.LoadInt64(&b.writeIndex) - atomic.LoadInt64(&b.readIndex)
	if off >= bufferStart && off < bufferEnd {
		// Data is in the buffer
		relativeOffset := int(off - bufferStart)
		n = copy(p, b.data[relativeOffset:])
		atomic.AddInt64(&b.metrics.CacheHits, 1)
		atomic.AddInt64(&b.metrics.BytesRead, int64(n))
		return n, nil
	}

	// Data is not in the buffer, read from the file
	atomic.AddInt64(&b.metrics.CacheMisses, 1)
	_, err = b.reader.Seek(off, io.SeekStart)
	if err != nil {
		return 0, err
	}
	n, err = b.retryRead(p)
	atomic.AddInt64(&b.metrics.BytesRead, int64(n))
	return n, err
}

// GetMetrics returns a copy of the current buffer metrics.
func (b *Buffer) GetMetrics() BufferMetrics {
	return BufferMetrics{
		CacheHits:        atomic.LoadInt64(&b.metrics.CacheHits),
		CacheMisses:      atomic.LoadInt64(&b.metrics.CacheMisses),
		BytesRead:        atomic.LoadInt64(&b.metrics.BytesRead),
		BytesWritten:     atomic.LoadInt64(&b.metrics.BytesWritten),
		PrefetchCount:    atomic.LoadInt64(&b.metrics.PrefetchCount),
		AvgPrefetchTime:  atomic.LoadInt64(&b.metrics.AvgPrefetchTime),
		ReadRetries:      atomic.LoadInt64(&b.metrics.ReadRetries),
		PrefetchFailures: atomic.LoadInt64(&b.metrics.PrefetchFailures),
	}
}

// ResetMetrics resets all buffer metrics to zero.
func (b *Buffer) ResetMetrics() {
	atomic.StoreInt64(&b.metrics.CacheHits, 0)
	atomic.StoreInt64(&b.metrics.CacheMisses, 0)
	atomic.StoreInt64(&b.metrics.BytesRead, 0)
	atomic.StoreInt64(&b.metrics.BytesWritten, 0)
	atomic.StoreInt64(&b.metrics.PrefetchCount, 0)
	atomic.StoreInt64(&b.metrics.AvgPrefetchTime, 0)
	atomic.StoreInt64(&b.metrics.ReadRetries, 0)
	atomic.StoreInt64(&b.metrics.PrefetchFailures, 0)
}

// StartBackgroundTasks starts all background tasks associated with the buffer.
func (b *Buffer) StartBackgroundTasks() {
	b.backgroundTasks.Go(func() error {
		return b.prefetchRoutine()
	})

	b.backgroundTasks.Go(func() error {
		return b.adaptiveCacheRoutine()
	})

	b.backgroundTasks.Go(func() error {
		return b.bandwidthMonitorRoutine()
	})

	b.backgroundTasks.Go(func() error {
		return b.qualityAdjustmentRoutine()
	})
}

// StopBackgroundTasks gracefully stops all background tasks.
func (b *Buffer) StopBackgroundTasks() {
	b.backgroundCancel()
	if err := b.backgroundTasks.Wait(); err != nil {
		// Log the error or handle it appropriately
		// For example: log.Printf("Error stopping background tasks: %v", err)
	}
}

// adaptiveCacheRoutine dynamically adjusts the cache size based on usage patterns
func (b *Buffer) adaptiveCacheRoutine() error {
	ticker := time.NewTicker(adaptiveCacheInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.backgroundContext.Done():
			return nil
		case <-ticker.C:
			b.adjustCacheSize()
		}
	}
}

// adjustCacheSize changes the cache size based on recent performance metrics
func (b *Buffer) adjustCacheSize() {
	metrics := b.GetMetrics()
	hitRatio := float64(metrics.CacheHits) / float64(metrics.CacheHits+metrics.CacheMisses)

	b.mu.Lock()
	defer b.mu.Unlock()

	if hitRatio < 0.5 && b.size < b.adaptiveCache.MaxCacheSize {
		newSize := int64(float64(b.size) * b.adaptiveCache.GrowthFactor)
		b.resizeBuffer(newSize)
	} else if hitRatio > 0.9 && b.size > b.adaptiveCache.MinCacheSize {
		newSize := int64(float64(b.size) * b.adaptiveCache.ShrinkFactor)
		b.resizeBuffer(newSize)
	}
}

// resizeBuffer changes the size of the buffer
func (b *Buffer) resizeBuffer(newSize int64) {
	if newSize < b.adaptiveCache.MinCacheSize {
		newSize = b.adaptiveCache.MinCacheSize
	} else if newSize > b.adaptiveCache.MaxCacheSize {
		newSize = b.adaptiveCache.MaxCacheSize
	}

	newData := make([]byte, newSize)
	copy(newData, b.data)
	b.data = newData
	b.size = newSize
}

// bandwidthMonitorRoutine continuously monitors bandwidth usage
func (b *Buffer) bandwidthMonitorRoutine() error {
	ticker := time.NewTicker(b.bandwidthMonitor.sampleInterval)
	defer ticker.Stop()

	var lastBytes int64
	for {
		select {
		case <-b.backgroundContext.Done():
			return nil
		case <-ticker.C:
			currentBytes := atomic.LoadInt64(&b.metrics.BytesRead)
			bandwidth := float64(currentBytes-lastBytes) / b.bandwidthMonitor.sampleInterval.Seconds()
			b.bandwidthMonitor.addSample(bandwidth)
			lastBytes = currentBytes
		}
	}
}

// addSample adds a bandwidth sample to the monitor
func (bm *BandwidthMonitor) addSample(bandwidth float64) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	bm.samples = append(bm.samples, bandwidth)
	if len(bm.samples) > 100 {
		bm.samples = bm.samples[1:]
	}
}

// getAverageBandwidth calculates the average bandwidth over recent samples
func (bm *BandwidthMonitor) getAverageBandwidth() float64 {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if len(bm.samples) == 0 {
		return 0
	}

	sum := 0.0
	for _, sample := range bm.samples {
		sum += sample
	}
	return sum / float64(len(bm.samples))
}

// qualityAdjustmentRoutine periodically adjusts streaming quality based on bandwidth
func (b *Buffer) qualityAdjustmentRoutine() error {
	ticker := time.NewTicker(qualityAdjustInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.backgroundContext.Done():
			return nil
		case <-ticker.C:
			b.adjustQuality()
		}
	}
}

// adjustQuality changes the streaming quality based on available bandwidth
func (b *Buffer) adjustQuality() {
	avgBandwidth := b.bandwidthMonitor.getAverageBandwidth()

	b.qualityAdjuster.mu.Lock()
	defer b.qualityAdjuster.mu.Unlock()

	switch {
	case avgBandwidth < 500*1024: // Less than 500 KB/s
		b.qualityAdjuster.currentLevel = QualityLow
	case avgBandwidth < 2*1024*1024: // Less than 2 MB/s
		b.qualityAdjuster.currentLevel = QualityMedium
	default:
		b.qualityAdjuster.currentLevel = QualityHigh
	}

	// Here you would typically signal to the streaming component to change quality
	// This might involve selecting a different torrent file or adjusting video encoding parameters
	// For example:
	// b.signalQualityChange(b.qualityAdjuster.currentLevel)
}

// GetCurrentQuality returns the current streaming quality level
func (b *Buffer) GetCurrentQuality() QualityLevel {
	b.qualityAdjuster.mu.Lock()
	defer b.qualityAdjuster.mu.Unlock()
	return b.qualityAdjuster.currentLevel
}

// signalQualityChange is a placeholder for the actual implementation
// This method would be responsible for communicating the quality change to other components
func (b *Buffer) signalQualityChange(newQuality QualityLevel) {
	// Implementation depends on how your streaming system handles quality changes
	// For example, you might send a message to a video player component:
	// b.videoPlayer.SetQuality(newQuality)
}

// GetAverageBandwidth returns the current average bandwidth
func (b *Buffer) GetAverageBandwidth() float64 {
	return b.bandwidthMonitor.getAverageBandwidth()
}

// GetBufferSize returns the current size of the buffer
func (b *Buffer) GetBufferSize() int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size
}

// GetPrefetchCount returns the number of pieces currently prefetched
func (b *Buffer) GetPrefetchCount() int {
	b.prefetchMu.Lock()
	defer b.prefetchMu.Unlock()
	count := 0
	b.prefetchBuffer.Do(func(p interface{}) {
		if p != nil {
			count++
		}
	})
	return count
}

// IsClosed returns whether the buffer is closed
func (b *Buffer) IsClosed() bool {
	return atomic.LoadInt32(&b.isClosed) == 1
}
