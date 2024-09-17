package config

// internal/internal/config/config.go
import (
	"fmt"
	"log"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Port                   string        `mapstructure:"port"`
	LogLevel               string        `mapstructure:"log_level"`
	TorrentTimeout         time.Duration `mapstructure:"torrent_timeout"`
	MaxConnections         int           `mapstructure:"max_connections"`
	DownloadRateLimit      int64         `mapstructure:"download_rate_limit"`
	UploadRateLimit        int64         `mapstructure:"upload_rate_limit"`
	CacheSize              int           `mapstructure:"cache_size"`
	CleanupInterval        time.Duration `mapstructure:"cleanup_interval"`
	HLSSegmentDuration     int           `mapstructure:"hls_segment_duration"`
	DownloadDir            string        `mapstructure:"download_dir"`
	KeepFiles              bool          `mapstructure:"keep_files"`
	MaxDownloadTime        time.Duration `mapstructure:"max_download_time"`
	DHTNodeCount           int           `mapstructure:"dht_node_count"`
	PeerIDPrefix           string        `mapstructure:"peer_id_prefix"`
	UserAgent              string        `mapstructure:"user_agent"`
	TCPPort                int           `mapstructure:"tcp_port"`
	UDPPort                int           `mapstructure:"udp_port"`
	EnableUPNP             bool          `mapstructure:"enable_upnp"`
	EnableDHT              bool          `mapstructure:"enable_dht"`
	EnablePEX              bool          `mapstructure:"enable_pex"`
	EnableLSD              bool          `mapstructure:"enable_lsd"`
	MaxOpenFiles           int           `mapstructure:"max_open_files"`
	WriteBufferSize        int           `mapstructure:"write_buffer_size"`
	ReadBufferSize         int           `mapstructure:"read_buffer_size"`
	MaxPeersPerTorrent     int           `mapstructure:"max_peers_per_torrent"`
	MaxActiveTorrents      int           `mapstructure:"max_active_torrents"`
	SeedRatioLimit         float64       `mapstructure:"seed_ratio_limit"`
	SeedTimeLimit          time.Duration `mapstructure:"seed_time_limit"`
	PieceBufferSize        int           `mapstructure:"piece_buffer_size"`
	EnableScrape           bool          `mapstructure:"enable_scrape"`
	TrackerTimeout         time.Duration `mapstructure:"tracker_timeout"`
	EnableMetadataExchange bool          `mapstructure:"enable_metadata_exchange"`
	MaxMetadataSize        int64         `mapstructure:"max_metadata_size"`
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/app") // Añadimos esta línea para buscar en el directorio /app
	viper.AutomaticEnv()

	// Configuraciones por defecto
	viper.SetDefault("port", "8080")
	viper.SetDefault("log_level", "info")
	viper.SetDefault("torrent_timeout", "120s")
	viper.SetDefault("max_connections", 500)
	viper.SetDefault("download_rate_limit", 10485760)
	viper.SetDefault("upload_rate_limit", 5242880)
	viper.SetDefault("cache_size", 10000)
	viper.SetDefault("cleanup_interval", "30m")
	viper.SetDefault("hls_segment_duration", 6)
	viper.SetDefault("download_dir", "/downloads")
	viper.SetDefault("keep_files", true)
	viper.SetDefault("max_download_time", "6h")
	viper.SetDefault("dht_node_count", 1000)
	viper.SetDefault("peer_id_prefix", "-TD2024-")
	viper.SetDefault("user_agent", "TorrentDownloader/2.0")
	viper.SetDefault("tcp_port", 6881)
	viper.SetDefault("udp_port", 6882)
	viper.SetDefault("enable_upnp", true)
	viper.SetDefault("enable_dht", true)
	viper.SetDefault("enable_pex", true)
	viper.SetDefault("enable_lsd", true)
	viper.SetDefault("max_open_files", 1000)
	viper.SetDefault("write_buffer_size", 2097152)
	viper.SetDefault("read_buffer_size", 2097152)
	viper.SetDefault("max_peers_per_torrent", 200)
	viper.SetDefault("max_active_torrents", 50)
	viper.SetDefault("seed_ratio_limit", 2.0)
	viper.SetDefault("seed_time_limit", "24h")
	viper.SetDefault("piece_buffer_size", 262144)
	viper.SetDefault("enable_scrape", true)
	viper.SetDefault("tracker_timeout", "30s")
	viper.SetDefault("enable_metadata_exchange", true)
	viper.SetDefault("max_metadata_size", 10485760)

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

	// Log the loaded configuration
	log.Printf("Loaded configuration: %+v", config)

	return &config, nil
}
