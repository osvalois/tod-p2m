// internal/api/router.go
package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"tod-p2m/internal/api/handlers"
	"tod-p2m/internal/api/middleware"
	"tod-p2m/internal/config"
	"tod-p2m/internal/torrent"
)

func NewRouter(cfg *config.Config, log zerolog.Logger, tm *torrent.Manager) (*chi.Mux, error) {
	r := chi.NewRouter()

	// Initialize tracer
	tracer, err := middleware.InitTracer("torrent-streamer")
	if err != nil {
		return nil, err
	}
	opentracing.SetGlobalTracer(tracer)

	// Middleware
	r.Use(middleware.RequestLogger(log))
	r.Use(middleware.Recoverer(log))
	r.Use(middleware.RateLimiter(cfg))
	r.Use(middleware.TimeoutMiddleware(cfg.RequestTimeout))
	r.Use(middleware.CorsMiddleware(cfg))
	r.Use(middleware.SecurityHeadersMiddleware)
	r.Use(middleware.TracingMiddleware(tracer))
	r.Use(middleware.CircuitBreakerMiddleware(cfg, log))
	r.Use(middleware.CompressMiddleware)
	r.Use(middleware.AdaptiveRateLimiter(cfg))

	// Create a new TorrentInfoCache with expiration
	cache := handlers.NewTorrentInfoCache(cfg.CacheTTL, cfg.CacheSize)

	// Routes
	r.Get("/metrics", promhttp.Handler().ServeHTTP)
	r.Get("/torrent/{infoHash}", handlers.GetTorrentInfo(tm, cache, log))
	r.Get("/stream/{infoHash}/{fileID}", handlers.StreamFile(tm, cfg))
	r.Get("/hls/{infoHash}/{fileID}/playlist.m3u8", handlers.HLSPlaylist(tm))
	r.Get("/hls/{infoHash}/{fileID}/{segmentID}.ts", handlers.HLSSegment(tm))
	r.Get("/documents/{infoHash}/{fileID}", handlers.ServeDocument(tm))
	r.Get("/images/{infoHash}/{fileID}", handlers.ServeImage(tm))
	return r, nil
}

func NewServer(cfg *config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  cfg.ServerReadTimeout,
		WriteTimeout: cfg.ServerWriteTimeout,
		IdleTimeout:  cfg.ServerIdleTimeout,
	}
}

func RunServer(srv *http.Server, shutdownTimeout time.Duration, log zerolog.Logger) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		<-ctx.Done()
		log.Info().Msg("Shutting down server...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutdownCancel()
		return srv.Shutdown(shutdownCtx)
	})

	g.Go(func() error {
		log.Info().Str("addr", srv.Addr).Msg("Starting server")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("Server error")
			return err
		}
		return nil
	})

	return g.Wait()
}
