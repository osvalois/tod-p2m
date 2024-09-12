package torrent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"tod-p2m/internal/config"
	"tod-p2m/internal/storage"
)

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

var (
	ErrRateLimitExceeded      = errors.New("rate limit exceeded")
	ErrMaxTorrentsReached     = errors.New("maximum number of concurrent torrents reached")
	ErrTorrentNotFound        = errors.New("torrent not found")
	ErrTorrentInfoNotReady    = errors.New("torrent info not ready")
	ErrTorrentTimeout         = errors.New("timeout waiting for torrent info")
	ErrInvalidFileIndex       = errors.New("invalid file index")
	ErrManagerContextCanceled = errors.New("manager context canceled")
	ErrFileNotFound           = errors.New("file not found")
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

// GetTorrent retrieves or adds a torrent to the manager
func (m *Manager) GetTorrent(infoHash string) (*torrent.Torrent, error) {
	if err := m.limiter.Wait(m.ctx); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRateLimitExceeded, err)
	}

	m.mu.RLock()
	wrapper, ok := m.torrents[infoHash]
	m.mu.RUnlock()

	if ok {
		m.updateLastAccessed(infoHash)
		return m.waitForTorrentInfo(wrapper)
	}

	select {
	case m.semaphore <- struct{}{}:
		defer func() { <-m.semaphore }()
	default:
		return nil, ErrMaxTorrentsReached
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check locking
	if wrapper, ok = m.torrents[infoHash]; ok {
		m.updateLastAccessed(infoHash)
		return m.waitForTorrentInfo(wrapper)
	}

	t, err := m.addTorrentWithRetry(infoHash)
	if err != nil {
		return nil, err
	}

	wrapper = &TorrentWrapper{
		Torrent:   t,
		infoReady: make(chan struct{}),
	}
	m.torrents[infoHash] = wrapper
	m.updateLastAccessed(infoHash)

	go m.manageTorrentInfo(wrapper, infoHash)
	go m.monitorTorrent(t, infoHash)

	return m.waitForTorrentInfo(wrapper)
}

func (m *Manager) addTorrentWithRetry(infoHash string) (*torrent.Torrent, error) {
	var t *torrent.Torrent
	var err error

	for i := 0; i < maxRetries; i++ {
		t, err = m.client.AddMagnet(fmt.Sprintf("magnet:?xt=urn:btih:%s", infoHash))
		if err == nil {
			return t, nil
		}
		time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
	}

	return nil, fmt.Errorf("failed to add magnet after %d retries: %w", maxRetries, err)
}

func (m *Manager) manageTorrentInfo(wrapper *TorrentWrapper, infoHash string) {
	defer func() {
		if r := recover(); r != nil {
			m.Logger.Error().Interface("panic", r).Str("infoHash", infoHash).Msg("Panic in torrent info goroutine")
		}
	}()

	select {
	case <-wrapper.GotInfo():
		wrapper.mu.Lock()
		defer wrapper.mu.Unlock()
		if wrapper.Info() != nil {
			select {
			case <-wrapper.infoReady:
				// Channel already closed, do nothing
			default:
				close(wrapper.infoReady)
			}
			m.initializePiecePriorities(wrapper)
		}
	case <-time.After(torrentTimeout):
		m.removeTorrent(infoHash)
	case <-m.ctx.Done():
		return
	}
}

func (m *Manager) waitForTorrentInfo(wrapper *TorrentWrapper) (*torrent.Torrent, error) {
	timeout := time.After(torrentTimeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-wrapper.infoReady:
			return wrapper.Torrent, nil
		case <-ticker.C:
			if wrapper.Info() != nil {
				wrapper.mu.Lock()
				select {
				case <-wrapper.infoReady:
					// Already closed, do nothing
				default:
					close(wrapper.infoReady)
				}
				wrapper.mu.Unlock()
				return wrapper.Torrent, nil
			}
		case <-timeout:
			return nil, ErrTorrentTimeout
		case <-m.ctx.Done():
			return nil, ErrManagerContextCanceled
		}
	}
}

func (m *Manager) updateLastAccessed(infoHash string) {
	now := time.Now()
	m.lastAccessed.Store(infoHash, now)
	if wrapper, ok := m.torrents[infoHash]; ok {
		wrapper.mu.Lock()
		wrapper.lastAccessed = now
		wrapper.mu.Unlock()
	}
}

