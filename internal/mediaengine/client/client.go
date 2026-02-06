/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package client

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// Client provides a high-level interface to the MediaEngine gRPC service
type Client struct {
	addr   string
	logger zerolog.Logger

	mu         sync.RWMutex
	conn       *grpc.ClientConn
	client     pb.MediaEngineClient
	connected  bool
	reconnectC chan struct{}
}

// Config holds client configuration
type Config struct {
	Address           string
	MaxRetries        int
	RetryInterval     time.Duration
	ConnectionTimeout time.Duration
}

// DefaultConfig returns default client configuration
func DefaultConfig(address string) *Config {
	return &Config{
		Address:           address,
		MaxRetries:        3,
		RetryInterval:     2 * time.Second,
		ConnectionTimeout: 10 * time.Second,
	}
}

// New creates a new media engine client
func New(cfg *Config, logger zerolog.Logger) *Client {
	return &Client{
		addr:       cfg.Address,
		logger:     logger.With().Str("component", "mediaengine_client").Logger(),
		reconnectC: make(chan struct{}, 1),
	}
}

// Connect establishes connection to the media engine
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	c.logger.Info().Str("address", c.addr).Msg("connecting to media engine")

	// Create gRPC connection with OpenTelemetry instrumentation
	conn, err := grpc.NewClient(
		c.addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(10*1024*1024),
			grpc.MaxCallSendMsgSize(10*1024*1024),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create gRPC client: %w", err)
	}

	c.conn = conn
	c.client = pb.NewMediaEngineClient(conn)
	c.connected = true

	c.logger.Info().Msg("connected to media engine")

	// Wait for connection to be ready
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Force connection by waiting for state to become ready
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			break
		}
		if state == connectivity.Shutdown || state == connectivity.TransientFailure {
			return fmt.Errorf("connection failed, state: %v", state)
		}
		if !conn.WaitForStateChange(ctx, state) {
			return fmt.Errorf("timeout waiting for connection")
		}
	}

	// Start connection monitoring
	go c.monitorConnection()

	return nil
}

// Close closes the connection to the media engine
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.logger.Info().Msg("closing media engine connection")
		err := c.conn.Close()
		c.conn = nil
		c.client = nil
		c.connected = false
		return err
	}

	return nil
}

// IsConnected returns whether the client is connected
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected && c.conn != nil && c.conn.GetState() == connectivity.Ready
}

// monitorConnection monitors connection state and triggers reconnection
func (c *Client) monitorConnection() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.RLock()
		if c.conn == nil {
			c.mu.RUnlock()
			return
		}
		state := c.conn.GetState()
		c.mu.RUnlock()

		if state == connectivity.TransientFailure || state == connectivity.Shutdown {
			c.logger.Warn().Str("state", state.String()).Msg("media engine connection unhealthy")
			select {
			case c.reconnectC <- struct{}{}:
			default:
			}
		}
	}
}

// LoadGraph loads a DSP processing graph
func (c *Client) LoadGraph(ctx context.Context, stationID, mountID string, graph *pb.DSPGraph) (string, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return "", fmt.Errorf("not connected to media engine")
	}

	req := &pb.LoadGraphRequest{
		StationId: stationID,
		MountId:   mountID,
		Graph:     graph,
	}

	resp, err := client.LoadGraph(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to load graph: %w", err)
	}

	if !resp.Success {
		return "", fmt.Errorf("load graph failed: %s", resp.Error)
	}

	return resp.GraphHandle, nil
}

// Play starts playback of a media source
func (c *Client) Play(ctx context.Context, req *pb.PlayRequest) (string, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return "", fmt.Errorf("not connected to media engine")
	}

	resp, err := client.Play(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to start playback: %w", err)
	}

	if !resp.Success {
		return "", fmt.Errorf("playback failed: %s", resp.Error)
	}

	return resp.PlaybackId, nil
}

// Stop halts playback
func (c *Client) Stop(ctx context.Context, stationID, mountID string, immediate bool) error {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("not connected to media engine")
	}

	req := &pb.StopRequest{
		StationId: stationID,
		MountId:   mountID,
		Immediate: immediate,
	}

	resp, err := client.Stop(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to stop playback: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("stop failed: %s", resp.Error)
	}

	return nil
}

// Fade initiates a crossfade between sources
func (c *Client) Fade(ctx context.Context, req *pb.FadeRequest) (string, int64, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return "", 0, fmt.Errorf("not connected to media engine")
	}

	resp, err := client.Fade(ctx, req)
	if err != nil {
		return "", 0, fmt.Errorf("failed to start fade: %w", err)
	}

	if !resp.Success {
		return "", 0, fmt.Errorf("fade failed: %s", resp.Error)
	}

	return resp.FadeId, resp.EstimatedDurationMs, nil
}

// InsertEmergency immediately plays emergency content
func (c *Client) InsertEmergency(ctx context.Context, stationID, mountID string, source *pb.SourceConfig) (string, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return "", fmt.Errorf("not connected to media engine")
	}

	req := &pb.InsertEmergencyRequest{
		StationId: stationID,
		MountId:   mountID,
		Source:    source,
	}

	resp, err := client.InsertEmergency(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to insert emergency: %w", err)
	}

	if !resp.Success {
		return "", fmt.Errorf("emergency insert failed: %s", resp.Error)
	}

	return resp.EmergencyId, nil
}

