package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/friendsincode/grimnir_radio/internal/logging"
	"github.com/friendsincode/grimnir_radio/internal/server"
	"github.com/friendsincode/grimnir_radio/internal/telemetry"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logger := logging.Setup(cfg.Environment)
	logger.Info().Msg("Grimnir Radio starting")

	// Initialize OpenTelemetry tracing
	tracerProvider, err := telemetry.InitTracer(context.Background(), telemetry.TracerConfig{
		ServiceName:    "grimnir-radio",
		ServiceVersion: "0.0.1-alpha",
		OTLPEndpoint:   cfg.OTLPEndpoint,
		Enabled:        cfg.TracingEnabled,
		SampleRate:     cfg.TracingSampleRate,
	}, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize tracer")
	}
	defer func() {
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			logger.Error().Err(err).Msg("failed to shutdown tracer provider")
		}
	}()

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