func (m *Manager) monitorTorrent(t *torrent.Torrent, infoHash string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	downloadTimeout := time.After(m.config.MaxDownloadTime)

	for {
		select {
		case <-ticker.C:
			m.mu.RLock()
			wrapper, ok := m.torrents[infoHash]
			m.mu.RUnlock()

			if !ok {
				m.Logger.Info().Str("infoHash", infoHash).Msg("Torrent not found in manager, stopping monitor")
				return
			}

			wrapper.mu.RLock()
			lastAccessed := wrapper.lastAccessed
			hasPieceStats := len(wrapper.pieceStats) > 0
			wrapper.mu.RUnlock()

			if time.Since(lastAccessed) > m.config.TorrentTimeout*2 && t.BytesCompleted() == t.Length() {
				m.Logger.Info().Str("infoHash", infoHash).Msg("Torrent completed and not accessed, removing")
				m.removeTorrent(infoHash)
				return
			}

			if hasPieceStats {
				m.updatePiecePriorities(wrapper)
			} else {
				m.Logger.Warn().Str("infoHash", infoHash).Msg("PieceStats not initialized, skipping priority update")
			}

		case <-downloadTimeout:
			m.Logger.Warn().Str("infoHash", infoHash).Msg("Download timeout reached")
			m.removeTorrent(infoHash)
			return

		case <-t.GotInfo():
			m.Logger.Info().Str("infoHash", infoHash).Msg("Torrent info received")
			// Aquí podrías agregar lógica adicional si es necesario cuando se recibe la información del torrent

		case <-t.Closed():
			m.Logger.Info().Str("infoHash", infoHash).Msg("Torrent closed, stopping monitor")
			return

		case <-m.ctx.Done():
			m.Logger.Info().Str("infoHash", infoHash).Msg("Manager context cancelled, stopping monitor")
			return
		}
	}
}

func (m *Manager) initializePiecePriorities(wrapper *TorrentWrapper) {
	numPieces := wrapper.NumPieces()
	if numPieces == 0 {
		m.Logger.Warn().Str("infoHash", wrapper.InfoHash().String()).Msg("Torrent has no pieces")
		return
	}

	wrapper.pieceStats = make([]pieceStats, numPieces)
	for i := 0; i < numPieces; i++ {
		priority := torrent.PiecePriorityNormal
		if i < pieceSelectionWindow {
			priority = torrent.PiecePriorityHigh
		}
		wrapper.Piece(i).SetPriority(priority)
		wrapper.pieceStats[i] = pieceStats{priority: priority}
	}
}

func (m *Manager) updatePiecePriorities(wrapper *TorrentWrapper) {
	wrapper.mu.Lock()
	defer wrapper.mu.Unlock()

	numPieces := wrapper.NumPieces()
	if numPieces == 0 || len(wrapper.pieceStats) == 0 {
		m.Logger.Warn().Str("infoHash", wrapper.InfoHash().String()).Msg("Torrent has no pieces or pieceStats not initialized")
		return
	}

	downloaded := wrapper.BytesCompleted()
	total := wrapper.Length()

	windowStart := int(float64(downloaded) / float64(total) * float64(numPieces))
	windowEnd := min(windowStart+pieceSelectionWindow, numPieces)

	for i := 0; i < numPieces; i++ {
		piece := wrapper.Piece(i)
		stats := &wrapper.pieceStats[i]

		if piece.State().Complete {
			continue
		}

		if i >= windowStart && i < windowEnd {
			if stats.priority != torrent.PiecePriorityHigh {
				piece.SetPriority(torrent.PiecePriorityHigh)
				stats.priority = torrent.PiecePriorityHigh
				stats.lastRequest = time.Now()
			}
		} else if time.Since(stats.lastRequest) > 5*time.Minute && stats.priority != torrent.PiecePriorityNormal {
			piece.SetPriority(torrent.PiecePriorityNormal)
			stats.priority = torrent.PiecePriorityNormal
		}
	}
}

func (m *Manager) removeTorrent(infoHash string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if t, ok := m.torrents[infoHash]; ok {
		m.Logger.Info().Str("infoHash", infoHash).Msg("Removing torrent")
		t.Drop()
		delete(m.torrents, infoHash)
		m.lastAccessed.Delete(infoHash)

		if !m.config.KeepFiles {
			go m.deleteFiles(t.Torrent)
		}
	}
}

func (m *Manager) deleteFiles(t *torrent.Torrent) {
	for _, file := range t.Files() {
		path := filepath.Join(m.downloadDir, file.Path())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			m.Logger.Error().Err(err).Str("path", path).Msg("Failed to delete file")
		} else {
			m.Logger.Info().Str("path", path).Msg("File deleted successfully")
		}
	}

	// Attempt to remove empty directories
	if err := m.removeEmptyDirs(m.downloadDir); err != nil {
		m.Logger.Error().Err(err).Str("path", m.downloadDir).Msg("Failed to remove empty directories")
	}
}

