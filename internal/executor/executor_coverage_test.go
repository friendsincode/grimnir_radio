/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package executor

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newTestDB creates an in-memory SQLite DB with executor_states table.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.ExecutorState{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

// newTestDBWithPriority creates an in-memory SQLite DB for executor + priority tables.
func newTestDBWithPriority(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.ExecutorState{}, &models.PrioritySource{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

// newTestStateManager creates a StateManager backed by SQLite.
func newTestStateManager(t *testing.T) (*StateManager, *gorm.DB) {
	t.Helper()
	db := newTestDB(t)
	sm := NewStateManager(db, zerolog.Nop())
	return sm, db
}

// ── stateToMetricValue ────────────────────────────────────────────────────

func TestStateToMetricValue_AllStates(t *testing.T) {
	tests := []struct {
		state models.ExecutorStateEnum
		want  int
	}{
		{models.ExecutorStateIdle, 0},
		{models.ExecutorStatePreloading, 1},
		{models.ExecutorStatePlaying, 2},
		{models.ExecutorStateFading, 3},
		{models.ExecutorStateLive, 4},
		{models.ExecutorStateEmergency, 5},
		{"unknown_state", 0}, // default
	}
	for _, tt := range tests {
		got := stateToMetricValue(tt.state)
		if got != tt.want {
			t.Errorf("stateToMetricValue(%q) = %d, want %d", tt.state, got, tt.want)
		}
	}
}

// ── priorityToString ──────────────────────────────────────────────────────

func TestPriorityToString_AllValues(t *testing.T) {
	tests := []struct {
		priority models.PriorityLevel
		want     string
	}{
		{models.PriorityEmergency, "emergency"},
		{models.PriorityLiveOverride, "live_override"},
		{models.PriorityLiveScheduled, "live_scheduled"},
		{models.PriorityAutomation, "automation"},
		{models.PriorityFallback, "fallback"},
		{models.PriorityLevel(999), "unknown"},
	}
	for _, tt := range tests {
		got := priorityToString(tt.priority)
		if got != tt.want {
			t.Errorf("priorityToString(%d) = %q, want %q", tt.priority, got, tt.want)
		}
	}
}

// ── StateManager ─────────────────────────────────────────────────────────

func TestStateManager_GetState_CreatesIfAbsent(t *testing.T) {
	sm, _ := newTestStateManager(t)
	ctx := context.Background()
	stationID := uuid.NewString()

	state, err := sm.GetState(ctx, stationID)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if state.StationID != stationID {
		t.Errorf("station_id = %q, want %q", state.StationID, stationID)
	}
	if state.State != models.ExecutorStateIdle {
		t.Errorf("initial state = %q, want idle", state.State)
	}
}

func TestStateManager_GetState_CachesResult(t *testing.T) {
	sm, _ := newTestStateManager(t)
	ctx := context.Background()
	stationID := uuid.NewString()

	// First call creates the state
	state1, err := sm.GetState(ctx, stationID)
	if err != nil {
		t.Fatalf("first GetState: %v", err)
	}

	// Second call should return cached result (same pointer)
	state2, err := sm.GetState(ctx, stationID)
	if err != nil {
		t.Fatalf("second GetState: %v", err)
	}

	if state1 != state2 {
		t.Error("second GetState should return the cached pointer")
	}
}

func TestStateManager_SetState(t *testing.T) {
	sm, _ := newTestStateManager(t)
	ctx := context.Background()
	stationID := uuid.NewString()

	// Create initial state
	if _, err := sm.GetState(ctx, stationID); err != nil {
		t.Fatalf("GetState: %v", err)
	}

	// Transition to playing
	if err := sm.SetState(ctx, stationID, models.ExecutorStatePlaying); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	state, err := sm.GetState(ctx, stationID)
	if err != nil {
		t.Fatalf("GetState after SetState: %v", err)
	}
	if state.State != models.ExecutorStatePlaying {
		t.Errorf("state = %q, want playing", state.State)
	}
}

func TestStateManager_SetCurrentSource(t *testing.T) {
	sm, _ := newTestStateManager(t)
	ctx := context.Background()
	stationID := uuid.NewString()

	// Create initial state
	if _, err := sm.GetState(ctx, stationID); err != nil {
		t.Fatalf("GetState: %v", err)
	}

	sourceID := uuid.NewString()
	err := sm.SetCurrentSource(ctx, stationID, sourceID, models.PriorityAutomation)
	if err != nil {
		t.Fatalf("SetCurrentSource: %v", err)
	}

	state, _ := sm.GetState(ctx, stationID)
	if state.CurrentSourceID != sourceID {
		t.Errorf("current_source_id = %q, want %q", state.CurrentSourceID, sourceID)
	}
	if state.CurrentPriority != models.PriorityAutomation {
		t.Errorf("current_priority = %d, want %d", state.CurrentPriority, models.PriorityAutomation)
	}
}

func TestStateManager_SetNextSource(t *testing.T) {
	sm, _ := newTestStateManager(t)
	ctx := context.Background()
	stationID := uuid.NewString()

	// Create initial state
	if _, err := sm.GetState(ctx, stationID); err != nil {
		t.Fatalf("GetState: %v", err)
	}

	nextID := uuid.NewString()
	if err := sm.SetNextSource(ctx, stationID, nextID); err != nil {
		t.Fatalf("SetNextSource: %v", err)
	}

	state, _ := sm.GetState(ctx, stationID)
	if state.NextSourceID != nextID {
		t.Errorf("next_source_id = %q, want %q", state.NextSourceID, nextID)
	}
}

func TestStateManager_UpdateTelemetry(t *testing.T) {
	sm, _ := newTestStateManager(t)
	ctx := context.Background()
	stationID := uuid.NewString()

	if _, err := sm.GetState(ctx, stationID); err != nil {
		t.Fatalf("GetState: %v", err)
	}

	tel := Telemetry{
		AudioLevelL:   -12.5,
		AudioLevelR:   -13.0,
		LoudnessLUFS:  -23.0,
		BufferDepthMS: 4000,
	}
	if err := sm.UpdateTelemetry(ctx, stationID, tel); err != nil {
		t.Fatalf("UpdateTelemetry: %v", err)
	}

	state, _ := sm.GetState(ctx, stationID)
	if state.AudioLevelL != tel.AudioLevelL {
		t.Errorf("AudioLevelL = %v, want %v", state.AudioLevelL, tel.AudioLevelL)
	}
	if state.AudioLevelR != tel.AudioLevelR {
		t.Errorf("AudioLevelR = %v, want %v", state.AudioLevelR, tel.AudioLevelR)
	}
	if state.LoudnessLUFS != tel.LoudnessLUFS {
		t.Errorf("LoudnessLUFS = %v, want %v", state.LoudnessLUFS, tel.LoudnessLUFS)
	}
	if state.BufferDepthMS != tel.BufferDepthMS {
		t.Errorf("BufferDepthMS = %v, want %v", state.BufferDepthMS, tel.BufferDepthMS)
	}
}

func TestStateManager_IncrementUnderrun(t *testing.T) {
	sm, _ := newTestStateManager(t)
	ctx := context.Background()
	stationID := uuid.NewString()

	if _, err := sm.GetState(ctx, stationID); err != nil {
		t.Fatalf("GetState: %v", err)
	}

	for i := 1; i <= 3; i++ {
		if err := sm.IncrementUnderrun(ctx, stationID); err != nil {
			t.Fatalf("IncrementUnderrun #%d: %v", i, err)
		}
	}

	state, _ := sm.GetState(ctx, stationID)
	if state.UnderrunCount != 3 {
		t.Errorf("underrun_count = %d, want 3", state.UnderrunCount)
	}
}

func TestStateManager_Heartbeat(t *testing.T) {
	sm, _ := newTestStateManager(t)
	ctx := context.Background()
	stationID := uuid.NewString()

	if _, err := sm.GetState(ctx, stationID); err != nil {
		t.Fatalf("GetState: %v", err)
	}

	before := time.Now().Add(-1 * time.Second)
	if err := sm.Heartbeat(ctx, stationID); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	after := time.Now().Add(1 * time.Second)

	state, _ := sm.GetState(ctx, stationID)
	if state.LastHeartbeat.Before(before) || state.LastHeartbeat.After(after) {
		t.Errorf("LastHeartbeat = %v, expected between %v and %v", state.LastHeartbeat, before, after)
	}
}

func TestStateManager_ListStates(t *testing.T) {
	sm, _ := newTestStateManager(t)
	ctx := context.Background()

	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for _, id := range ids {
		if _, err := sm.GetState(ctx, id); err != nil {
			t.Fatalf("GetState(%s): %v", id, err)
		}
	}

	states, err := sm.ListStates(ctx)
	if err != nil {
		t.Fatalf("ListStates: %v", err)
	}
	if len(states) < 3 {
		t.Errorf("ListStates returned %d states, want at least 3", len(states))
	}
}

func TestStateManager_ClearCache(t *testing.T) {
	sm, _ := newTestStateManager(t)
	ctx := context.Background()
	stationID := uuid.NewString()

	if _, err := sm.GetState(ctx, stationID); err != nil {
		t.Fatalf("GetState: %v", err)
	}

	sm.ClearCache(stationID)

	sm.mu.RLock()
	_, ok := sm.states[stationID]
	sm.mu.RUnlock()

	if ok {
		t.Error("ClearCache should remove station from in-memory cache")
	}
}

// ── Executor (New / IsRunning / basic lifecycle) ──────────────────────────

func TestExecutorNew_InitializesFields(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()

	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())
	if ex == nil {
		t.Fatal("New returned nil")
	}
	if ex.stationID != stationID {
		t.Errorf("stationID = %q, want %q", ex.stationID, stationID)
	}
	if ex.IsRunning() {
		t.Error("newly created executor should not be running")
	}
}

func TestExecutorIsRunning_FalseBeforeStart(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()

	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())
	if ex.IsRunning() {
		t.Error("executor should not be running before Start()")
	}
}

func TestExecutorStop_ErrorWhenNotRunning(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	ex := New(uuid.NewString(), db, sm, nil, bus, nil, zerolog.Nop())

	err := ex.Stop()
	if err != ErrExecutorNotRunning {
		t.Errorf("Stop() when not running = %v, want ErrExecutorNotRunning", err)
	}
}

func TestExecutorStart_CreatesStateAndRuns(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ex.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ex.Stop() //nolint:errcheck

	if !ex.IsRunning() {
		t.Error("executor should be running after Start()")
	}
}

func TestExecutorStart_ErrorIfAlreadyRunning(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ex.Start(ctx); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer ex.Stop() //nolint:errcheck

	err := ex.Start(ctx)
	if err == nil {
		t.Error("second Start() should return error")
	}
}

func TestExecutorStop_SetsRunningFalse(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ex.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := ex.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if ex.IsRunning() {
		t.Error("executor should not be running after Stop()")
	}
}

func TestExecutorTransitionTo_ValidTransition(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ex.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ex.Stop() //nolint:errcheck

	if err := ex.TransitionTo(ctx, models.ExecutorStatePlaying); err != nil {
		t.Errorf("TransitionTo(playing): %v", err)
	}

	state, err := sm.GetState(ctx, stationID)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.State != models.ExecutorStatePlaying {
		t.Errorf("state = %q, want playing", state.State)
	}
}

func TestExecutorTransitionTo_InvalidTransition(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ex.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ex.Stop() //nolint:errcheck

	// Idle → Fading is invalid
	err := ex.TransitionTo(ctx, models.ExecutorStateFading)
	if err == nil {
		t.Error("Idle → Fading should fail")
	}
}

func TestExecutorPreload_SetsNextSource(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ex.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ex.Stop() //nolint:errcheck

	sourceID := uuid.NewString()
	if err := ex.Preload(ctx, sourceID); err != nil {
		t.Fatalf("Preload: %v", err)
	}

	state, _ := sm.GetState(ctx, stationID)
	if state.State != models.ExecutorStatePreloading {
		t.Errorf("state after Preload = %q, want preloading", state.State)
	}
	if state.NextSourceID != sourceID {
		t.Errorf("next_source_id = %q, want %q", state.NextSourceID, sourceID)
	}
}

func TestExecutorPlay_AutomationSetsPlayingState(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ex.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ex.Stop() //nolint:errcheck

	sourceID := uuid.NewString()
	if err := ex.Play(ctx, sourceID, models.PriorityAutomation); err != nil {
		t.Fatalf("Play: %v", err)
	}

	state, _ := sm.GetState(ctx, stationID)
	if state.State != models.ExecutorStatePlaying {
		t.Errorf("state after Play(automation) = %q, want playing", state.State)
	}
	if state.CurrentSourceID != sourceID {
		t.Errorf("current_source_id = %q, want %q", state.CurrentSourceID, sourceID)
	}
}

func TestExecutorPlay_LiveOverrideSetsLiveState(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ex.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ex.Stop() //nolint:errcheck

	if err := ex.Play(ctx, uuid.NewString(), models.PriorityLiveOverride); err != nil {
		t.Fatalf("Play(live_override): %v", err)
	}

	state, _ := sm.GetState(ctx, stationID)
	if state.State != models.ExecutorStateLive {
		t.Errorf("state after Play(live_override) = %q, want live", state.State)
	}
}

func TestExecutorPlay_EmergencySetsEmergencyState(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ex.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ex.Stop() //nolint:errcheck

	if err := ex.Play(ctx, uuid.NewString(), models.PriorityEmergency); err != nil {
		t.Fatalf("Play(emergency): %v", err)
	}

	state, _ := sm.GetState(ctx, stationID)
	if state.State != models.ExecutorStateEmergency {
		t.Errorf("state after Play(emergency) = %q, want emergency", state.State)
	}
}

func TestExecutorUpdateTelemetry(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ex.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ex.Stop() //nolint:errcheck

	tel := Telemetry{
		AudioLevelL:   -10.0,
		AudioLevelR:   -11.0,
		LoudnessLUFS:  -18.0,
		BufferDepthMS: 8000,
	}
	if err := ex.UpdateTelemetry(ctx, tel); err != nil {
		t.Fatalf("UpdateTelemetry: %v", err)
	}

	state, _ := sm.GetState(ctx, stationID)
	if state.AudioLevelL != tel.AudioLevelL {
		t.Errorf("AudioLevelL = %v, want %v", state.AudioLevelL, tel.AudioLevelL)
	}
}

func TestExecutorFade_TransitionsToFading(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ex.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ex.Stop() //nolint:errcheck

	// Move to playing first (fading requires playing state)
	if err := ex.Play(ctx, uuid.NewString(), models.PriorityAutomation); err != nil {
		t.Fatalf("Play: %v", err)
	}

	nextSource := uuid.NewString()
	if err := ex.Fade(ctx, nextSource, models.PriorityAutomation); err != nil {
		t.Fatalf("Fade: %v", err)
	}

	state, _ := sm.GetState(ctx, stationID)
	if state.State != models.ExecutorStateFading {
		t.Errorf("state after Fade = %q, want fading", state.State)
	}
	if state.NextSourceID != nextSource {
		t.Errorf("next_source_id = %q, want %q", state.NextSourceID, nextSource)
	}
}

// ── handlePriorityEvent / handleEmergencyEvent ────────────────────────────

func TestHandlePriorityEvent_WrongStation_Ignored(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	// Should not panic and should take no action for a different station ID
	payload := events.Payload{
		"station_id": "different-station",
		"source_id":  uuid.NewString(),
		"priority":   models.PriorityAutomation,
	}
	ex.handlePriorityEvent(payload)
}

func TestHandlePriorityEvent_MissingSourceID_Ignored(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	payload := events.Payload{
		"station_id": stationID,
		// source_id intentionally missing
	}
	// Should not panic
	ex.handlePriorityEvent(payload)
}

func TestHandleEmergencyEvent_WrongStation_Ignored(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	payload := events.Payload{
		"station_id": "wrong-station",
		"source_id":  uuid.NewString(),
		"path":       "/some/path.mp3",
	}
	ex.handleEmergencyEvent(payload)
}

func TestHandleEmergencyEvent_MissingFields_Ignored(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	// source_id and path both missing
	payload := events.Payload{
		"station_id": stationID,
	}
	ex.handleEmergencyEvent(payload)
}

// ── models.ExecutorState helpers ─────────────────────────────────────────

func TestExecutorStateIsHealthy(t *testing.T) {
	healthy := &models.ExecutorState{LastHeartbeat: time.Now()}
	if !healthy.IsHealthy() {
		t.Error("IsHealthy should return true when heartbeat is recent")
	}

	stale := &models.ExecutorState{LastHeartbeat: time.Now().Add(-30 * time.Second)}
	if stale.IsHealthy() {
		t.Error("IsHealthy should return false when heartbeat is stale (>10s)")
	}
}

func TestExecutorStateIsPlaying(t *testing.T) {
	playing := []models.ExecutorStateEnum{
		models.ExecutorStatePlaying,
		models.ExecutorStateFading,
		models.ExecutorStateLive,
		models.ExecutorStateEmergency,
	}
	notPlaying := []models.ExecutorStateEnum{
		models.ExecutorStateIdle,
		models.ExecutorStatePreloading,
	}

	for _, s := range playing {
		es := &models.ExecutorState{State: s}
		if !es.IsPlaying() {
			t.Errorf("IsPlaying(%q) should be true", s)
		}
	}
	for _, s := range notPlaying {
		es := &models.ExecutorState{State: s}
		if es.IsPlaying() {
			t.Errorf("IsPlaying(%q) should be false", s)
		}
	}
}

// ── Distributor CalculateChurn edge cases ─────────────────────────────────

func TestDistributor_CalculateChurnUnknownOp(t *testing.T) {
	dist := NewDistributor(100)
	dist.AddInstance("i1")

	// Unknown operation should return 0
	churn := dist.CalculateChurn([]string{"s1", "s2"}, "unknown", "i2")
	if churn != 0 {
		t.Errorf("unknown operation should return 0 churn, got %f", churn)
	}
}

func TestDistributor_CalculateChurnEmptyStations(t *testing.T) {
	dist := NewDistributor(100)
	dist.AddInstance("i1")

	churn := dist.CalculateChurn(nil, "add", "i2")
	if churn != 0 {
		t.Errorf("empty station list should return 0 churn, got %f", churn)
	}
}

func TestDistributor_DefaultVirtualNodes(t *testing.T) {
	// virtualNodes <= 0 defaults to 500
	dist := NewDistributor(0)
	if dist.virtualNodes != 500 {
		t.Errorf("virtualNodes = %d, want 500", dist.virtualNodes)
	}

	dist2 := NewDistributor(-1)
	if dist2.virtualNodes != 500 {
		t.Errorf("virtualNodes = %d, want 500", dist2.virtualNodes)
	}
}

func TestDistributor_GetAllAssignments_NoInstances(t *testing.T) {
	dist := NewDistributor(100)

	// No instances — GetInstance will fail for all stations
	assignments := dist.GetAllAssignments([]string{"s1", "s2", "s3"})
	if len(assignments) != 0 {
		t.Errorf("expected 0 assignments with no instances, got %d", len(assignments))
	}
}

// ── hashKey ───────────────────────────────────────────────────────────────

func TestHashKey_Deterministic(t *testing.T) {
	k1 := hashKey("test-key")
	k2 := hashKey("test-key")
	if k1 != k2 {
		t.Errorf("hashKey not deterministic: %d != %d", k1, k2)
	}
}

func TestHashKey_DifferentInputsDifferentHashes(t *testing.T) {
	h1 := hashKey("instance-1:0:vnode")
	h2 := hashKey("instance-2:0:vnode")
	if h1 == h2 {
		t.Error("different keys should have different hashes (collision possible but unlikely)")
	}
}

func TestHashKeyConsistent_Deterministic(t *testing.T) {
	k1 := hashKeyConsistent("station-abc")
	k2 := hashKeyConsistent("station-abc")
	if k1 != k2 {
		t.Errorf("hashKeyConsistent not deterministic: %d != %d", k1, k2)
	}
}

// ── CompleteFade ─────────────────────────────────────────────────────────

func TestExecutorCompleteFade_NotInFadingState(t *testing.T) {
	sm, db := newTestStateManager(t)
	bus := events.NewBus()
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, nil, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ex.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ex.Stop() //nolint:errcheck

	// State is idle — CompleteFade should fail
	err := ex.CompleteFade(ctx)
	if err == nil {
		t.Error("CompleteFade while not fading should return error")
	}
}

func TestExecutorCompleteFade_FromFadingState(t *testing.T) {
	db := newTestDBWithPriority(t)
	sm := NewStateManager(db, zerolog.Nop())
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, zerolog.Nop())
	stationID := uuid.NewString()
	ex := New(stationID, db, sm, prioritySvc, bus, nil, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ex.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ex.Stop() //nolint:errcheck

	// Get to Playing first, then Fading
	if err := ex.Play(ctx, uuid.NewString(), models.PriorityAutomation); err != nil {
		t.Fatalf("Play: %v", err)
	}

	nextSource := uuid.NewString()
	if err := ex.Fade(ctx, nextSource, models.PriorityAutomation); err != nil {
		t.Fatalf("Fade: %v", err)
	}

	// Seed an active priority source so GetCurrent succeeds
	src := models.PrioritySource{
		ID:         uuid.NewString(),
		StationID:  stationID,
		Priority:   models.PriorityAutomation,
		SourceType: models.SourceTypeMedia,
		SourceID:   nextSource,
		Active:     true,
	}
	if err := db.Create(&src).Error; err != nil {
		t.Fatalf("seed priority source: %v", err)
	}

	if err := ex.CompleteFade(ctx); err != nil {
		t.Fatalf("CompleteFade: %v", err)
	}

	state, _ := sm.GetState(ctx, stationID)
	if state.State != models.ExecutorStatePlaying {
		t.Errorf("state after CompleteFade = %q, want playing", state.State)
	}
	if state.CurrentSourceID != nextSource {
		t.Errorf("current_source_id after CompleteFade = %q, want %q", state.CurrentSourceID, nextSource)
	}
	if state.NextSourceID != "" {
		t.Errorf("next_source_id after CompleteFade should be empty, got %q", state.NextSourceID)
	}
}

// ── Pool functions ────────────────────────────────────────────────────────

// newTestPool creates a Pool with in-memory state for testing (no real gRPC).
func newTestPool(t *testing.T, instanceID string) *Pool {
	t.Helper()
	db := newTestDB(t)
	sm := NewStateManager(db, zerolog.Nop())
	bus := events.NewBus()
	ring := newConsistentHashRing(50)
	ring.addNode(instanceID)
	return &Pool{
		instanceID:   instanceID,
		db:           db,
		stateManager: sm,
		bus:          bus,
		logger:       zerolog.Nop(),
		executors:    make(map[string]*Executor),
		instances:    []string{instanceID},
		ring:         ring,
	}
}

func TestPool_GetExecutor_NotFound(t *testing.T) {
	pool := newTestPool(t, "inst-1")

	_, err := pool.GetExecutor("nonexistent-station")
	if err != ErrExecutorNotRunning {
		t.Errorf("GetExecutor for unknown station = %v, want ErrExecutorNotRunning", err)
	}
}

func TestPool_ListExecutors_Empty(t *testing.T) {
	pool := newTestPool(t, "inst-1")

	stations := pool.ListExecutors()
	if len(stations) != 0 {
		t.Errorf("expected empty list, got %v", stations)
	}
}

func TestPool_StopExecutor_NotFound(t *testing.T) {
	pool := newTestPool(t, "inst-1")
	ctx := context.Background()

	err := pool.StopExecutor(ctx, "nonexistent")
	if err != ErrExecutorNotRunning {
		t.Errorf("StopExecutor for unknown station = %v, want ErrExecutorNotRunning", err)
	}
}

func TestPool_Stop_Empty(t *testing.T) {
	pool := newTestPool(t, "inst-1")
	ctx := context.Background()

	// Stop on empty pool should not error
	if err := pool.Stop(ctx); err != nil {
		t.Errorf("Stop on empty pool = %v, want nil", err)
	}
}

func TestPool_AddInstance_DuplicateError(t *testing.T) {
	pool := newTestPool(t, "inst-1")
	ctx := context.Background()

	// inst-1 is already in the pool
	err := pool.AddInstance(ctx, "inst-1")
	if err == nil {
		t.Error("AddInstance with duplicate ID should return error")
	}
}

func TestPool_AddInstance_NewInstance(t *testing.T) {
	pool := newTestPool(t, "inst-1")
	ctx := context.Background()

	if err := pool.AddInstance(ctx, "inst-2"); err != nil {
		t.Fatalf("AddInstance: %v", err)
	}

	if len(pool.instances) != 2 {
		t.Errorf("expected 2 instances, got %d", len(pool.instances))
	}
}

func TestPool_RemoveInstance_NotFound(t *testing.T) {
	pool := newTestPool(t, "inst-1")
	ctx := context.Background()

	err := pool.RemoveInstance(ctx, "nonexistent")
	if err == nil {
		t.Error("RemoveInstance for unknown instance should return error")
	}
}

func TestPool_RemoveInstance_Existing(t *testing.T) {
	pool := newTestPool(t, "inst-1")
	ctx := context.Background()

	pool.mu.Lock()
	pool.instances = append(pool.instances, "inst-2")
	pool.ring.addNode("inst-2")
	pool.mu.Unlock()

	if err := pool.RemoveInstance(ctx, "inst-2"); err != nil {
		t.Fatalf("RemoveInstance: %v", err)
	}

	if len(pool.instances) != 1 {
		t.Errorf("expected 1 instance after removal, got %d", len(pool.instances))
	}
}

func TestPool_GetAssignment_EmptyRing(t *testing.T) {
	pool := &Pool{
		instanceID: "inst-1",
		instances:  []string{},
		ring:       newConsistentHashRing(50),
		executors:  make(map[string]*Executor),
		logger:     zerolog.Nop(),
	}

	_, err := pool.GetAssignment("some-station")
	if err == nil {
		t.Error("GetAssignment with empty ring should return error")
	}
}

func TestPool_Stop_WithRunningExecutors(t *testing.T) {
	pool := newTestPool(t, "inst-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Manually create and start an executor, add it to pool
	stationID := uuid.NewString()
	ex := New(stationID, pool.db, pool.stateManager, nil, pool.bus, nil, zerolog.Nop())
	if err := ex.Start(ctx); err != nil {
		t.Fatalf("Start executor: %v", err)
	}

	pool.mu.Lock()
	pool.executors[stationID] = ex
	pool.mu.Unlock()

	stations := pool.ListExecutors()
	if len(stations) != 1 {
		t.Fatalf("expected 1 executor, got %d", len(stations))
	}

	if err := pool.Stop(ctx); err != nil {
		t.Fatalf("Pool.Stop: %v", err)
	}

	remaining := pool.ListExecutors()
	if len(remaining) != 0 {
		t.Errorf("expected 0 executors after Pool.Stop, got %d", len(remaining))
	}
}

func TestPool_StopExecutor_Existing(t *testing.T) {
	pool := newTestPool(t, "inst-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stationID := uuid.NewString()
	ex := New(stationID, pool.db, pool.stateManager, nil, pool.bus, nil, zerolog.Nop())
	if err := ex.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	pool.mu.Lock()
	pool.executors[stationID] = ex
	pool.mu.Unlock()

	if err := pool.StopExecutor(ctx, stationID); err != nil {
		t.Fatalf("StopExecutor: %v", err)
	}

	if _, err := pool.GetExecutor(stationID); err != ErrExecutorNotRunning {
		t.Error("executor should be gone after StopExecutor")
	}
}

func TestPool_GetExecutor_Existing(t *testing.T) {
	pool := newTestPool(t, "inst-1")

	stationID := uuid.NewString()
	ex := &Executor{stationID: stationID}
	pool.mu.Lock()
	pool.executors[stationID] = ex
	pool.mu.Unlock()

	got, err := pool.GetExecutor(stationID)
	if err != nil {
		t.Fatalf("GetExecutor: %v", err)
	}
	if got != ex {
		t.Error("GetExecutor should return the same executor pointer")
	}
}

func TestPool_ListExecutors_Multiple(t *testing.T) {
	pool := newTestPool(t, "inst-1")

	pool.mu.Lock()
	for i := 0; i < 3; i++ {
		id := uuid.NewString()
		pool.executors[id] = &Executor{stationID: id}
	}
	pool.mu.Unlock()

	stations := pool.ListExecutors()
	if len(stations) != 3 {
		t.Errorf("expected 3 executors, got %d", len(stations))
	}
}

// ── MediaController ───────────────────────────────────────────────────────

func TestNewMediaController_CreatesInstance(t *testing.T) {
	mc := NewMediaController(nil, "station-1", "mount-1", zerolog.Nop())
	if mc == nil {
		t.Fatal("NewMediaController returned nil")
	}
	if mc.stationID != "station-1" {
		t.Errorf("stationID = %q, want station-1", mc.stationID)
	}
	if mc.mountID != "mount-1" {
		t.Errorf("mountID = %q, want mount-1", mc.mountID)
	}
}

func TestMediaController_FieldsSet(t *testing.T) {
	mc := NewMediaController(nil, "station-x", "mount-x", zerolog.Nop())
	if mc.stationID != "station-x" {
		t.Errorf("stationID = %q, want station-x", mc.stationID)
	}
	if mc.mountID != "mount-x" {
		t.Errorf("mountID = %q, want mount-x", mc.mountID)
	}
	if mc.client != nil {
		t.Error("client should be nil")
	}
}

// ── Pool.NewPool ──────────────────────────────────────────────────────────

func TestNewPool_Creation(t *testing.T) {
	db := newTestDB(t)
	sm := NewStateManager(db, zerolog.Nop())
	bus := events.NewBus()

	pool := NewPool("inst-1", db, sm, nil, bus, nil, zerolog.Nop())
	if pool == nil {
		t.Fatal("NewPool returned nil")
	}
	if pool.instanceID != "inst-1" {
		t.Errorf("instanceID = %q, want inst-1", pool.instanceID)
	}
	if len(pool.instances) != 1 {
		t.Errorf("expected 1 instance, got %d", len(pool.instances))
	}
	if len(pool.executors) != 0 {
		t.Errorf("expected 0 executors, got %d", len(pool.executors))
	}
	// The ring should have the instance added
	node, ok := pool.ring.getNode("any-station")
	if !ok || node != "inst-1" {
		t.Errorf("ring should assign to inst-1, got %q (ok=%v)", node, ok)
	}
}

// ── Pool.rebalanceExecutors ───────────────────────────────────────────────

func TestPool_RebalanceExecutors_StopsEvictedExecutors(t *testing.T) {
	pool := newTestPool(t, "inst-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Add inst-2 to pool ring so some stations get reassigned away from inst-1
	pool.mu.Lock()
	pool.instances = append(pool.instances, "inst-2")
	pool.ring.addNode("inst-2")
	pool.mu.Unlock()

	// Find a station that is now assigned to inst-2 (not inst-1)
	var evictedStation string
	for i := 0; i < 200; i++ {
		id := uuid.NewString()
		assignedTo, ok := pool.ring.getNode(id)
		if ok && assignedTo != "inst-1" {
			evictedStation = id
			break
		}
	}
	if evictedStation == "" {
		t.Skip("could not find station assigned to inst-2 — skipping rebalance test")
	}

	// Manually place a running executor for that station in inst-1's pool
	ex := New(evictedStation, pool.db, pool.stateManager, nil, pool.bus, nil, zerolog.Nop())
	if err := ex.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	pool.mu.Lock()
	pool.executors[evictedStation] = ex
	err := pool.rebalanceExecutors(ctx)
	pool.mu.Unlock()

	if err != nil {
		t.Fatalf("rebalanceExecutors: %v", err)
	}

	pool.mu.RLock()
	_, stillRunning := pool.executors[evictedStation]
	pool.mu.RUnlock()

	if stillRunning {
		t.Error("executor should have been stopped during rebalance when assigned to different instance")
	}
}

// ── StateManager UpdateState without pre-cached state ────────────────────

func TestStateManager_UpdateState_LoadsFromDB(t *testing.T) {
	sm, db := newTestStateManager(t)
	ctx := context.Background()
	stationID := uuid.NewString()

	// Create state via GetState (which caches it)
	if _, err := sm.GetState(ctx, stationID); err != nil {
		t.Fatalf("GetState: %v", err)
	}

	// Clear the cache to force DB reload
	sm.ClearCache(stationID)

	// UpdateState should now load from DB
	err := sm.UpdateState(ctx, stationID, func(s *models.ExecutorState) {
		s.State = models.ExecutorStateLive
	})
	if err != nil {
		t.Fatalf("UpdateState after cache clear: %v", err)
	}

	// Reload and check
	var loaded models.ExecutorState
	if err := db.Where("station_id = ?", stationID).First(&loaded).Error; err != nil {
		t.Fatalf("reload from DB: %v", err)
	}
	if loaded.State != models.ExecutorStateLive {
		t.Errorf("state = %q, want live", loaded.State)
	}
}
