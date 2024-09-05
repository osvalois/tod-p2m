package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/rs/zerolog"

	"tod-p2m/internal/api/handlers"
	"tod-p2m/internal/api/middleware"
	"tod-p2m/internal/config"
	"tod-p2m/internal/torrent"
)

func NewRouter(cfg *config.Config, log zerolog.Logger, tm *torrent.Manager) *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.RequestLogger(log))
	r.Use(middleware.Recoverer(log))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/torrent/{infoHash}", handlers.GetTorrentInfo(tm))
	r.Get("/stream/{infoHash}/{fileID}", handlers.StreamFile(tm))
	r.Get("/hls/{infoHash}/{fileID}/playlist.m3u8", handlers.HLSPlaylist(tm))
	r.Get("/hls/{infoHash}/{fileID}/{segmentID}.ts", handlers.HLSSegment(tm))
	r.Get("/documents/{infoHash}/{fileID}", handlers.ServeDocument(tm))
	r.Get("/images/{infoHash}/{fileID}", handlers.ServeImage(tm))

	return r
}

func NewServer(cfg *config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}
