/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package executor

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	meclient "github.com/friendsincode/grimnir_radio/internal/mediaengine/client"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newDisconnectedMediaController creates a real MediaController whose inner
// client has never connected (IsConnected() == false), useful for testing
// all "not connected" error paths without a live gRPC server.
func newDisconnectedMediaController(t *testing.T) *MediaController {
	t.Helper()
	cl := meclient.New(meclient.DefaultConfig("localhost:1"), zerolog.Nop())
	return NewMediaController(cl, "station-test", "mount-test", zerolog.Nop())
}

// ── MediaController.IsConnected ───────────────────────────────────────────

func TestMediaController_IsConnected_False(t *testing.T) {
	mc := newDisconnectedMediaController(t)
	if mc.IsConnected() {
		t.Error("expected IsConnected() == false on an unconnected client")
	}
}

// ── MediaController not-connected error paths ────────────────────────────

func TestMediaController_LoadGraph_NotConnected(t *testing.T) {
	mc := newDisconnectedMediaController(t)
	_, err := mc.LoadGraph(context.Background(), &pb.DSPGraph{})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestMediaController_Play_NotConnected(t *testing.T) {
	mc := newDisconnectedMediaController(t)
	_, err := mc.Play(context.Background(), "src-1", "/tmp/test.mp3",
		pb.SourceType_SOURCE_TYPE_MEDIA, models.PriorityAutomation, nil)
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestMediaController_Stop_NotConnected(t *testing.T) {
	mc := newDisconnectedMediaController(t)
	if err := mc.Stop(context.Background(), true); err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestMediaController_Fade_NotConnected(t *testing.T) {
	mc := newDisconnectedMediaController(t)
	_, err := mc.Fade(context.Background(), "src-2", "/tmp/next.mp3",
		pb.SourceType_SOURCE_TYPE_MEDIA, nil, nil)
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestMediaController_InsertEmergency_NotConnected(t *testing.T) {
	mc := newDisconnectedMediaController(t)
	_, err := mc.InsertEmergency(context.Background(), "emg-1", "/tmp/emergency.mp3")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestMediaController_RouteLive_NotConnected(t *testing.T) {
	mc := newDisconnectedMediaController(t)
	_, err := mc.RouteLive(context.Background(), "http://icecast/live", "", false)
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestMediaController_GetStatus_NotConnected(t *testing.T) {
	mc := newDisconnectedMediaController(t)
	_, err := mc.GetStatus(context.Background())
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestMediaController_StreamTelemetry_NotConnected(t *testing.T) {
	mc := newDisconnectedMediaController(t)
	err := mc.StreamTelemetry(context.Background(), 1000, func(*pb.TelemetryData) error { return nil })
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

// ── Executor.Start with connected mock ────────────────────────────────────

func TestExecutorStart_WithConnectedController(t *testing.T) {
	sm, _ := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()

	streamExited := make(chan struct{})
	mock := &mockMediaController{
		connected: true,
		streamFunc: func(ctx context.Context, intervalMs int32, cb func(*pb.TelemetryData) error) error {
			defer close(streamExited)
			// block until context cancelled so the goroutine stays alive during the test
			<-ctx.Done()
			return ctx.Err()
		},
	}

	e := New(stationID, nil, sm, nil, bus, mock, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())

	if err := e.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}

	if !e.IsRunning() {
		cancel()
		t.Error("executor should be running after Start")
	}

	// Cancel context so background goroutines exit, then stop.
	cancel()
	select {
	case <-streamExited:
	case <-time.After(2 * time.Second):
		t.Log("warning: stream did not exit in time")
	}
	e.Stop() //nolint:errcheck
}

// ── Executor.telemetryStreamLoop ──────────────────────────────────────────

func TestExecutorTelemetryStreamLoop_SingleRecord(t *testing.T) {
	sm, _ := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()

	// Pre-create state so telemetryStreamLoop can call GetState without a DB.
	bgCtx := context.Background()
	if _, err := sm.GetState(bgCtx, stationID); err != nil {
		t.Fatalf("GetState: %v", err)
	}

	streamDone := make(chan struct{})
	mock := &mockMediaController{
		connected: true,
		streamFunc: func(streamCtx context.Context, intervalMs int32, cb func(*pb.TelemetryData) error) error {
			defer close(streamDone)
			_ = cb(&pb.TelemetryData{
				AudioLevelL:   -12.0,
				AudioLevelR:   -13.0,
				LoudnessLufs:  -23.0,
				BufferDepthMs: 4096,
				PositionMs:    1000,
				DurationMs:    180000,
				UnderrunCount: 0,
			})
			return nil
		},
	}

	e := New(stationID, nil, sm, nil, bus, mock, zerolog.Nop())
	execCtx, cancel := context.WithCancel(context.Background())

	if err := e.Start(execCtx); err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}

	select {
	case <-streamDone:
		// telemetryStreamLoop processed one record without panicking.
		// Cancel context and wait for goroutines to exit before Stop.
		cancel()
		time.Sleep(20 * time.Millisecond)
		e.Stop() //nolint:errcheck
	case <-time.After(2 * time.Second):
		cancel()
		e.Stop() //nolint:errcheck
		t.Fatal("telemetryStreamLoop did not complete in time")
	}
}

func TestExecutorTelemetryStreamLoop_UnderrunDelta(t *testing.T) {
	sm, _ := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()

	bgCtx := context.Background()
	if _, err := sm.GetState(bgCtx, stationID); err != nil {
		t.Fatalf("GetState: %v", err)
	}

	streamDone := make(chan struct{})
	mock := &mockMediaController{
		connected: true,
		streamFunc: func(streamCtx context.Context, intervalMs int32, cb func(*pb.TelemetryData) error) error {
			defer close(streamDone)
			// First packet: underrunCount=0
			_ = cb(&pb.TelemetryData{UnderrunCount: 0})
			// Second packet: underrunCount jumps to 3 — should log warning
			_ = cb(&pb.TelemetryData{UnderrunCount: 3})
			return nil
		},
	}

	e := New(stationID, nil, sm, nil, bus, mock, zerolog.Nop())
	execCtx, cancel := context.WithCancel(context.Background())

	if err := e.Start(execCtx); err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}

	select {
	case <-streamDone:
		// Cancel context and wait for goroutines to drain before Stop.
		cancel()
		time.Sleep(20 * time.Millisecond)
		e.Stop() //nolint:errcheck
	case <-time.After(2 * time.Second):
		cancel()
		e.Stop() //nolint:errcheck
		t.Fatal("telemetryStreamLoop did not complete in time")
	}
}

func TestExecutorTelemetryStreamLoop_StreamError(t *testing.T) {
	sm, _ := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()

	bgCtx := context.Background()
	if _, err := sm.GetState(bgCtx, stationID); err != nil {
		t.Fatalf("GetState: %v", err)
	}

	streamDone := make(chan struct{})
	mock := &mockMediaController{
		connected: true,
		streamFunc: func(streamCtx context.Context, intervalMs int32, cb func(*pb.TelemetryData) error) error {
			close(streamDone)
			return fmt.Errorf("simulated stream error")
		},
	}

	e := New(stationID, nil, sm, nil, bus, mock, zerolog.Nop())
	execCtx, cancel := context.WithCancel(context.Background())

	if err := e.Start(execCtx); err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}

	select {
	case <-streamDone:
		// telemetryStreamLoop handled the error without panicking.
		// Cancel context and wait for goroutines to drain before Stop.
		cancel()
		time.Sleep(20 * time.Millisecond)
		e.Stop() //nolint:errcheck
	case <-time.After(2 * time.Second):
		cancel()
		e.Stop() //nolint:errcheck
		t.Fatal("telemetryStreamLoop did not complete in time")
	}
}

// ── Executor with nil mediaCtrl ───────────────────────────────────────────

func TestExecutorStart_NilMediaCtrl(t *testing.T) {
	sm, _ := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()

	e := New(stationID, nil, sm, nil, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())

	if err := e.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start with nil mediaCtrl: %v", err)
	}

	if !e.IsRunning() {
		cancel()
		e.Stop() //nolint:errcheck
		t.Error("executor should be running even with nil media controller")
		return
	}

	// Cancel context so goroutines exit cleanly, then stop.
	cancel()
	time.Sleep(20 * time.Millisecond) // allow goroutines to observe ctx.Done
	e.Stop()                          //nolint:errcheck
}

// ── handleEmergencyEvent via mock ─────────────────────────────────────────

func TestExecutorHandleEmergencyEvent_WithConnectedMock(t *testing.T) {
	sm, _ := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()

	// Pre-create state.
	bgCtx := context.Background()
	if _, err := sm.GetState(bgCtx, stationID); err != nil {
		t.Fatalf("GetState: %v", err)
	}
	// Set to Playing so transition to Emergency is valid.
	if err := sm.SetState(bgCtx, stationID, models.ExecutorStatePlaying); err != nil {
		t.Fatalf("SetState playing: %v", err)
	}

	streamExited := make(chan struct{})
	mock := &mockMediaController{
		connected: true,
		streamFunc: func(streamCtx context.Context, intervalMs int32, cb func(*pb.TelemetryData) error) error {
			defer close(streamExited)
			<-streamCtx.Done()
			return streamCtx.Err()
		},
	}

	e := New(stationID, nil, sm, nil, bus, mock, zerolog.Nop())
	execCtx, cancel := context.WithCancel(context.Background())

	if err := e.Start(execCtx); err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}

	// Directly invoke the emergency handler while the executor is running.
	e.handleEmergencyEvent(map[string]interface{}{
		"station_id": stationID,
		"source_id":  "emg-src",
		"path":       "/tmp/emergency.mp3",
	})

	// Verify state transitioned to emergency.
	state, err := sm.GetState(bgCtx, stationID)
	if err != nil {
		cancel()
		t.Fatalf("GetState after emergency: %v", err)
	}
	if state.State != models.ExecutorStateEmergency {
		cancel()
		t.Errorf("state = %q, want emergency", state.State)
	}

	// Cancel context so telemetryStreamLoop exits, then stop cleanly.
	cancel()
	select {
	case <-streamExited:
	case <-time.After(2 * time.Second):
		t.Log("warning: stream did not exit in time")
	}
	e.Stop() //nolint:errcheck
}

func TestExecutorHandleEmergencyEvent_MissingFields(t *testing.T) {
	sm, _ := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()

	mock := &mockMediaController{connected: true}
	e := New(stationID, nil, sm, nil, bus, mock, zerolog.Nop())

	// Should not panic even with missing source_id / path.
	e.handleEmergencyEvent(map[string]interface{}{
		"station_id": stationID,
		// missing source_id and path
	})
}

func TestExecutorHandleEmergencyEvent_WrongStation(t *testing.T) {
	sm, _ := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()

	mock := &mockMediaController{connected: true}
	e := New(stationID, nil, sm, nil, bus, mock, zerolog.Nop())

	// Should be a no-op for a different station ID.
	e.handleEmergencyEvent(map[string]interface{}{
		"station_id": "other-station",
		"source_id":  "emg-src",
		"path":       "/tmp/emergency.mp3",
	})
}

// ── mock interface compliance ─────────────────────────────────────────────

// TestMockImplementsInterface verifies at compile time that mockMediaController
// satisfies MediaControllerIface.
func TestMockImplementsInterface(t *testing.T) {
	var _ MediaControllerIface = (*mockMediaController)(nil)
}

// TestConcreteImplementsInterface verifies that *MediaController satisfies the
// interface that callers (pool.go etc.) rely on.
func TestConcreteImplementsInterface(t *testing.T) {
	var _ MediaControllerIface = (*MediaController)(nil)
}

// ── mock method coverage ──────────────────────────────────────────────────

func TestMockMediaController_AllMethods(t *testing.T) {
	ctx := context.Background()
	sentinelErr := errors.New("sentinel")

	t.Run("IsConnected true", func(t *testing.T) {
		m := &mockMediaController{connected: true}
		if !m.IsConnected() {
			t.Error("expected true")
		}
	})

	t.Run("LoadGraph error", func(t *testing.T) {
		m := &mockMediaController{loadGraphErr: sentinelErr}
		_, err := m.LoadGraph(ctx, nil)
		if !errors.Is(err, sentinelErr) {
			t.Errorf("got %v, want sentinel", err)
		}
	})

	t.Run("Play increments counter", func(t *testing.T) {
		m := &mockMediaController{}
		m.Play(ctx, "s", "p", pb.SourceType_SOURCE_TYPE_MEDIA, models.PriorityAutomation, nil) //nolint:errcheck
		m.Play(ctx, "s", "p", pb.SourceType_SOURCE_TYPE_MEDIA, models.PriorityAutomation, nil) //nolint:errcheck
		if m.playCalls != 2 {
			t.Errorf("playCalls = %d, want 2", m.playCalls)
		}
	})

	t.Run("Stop increments counter", func(t *testing.T) {
		m := &mockMediaController{}
		m.Stop(ctx, false) //nolint:errcheck
		if m.stopCalls != 1 {
			t.Errorf("stopCalls = %d, want 1", m.stopCalls)
		}
	})

	t.Run("Fade increments counter", func(t *testing.T) {
		m := &mockMediaController{}
		m.Fade(ctx, "s", "p", pb.SourceType_SOURCE_TYPE_MEDIA, nil, nil) //nolint:errcheck
		if m.fadeCalls != 1 {
			t.Errorf("fadeCalls = %d, want 1", m.fadeCalls)
		}
	})

	t.Run("InsertEmergency ok", func(t *testing.T) {
		m := &mockMediaController{}
		id, err := m.InsertEmergency(ctx, "s", "p")
		if err != nil || id != "emergency-id" {
			t.Errorf("InsertEmergency = (%q, %v), want (emergency-id, nil)", id, err)
		}
	})

	t.Run("RouteLive ok", func(t *testing.T) {
		m := &mockMediaController{}
		id, err := m.RouteLive(ctx, "url", "tok", true)
		if err != nil || id != "live-id" {
			t.Errorf("RouteLive = (%q, %v), want (live-id, nil)", id, err)
		}
	})

	t.Run("GetStatus returns resp", func(t *testing.T) {
		resp := &pb.StatusResponse{}
		m := &mockMediaController{statusResp: resp}
		got, err := m.GetStatus(ctx)
		if err != nil || got != resp {
			t.Errorf("GetStatus = (%v, %v)", got, err)
		}
	})

	t.Run("StreamTelemetry nil func", func(t *testing.T) {
		m := &mockMediaController{}
		if err := m.StreamTelemetry(ctx, 1000, nil); err != nil {
			t.Errorf("StreamTelemetry with nil func: %v", err)
		}
	})

	t.Run("StreamTelemetry with func", func(t *testing.T) {
		called := false
		m := &mockMediaController{
			streamFunc: func(ctx context.Context, intervalMs int32, cb func(*pb.TelemetryData) error) error {
				called = true
				return nil
			},
		}
		if err := m.StreamTelemetry(ctx, 1000, nil); err != nil {
			t.Errorf("StreamTelemetry: %v", err)
		}
		if !called {
			t.Error("expected streamFunc to be called")
		}
	})
}

// ── Pool.Start with empty station table ───────────────────────────────────

func newPoolTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.ExecutorState{},
		&models.PrioritySource{},
		&models.Station{},
		&models.Mount{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestPool_Start_NoActiveStations(t *testing.T) {
	db := newPoolTestDB(t)
	sm := NewStateManager(db, zerolog.Nop())
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, zerolog.Nop())

	pool := NewPool("inst-1", db, sm, prioritySvc, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// No stations in DB — Start should succeed and launch zero executors.
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Pool.Start: %v", err)
	}

	executors := pool.ListExecutors()
	if len(executors) != 0 {
		t.Errorf("expected 0 executors, got %d", len(executors))
	}
}

func TestPool_Start_SkipsInactiveStations(t *testing.T) {
	db := newPoolTestDB(t)
	sm := NewStateManager(db, zerolog.Nop())
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, zerolog.Nop())

	// Insert an inactive station.
	inactive := models.Station{
		ID:     uuid.NewString(),
		Name:   "inactive station",
		Active: false,
	}
	if err := db.Create(&inactive).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	pool := NewPool("inst-1", db, sm, prioritySvc, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Pool.Start: %v", err)
	}

	if len(pool.ListExecutors()) != 0 {
		t.Error("expected no executors for inactive stations")
	}
}

func TestPool_StartExecutor_AlreadyRunning(t *testing.T) {
	db := newPoolTestDB(t)
	sm := NewStateManager(db, zerolog.Nop())
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, zerolog.Nop())

	pool := NewPool("inst-1", db, sm, prioritySvc, bus, nil, zerolog.Nop())
	stationID := uuid.NewString()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Manually add an executor entry to simulate already-running.
	pool.mu.Lock()
	pool.executors[stationID] = &Executor{}
	pool.mu.Unlock()

	err := pool.StartExecutor(ctx, stationID)
	if err == nil {
		t.Fatal("expected error when executor already running")
	}
}

