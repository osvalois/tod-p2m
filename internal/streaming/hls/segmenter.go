package hls

// internal/internal/streaming/adaptive/segmenter.go
import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
)

const (
	defaultSegmentSize = 10 * 1024 * 1024 // 10MB
	bufferSize         = 1024 * 1024      // 1MB buffer for reading
)

type Segmenter struct {
	file         *torrent.File
	length       int64
	segmentSize  int64
	segmentCount int
	mu           sync.RWMutex
	cache        map[int]*cachedSegment
}

type cachedSegment struct {
	data       []byte
	lastAccess time.Time
}

type segmentReadSeeker struct {
	segmenter *Segmenter
	segmentID int
	offset    int64
}

func NewSegmenter(file *torrent.File, segmentSize int64) *Segmenter {
	if segmentSize <= 0 {
		segmentSize = defaultSegmentSize
	}
	length := file.Length()
	segmentCount := int((length + segmentSize - 1) / segmentSize)

	return &Segmenter{
		file:         file,
		length:       length,
		segmentSize:  segmentSize,
		segmentCount: segmentCount,
		cache:        make(map[int]*cachedSegment),
	}
}

func (s *Segmenter) GetSegment(segmentID int) (io.ReadSeeker, error) {
	if segmentID < 0 || segmentID >= s.segmentCount {
		return nil, fmt.Errorf("invalid segment ID: %d", segmentID)
	}

	return &segmentReadSeeker{
		segmenter: s,
		segmentID: segmentID,
		offset:    0,
	}, nil
}

func (s *Segmenter) SegmentCount() int {
	return s.segmentCount
}

func (s *Segmenter) cacheSegment(segmentID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.cache[segmentID]; exists {
		s.cache[segmentID].lastAccess = time.Now()
		return nil
	}

	offset := int64(segmentID) * s.segmentSize
	remainingBytes := s.length - offset
	segmentSize := s.segmentSize
	if remainingBytes < segmentSize {
		segmentSize = remainingBytes
	}

	data := make([]byte, segmentSize)
	reader := s.file.NewReader()
	_, err := reader.Seek(offset, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek to segment: %w", err)
	}
	_, err = io.ReadFull(reader, data)
	if err != nil {
		return fmt.Errorf("failed to read segment data: %w", err)
	}

	s.cache[segmentID] = &cachedSegment{
		data:       data,
		lastAccess: time.Now(),
	}

	// Cleanup old cache entries if necessary
	s.cleanupCache()

	return nil
}

func (s *Segmenter) cleanupCache() {
	const maxCacheSize = 100 // Maximum number of segments to keep in cache
	const maxCacheAge = 5 * time.Minute

	if len(s.cache) <= maxCacheSize {
		return
	}

	now := time.Now()
	for id, segment := range s.cache {
		if now.Sub(segment.lastAccess) > maxCacheAge {
			delete(s.cache, id)
		}
		if len(s.cache) <= maxCacheSize {
			break
		}
	}
}

func (srs *segmentReadSeeker) Read(p []byte) (n int, err error) {
	err = srs.segmenter.cacheSegment(srs.segmentID)
	if err != nil {
		return 0, err
	}

	srs.segmenter.mu.RLock()
	defer srs.segmenter.mu.RUnlock()

	segment := srs.segmenter.cache[srs.segmentID]
	if srs.offset >= int64(len(segment.data)) {
		return 0, io.EOF
	}

	n = copy(p, segment.data[srs.offset:])
	srs.offset += int64(n)
	return n, nil
}

func (srs *segmentReadSeeker) Seek(offset int64, whence int) (int64, error) {
	var absoluteOffset int64

	switch whence {
	case io.SeekStart:
		absoluteOffset = offset
	case io.SeekCurrent:
		absoluteOffset = srs.offset + offset
	case io.SeekEnd:
		srs.segmenter.mu.RLock()
		segmentSize := int64(len(srs.segmenter.cache[srs.segmentID].data))
		srs.segmenter.mu.RUnlock()
		absoluteOffset = segmentSize + offset
	default:
		return 0, fmt.Errorf("invalid whence")
	}

	if absoluteOffset < 0 {
		return 0, fmt.Errorf("negative offset")
	}

	srs.offset = absoluteOffset
	return srs.offset, nil
}

func (s *Segmenter) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clear the cache
	s.cache = make(map[int]*cachedSegment)

	return nil
}
