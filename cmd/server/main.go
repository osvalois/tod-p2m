// internal/cmd/server/main.go
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tod-p2m/internal/api"
	"tod-p2m/internal/config"
	"tod-p2m/internal/torrent"
	"tod-p2m/pkg/logger"

	"golang.org/x/sync/errgroup"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("error al cargar la configuración: %w", err)
	}

	log := logger.NewLogger(cfg.LogLevel)

	// Contexto principal para la aplicación
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Grupo de error para manejar goroutines
	g, ctx := errgroup.WithContext(ctx)

	torrentManager, err := torrent.NewManager(cfg, log)
	if err != nil {
		return fmt.Errorf("error al crear el gestor de torrents: %w", err)
	}
	defer torrentManager.Close()

	router, err := api.NewRouter(cfg, log, torrentManager)
	if err != nil {
		return fmt.Errorf("error al crear el router: %w", err)
	}

	server := api.NewServer(cfg, router)

	// Manejar señales de apagado
	g.Go(func() error {
		signalChan := make(chan os.Signal, 1)
		signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
		select {
		case <-signalChan:
			log.Info().Msg("Señal de apagado recibida")
		case <-ctx.Done():
			log.Info().Msg("Cerrando el servidor debido a un error en otra goroutine")
		}
		cancel() // Cancelar el contexto principal
		return nil
	})

	// Iniciar el servidor en una goroutine
	g.Go(func() error {
		log.Info().Str("puerto", cfg.Port).Msg("Servidor iniciado")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("Error del servidor")
			return fmt.Errorf("error del servidor: %w", err)
		}
		return nil
	})

	// Esperar a que todas las goroutines terminen o se reciba una señal de apagado
	if err := g.Wait(); err != nil {
		log.Error().Err(err).Msg("Error en una goroutine")
	}

	// Apagado graceful del servidor
	log.Info().Msg("Iniciando apagado graceful")

	// Usar un valor predeterminado para el tiempo de apagado si no está definido en la configuración
	shutdownTimeout := 30 * time.Second
	if cfg.ShutdownTimeout > 0 {
		shutdownTimeout = cfg.ShutdownTimeout
	}

	if err := api.RunServer(server, shutdownTimeout, log); err != nil {
		log.Error().Err(err).Msg("Error durante el apagado del servidor")
		return fmt.Errorf("error al apagar el servidor: %w", err)
	}

	log.Info().Msg("Servidor apagado correctamente")
	return nil
}
