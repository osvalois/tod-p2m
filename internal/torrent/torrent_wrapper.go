package torrent

import (
	"time"

	"github.com/anacrolix/torrent"
)

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
