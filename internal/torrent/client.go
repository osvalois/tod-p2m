// File: internal/torrent/client.go

package torrent

import (
	"tod-p2m/internal/config"

	"github.com/anacrolix/torrent"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

type Client struct {
	*torrent.Client
	logger zerolog.Logger
}

func NewClient(cfg *config.Config, logger zerolog.Logger) (*Client, error) {
	clientConfig := torrent.NewDefaultClientConfig()
	clientConfig.DataDir = ""
	clientConfig.NoUpload = false
	clientConfig.DisableTrackers = false
	clientConfig.NoDHT = false
	clientConfig.DisableTCP = false
	clientConfig.DisableUTP = false
	clientConfig.EstablishedConnsPerTorrent = cfg.MaxConnections
	clientConfig.HalfOpenConnsPerTorrent = cfg.MaxConnections / 2
	clientConfig.TorrentPeersHighWater = cfg.MaxConnections * 2
	clientConfig.TorrentPeersLowWater = cfg.MaxConnections
	clientConfig.Seed = true

	if cfg.DownloadRateLimit > 0 {
		clientConfig.DownloadRateLimiter = rate.NewLimiter(rate.Limit(cfg.DownloadRateLimit), int(cfg.DownloadRateLimit))
	}
	if cfg.UploadRateLimit > 0 {
		clientConfig.UploadRateLimiter = rate.NewLimiter(rate.Limit(cfg.UploadRateLimit), int(cfg.UploadRateLimit))
	}

	client, err := torrent.NewClient(clientConfig)
	if err != nil {
		return nil, err
	}

	return &Client{
		Client: client,
		logger: logger,
	}, nil
}