func (m *Manager) removeEmptyDirs(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subdir := filepath.Join(dir, entry.Name())
			if err := m.removeEmptyDirs(subdir); err != nil {
				return err
			}
		}
	}

	// Check if directory is empty
	entries, err = os.ReadDir(dir)
	if err != nil {
		return err
	}

	if len(entries) == 0 && dir != m.downloadDir {
		if err := os.Remove(dir); err != nil {
			return err
		}
		m.Logger.Info().Str("path", dir).Msg("Removed empty directory")
	}

	return nil
}

func (m *Manager) cleanupRoutine() {
	ticker := time.NewTicker(m.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.mu.Lock()
			for infoHash, wrapper := range m.torrents {
				if lastAccessed, ok := m.lastAccessed.Load(infoHash); ok {
					if time.Since(lastAccessed.(time.Time)) > m.config.TorrentTimeout*2 {
						m.Logger.Info().Str("infoHash", infoHash).Msg("Removing inactive torrent")
						wrapper.Drop()
						delete(m.torrents, infoHash)
						m.lastAccessed.Delete(infoHash)
						if !m.config.KeepFiles {
							go m.deleteFiles(wrapper.Torrent)
						}
					}
				}
			}
			m.mu.Unlock()

			// Eliminamos la llamada a m.cache.Cleanup() ya que no está definida
			// Si es necesario limpiar la caché, deberías implementar este método en FileStore
		case <-m.ctx.Done():
			return
		}
	}
}

func (m *Manager) statsRoutine() {
	ticker := time.NewTicker(statsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.mu.RLock()
			activeTorrents := len(m.torrents)
			m.mu.RUnlock()

			clientStats := m.client.Stats()
			m.Logger.Info().
				Int("active_torrents", activeTorrents).
				Int64("total_upload", clientStats.BytesWritten.Int64()).
				Int64("total_download", clientStats.BytesRead.Int64()).
				Float64("download_speed", float64(clientStats.BytesReadUsefulData.Int64())/statsInterval.Seconds()).
				Float64("upload_speed", float64(clientStats.BytesWritten.Int64())/statsInterval.Seconds()).
				Msg("Torrent manager stats")
		case <-m.ctx.Done():
			return
		}
	}
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

	// Clean up the temporary directory
	if err := os.RemoveAll(m.downloadDir); err != nil {
		m.Logger.Error().Err(err).Msg("Error removing temporary directory")
	}

	return nil
}

func (m *Manager) GetFile(infoHash string, fileIndex int) (io.ReadSeeker, error) {
	t, err := m.GetTorrent(infoHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent: %w", err)
	}

	files := t.Files()
	if fileIndex < 0 || fileIndex >= len(files) {
		return nil, ErrInvalidFileIndex
	}

	file := files[fileIndex]
	filePath := filepath.Join(m.downloadDir, file.Path())

	if m.config.KeepFiles {
		if _, err := os.Stat(filePath); err == nil {
			return os.Open(filePath)
		}
	}

	return file.NewReader(), nil
}

func (m *Manager) GetFileInfo(infoHash string, fileIndex int) (*FileInfo, error) {
	t, err := m.GetTorrent(infoHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent: %w", err)
	}

	files := t.Files()
	if fileIndex < 0 || fileIndex >= len(files) {
		return nil, ErrInvalidFileIndex
	}

	file := files[fileIndex]
	return &FileInfo{
		Name:     file.DisplayPath(),
		Size:     file.Length(),
		Progress: float64(file.BytesCompleted()) / float64(file.Length()),
	}, nil
}

func (m *Manager) GetTorrentProgress(infoHash string) (float64, error) {
	t, err := m.GetTorrent(infoHash)
	if err != nil {
		return 0, fmt.Errorf("failed to get torrent: %w", err)
	}

	return float64(t.BytesCompleted()) / float64(t.Length()), nil
}

func (m *Manager) PauseTorrent(infoHash string) error {
	m.mu.RLock()
	wrapper, ok := m.torrents[infoHash]
	m.mu.RUnlock()

	if !ok {
		return ErrTorrentNotFound
	}

	wrapper.Torrent.SetInfoBytes(nil)
	return nil
}

func (m *Manager) ResumeTorrent(infoHash string) error {
	m.mu.RLock()
	wrapper, ok := m.torrents[infoHash]
	m.mu.RUnlock()

	if !ok {
		return ErrTorrentNotFound
	}

	// Instead of using SetInfoBytes, we'll just start the download
	wrapper.Torrent.DownloadAll()
	return nil
}

func (m *Manager) SetMaxConnections(maxConnections int) {
	m.client.SetMaxConnections(maxConnections)
}

func (m *Manager) SetDownloadLimit(downloadLimit int64) {
	m.client.SetDownloadLimit(downloadLimit)
}

func (m *Manager) SetUploadLimit(uploadLimit int64) {
	m.client.SetUploadLimit(uploadLimit)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
