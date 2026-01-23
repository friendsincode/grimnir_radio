package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/friendsincode/grimnir_radio/internal/db"
	"github.com/friendsincode/grimnir_radio/internal/logging"
	"github.com/friendsincode/grimnir_radio/internal/server"
	"github.com/friendsincode/grimnir_radio/internal/telemetry"
)

var (
	logger zerolog.Logger
	cfg    *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "grimnirradio",
	Short: "Grimnir Radio - Modern broadcast automation system",
	Long:  "Grimnir Radio is a modern, cloud-native broadcast automation system with advanced scheduling and playout capabilities.",
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the Grimnir Radio server",
	Long:  "Start the HTTP API server and scheduler for broadcast automation",
	RunE:  runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// loadConfig loads configuration (called by commands that need it)
func loadConfig() error {
	var err error
	cfg, err = config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger = logging.Setup(cfg.Environment)
	return nil
}

func runServe(cmd *cobra.Command, args []string) error {
	if err := loadConfig(); err != nil {
		return err
	}

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
		return fmt.Errorf("initialize tracer: %w", err)
	}
	defer func() {
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			logger.Error().Err(err).Msg("failed to shutdown tracer provider")
		}
	}()

	srv, err := server.New(cfg, logger)
	if err != nil {
		return fmt.Errorf("initialize server: %w", err)
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

	logger.Info().Msg("shutting down gracefully...")

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(timeoutCtx); err != nil {
		logger.Error().Err(err).Msg("graceful shutdown failed")
	}

	if err := srv.Close(); err != nil {
		logger.Error().Err(err).Msg("shutdown cleanup failed")
	}

	logger.Info().Msg("Grimnir Radio stopped")
	return nil
}

// initDatabase initializes the database connection (used by import commands)
func initDatabase() (*gorm.DB, error) {
	return db.Connect(cfg)
}

