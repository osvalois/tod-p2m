package torrent

import (
	"context"
	"sync"
	"time"
	"tod-p2m/internal/config"
	"tod-p2m/internal/storage"

	"github.com/anacrolix/torrent"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

// TorrentWrapper extends torrent.Torrent with additional metadata
type TorrentWrapper struct {
	*torrent.Torrent
	infoReady    chan struct{}
	lastAccessed time.Time
	pieceStats   []pieceStats
	mu           sync.RWMutex
}

type pieceStats struct {
	priority    torrent.PiecePriority
	lastRequest time.Time
}

type TorrentInfo struct {
	InfoHash string
	Name     string
	Files    []FileInfo
}

type FileInfo struct {
	ID       int
	Name     string
	Size     int64
	Progress float64
}

const (
	maxConcurrentTorrents = 100
	pieceSelectionWindow  = 30
	pollInterval          = 50 * time.Millisecond
	cleanupInterval       = 5 * time.Minute
	statsInterval         = 1 * time.Minute
	torrentTimeout        = 2 * time.Minute
	maxRetries            = 3
	bufferSize            = 32 * 1024 // 32KB buffer for file operations
)

type Manager struct {
	client       *Client
	cache        *storage.FileStore // Cambiado de fileStore a cache
	config       *config.Config
	Logger       zerolog.Logger
	mu           sync.RWMutex
	torrents     map[string]*TorrentWrapper
	lastAccessed sync.Map
	limiter      *rate.Limiter
	semaphore    chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
	downloadDir  string
}
