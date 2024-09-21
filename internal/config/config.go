package config

import (
	"fmt"
	"log"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	// Network and Server Configuration
	Port              string `mapstructure:"port"`
	LogLevel          string `mapstructure:"log_level"`
	MaxConnections    int    `mapstructure:"max_connections"`
	DownloadRateLimit int64  `mapstructure:"download_rate_limit"`
	UploadRateLimit   int64  `mapstructure:"upload_rate_limit"`
	RateLimit         int    `mapstructure:"rate_limit"`
	TCPPort           int    `mapstructure:"tcp_port"`
	UDPPort           int    `mapstructure:"udp_port"`
	EnableUPNP        bool   `mapstructure:"enable_upnp"`
	EnableDHT         bool   `mapstructure:"enable_dht"`
	EnablePEX         bool   `mapstructure:"enable_pex"`
	EnableLSD         bool   `mapstructure:"enable_lsd"`

	// Torrent Client Configuration
	PeerIDPrefix           string        `mapstructure:"peer_id_prefix"`
	UserAgent              string        `mapstructure:"user_agent"`
	DHTNodeCount           int           `mapstructure:"dht_node_count"`
	MaxPeersPerTorrent     int           `mapstructure:"max_peers_per_torrent"`
	MaxActiveTorrents      int           `mapstructure:"max_active_torrents"`
	SeedRatioLimit         float64       `mapstructure:"seed_ratio_limit"`
	SeedTimeLimit          time.Duration `mapstructure:"seed_time_limit"`
	MaxMetadataSize        int64         `mapstructure:"max_metadata_size"`
	EnableScrape           bool          `mapstructure:"enable_scrape"`
	TrackerTimeout         time.Duration `mapstructure:"tracker_timeout"`
	EnableMetadataExchange bool          `mapstructure:"enable_metadata_exchange"`

	// Connection and Peer Management
	ConnectionPoolSize     int    `mapstructure:"connection_pool_size"`
	MaxIncomingConnections int    `mapstructure:"max_incoming_connections"`
	MaxOutgoingConnections int    `mapstructure:"max_outgoing_connections"`
	MaxPeerUploadSlots     int    `mapstructure:"max_peer_upload_slots"`
	MaxPeerDownloadSlots   int    `mapstructure:"max_peer_download_slots"`
	MaxActiveDownloads     int    `mapstructure:"max_active_downloads"`
	MaxActiveSeeds         int    `mapstructure:"max_active_seeds"`
	EnableIPFiltering      bool   `mapstructure:"enable_ip_filtering"`
	IPFilterFile           string `mapstructure:"ip_filter_file"`

	// Performance Tuning
	MaxOpenFiles     int   `mapstructure:"max_open_files"`
	WriteBufferSize  int   `mapstructure:"write_buffer_size"`
	ReadBufferSize   int   `mapstructure:"read_buffer_size"`
	PieceBufferSize  int   `mapstructure:"piece_buffer_size"`
	WriteCacheSize   int64 `mapstructure:"write_cache_size"`
	ReadCacheSize    int64 `mapstructure:"read_cache_size"`
	MaxPieceHandlers int   `mapstructure:"max_piece_handlers"`
	MaxRequestBlocks int   `mapstructure:"max_request_blocks"`

	// Streaming Optimization
	BufferSize         int `mapstructure:"buffer_size"`
	PrefetchSize       int `mapstructure:"prefetch_size"`
	StreamChunkSize    int `mapstructure:"stream_chunk_size"`
	HLSSegmentDuration int `mapstructure:"hls_segment_duration"`
	PiecePreloadCount  int `mapstructure:"piece_preload_count"`
	PieceReadAhead     int `mapstructure:"piece_read_ahead"`

	// Resource Management
	CacheSize                   int           `mapstructure:"cache_size"`
	CleanupInterval             time.Duration `mapstructure:"cleanup_interval"`
	DownloadDir                 string        `mapstructure:"download_dir"`
	KeepFiles                   bool          `mapstructure:"keep_files"`
	MaxDownloadTime             time.Duration `mapstructure:"max_download_time"`
	EnableBandwidthManagement   bool          `mapstructure:"enable_bandwidth_management"`
	BandwidthManagementInterval time.Duration `mapstructure:"bandwidth_management_interval"`

	// Concurrency and Request Handling
	MaxConcurrentTorrents int `mapstructure:"max_concurrent_torrents"`
	MaxPendingRequests    int `mapstructure:"max_pending_requests"`
	PieceSelectionWindow  int `mapstructure:"piece_selection_window"`
	MaxPeers              int `mapstructure:"max_peers"`

	// Timeouts and Retries
	WaitTimeout        time.Duration `mapstructure:"wait_timeout"`
	RequestTimeout     time.Duration `mapstructure:"request_timeout"`
	PieceTimeout       time.Duration `mapstructure:"piece_timeout"`
	TorrentTimeout     time.Duration `mapstructure:"torrent_timeout"`
	MetadataTimeout    time.Duration `mapstructure:"metadata_timeout"`
	PollInterval       time.Duration `mapstructure:"poll_interval"`
	StatsInterval      time.Duration `mapstructure:"stats_interval"`
	InitialRetryWait   time.Duration `mapstructure:"initial_retry_wait"`
	MaxRetryWait       time.Duration `mapstructure:"max_retry_wait"`
	MaxRetries         int           `mapstructure:"max_retries"`
	InitialRetryDelay  time.Duration `mapstructure:"initial_retry_delay"`
	MaxRetryDelay      time.Duration `mapstructure:"max_retry_delay"`
	RetryBackoffFactor float64       `mapstructure:"retry_backoff_factor"`
	RetryDelay         time.Duration `mapstructure:"retry_delay"`

	// Speed Limits
	MaxDownloadSpeed int64 `mapstructure:"max_download_speed"`
	MaxUploadSpeed   int64 `mapstructure:"max_upload_speed"`

	// Caching
	PieceCacheSize int           `mapstructure:"piece_cache_size"`
	SessionTimeout time.Duration `mapstructure:"session_timeout"`

	// New fields
	CORSAllowedOrigins           []string      `mapstructure:"cors_allowed_origins"`
	CacheTTL                     time.Duration `mapstructure:"cache_ttl"`
	ServerReadTimeout            time.Duration `mapstructure:"server_read_timeout"`
	ServerWriteTimeout           time.Duration `mapstructure:"server_write_timeout"`
	ServerIdleTimeout            time.Duration `mapstructure:"server_idle_timeout"`
	CircuitBreakerTimeout        time.Duration `mapstructure:"circuit_breaker_timeout"`
	CircuitBreakerMaxFailures    int           `mapstructure:"circuit_breaker_max_failures"`
	CircuitBreakerMaxRequests    uint32        `mapstructure:"circuit_breaker_max_requests"`
	CircuitBreakerInterval       time.Duration `mapstructure:"circuit_breaker_interval"`
	CircuitBreakerMinRequests    uint32        `mapstructure:"circuit_breaker_min_requests"`
	CircuitBreakerErrorThreshold float64       `mapstructure:"circuit_breaker_error_threshold"`
	ShutdownTimeout              time.Duration `mapstructure:"shutdown_timeout"`
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/app")
	viper.AutomaticEnv()

	// Set default values
	viper.SetDefault("port", "8080")
	viper.SetDefault("log_level", "debug")
	viper.SetDefault("max_connections", 5000)
	viper.SetDefault("download_rate_limit", 209715200)
	viper.SetDefault("upload_rate_limit", 1048576)
	viper.SetDefault("rate_limit", 1000)
	viper.SetDefault("tcp_port", 6881)
	viper.SetDefault("udp_port", 6882)
	viper.SetDefault("enable_upnp", true)
	viper.SetDefault("enable_dht", true)
	viper.SetDefault("enable_pex", true)
	viper.SetDefault("enable_lsd", true)
	viper.SetDefault("peer_id_prefix", "-TD2024-")
	viper.SetDefault("user_agent", "TorrentStreamer/3.0")
	viper.SetDefault("dht_node_count", 5000)
	viper.SetDefault("max_peers_per_torrent", 500)
	viper.SetDefault("max_active_torrents", 200)
	viper.SetDefault("seed_ratio_limit", 0.1)
	viper.SetDefault("seed_time_limit", "5m")
	viper.SetDefault("max_metadata_size", 20971520)
	viper.SetDefault("enable_scrape", true)
	viper.SetDefault("tracker_timeout", "30s")
	viper.SetDefault("enable_metadata_exchange", true)
	viper.SetDefault("connection_pool_size", 2000)
	viper.SetDefault("max_incoming_connections", 2500)
	viper.SetDefault("max_outgoing_connections", 2500)
	viper.SetDefault("max_peer_upload_slots", 2)
	viper.SetDefault("max_peer_download_slots", 8)
	viper.SetDefault("max_active_downloads", 100)
	viper.SetDefault("max_active_seeds", 10)
	viper.SetDefault("enable_ip_filtering", true)
	viper.SetDefault("ip_filter_file", "/etc/torrent/ipfilter.dat")
	viper.SetDefault("max_open_files", 8192)
	viper.SetDefault("write_buffer_size", 16777216)
	viper.SetDefault("read_buffer_size", 16777216)
	viper.SetDefault("piece_buffer_size", 1048576)
	viper.SetDefault("write_cache_size", 134217728)
	viper.SetDefault("read_cache_size", 268435456)
	viper.SetDefault("max_piece_handlers", 200)
	viper.SetDefault("max_request_blocks", 500)
	viper.SetDefault("buffer_size", 41943040)
	viper.SetDefault("prefetch_size", 20)
	viper.SetDefault("stream_chunk_size", 2097152)
	viper.SetDefault("hls_segment_duration", 4)
	viper.SetDefault("piece_preload_count", 10)
	viper.SetDefault("piece_read_ahead", 10)
	viper.SetDefault("cache_size", 100000)
	viper.SetDefault("cleanup_interval", "120m")
	viper.SetDefault("download_dir", "/downloads")
	viper.SetDefault("keep_files", false)
	viper.SetDefault("max_download_time", "24h")
	viper.SetDefault("enable_bandwidth_management", true)
	viper.SetDefault("bandwidth_management_interval", "500ms")
	viper.SetDefault("max_concurrent_torrents", 100)
	viper.SetDefault("max_pending_requests", 400)
	viper.SetDefault("piece_selection_window", 100)
	viper.SetDefault("max_peers", 100)
	viper.SetDefault("wait_timeout", "20s")
	viper.SetDefault("request_timeout", "15s")
	viper.SetDefault("piece_timeout", "20s")
	viper.SetDefault("torrent_timeout", "10m")
	viper.SetDefault("metadata_timeout", "2m")
	viper.SetDefault("poll_interval", "50ms")
	viper.SetDefault("stats_interval", "1m")
	viper.SetDefault("initial_retry_wait", "50ms")
	viper.SetDefault("max_retry_wait", "3s")
	viper.SetDefault("max_retries", 8)
	viper.SetDefault("initial_retry_delay", "500ms")
	viper.SetDefault("max_retry_delay", "30s")
	viper.SetDefault("retry_backoff_factor", 1.5)
	viper.SetDefault("retry_delay", "50ms")
	viper.SetDefault("max_download_speed", 209715200)
	viper.SetDefault("max_upload_speed", 10485760)
	viper.SetDefault("piece_cache_size", 2000)
	viper.SetDefault("session_timeout", "180m")
	viper.SetDefault("circuit_breaker_max_requests", 100)
	viper.SetDefault("circuit_breaker_interval", "1m")
	viper.SetDefault("circuit_breaker_timeout", "30s")
	viper.SetDefault("circuit_breaker_min_requests", 10)
	viper.SetDefault("circuit_breaker_error_threshold", 0.5)
	// New default values
	viper.SetDefault("cors_allowed_origins", []string{"*"})
	viper.SetDefault("cache_ttl", "5m")
	viper.SetDefault("server_read_timeout", "15s")
	viper.SetDefault("server_write_timeout", "15s")
	viper.SetDefault("server_idle_timeout", "60s")
	viper.SetDefault("circuit_breaker_timeout", "30s")
	viper.SetDefault("circuit_breaker_max_failures", 5)
	viper.SetDefault("shutdown_timeout", "30s")
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
