package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/grimnirradio/internal/config"
	"github.com/example/grimnirradio/internal/logging"
	"github.com/example/grimnirradio/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logger := logging.Setup(cfg.Environment)
	logger.Info().Msg("Grimnir Radio starting")

	srv, err := server.New(cfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize server")
	}

	httpServer := srv.HTTPServer()

	go func() {
		addr := fmt.Sprintf("%s:%d", cfg.HTTPBind, cfg.HTTPPort)
		logger.Info().Str("addr", addr).Msg("HTTP server listening")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("http server error")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(timeoutCtx); err != nil {
		logger.Error().Err(err).Msg("graceful shutdown failed")
	}

	if err := srv.Close(); err != nil {
		logger.Error().Err(err).Msg("shutdown cleanup failed")
	}

	logger.Info().Msg("Grimnir Radio stopped")
}

