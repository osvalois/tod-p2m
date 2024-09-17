package torrent

import (
	"context"
	"fmt"
	"time"

	"github.com/anacrolix/torrent"
)

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
		return wrapper.Torrent, nil
	}

	select {
	case m.semaphore <- struct{}{}:
		defer func() { <-m.semaphore }()
	default:
		return nil, ErrMaxTorrentsReached
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if wrapper, ok = m.torrents[infoHash]; ok {
		m.updateLastAccessed(infoHash)
		return wrapper.Torrent, nil
	}

	t, err := m.addTorrentWithRetry(infoHash)
	if err != nil {
		return nil, err
	}

	wrapper = &TorrentWrapper{
		Torrent: t,
	}
	m.torrents[infoHash] = wrapper
	m.updateLastAccessed(infoHash)

	go m.monitorTorrent(t, infoHash)

	return t, nil
}

func (m *Manager) addTorrentWithRetry(infoHash string) (*torrent.Torrent, error) {
	var t *torrent.Torrent
	var err error

	for i := 0; i < maxRetries; i++ {
		t, err = m.client.AddMagnet(fmt.Sprintf("magnet:?xt=urn:btih:%s", infoHash))
		if err == nil {
			// Set a short timeout for metadata retrieval
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Wait for info with a timeout
			select {
			case <-t.GotInfo():
				return t, nil
			case <-ctx.Done():
				return nil, fmt.Errorf("timeout waiting for torrent info")
			}
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

		case <-t.Closed():
			m.Logger.Info().Str("infoHash", infoHash).Msg("Torrent closed, stopping monitor")
			return

		case <-m.ctx.Done():
			m.Logger.Info().Str("infoHash", infoHash).Msg("Manager context cancelled, stopping monitor")
			return
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

func (m *Manager) updateLastAccessed(infoHash string) {
	now := time.Now()
	m.lastAccessed.Store(infoHash, now)
	if wrapper, ok := m.torrents[infoHash]; ok {
		wrapper.mu.Lock()
		wrapper.lastAccessed = now
		wrapper.mu.Unlock()
	}
}

// PauseTorrent pauses a torrent
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

// ResumeTorrent resumes a paused torrent
func (m *Manager) ResumeTorrent(infoHash string) error {
	m.mu.RLock()
	wrapper, ok := m.torrents[infoHash]
	m.mu.RUnlock()

	if !ok {
		return ErrTorrentNotFound
	}

	wrapper.Torrent.DownloadAll()
	return nil
}

// GetTorrentProgress returns the download progress of a torrent
func (m *Manager) GetTorrentProgress(infoHash string) (float64, error) {
	t, err := m.GetTorrent(infoHash)
	if err != nil {
		return 0, fmt.Errorf("failed to get torrent: %w", err)
	}

	return float64(t.BytesCompleted()) / float64(t.Length()), nil
}
