package torrent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"tod-p2m/internal/config"
	"tod-p2m/internal/storage"
)

func (m *Manager) retryFileOperation(operation func() error, maxRetries int) error {
	var err error
	for i := 0; i < maxRetries; i++ {
		err = operation()
		if err == nil {
			return nil
		}
		m.Logger.Warn().Err(err).Int("attempt", i+1).Msg("File operation failed, retrying")
		time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
	}
	return fmt.Errorf("file operation failed after %d attempts: %w", maxRetries, err)
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

	err := m.retryFileOperation(func() error {
		return os.RemoveAll(m.downloadDir)
	}, 3)

	if err != nil {
		m.Logger.Error().Err(err).Msg("Error removing temporary directory after retries")
	}

	return nil
}
func getInfoHashFromPath(path string) string {
	base := filepath.Base(path)
	parts := strings.Split(base, "_")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}
func (m *Manager) handleBusyResource(path string) error {
	m.Logger.Warn().Str("path", path).Msg("Resource busy, attempting to release")

	infoHash := getInfoHashFromPath(path)
	if infoHash != "" {
		if err := m.cache.CloseFile(infoHash); err != nil {
			m.Logger.Error().Err(err).Str("infoHash", infoHash).Msg("Failed to close file in cache")
		}
	}

	time.Sleep(1 * time.Second)

	return m.retryFileOperation(func() error {
		err := os.Remove(path)
		if err != nil {
			if os.IsPermission(err) {
				chmodErr := os.Chmod(path, 0666)
				if chmodErr != nil {
					m.Logger.Error().Err(chmodErr).Str("path", path).Msg("Failed to change file permissions")
				}
				return os.Remove(path)
			}
		}
		return err
	}, maxRetries)
}
