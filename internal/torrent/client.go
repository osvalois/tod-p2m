package torrent

// internal/torrent/client.go
import (
	"tod-p2m/internal/config"

	"github.com/anacrolix/torrent"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

type Client struct {
	*torrent.Client
	logger zerolog.Logger
	config *torrent.ClientConfig
}

func NewClient(cfg *config.Config, logger zerolog.Logger, tmpDir string) (*Client, error) {
	clientConfig := torrent.NewDefaultClientConfig()
	clientConfig.DataDir = tmpDir
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
		config: clientConfig,
	}, nil
}

// SetMaxConnections sets the maximum number of established connections per torrent
func (c *Client) SetMaxConnections(maxConnections int) {
	c.config.EstablishedConnsPerTorrent = maxConnections
	c.config.HalfOpenConnsPerTorrent = maxConnections / 2
	c.config.TorrentPeersHighWater = maxConnections * 2
	c.config.TorrentPeersLowWater = maxConnections
	// Note: This change won't affect existing torrents, only new ones
}

// SetDownloadLimit sets the download rate limit
func (c *Client) SetDownloadLimit(downloadLimit int64) {
	if downloadLimit > 0 {
		c.config.DownloadRateLimiter = rate.NewLimiter(rate.Limit(downloadLimit), int(downloadLimit))
	} else {
		c.config.DownloadRateLimiter = nil
	}
	// Note: This change won't affect existing torrents, only new ones
}

// SetUploadLimit sets the upload rate limit
func (c *Client) SetUploadLimit(uploadLimit int64) {
	if uploadLimit > 0 {
		c.config.UploadRateLimiter = rate.NewLimiter(rate.Limit(uploadLimit), int(uploadLimit))
	} else {
		c.config.UploadRateLimiter = nil
	}
	// Note: This change won't affect existing torrents, only new ones
}
