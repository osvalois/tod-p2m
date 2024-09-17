package torrent

import (
	"time"
)

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
