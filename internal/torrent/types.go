package torrent

import "time"

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
