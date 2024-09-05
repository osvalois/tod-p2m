package config

import (
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Port               string        `mapstructure:"port"`
	LogLevel           string        `mapstructure:"log_level"`
	TorrentTimeout     time.Duration `mapstructure:"torrent_timeout"`
	MaxConnections     int           `mapstructure:"max_connections"`
	DownloadRateLimit  int64         `mapstructure:"download_rate_limit"`
	UploadRateLimit    int64         `mapstructure:"upload_rate_limit"`
	CacheSize          int           `mapstructure:"cache_size"`
	CleanupInterval    time.Duration `mapstructure:"cleanup_interval"`
	HLSSegmentDuration int           `mapstructure:"hls_segment_duration"`
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()

	viper.SetDefault("port", "8080")
	viper.SetDefault("log_level", "info")
	viper.SetDefault("torrent_timeout", "60s")
	viper.SetDefault("max_connections", 100)
	viper.SetDefault("download_rate_limit", 0)
	viper.SetDefault("upload_rate_limit", 0)
	viper.SetDefault("cache_size", 100)
	viper.SetDefault("cleanup_interval", "10m")
	viper.SetDefault("hls_segment_duration", 10)

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
