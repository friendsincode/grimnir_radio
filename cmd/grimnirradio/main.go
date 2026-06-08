/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/friendsincode/grimnir_radio/internal/db"
	"github.com/friendsincode/grimnir_radio/internal/live"
	"github.com/friendsincode/grimnir_radio/internal/logbuffer"
	"github.com/friendsincode/grimnir_radio/internal/logging"
	"github.com/friendsincode/grimnir_radio/internal/server"
	"github.com/friendsincode/grimnir_radio/internal/telemetry"
	"github.com/friendsincode/grimnir_radio/internal/version"
)

var (
	logger    zerolog.Logger
	cfg       *config.Config
	logBuffer *logbuffer.Buffer
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

	// Create log buffer for in-memory log storage (10,000 entries)
	logBuffer = logbuffer.New(10000)
	// Keep station log history independent of system log churn.
	// Capacity is per-station; maxStations caps memory growth.
	logBuffer.EnableStationBuffers(5000, 200)

	// Setup logging with log buffer capture
	logWriter := logbuffer.NewWriter(logBuffer, nil)
	logger = logging.SetupWithWriter(cfg.Environment, logWriter)
	return nil
}

func runServe(cmd *cobra.Command, args []string) error {
	if err := loadConfig(); err != nil {
		return err
	}

	logger.Info().Str("version", version.Version).Msg("Grimnir Radio starting")

	// Initialize OpenTelemetry tracing
	tracerProvider, err := telemetry.InitTracer(context.Background(), telemetry.TracerConfig{
		ServiceName:    "grimnir-radio",
		ServiceVersion: version.Version,
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

	srv, err := server.New(cfg, logBuffer, logger)
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

	// Start the DJAuth gRPC server. The fan-out binary dials this to
	// validate live-input credentials. When the live service is nil
	// (degraded boot path) or the port is zero, we skip the listen.
	grpcServer, grpcStop := startDJAuthGRPC(cfg, srv.LiveService(), logger)
	if grpcStop != nil {
		defer grpcStop()
	}
	_ = grpcServer // currently no other services share this listener; keep handle for future expansion

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

// startDJAuthGRPC binds a gRPC listener on (cfg.GRPCBind, cfg.GRPCPort) and
// registers the DJAuth service against the wired *live.Service. Returns the
// server (so future RPC services can register on the same listener) & a stop
// func the caller defers. nil/nil means "disabled or failed to bind"; the
// fan-out keeps falling back to AcceptAllAuthenticator in that case.
func startDJAuthGRPC(cfg *config.Config, liveSvc *live.Service, logger zerolog.Logger) (*grpc.Server, func()) {
	if liveSvc == nil {
		logger.Warn().Msg("DJAuth gRPC: live service nil; not starting")
		return nil, nil
	}
	if cfg.GRPCPort == 0 {
		logger.Info().Msg("DJAuth gRPC: GRIMNIR_GRPC_PORT=0; disabled")
		return nil, nil
	}
	addr := net.JoinHostPort(cfg.GRPCBind, fmt.Sprintf("%d", cfg.GRPCPort))
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error().Err(err).Str("addr", addr).Msg("DJAuth gRPC: listen failed; fan-out auth degraded")
		return nil, nil
	}
	gs := grpc.NewServer(grpc.ForceServerCodecV2(live.DJAuthJSONCodec()))
	live.RegisterDJAuthServer(gs, liveSvc)
	go func() {
		logger.Info().Str("addr", addr).Msg("DJAuth gRPC listening")
		if serveErr := gs.Serve(lis); serveErr != nil {
			logger.Error().Err(serveErr).Msg("DJAuth gRPC server exited")
		}
	}()
	return gs, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done := make(chan struct{})
		go func() {
			gs.GracefulStop()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			gs.Stop()
		}
	}
}
