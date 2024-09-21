// internal/api/router.go
package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/opentracing/opentracing-go"
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
	r.Use(middleware.CircuitBreakerMiddleware(cfg))
	r.Use(middleware.CompressMiddleware)

	// Create a new TorrentInfoCache with expiration
	cache := handlers.NewTorrentInfoCache(cfg.CacheTTL, cfg.CacheSize)

	// Routes
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

func RunServer(srv *http.Server, shutdownTimeout time.Duration) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutdownCancel()
		return srv.Shutdown(shutdownCtx)
	})

	g.Go(func() error {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	})

	return g.Wait()
}
