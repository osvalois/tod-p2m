package torrent

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/anacrolix/torrent"
	lru "github.com/hashicorp/golang-lru"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"tod-p2m/internal/config"
	"tod-p2m/internal/storage"
)

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

	pieceCache, err := lru.New(1000) // Cache for 1000 pieces
	if err != nil {
		cancel()
		client.Close()
		cache.Close()
		return nil, fmt.Errorf("failed to create piece cache: %w", err)
	}

	m := &Manager{
		client:      client,
		cache:       cache,
		config:      cfg,
		Logger:      log,
		torrents:    make(map[string]*TorrentWrapper),
		limiter:     rate.NewLimiter(rate.Every(cfg.RetryDelay), cfg.MaxPendingRequests),
		semaphore:   make(chan struct{}, cfg.MaxConcurrentTorrents),
		ctx:         ctx,
		cancel:      cancel,
		downloadDir: cfg.DownloadDir,
		pieceCache:  pieceCache,
	}

	go m.cleanupRoutine()
	go m.statsRoutine()

	return m, nil
}

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
	}, m.config.MaxRetries)

	if err != nil {
		m.Logger.Error().Err(err).Msg("Error removing temporary directory after retries")
	}

	return nil
}

func (m *Manager) GetPiece(t *torrent.Torrent, index int) ([]byte, error) {
	cacheKey := fmt.Sprintf("%s-%d", t.InfoHash().String(), index)
	if cachedPiece, ok := m.pieceCache.Get(cacheKey); ok {
		return cachedPiece.([]byte), nil
	}

	// Obtener la longitud de la pieza directamente como un campo
	pieceLength := t.Info().PieceLength

	// Verifica si el índice de la pieza es válido
	if index < 0 || index >= t.NumPieces() {
		return nil, fmt.Errorf("índice de pieza fuera de rango")
	}

	// Crear un lector para la pieza
	reader := t.NewReader()

	// Mover el lector a la posición de inicio de la pieza
	if _, err := reader.Seek(int64(index)*pieceLength, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek to piece: %w", err)
	}

	// Leer los datos de la pieza
	pieceData := make([]byte, pieceLength)
	if _, err := io.ReadFull(reader, pieceData); err != nil {
		return nil, fmt.Errorf("failed to read piece data: %w", err)
	}

	// Almacenar la pieza en la caché
	m.pieceCache.Add(cacheKey, pieceData)

	return pieceData, nil
}

func (m *Manager) UpdateNetworkSpeed(speed float64) {
	m.speedMu.Lock()
	defer m.speedMu.Unlock()
	m.networkSpeed = speed
}

func (m *Manager) GetNetworkSpeed() float64 {
	m.speedMu.RLock()
	defer m.speedMu.RUnlock()
	return m.networkSpeed
}
