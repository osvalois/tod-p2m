package config

import (
	"fmt"
	"log"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Port                        string        `mapstructure:"port"`
	LogLevel                    string        `mapstructure:"log_level"`
	TorrentTimeout              time.Duration `mapstructure:"torrent_timeout"`
	MaxConnections              int           `mapstructure:"max_connections"`
	DownloadRateLimit           int64         `mapstructure:"download_rate_limit"`
	UploadRateLimit             int64         `mapstructure:"upload_rate_limit"`
	CacheSize                   int           `mapstructure:"cache_size"`
	CleanupInterval             time.Duration `mapstructure:"cleanup_interval"`
	HLSSegmentDuration          int           `mapstructure:"hls_segment_duration"`
	DownloadDir                 string        `mapstructure:"download_dir"`
	KeepFiles                   bool          `mapstructure:"keep_files"`
	MaxDownloadTime             time.Duration `mapstructure:"max_download_time"`
	DHTNodeCount                int           `mapstructure:"dht_node_count"`
	PeerIDPrefix                string        `mapstructure:"peer_id_prefix"`
	UserAgent                   string        `mapstructure:"user_agent"`
	TCPPort                     int           `mapstructure:"tcp_port"`
	UDPPort                     int           `mapstructure:"udp_port"`
	EnableUPNP                  bool          `mapstructure:"enable_upnp"`
	EnableDHT                   bool          `mapstructure:"enable_dht"`
	EnablePEX                   bool          `mapstructure:"enable_pex"`
	EnableLSD                   bool          `mapstructure:"enable_lsd"`
	MaxOpenFiles                int           `mapstructure:"max_open_files"`
	WriteBufferSize             int           `mapstructure:"write_buffer_size"`
	ReadBufferSize              int           `mapstructure:"read_buffer_size"`
	MaxPeersPerTorrent          int           `mapstructure:"max_peers_per_torrent"`
	MaxActiveTorrents           int           `mapstructure:"max_active_torrents"`
	SeedRatioLimit              float64       `mapstructure:"seed_ratio_limit"`
	SeedTimeLimit               time.Duration `mapstructure:"seed_time_limit"`
	PieceBufferSize             int           `mapstructure:"piece_buffer_size"`
	EnableScrape                bool          `mapstructure:"enable_scrape"`
	TrackerTimeout              time.Duration `mapstructure:"tracker_timeout"`
	EnableMetadataExchange      bool          `mapstructure:"enable_metadata_exchange"`
	MaxMetadataSize             int64         `mapstructure:"max_metadata_size"`
	ConnectionPoolSize          int           `mapstructure:"connection_pool_size"`
	MaxIncomingConnections      int           `mapstructure:"max_incoming_connections"`
	MaxOutgoingConnections      int           `mapstructure:"max_outgoing_connections"`
	ReadCacheSize               int64         `mapstructure:"read_cache_size"`
	WriteCacheSize              int64         `mapstructure:"write_cache_size"`
	MaxPieceHandlers            int           `mapstructure:"max_piece_handlers"`
	MaxRequestBlocks            int           `mapstructure:"max_request_blocks"`
	RequestTimeout              time.Duration `mapstructure:"request_timeout"`
	PieceTimeout                time.Duration `mapstructure:"piece_timeout"`
	MaxPeerUploadSlots          int           `mapstructure:"max_peer_upload_slots"`
	MaxPeerDownloadSlots        int           `mapstructure:"max_peer_download_slots"`
	MaxActiveDownloads          int           `mapstructure:"max_active_downloads"`
	MaxActiveSeeds              int           `mapstructure:"max_active_seeds"`
	EnableIPFiltering           bool          `mapstructure:"enable_ip_filtering"`
	IPFilterFile                string        `mapstructure:"ip_filter_file"`
	EnableBandwidthManagement   bool          `mapstructure:"enable_bandwidth_management"`
	BandwidthManagementInterval time.Duration `mapstructure:"bandwidth_management_interval"`
	BufferSize                  int           `mapstructure:"buffer_size"`
	PrefetchSize                int           `mapstructure:"prefetch_size"`
	StreamChunkSize             int           `mapstructure:"stream_chunk_size"`
	WaitTimeout                 time.Duration `mapstructure:"wait_timeout"`
	MaxConcurrentTorrents       int           `mapstructure:"max_concurrent_torrents"`
	MaxPendingRequests          int           `mapstructure:"max_pending_requests"`
	PieceSelectionWindow        int           `mapstructure:"piece_selection_window"`
	PollInterval                time.Duration `mapstructure:"poll_interval"`
	StatsInterval               time.Duration `mapstructure:"stats_interval"`
	MetadataTimeout             time.Duration `mapstructure:"metadata_timeout"`
	PiecePreloadCount           int           `mapstructure:"piece_preload_count"`
	InitialRetryWait            time.Duration `mapstructure:"initial_retry_wait"`
	MaxRetryWait                time.Duration `mapstructure:"max_retry_wait"`
	MaxRetries                  int           `mapstructure:"max_retries"`
	InitialRetryDelay           time.Duration `mapstructure:"initial_retry_delay"`
	MaxRetryDelay               time.Duration `mapstructure:"max_retry_delay"`
	RetryBackoffFactor          float64       `mapstructure:"retry_backoff_factor"`
	RetryDelay                  time.Duration `mapstructure:"retry_delay"`
	MaxDownloadSpeed            int64         `mapstructure:"max_download_speed"`
	MaxUploadSpeed              int64         `mapstructure:"max_upload_speed"`
	MaxPeers                    int           `mapstructure:"max_peers"`
	PieceReadAhead              int           `mapstructure:"piece_read_ahead"`
	PieceCacheSize              int           `mapstructure:"piece_cache_size"`
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/app")
	viper.AutomaticEnv()

	// Set default values
	viper.SetDefault("port", "8080")
	viper.SetDefault("log_level", "info")
	viper.SetDefault("torrent_timeout", "180s")
	viper.SetDefault("max_connections", 2000)
	viper.SetDefault("download_rate_limit", 104857600)
	viper.SetDefault("upload_rate_limit", 1024)
	viper.SetDefault("cache_size", 50000)
	viper.SetDefault("cleanup_interval", "500m")
	viper.SetDefault("hls_segment_duration", 4)
	viper.SetDefault("download_dir", "/Users/oscarvalois/Downloads/torrents")
	viper.SetDefault("keep_files", true)
	viper.SetDefault("max_download_time", "12h")
	viper.SetDefault("dht_node_count", 2000)
	viper.SetDefault("peer_id_prefix", "-TD2024-")
	viper.SetDefault("user_agent", "TorrentDownloader/2.1")
	viper.SetDefault("tcp_port", 6881)
	viper.SetDefault("udp_port", 6882)
	viper.SetDefault("enable_upnp", true)
	viper.SetDefault("enable_dht", true)
	viper.SetDefault("enable_pex", true)
	viper.SetDefault("enable_lsd", true)
	viper.SetDefault("max_open_files", 4096)
	viper.SetDefault("write_buffer_size", 8388608)
	viper.SetDefault("read_buffer_size", 8388608)
	viper.SetDefault("max_peers_per_torrent", 300)
	viper.SetDefault("max_active_torrents", 100)
	viper.SetDefault("seed_ratio_limit", 0.0001)
	viper.SetDefault("seed_time_limit", "1m")
	viper.SetDefault("piece_buffer_size", 524288)
	viper.SetDefault("enable_scrape", true)
	viper.SetDefault("tracker_timeout", "45s")
	viper.SetDefault("enable_metadata_exchange", true)
	viper.SetDefault("max_metadata_size", 20971520)
	viper.SetDefault("connection_pool_size", 1000)
	viper.SetDefault("max_incoming_connections", 1000)
	viper.SetDefault("max_outgoing_connections", 1000)
	viper.SetDefault("read_cache_size", 67108864)
	viper.SetDefault("write_cache_size", 67108864)
	viper.SetDefault("max_piece_handlers", 100)
	viper.SetDefault("max_request_blocks", 250)
	viper.SetDefault("request_timeout", "20s")
	viper.SetDefault("piece_timeout", "30s")
	viper.SetDefault("max_peer_upload_slots", 1)
	viper.SetDefault("max_peer_download_slots", 4)
	viper.SetDefault("max_active_downloads", 50)
	viper.SetDefault("max_active_seeds", 1)
	viper.SetDefault("enable_ip_filtering", true)
	viper.SetDefault("ip_filter_file", "/etc/torrent/ipfilter.dat")
	viper.SetDefault("enable_bandwidth_management", true)
	viper.SetDefault("bandwidth_management_interval", "1s")
	viper.SetDefault("buffer_size", 20971520)
	viper.SetDefault("prefetch_size", 10)
	viper.SetDefault("stream_chunk_size", 1048576)
	viper.SetDefault("wait_timeout", "30s")
	viper.SetDefault("max_concurrent_torrents", 50)
	viper.SetDefault("max_pending_requests", 200)
	viper.SetDefault("piece_selection_window", 50)
	viper.SetDefault("poll_interval", "100ms")
	viper.SetDefault("stats_interval", "5m")
	viper.SetDefault("metadata_timeout", "1m")
	viper.SetDefault("piece_preload_count", 5)
	viper.SetDefault("initial_retry_wait", "100ms")
	viper.SetDefault("max_retry_wait", "5s")
	viper.SetDefault("max_retries", 5)
	viper.SetDefault("initial_retry_delay", "1s")
	viper.SetDefault("max_retry_delay", "1m")
	viper.SetDefault("retry_backoff_factor", 2.0)
	viper.SetDefault("retry_delay", "100ms")
	viper.SetDefault("max_download_speed", 20971520)
	viper.SetDefault("max_upload_speed", 5242880)
	viper.SetDefault("max_peers", 50)
	viper.SetDefault("piece_read_ahead", 5)
	viper.SetDefault("piece_cache_size", 1000)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Println("Config file not found. Using default values.")
		} else {
			return nil, fmt.Errorf("error reading config file: %s", err)
		}
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("unable to decode into struct: %s", err)
	}

	log.Printf("Loaded configuration: %+v", config)

	return &config, nil
}
