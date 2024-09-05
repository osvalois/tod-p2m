package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/render"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"golang.org/x/time/rate"
)

// Config holds the application configuration
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

// App represents the application
type App struct {
	config        *Config
	router        *chi.Mux
	torrentClient *torrent.Client
	cache         *sync.Map
	logger        zerolog.Logger
}

// TorrentInfo represents information about a torrent
type TorrentInfo struct {
	InfoHash string     `json:"infoHash"`
	Name     string     `json:"name"`
	Files    []FileInfo `json:"files"`
}

// FileInfo represents information about a file in a torrent
type FileInfo struct {
	ID       int     `json:"id"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	config, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	logger := setupLogging(config.LogLevel)

	app, err := NewApp(config, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize application: %w", err)
	}

	logger.Info().Str("port", config.Port).Msg("Starting server")
	return app.Serve()
}

func loadConfig() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()

	viper.SetDefault("port", "8080")
	viper.SetDefault("log_level", "info")
	viper.SetDefault("torrent_timeout", "30s")
	viper.SetDefault("max_connections", 100)
	viper.SetDefault("download_rate_limit", 0)
	viper.SetDefault("upload_rate_limit", 0)
	viper.SetDefault("cache_size", 100)
	viper.SetDefault("cleanup_interval", "10m")
	viper.SetDefault("hls_segment_duration", 10)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}

func setupLogging(level string) zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	logLevel, err := zerolog.ParseLevel(level)
	if err != nil {
		logLevel = zerolog.InfoLevel
	}
	return zerolog.New(os.Stdout).With().Timestamp().Logger().Level(logLevel)
}

func NewApp(config *Config, logger zerolog.Logger) (*App, error) {
	torrentClient, err := newTorrentClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create torrent client: %w", err)
	}

	app := &App{
		config:        config,
		router:        chi.NewRouter(),
		torrentClient: torrentClient,
		cache:         &sync.Map{},
		logger:        logger,
	}

	app.setupRoutes()
	go app.cleanupTorrents()

	return app, nil
}

func newTorrentClient(config *Config) (*torrent.Client, error) {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = ""
	cfg.NoUpload = false
	cfg.DisableTrackers = false
	cfg.NoDHT = false
	cfg.DisableTCP = false
	cfg.DisableUTP = false
	cfg.EstablishedConnsPerTorrent = 30
	cfg.HalfOpenConnsPerTorrent = 25
	cfg.TorrentPeersHighWater = 500
	cfg.TorrentPeersLowWater = 50
	cfg.Seed = true

	if config.DownloadRateLimit > 0 {
		cfg.DownloadRateLimiter = rate.NewLimiter(rate.Limit(config.DownloadRateLimit), int(config.DownloadRateLimit))
	}
	if config.UploadRateLimit > 0 {
		cfg.UploadRateLimiter = rate.NewLimiter(rate.Limit(config.UploadRateLimit), int(config.UploadRateLimit))
	}

	return torrent.NewClient(cfg)
}

func (a *App) setupRoutes() {
	a.router.Use(middleware.RequestID)
	a.router.Use(middleware.RealIP)
	a.router.Use(middleware.Logger)
	a.router.Use(middleware.Recoverer)
	a.router.Use(middleware.Timeout(60 * time.Second))
	a.router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	a.router.Get("/torrent/{infoHash}", a.getTorrentInfoHandler)
	a.router.Get("/stream/{infoHash}/{fileID}", a.streamHandler)
	a.router.Get("/hls/{infoHash}/{fileID}/playlist.m3u8", a.hlsPlaylistHandler)
	a.router.Get("/hls/{infoHash}/{fileID}/{segmentID}.ts", a.hlsSegmentHandler)
}

func (a *App) Serve() error {
	return http.ListenAndServe(":"+a.config.Port, a.router)
}

func (a *App) getTorrentInfoHandler(w http.ResponseWriter, r *http.Request) {
	infoHash := chi.URLParam(r, "infoHash")

	t, err := a.getTorrent(infoHash)
	if err != nil {
		a.logger.Error().Err(err).Str("infoHash", infoHash).Msg("Failed to get torrent")
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	info := TorrentInfo{
		InfoHash: t.InfoHash().String(),
		Name:     t.Name(),
		Files:    make([]FileInfo, 0, len(t.Files())),
	}

	for i, f := range t.Files() {
		info.Files = append(info.Files, FileInfo{
			ID:       i,
			Name:     f.DisplayPath(),
			Size:     f.Length(),
			Progress: float64(f.BytesCompleted()) / float64(f.Length()),
		})
	}

	render.JSON(w, r, info)
}

func (a *App) streamHandler(w http.ResponseWriter, r *http.Request) {
	infoHash := chi.URLParam(r, "infoHash")
	fileIDStr := chi.URLParam(r, "fileID")

	fileID, err := strconv.Atoi(fileIDStr)
	if err != nil {
		a.logger.Error().Err(err).Str("fileID", fileIDStr).Msg("Invalid file ID")
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Invalid file ID"})
		return
	}

	t, err := a.getTorrent(infoHash)
	if err != nil {
		a.logger.Error().Err(err).Str("infoHash", infoHash).Msg("Failed to get torrent")
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to get torrent"})
		return
	}

	if fileID < 0 || fileID >= len(t.Files()) {
		a.logger.Error().Int("fileID", fileID).Msg("File not found")
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": "File not found"})
		return
	}

	file := t.Files()[fileID]
	reader := file.NewReader()
	defer reader.Close()

	fileName := filepath.Base(file.DisplayPath())
	fileExt := filepath.Ext(fileName)
	mimeType := getMIMEType(fileExt)

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, fileName))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "public, max-age=3600")

	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		if err := handleRangeRequest(w, r, reader, file.Length(), mimeType); err != nil {
			a.logger.Error().Err(err).Msg("Failed to handle range request")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(file.Length(), 10))
		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, reader); err != nil {
			a.logger.Error().Err(err).Msg("Failed to stream file")
		}
	}
}

func handleRangeRequest(w http.ResponseWriter, r *http.Request, reader io.ReadSeeker, fileSize int64, mimeType string) error {
	rangeHeader := r.Header.Get("Range")
	rangeParts := strings.Split(strings.TrimPrefix(rangeHeader, "bytes="), "-")
	if len(rangeParts) != 2 {
		return fmt.Errorf("invalid range header")
	}

	start, err := strconv.ParseInt(rangeParts[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid range start: %w", err)
	}

	var end int64
	if rangeParts[1] != "" {
		end, err = strconv.ParseInt(rangeParts[1], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid range end: %w", err)
		}
	} else {
		end = fileSize - 1
	}

	if start >= fileSize || end >= fileSize || start > end {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return nil
	}

	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)

	_, err = reader.Seek(start, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek: %w", err)
	}

	_, err = io.CopyN(w, reader, end-start+1)
	if err != nil {
		return fmt.Errorf("failed to copy range: %w", err)
	}

	return nil
}

func (a *App) hlsPlaylistHandler(w http.ResponseWriter, r *http.Request) {
	infoHash := chi.URLParam(r, "infoHash")
	fileIDStr := chi.URLParam(r, "fileID")

	fileID, err := strconv.Atoi(fileIDStr)
	if err != nil {
		a.logger.Error().Err(err).Str("fileID", fileIDStr).Msg("Invalid file ID")
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Invalid file ID"})
		return
	}

	t, err := a.getTorrent(infoHash)
	if err != nil {
		a.logger.Error().Err(err).Str("infoHash", infoHash).Msg("Failed to get torrent")
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to get torrent"})
		return
	}

	if fileID < 0 || fileID >= len(t.Files()) {
		a.logger.Error().Int("fileID", fileID).Msg("File not found")
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": "File not found"})
		return
	}

	file := t.Files()[fileID]
	fileSize := file.Length()
	segmentDuration := a.config.HLSSegmentDuration
	segmentCount := int(fileSize / (10 * 1024 * 1024)) // Assuming 10MB segments

	w.Header().Set("Content-Type", "application/x-mpegURL")
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, "#EXTM3U\n")
	fmt.Fprintf(w, "#EXT-X-VERSION:3\n")
	fmt.Fprintf(w, "#EXT-X-TARGETDURATION:%d\n", segmentDuration)
	fmt.Fprintf(w, "#EXT-X-MEDIA-SEQUENCE:0\n")

	for i := 0; i < segmentCount; i++ {
		fmt.Fprintf(w, "#EXTINF:%d.0,\n", segmentDuration)
		fmt.Fprintf(w, "%d.ts\n", i)
	}

	fmt.Fprintf(w, "#EXT-X-ENDLIST\n")
}

func (a *App) hlsSegmentHandler(w http.ResponseWriter, r *http.Request) {
	infoHash := chi.URLParam(r, "infoHash")
	fileIDStr := chi.URLParam(r, "fileID")
	segmentIDStr := chi.URLParam(r, "segmentID")

	fileID, err := strconv.Atoi(fileIDStr)
	if err != nil {
		a.logger.Error().Err(err).Str("fileID", fileIDStr).Msg("Invalid file ID")
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Invalid file ID"})
		return
	}

	segmentID, err := strconv.Atoi(segmentIDStr)
	if err != nil {
		a.logger.Error().Err(err).Str("segmentID", segmentIDStr).Msg("Invalid segment ID")
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Invalid segment ID"})
		return
	}

	t, err := a.getTorrent(infoHash)
	if err != nil {
		a.logger.Error().Err(err).Str("infoHash", infoHash).Msg("Failed to get torrent")
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to get torrent"})
		return
	}

	if fileID < 0 || fileID >= len(t.Files()) {
		a.logger.Error().Int("fileID", fileID).Msg("File not found")
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": "File not found"})
		return
	}

	file := t.Files()[fileID]
	segmentSize := int64(10 * 1024 * 1024) // 10MB segment size
	offset := int64(segmentID) * segmentSize

	if offset >= file.Length() {
		a.logger.Error().Int("segmentID", segmentID).Msg("Segment not found")
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": "Segment not found"})
		return
	}

	reader := file.NewReader()
	defer reader.Close()

	_, err = reader.Seek(offset, io.SeekStart)
	if err != nil {
		a.logger.Error().Err(err).Msg("Failed to seek to segment")
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to seek to segment"})
		return
	}

	w.Header().Set("Content-Type", "video/MP2T")
	w.WriteHeader(http.StatusOK)

	_, err = io.CopyN(w, reader, segmentSize)
	if err != nil && err != io.EOF {
		a.logger.Error().Err(err).Msg("Failed to stream segment")
	}
}

func (a *App) getTorrent(infoHash string) (*torrent.Torrent, error) {
	// Verifica si el infoHash ya tiene el prefijo "magnet:"
	if !strings.HasPrefix(infoHash, "magnet:") {
		// Si no lo tiene, añade el prefijo
		infoHash = "magnet:?xt=urn:btih:" + infoHash
	}

	ctx, cancel := context.WithTimeout(context.Background(), a.config.TorrentTimeout)
	defer cancel()

	t, err := a.torrentClient.AddMagnet(infoHash)
	if err != nil {
		return nil, fmt.Errorf("failed to add magnet: %w", err)
	}

	if t == nil {
		return nil, fmt.Errorf("torrent is nil after adding magnet")
	}

	select {
	case <-t.GotInfo():
		return t, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("timeout waiting for torrent info")
	case <-t.Closed():
		return nil, fmt.Errorf("torrent was closed before getting info")
	}
}

func (a *App) cleanupTorrents() {
	ticker := time.NewTicker(a.config.CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		torrents := a.torrentClient.Torrents()
		for _, t := range torrents {
			select {
			case <-t.Closed():
				// Torrent is already closed, try to remove it
				if method := reflect.ValueOf(a.torrentClient).MethodByName("RemoveTorrent"); method.IsValid() {
					method.Call([]reflect.Value{reflect.ValueOf(t.InfoHash())})
				}
			default:
				// Check if the torrent is inactive
				if t.BytesCompleted() == t.Length() {
					if method := reflect.ValueOf(t).MethodByName("AddedTime"); method.IsValid() {
						addedTime := method.Call(nil)[0].Interface().(time.Time)
						if time.Since(addedTime) > 30*time.Minute {
							a.logger.Info().Str("infoHash", t.InfoHash().String()).Msg("Removing completed and inactive torrent")
							if dropMethod := reflect.ValueOf(t).MethodByName("Drop"); dropMethod.IsValid() {
								dropMethod.Call(nil)
							}
						}
					}
				}
			}
		}
	}
}

func getMIMEType(fileExt string) string {
	switch strings.ToLower(fileExt) {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".ogg":
		return "video/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	default:
		return "application/octet-stream"
	}
}
