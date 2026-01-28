/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

//go:build integration

package client

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	"github.com/friendsincode/grimnir_radio/internal/mediaengine"
	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// startTestMediaEngine starts a media engine server for testing
func startTestMediaEngine(t *testing.T, port int) (*grpc.Server, func()) {
	t.Helper()

	logger := zerolog.Nop()
	cfg := &mediaengine.Config{
		GRPCBind:     "127.0.0.1",
		GRPCPort:     port,
		LogLevel:     "info",
		GStreamerBin: "gst-launch-1.0",
	}

	service := mediaengine.New(cfg, logger)

	grpcServer := grpc.NewServer()
	pb.RegisterMediaEngineServer(grpcServer, service)

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	// Start server in background
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			t.Logf("server error: %v", err)
		}
	}()

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		service.Shutdown(ctx)
		grpcServer.GracefulStop()
	}

	return grpcServer, cleanup
}

func TestClientConnection(t *testing.T) {
	_, cleanup := startTestMediaEngine(t, 9191)
	defer cleanup()

	cfg := DefaultConfig("localhost:9191")
	client := New(cfg, zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test connection
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for connection to be ready
	time.Sleep(200 * time.Millisecond)

	// Verify connected
	if !client.IsConnected() {
		t.Error("client should be connected")
	}

	// Test a simple RPC call to verify connection works
	_, err := client.GetStatus(ctx, "test-station", "test-mount")
	if err != nil {
		t.Errorf("RPC call failed: %v", err)
	}
}

func TestLoadGraph(t *testing.T) {
	_, cleanup := startTestMediaEngine(t, 9192)
	defer cleanup()

	cfg := DefaultConfig("localhost:9192")
	client := New(cfg, zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	// Create a simple DSP graph
	graph := &pb.DSPGraph{
		Nodes: []*pb.DSPNode{
			{
				Id:   "input",
				Type: pb.NodeType_NODE_TYPE_INPUT,
			},
			{
				Id:   "loudness",
				Type: pb.NodeType_NODE_TYPE_LOUDNESS_NORMALIZE,
				Params: map[string]string{
					"target_lufs": "-16",
				},
			},
			{
				Id:   "output",
				Type: pb.NodeType_NODE_TYPE_OUTPUT,
			},
		},
		Connections: []*pb.DSPConnection{
			{FromNode: "input", ToNode: "loudness"},
			{FromNode: "loudness", ToNode: "output"},
		},
	}

	// Load graph
	graphHandle, err := client.LoadGraph(ctx, "test-station", "test-mount", graph)
	if err != nil {
		t.Fatalf("failed to load graph: %v", err)
	}

	if graphHandle == "" {
		t.Error("graph handle should not be empty")
	}

	t.Logf("loaded graph with handle: %s", graphHandle)
}

func TestPlayAndStop(t *testing.T) {
	_, cleanup := startTestMediaEngine(t, 9193)
	defer cleanup()

	cfg := DefaultConfig("localhost:9193")
	client := New(cfg, zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	// Load graph first
	graph := &pb.DSPGraph{
		Nodes: []*pb.DSPNode{
			{Id: "input", Type: pb.NodeType_NODE_TYPE_INPUT},
			{Id: "output", Type: pb.NodeType_NODE_TYPE_OUTPUT},
		},
		Connections: []*pb.DSPConnection{
			{FromNode: "input", ToNode: "output"},
		},
	}

	_, err := client.LoadGraph(ctx, "test-station", "test-mount", graph)
	if err != nil {
		t.Fatalf("failed to load graph: %v", err)
	}

	// Test play
	playReq := &pb.PlayRequest{
		StationId: "test-station",
		MountId:   "test-mount",
		Source: &pb.SourceConfig{
			Type:     pb.SourceType_SOURCE_TYPE_MEDIA,
			SourceId: "test-media-1",
			Path:     "/tmp/test.mp3",
		},
		Priority: 3, // Automation priority
	}

	playbackID, err := client.Play(ctx, playReq)
	if err != nil {
		t.Fatalf("failed to start playback: %v", err)
	}

	if playbackID == "" {
		t.Error("playback ID should not be empty")
	}

	t.Logf("started playback with ID: %s", playbackID)

	// Wait a bit
	time.Sleep(500 * time.Millisecond)

	// Test stop
	if err := client.Stop(ctx, "test-station", "test-mount", false); err != nil {
		t.Fatalf("failed to stop playback: %v", err)
	}

	t.Log("stopped playback successfully")
}

func TestFade(t *testing.T) {
	_, cleanup := startTestMediaEngine(t, 9194)
	defer cleanup()

	cfg := DefaultConfig("localhost:9194")
	client := New(cfg, zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	// Load graph
	graph := &pb.DSPGraph{
		Nodes: []*pb.DSPNode{
			{Id: "input", Type: pb.NodeType_NODE_TYPE_INPUT},
			{Id: "output", Type: pb.NodeType_NODE_TYPE_OUTPUT},
		},
		Connections: []*pb.DSPConnection{
			{FromNode: "input", ToNode: "output"},
		},
	}

	_, err := client.LoadGraph(ctx, "test-station", "test-mount", graph)
	if err != nil {
		t.Fatalf("failed to load graph: %v", err)
	}

	// Start initial playback
	playReq := &pb.PlayRequest{
		StationId: "test-station",
		MountId:   "test-mount",
		Source: &pb.SourceConfig{
			Type:     pb.SourceType_SOURCE_TYPE_MEDIA,
			SourceId: "test-media-1",
			Path:     "/tmp/test1.mp3",
		},
	}

	_, err = client.Play(ctx, playReq)
	if err != nil {
		t.Fatalf("failed to start initial playback: %v", err)
	}

	// Test fade
	fadeReq := &pb.FadeRequest{
		StationId: "test-station",
		MountId:   "test-mount",
		NextSource: &pb.SourceConfig{
			Type:     pb.SourceType_SOURCE_TYPE_MEDIA,
			SourceId: "test-media-2",
			Path:     "/tmp/test2.mp3",
		},
		FadeConfig: &pb.FadeConfig{
			FadeInMs:  2000,
			FadeOutMs: 2000,
			Curve:     pb.FadeCurve_FADE_CURVE_SCURVE,
		},
	}

	fadeID, duration, err := client.Fade(ctx, fadeReq)
	if err != nil {
		t.Fatalf("failed to start fade: %v", err)
	}

	if fadeID == "" {
		t.Error("fade ID should not be empty")
	}

	if duration != 4000 {
		t.Errorf("expected fade duration 4000ms, got %d", duration)
	}

	t.Logf("started fade with ID: %s, duration: %dms", fadeID, duration)
}

func TestInsertEmergency(t *testing.T) {
	_, cleanup := startTestMediaEngine(t, 9195)
	defer cleanup()

	cfg := DefaultConfig("localhost:9195")
	client := New(cfg, zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	// Load graph
	graph := &pb.DSPGraph{
		Nodes: []*pb.DSPNode{
			{Id: "input", Type: pb.NodeType_NODE_TYPE_INPUT},
			{Id: "output", Type: pb.NodeType_NODE_TYPE_OUTPUT},
		},
		Connections: []*pb.DSPConnection{
			{FromNode: "input", ToNode: "output"},
		},
	}

	_, err := client.LoadGraph(ctx, "test-station", "test-mount", graph)
	if err != nil {
		t.Fatalf("failed to load graph: %v", err)
	}

	// Insert emergency
	emergencySource := &pb.SourceConfig{
		Type:     pb.SourceType_SOURCE_TYPE_EMERGENCY,
		SourceId: "emergency-alert",
		Path:     "/tmp/emergency.mp3",
	}

	emergencyID, err := client.InsertEmergency(ctx, "test-station", "test-mount", emergencySource)
	if err != nil {
		t.Fatalf("failed to insert emergency: %v", err)
	}

	if emergencyID == "" {
		t.Error("emergency ID should not be empty")
	}

	t.Logf("inserted emergency with ID: %s", emergencyID)
}

func TestRouteLive(t *testing.T) {
	_, cleanup := startTestMediaEngine(t, 9196)
	defer cleanup()

	cfg := DefaultConfig("localhost:9196")
	client := New(cfg, zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	// Load graph
	graph := &pb.DSPGraph{
		Nodes: []*pb.DSPNode{
			{Id: "input", Type: pb.NodeType_NODE_TYPE_INPUT},
			{Id: "compressor", Type: pb.NodeType_NODE_TYPE_COMPRESSOR},
			{Id: "output", Type: pb.NodeType_NODE_TYPE_OUTPUT},
		},
		Connections: []*pb.DSPConnection{
			{FromNode: "input", ToNode: "compressor"},
			{FromNode: "compressor", ToNode: "output"},
		},
	}

	_, err := client.LoadGraph(ctx, "test-station", "test-mount", graph)
	if err != nil {
		t.Fatalf("failed to load graph: %v", err)
	}

	// Route live input
	liveInput := &pb.LiveInputConfig{
		InputUrl:        "http://localhost:8001/live",
		AuthToken:       "test-token",
		BufferMs:        2000,
		ApplyProcessing: true,
	}

	liveID, err := client.RouteLive(ctx, "test-station", "test-mount", liveInput)
	if err != nil {
		t.Fatalf("failed to route live: %v", err)
	}

	if liveID == "" {
		t.Error("live ID should not be empty")
	}

	t.Logf("routed live input with ID: %s", liveID)
}

func TestGetStatus(t *testing.T) {
	_, cleanup := startTestMediaEngine(t, 9197)
	defer cleanup()

	cfg := DefaultConfig("localhost:9197")
	client := New(cfg, zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	// Get status (should return not running initially)
	status, err := client.GetStatus(ctx, "test-station", "test-mount")
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	if status.Running {
		t.Error("station should not be running initially")
	}

	t.Logf("status: running=%v, state=%v", status.Running, status.State)
}

func TestStreamTelemetry(t *testing.T) {
	_, cleanup := startTestMediaEngine(t, 9198)
	defer cleanup()

	cfg := DefaultConfig("localhost:9198")
	client := New(cfg, zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	// Load graph
	graph := &pb.DSPGraph{
		Nodes: []*pb.DSPNode{
			{Id: "input", Type: pb.NodeType_NODE_TYPE_INPUT},
			{Id: "meter", Type: pb.NodeType_NODE_TYPE_LEVEL_METER},
			{Id: "output", Type: pb.NodeType_NODE_TYPE_OUTPUT},
		},
		Connections: []*pb.DSPConnection{
			{FromNode: "input", ToNode: "meter"},
			{FromNode: "meter", ToNode: "output"},
		},
	}

	_, err := client.LoadGraph(ctx, "test-station", "test-mount", graph)
	if err != nil {
		t.Fatalf("failed to load graph: %v", err)
	}

	// Stream telemetry
	telemetryCount := 0
	maxTelemetry := 3

	telemetryCtx, telemetryCancel := context.WithCancel(ctx)
	defer telemetryCancel()

	callback := func(data *pb.TelemetryData) error {
		telemetryCount++
		t.Logf("received telemetry #%d: station=%s, state=%v",
			telemetryCount, data.StationId, data.State)

		if telemetryCount >= maxTelemetry {
			telemetryCancel()
		}
		return nil
	}

	err = client.StreamTelemetry(telemetryCtx, "test-station", "test-mount", 500, callback)
	if err != nil && err != context.Canceled {
		t.Fatalf("telemetry stream error: %v", err)
	}

	if telemetryCount < maxTelemetry {
		t.Errorf("expected at least %d telemetry updates, got %d", maxTelemetry, telemetryCount)
	}

	t.Logf("received %d telemetry updates", telemetryCount)
}

func TestRetryLogic(t *testing.T) {
	cfg := DefaultConfig("localhost:9999") // Non-existent server
	client := New(cfg, zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// This should fail to connect
	err := client.Connect(ctx)
	if err == nil {
		t.Error("expected connection to fail")
		client.Close()
	}

	// Test retry wrapper
	attempts := 0
	operation := func() error {
		attempts++
		if attempts < 2 {
			return fmt.Errorf("simulated failure")
		}
		return nil
	}

	err = client.Retry(ctx, operation)
	if err != nil {
		t.Errorf("retry should have succeeded after %d attempts: %v", attempts, err)
	}

	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestConcurrentOperations(t *testing.T) {
	_, cleanup := startTestMediaEngine(t, 9199)
	defer cleanup()

	cfg := DefaultConfig("localhost:9199")
	client := New(cfg, zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	// Load graph
	graph := &pb.DSPGraph{
		Nodes: []*pb.DSPNode{
			{Id: "input", Type: pb.NodeType_NODE_TYPE_INPUT},
			{Id: "output", Type: pb.NodeType_NODE_TYPE_OUTPUT},
		},
		Connections: []*pb.DSPConnection{
			{FromNode: "input", ToNode: "output"},
		},
	}

	_, err := client.LoadGraph(ctx, "test-station", "test-mount", graph)
	if err != nil {
		t.Fatalf("failed to load graph: %v", err)
	}

	// Run concurrent GetStatus calls
	const numConcurrent = 10
	errChan := make(chan error, numConcurrent)

	for i := 0; i < numConcurrent; i++ {
		go func(id int) {
			status, err := client.GetStatus(ctx, "test-station", "test-mount")
			if err != nil {
				errChan <- fmt.Errorf("goroutine %d: %w", id, err)
				return
			}
			t.Logf("goroutine %d: got status, running=%v", id, status.Running)
			errChan <- nil
		}(i)
	}

	// Wait for all to complete
	for i := 0; i < numConcurrent; i++ {
		if err := <-errChan; err != nil {
			t.Errorf("concurrent operation failed: %v", err)
		}
	}
}