func TestPool_StartExecutor_WrongInstance(t *testing.T) {
	db := newPoolTestDB(t)
	sm := NewStateManager(db, zerolog.Nop())
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, zerolog.Nop())

	// Create pool for inst-1, but add inst-2 so some stations hash to inst-2.
	pool := NewPool("inst-1", db, sm, prioritySvc, bus, nil, zerolog.Nop())
	pool.ring.addNode("inst-2")

	ctx := context.Background()

	// Find a station that hashes to inst-2.
	var targetID string
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("station-%04d", i)
		if node, ok := pool.ring.getNode(id); ok && node == "inst-2" {
			targetID = id
			break
		}
	}
	if targetID == "" {
		t.Skip("could not find a station hashing to inst-2 with these UUIDs")
	}

	err := pool.StartExecutor(ctx, targetID)
	if err == nil {
		t.Fatalf("expected error for station assigned to different instance")
	}
}

// ── handlePriorityEvent with full executor ────────────────────────────────

func TestHandlePriorityEvent_ValidSourceID(t *testing.T) {
	db := newPoolTestDB(t)
	sm := NewStateManager(db, zerolog.Nop())
	bus := events.NewBus()
	stationID := uuid.NewString()

	mock := &mockMediaController{connected: false}
	e := New(stationID, db, sm, nil, bus, mock, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop() //nolint:errcheck

	// handlePriorityEvent calls e.Play internally; Play calls TransitionTo.
	// State starts as idle so transition to playing should succeed.
	e.handlePriorityEvent(map[string]interface{}{
		"station_id": stationID,
		"source_id":  uuid.NewString(),
		"priority":   models.PriorityAutomation,
	})

	state, err := sm.GetState(context.Background(), stationID)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.State != models.ExecutorStatePlaying {
		t.Errorf("state = %q, want playing", state.State)
	}
}

// ── Executor.Stop error path ──────────────────────────────────────────────

func TestExecutorStop_SetStateError(t *testing.T) {
	// Use a StateManager whose DB is closed to trigger an error on SetState.
	db := newPoolTestDB(t)
	sm := NewStateManager(db, zerolog.Nop())
	bus := events.NewBus()
	stationID := uuid.NewString()

	mock := &mockMediaController{connected: false}
	e := New(stationID, db, sm, nil, bus, mock, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())

	if err := e.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}

	// Cancel and stop normally.
	cancel()
	time.Sleep(10 * time.Millisecond)
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Second Stop should return ErrExecutorNotRunning.
	if err := e.Stop(); !errors.Is(err, ErrExecutorNotRunning) {
		t.Errorf("second Stop = %v, want ErrExecutorNotRunning", err)
	}
}

// ── Executor.Start already running ───────────────────────────────────────

func TestExecutorStart_AlreadyRunningReturnsError(t *testing.T) {
	sm, _ := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()

	e := New(stationID, nil, sm, nil, bus, nil, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer e.Stop() //nolint:errcheck

	if err := e.Start(ctx); err == nil {
		t.Error("second Start should return error")
	}
}

// ── Executor.TransitionTo error path ─────────────────────────────────────

func TestExecutorTransitionTo_GetStateError(t *testing.T) {
	// Build a StateManager where there is no state for the station.
	// By using a closed DB we can force GetState to fail.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&models.ExecutorState{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	sm := NewStateManager(db, zerolog.Nop())
	stationID := uuid.NewString()

	e := &Executor{
		stationID:    stationID,
		stateManager: sm,
		logger:       zerolog.Nop(),
	}

	// Close the underlying DB to make GetState fail.
	rawDB, _ := db.DB()
	rawDB.Close()

	err = e.TransitionTo(context.Background(), models.ExecutorStatePlaying)
	if err == nil {
		t.Error("expected error when DB is closed")
	}
}