// RouteLiveRequest contains parameters for routing a live input.
type RouteLiveRequest struct {
	StationID string
	MountID   string
	SessionID string
	InputType pb.LiveInputType
	InputURL  string
	Port      int32
	FadeInMs  int32
}

// RouteLive routes a live input stream
func (c *Client) RouteLive(ctx context.Context, req *RouteLiveRequest) (string, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return "", fmt.Errorf("not connected to media engine")
	}

	pbReq := &pb.RouteLiveRequest{
		StationId: req.StationID,
		MountId:   req.MountID,
		SessionId: req.SessionID,
		InputType: req.InputType,
		InputUrl:  req.InputURL,
		Port:      req.Port,
		FadeInMs:  req.FadeInMs,
	}

	resp, err := client.RouteLive(ctx, pbReq)
	if err != nil {
		return "", fmt.Errorf("failed to route live: %w", err)
	}

	if !resp.Success {
		return "", fmt.Errorf("live routing failed: %s", resp.Error)
	}

	// Use session_id from response (modern) or fall back to live_id (legacy)
	if resp.SessionId != "" {
		return resp.SessionId, nil
	}
	return resp.LiveId, nil
}

// GetStatus returns current engine status
func (c *Client) GetStatus(ctx context.Context, stationID, mountID string) (*pb.StatusResponse, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("not connected to media engine")
	}

	req := &pb.StatusRequest{
		StationId: stationID,
		MountId:   mountID,
	}

	resp, err := client.GetStatus(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}

	return resp, nil
}

// TelemetryCallback is called for each telemetry update
type TelemetryCallback func(*pb.TelemetryData) error

// StreamTelemetry streams real-time audio telemetry
func (c *Client) StreamTelemetry(ctx context.Context, stationID, mountID string, intervalMs int32, callback TelemetryCallback) error {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("not connected to media engine")
	}

	req := &pb.TelemetryRequest{
		StationId:  stationID,
		MountId:    mountID,
		IntervalMs: intervalMs,
	}

	stream, err := client.StreamTelemetry(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to start telemetry stream: %w", err)
	}

	for {
		telemetry, err := stream.Recv()
		if err == io.EOF {
			c.logger.Info().Msg("telemetry stream closed")
			return nil
		}
		if err != nil {
			if status.Code(err) == codes.Canceled {
				return nil
			}
			return fmt.Errorf("telemetry stream error: %w", err)
		}

		if err := callback(telemetry); err != nil {
			return fmt.Errorf("telemetry callback error: %w", err)
		}
	}
}

// AnalyzeMedia performs media analysis (metadata, loudness, cue points)
func (c *Client) AnalyzeMedia(ctx context.Context, filePath string) (*pb.AnalyzeMediaResponse, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("not connected to media engine")
	}

	req := &pb.AnalyzeMediaRequest{
		FilePath: filePath,
	}

	resp, err := client.AnalyzeMedia(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze media: %w", err)
	}

	return resp, nil
}

// ExtractArtwork extracts embedded album art from media
func (c *Client) ExtractArtwork(ctx context.Context, filePath string, maxWidth, maxHeight int32, format string, quality int32) (*pb.ExtractArtworkResponse, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("not connected to media engine")
	}

	req := &pb.ExtractArtworkRequest{
		FilePath:  filePath,
		MaxWidth:  maxWidth,
		MaxHeight: maxHeight,
		Format:    format,
		Quality:   quality,
	}

	resp, err := client.ExtractArtwork(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to extract artwork: %w", err)
	}

	return resp, nil
}

// GenerateWaveform generates peak/RMS waveform data for visualization
func (c *Client) GenerateWaveform(ctx context.Context, filePath string, samplesPerSecond int32, waveformType pb.WaveformType) (*pb.GenerateWaveformResponse, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("not connected to media engine")
	}

	req := &pb.GenerateWaveformRequest{
		FilePath:         filePath,
		SamplesPerSecond: samplesPerSecond,
		Type:             waveformType,
	}

	resp, err := client.GenerateWaveform(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to generate waveform: %w", err)
	}

	return resp, nil
}

// Retry wraps an operation with retry logic
func (c *Client) Retry(ctx context.Context, operation func() error) error {
	maxRetries := 3
	retryInterval := 2 * time.Second

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			c.logger.Warn().
				Int("attempt", i+1).
				Int("max_retries", maxRetries).
				Msg("retrying operation")

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryInterval):
			}
		}

		err := operation()
		if err == nil {
			return nil
		}

		lastErr = err

		// Don't retry on certain errors
		if status.Code(err) == codes.InvalidArgument ||
			status.Code(err) == codes.NotFound ||
			status.Code(err) == codes.AlreadyExists {
			return err
		}
	}

	return fmt.Errorf("operation failed after %d retries: %w", maxRetries, lastErr)
}
