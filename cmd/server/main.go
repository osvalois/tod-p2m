package main

// internal/cmd/server/main.go
import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"tod-p2m/internal/api"
	"tod-p2m/internal/config"
	"tod-p2m/internal/torrent"
	"tod-p2m/pkg/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	log := logger.NewLogger(cfg.LogLevel)

	torrentManager, err := torrent.NewManager(cfg, log)
	if err != nil {
		return fmt.Errorf("failed to create torrent manager: %w", err)
	}
	defer torrentManager.Close()

	router := api.NewRouter(cfg, log, torrentManager)

	server := api.NewServer(cfg, router)

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Error().Err(err).Msg("Server error")
		}
	}()

	log.Info().Str("port", cfg.Port).Msg("Server started")

	<-ctx.Done()
	log.Info().Msg("Shutting down gracefully")

	if err := server.Shutdown(context.Background()); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	return nil
}
