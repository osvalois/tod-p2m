package torrent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"tod-p2m/internal/config"
	"tod-p2m/internal/storage"
)

// Manager handles the core functionality of torrent management
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
}

// NewManager creates and initializes a new Manager
func NewManager(cfg *config.Config, log zerolog.Logger) (*Manager, error) {
	ctx, cancel := context.WithCancel(context.Background())

	if err := os.MkdirAll(cfg.DownloadDir, 0755); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create download directory: %w", err)
	}

	client, err := NewClient(cfg, log, cfg.DownloadDir)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create torrent client: %w", err)
	}

	cacheDir := filepath.Join(cfg.DownloadDir, "cache")
	cache, err := storage.NewFileStore(cfg.CacheSize, cacheDir)
	if err != nil {
		cancel()
		client.Close()
		return nil, fmt.Errorf("failed to create file store: %w", err)
	}

	m := &Manager{
		client:      client,
		cache:       cache,
		config:      cfg,
		Logger:      log,
		torrents:    make(map[string]*TorrentWrapper),
		limiter:     rate.NewLimiter(rate.Every(10*time.Millisecond), 100),
		semaphore:   make(chan struct{}, maxConcurrentTorrents),
		ctx:         ctx,
		cancel:      cancel,
		downloadDir: cfg.DownloadDir,
	}

	go m.cleanupRoutine()
	go m.statsRoutine()

	return m, nil
}

// Close shuts down the Manager and releases resources
func (m *Manager) Close() error {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()

	var g errgroup.Group
	for _, t := range m.torrents {
		t := t
		g.Go(func() error {
			t.Drop()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		m.Logger.Error().Err(err).Msg("Error dropping torrents")
	}

	m.client.Close()
	if err := m.cache.Close(); err != nil {
		m.Logger.Error().Err(err).Msg("Error closing cache")
	}

	if err := os.RemoveAll(m.downloadDir); err != nil {
		m.Logger.Error().Err(err).Msg("Error removing temporary directory")
	}

	return nil
}
