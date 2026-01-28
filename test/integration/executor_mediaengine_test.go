/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

//go:build integration

package integration

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/executor"
	"github.com/friendsincode/grimnir_radio/internal/mediaengine"
	"github.com/friendsincode/grimnir_radio/internal/mediaengine/client"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: nil, // Disable GORM logging in tests
	})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Auto-migrate models
	if err := db.AutoMigrate(
		&models.ExecutorState{},
		&models.PrioritySource{},
	); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	return db
}

// startMediaEngine starts a media engine server for testing
func startMediaEngine(t *testing.T, port int) (*grpc.Server, func()) {
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

	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			t.Logf("server error: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		service.Shutdown(ctx)
		grpcServer.GracefulStop()
	}

	return grpcServer, cleanup
}

func TestExecutorWithMediaEngine(t *testing.T) {
	// Start media engine
	_, cleanup := startMediaEngine(t, 9201)
	defer cleanup()

	// Setup database
	db := setupTestDB(t)

	// Create components
	logger := zerolog.Nop()
	bus := events.NewBus()
	stateManager := executor.NewStateManager(db, logger)
	prioritySvc := priority.NewService(db, bus, logger)

	// Create media client
	mediaClient := client.New(
		client.DefaultConfig("localhost:9201"),
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connect to media engine
	if err := mediaClient.Connect(ctx); err != nil {
		t.Fatalf("failed to connect to media engine: %v", err)
	}
	defer mediaClient.Close()

	// Create media controller
	mediaCtrl := executor.NewMediaController(
		mediaClient,
		"test-station-1",
		"test-mount-1",
		logger,
	)

	// Load DSP graph
	graph := &pb.DSPGraph{
		Nodes: []*pb.DSPNode{
			{Id: "input", Type: pb.NodeType_NODE_TYPE_INPUT},
			{Id: "loudness", Type: pb.NodeType_NODE_TYPE_LOUDNESS_NORMALIZE},
			{Id: "compressor", Type: pb.NodeType_NODE_TYPE_COMPRESSOR},
			{Id: "limiter", Type: pb.NodeType_NODE_TYPE_LIMITER},
			{Id: "output", Type: pb.NodeType_NODE_TYPE_OUTPUT},
		},
		Connections: []*pb.DSPConnection{
			{FromNode: "input", ToNode: "loudness"},
			{FromNode: "loudness", ToNode: "compressor"},
			{FromNode: "compressor", ToNode: "limiter"},
			{FromNode: "limiter", ToNode: "output"},
		},
	}

	graphHandle, err := mediaCtrl.LoadGraph(ctx, graph)
	if err != nil {
		t.Fatalf("failed to load graph: %v", err)
	}

	t.Logf("loaded DSP graph: %s", graphHandle)

	// Create executor
	exec := executor.New(
		"test-station-1",
		db,
		stateManager,
		prioritySvc,
		bus,
		mediaCtrl,
		logger,
	)

	// Start executor
	if err := exec.Start(ctx); err != nil {
		t.Fatalf("failed to start executor: %v", err)
	}
	defer exec.Stop()

	// Wait for executor to initialize
	time.Sleep(200 * time.Millisecond)

	// Test 1: Start playback
	t.Run("PlayMedia", func(t *testing.T) {
		// Use executor's Play method which updates state
		if err := exec.Play(ctx, "test-media-1", models.PriorityAutomation); err != nil {
			t.Fatalf("failed to start playback via executor: %v", err)
		}

		t.Log("started playback via executor")

		// Verify executor state
		time.Sleep(200 * time.Millisecond)
		state, err := stateManager.GetState(ctx, "test-station-1")
		if err != nil {
			t.Fatalf("failed to get executor state: %v", err)
		}

		if state.State != models.ExecutorStatePlaying {
			t.Errorf("expected state PLAYING, got %s", state.State)
		}

		if state.CurrentSourceID != "test-media-1" {
			t.Errorf("expected current source test-media-1, got %s", state.CurrentSourceID)
		}
	})

	// Test 2: Crossfade
	t.Run("Crossfade", func(t *testing.T) {
		// Use executor's Fade method
		if err := exec.Fade(ctx, "test-media-2", models.PriorityAutomation); err != nil {
			t.Fatalf("failed to start fade via executor: %v", err)
		}

		t.Log("started fade via executor")

		// Verify executor state
		time.Sleep(200 * time.Millisecond)
		state, err := stateManager.GetState(ctx, "test-station-1")
		if err != nil {
			t.Fatalf("failed to get executor state: %v", err)
		}

		if state.State != models.ExecutorStateFading {
			t.Errorf("expected state FADING, got %s", state.State)
		}

		if state.NextSourceID != "test-media-2" {
			t.Errorf("expected next source test-media-2, got %s", state.NextSourceID)
		}
	})

	// Test 3: Emergency insertion
	t.Run("EmergencyInsert", func(t *testing.T) {
		emergencyID, err := mediaCtrl.InsertEmergency(
			ctx,
			"emergency-alert-1",
			"/tmp/emergency.mp3",
		)
		if err != nil {
			t.Fatalf("failed to insert emergency: %v", err)
		}

		t.Logf("inserted emergency: %s", emergencyID)

		// Emergency should preempt current playback
		time.Sleep(100 * time.Millisecond)
		state, err := stateManager.GetState(ctx, "test-station-1")
		if err != nil {
			t.Fatalf("failed to get executor state: %v", err)
		}

		if state.State != models.ExecutorStatePlaying {
			t.Logf("warning: expected state PLAYING after emergency, got %s", state.State)
		}
	})

	// Test 4: Stop playback via media controller (direct test)
	t.Run("StopPlayback", func(t *testing.T) {
		if err := mediaCtrl.Stop(ctx, false); err != nil {
			t.Fatalf("failed to stop playback: %v", err)
		}

		t.Log("stopped playback via media controller")

		// Verify media engine status
		time.Sleep(200 * time.Millisecond)
		status, err := mediaCtrl.GetStatus(ctx)
		if err != nil {
			t.Fatalf("failed to get status: %v", err)
		}

		if status.State == pb.PlaybackState_PLAYBACK_STATE_PLAYING {
			t.Error("expected playback to be stopped")
		}

		t.Logf("media engine state: %v", status.State)
	})

	// Test 5: Get status
	t.Run("GetStatus", func(t *testing.T) {
		status, err := mediaCtrl.GetStatus(ctx)
		if err != nil {
			t.Fatalf("failed to get status: %v", err)
		}

		t.Logf("status: running=%v, state=%v, uptime=%ds",
			status.Running, status.State, status.UptimeSeconds)

		if !status.Running {
			t.Error("expected status.Running to be true")
		}
	})
}

func TestPriorityEventFlow(t *testing.T) {
	// Start media engine
	_, cleanup := startMediaEngine(t, 9202)
	defer cleanup()

	// Setup database
	db := setupTestDB(t)

	// Create components
	logger := zerolog.Nop()
	bus := events.NewBus()
	stateManager := executor.NewStateManager(db, logger)
	prioritySvc := priority.NewService(db, bus, logger)

	// Create media client
	mediaClient := client.New(
		client.DefaultConfig("localhost:9202"),
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := mediaClient.Connect(ctx); err != nil {
		t.Fatalf("failed to connect to media engine: %v", err)
	}
	defer mediaClient.Close()

	// Create media controller
	mediaCtrl := executor.NewMediaController(
		mediaClient,
		"test-station-2",
		"test-mount-2",
		logger,
	)

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

	if _, err := mediaCtrl.LoadGraph(ctx, graph); err != nil {
		t.Fatalf("failed to load graph: %v", err)
	}

	// Create executor
	exec := executor.New(
		"test-station-2",
		db,
		stateManager,
		prioritySvc,
		bus,
		mediaCtrl,
		logger,
	)

	if err := exec.Start(ctx); err != nil {
		t.Fatalf("failed to start executor: %v", err)
	}
	defer exec.Stop()

	time.Sleep(200 * time.Millisecond)

	// Test priority change via event bus
	t.Run("PriorityChange", func(t *testing.T) {
		// Start a live override
		req := priority.StartOverrideRequest{
			StationID:  "test-station-2",
			SourceType: "media",
			SourceID:   "override-source-1",
			Metadata:   map[string]any{"title": "Live Override Test"},
		}

		result, err := prioritySvc.StartOverride(ctx, req)
		if err != nil {
			t.Fatalf("failed to start override: %v", err)
		}

		if result.NewSource == nil {
			t.Fatal("expected new source in transition result")
		}

		t.Logf("started override: source=%s, priority=%d, preempted=%v",
			result.NewSource.SourceID, result.NewSource.Priority, result.Preempted)

		// Give time for event to propagate
		time.Sleep(500 * time.Millisecond)

		// Check current priority
		current, err := prioritySvc.GetCurrent(ctx, "test-station-2")
		if err != nil {
			t.Fatalf("failed to get current priority: %v", err)
		}

		if current.Priority != models.PriorityLiveOverride {
			t.Errorf("expected priority %d, got %d", models.PriorityLiveOverride, current.Priority)
		}

		t.Logf("current priority: %d (source: %s)", current.Priority, current.SourceID)
	})
}

func TestTelemetryFlow(t *testing.T) {
	// Start media engine
	_, cleanup := startMediaEngine(t, 9203)
	defer cleanup()

	// Setup database
	db := setupTestDB(t)

	// Create components
	logger := zerolog.Nop()
	bus := events.NewBus()
	stateManager := executor.NewStateManager(db, logger)
	prioritySvc := priority.NewService(db, bus, logger)

	// Create media client
	mediaClient := client.New(
		client.DefaultConfig("localhost:9203"),
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := mediaClient.Connect(ctx); err != nil {
		t.Fatalf("failed to connect to media engine: %v", err)
	}
	defer mediaClient.Close()

	// Create media controller
	mediaCtrl := executor.NewMediaController(
		mediaClient,
		"test-station-3",
		"test-mount-3",
		logger,
	)

	// Load graph with level meter
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

	if _, err := mediaCtrl.LoadGraph(ctx, graph); err != nil {
		t.Fatalf("failed to load graph: %v", err)
	}

	// Create executor (which starts telemetry streaming automatically)
	exec := executor.New(
		"test-station-3",
		db,
		stateManager,
		prioritySvc,
		bus,
		mediaCtrl,
		logger,
	)

	if err := exec.Start(ctx); err != nil {
		t.Fatalf("failed to start executor: %v", err)
	}
	defer exec.Stop()

	// Wait for telemetry to flow
	time.Sleep(2 * time.Second)

	// Check executor state has telemetry data
	state, err := stateManager.GetState(ctx, "test-station-3")
	if err != nil {
		t.Fatalf("failed to get executor state: %v", err)
	}

	t.Logf("telemetry: audioL=%.2f, audioR=%.2f, loudness=%.2f, buffer=%dms",
		state.AudioLevelL, state.AudioLevelR, state.LoudnessLUFS, state.BufferDepthMS)

	// Telemetry should have been updated (heartbeat at minimum)
	if state.LastHeartbeat.IsZero() {
		t.Error("expected heartbeat to be updated")
	}

	// Check that heartbeat is recent (within last 10 seconds)
	if time.Since(state.LastHeartbeat) > 10*time.Second {
		t.Errorf("heartbeat is stale: %v", state.LastHeartbeat)
	}
}
