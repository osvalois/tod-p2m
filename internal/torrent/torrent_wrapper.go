package torrent

import (
	"math"
	"time"

	"github.com/anacrolix/torrent"
)

// internal/torrent/torrent_wrapper.go
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
		return
	}

	downloadedBytes := wrapper.BytesCompleted()
	totalBytes := wrapper.Length()
	playbackPosition := float64(downloadedBytes) / float64(totalBytes)

	// Tamaño de la ventana de prioridad (ajustable según las necesidades)
	windowSize := 50

	for i := 0; i < numPieces; i++ {
		piece := wrapper.Piece(i)
		stats := &wrapper.pieceStats[i]

		if piece.State().Complete {
			continue
		}

		piecePosition := float64(i) / float64(numPieces)
		distance := math.Abs(piecePosition - playbackPosition)

		var priority torrent.PiecePriority
		if distance < float64(windowSize)/float64(numPieces) {
			priority = torrent.PiecePriorityNow
		} else if distance < float64(windowSize*2)/float64(numPieces) {
			priority = torrent.PiecePriorityHigh
		} else {
			priority = torrent.PiecePriorityNormal
		}

		if priority != stats.priority {
			piece.SetPriority(priority)
			stats.priority = priority
			stats.lastRequest = time.Now()
		}
	}
}
