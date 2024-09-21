package torrent

import (
	"context"
	"sync"
	"time"
	"tod-p2m/internal/config"
	"tod-p2m/internal/storage"

	"github.com/anacrolix/torrent"
	lru "github.com/hashicorp/golang-lru"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

// internal/torrent/types.go
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

type Manager struct {
	client       *Client
	cache        *storage.FileStore
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
	pieceCache   *lru.Cache
	networkSpeed float64
	speedMu      sync.RWMutex
}
