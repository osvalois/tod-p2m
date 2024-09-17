package torrent

// internal/torrent/errors.go
import "errors"

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
