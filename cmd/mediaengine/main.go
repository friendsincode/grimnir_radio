package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/friendsincode/grimnir_radio/internal/mediaengine"
	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

const (
	version = "0.0.1-alpha"
)

// loadConfig creates and loads configuration for the media engine
func loadConfig() (*mediaengine.Config, error) {
	cfg := &mediaengine.Config{
		GRPCBind:     getEnv("MEDIAENGINE_GRPC_BIND", "0.0.0.0"),
		GRPCPort:     getEnvInt("MEDIAENGINE_GRPC_PORT", 9091),
		LogLevel:     getEnv("MEDIAENGINE_LOG_LEVEL", "info"),
		GStreamerBin: getEnv("GSTREAMER_BIN", "gst-launch-1.0"),
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var parsed int
		if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func main() {
	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	logger := zerolog.New(os.Stdout).With().Timestamp().Str("component", "mediaengine").Logger()

	logger.Info().Str("version", version).Msg("Grimnir Radio Media Engine starting")

	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load configuration")
	}

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Create media engine service
	engine := mediaengine.New(cfg, logger)

	// Create gRPC server
	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(10*1024*1024), // 10MB max message size
		grpc.MaxSendMsgSize(10*1024*1024),
		grpc.ConnectionTimeout(30*time.Second),
	)

	// Register media engine service
	pb.RegisterMediaEngineServer(grpcServer, engine)

	// Register reflection service for development
	reflection.Register(grpcServer)

	// Start gRPC server
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.GRPCBind, cfg.GRPCPort))
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create listener")
	}

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		logger.Info().
			Str("bind", cfg.GRPCBind).
			Int("port", cfg.GRPCPort).
			Msg("gRPC server listening")
		if err := grpcServer.Serve(listener); err != nil {
			errChan <- err
		}
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-sigChan:
		logger.Info().Str("signal", sig.String()).Msg("received shutdown signal")
	case err := <-errChan:
		logger.Error().Err(err).Msg("gRPC server error")
	}

	// Graceful shutdown
	logger.Info().Msg("shutting down gracefully...")

	// Stop accepting new requests
	grpcServer.GracefulStop()

	// Shutdown engine
	if err := engine.Shutdown(ctx); err != nil {
		logger.Error().Err(err).Msg("engine shutdown error")
	}

	logger.Info().Msg("media engine stopped")
}
