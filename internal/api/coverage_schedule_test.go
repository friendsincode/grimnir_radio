/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// Tests for handleScheduleList, handleScheduleRefresh, handleSmartBlockMaterialize,
// and handleClockSimulate — specifically testing the DB-backed paths and validation
// branches that don't require a real scheduler/planner to be running.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/clock"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/scheduler"
	schedulerState "github.com/friendsincode/grimnir_radio/internal/scheduler/state"
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
)

// newScheduleTestAPI sets up an API with a real scheduler.Service backed by SQLite.
// The scheduler has nil planner/engine so only DB-only methods (Upcoming) work safely.
func newScheduleTestAPI(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	// Use in-memory SQLite with a unique name per test to prevent cross-test contamination.
	// cache=shared with a single max connection ensures all GORM contexts see the same data.
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// Single connection ensures all contexts share the same SQLite in-memory database.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.Station{},
		&models.Mount{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.ScheduleEntry{},
		&models.SmartBlock{},
		&models.MediaItem{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	planner := clock.NewPlanner(db, zerolog.Nop())
	engine := smartblock.New(db, zerolog.Nop())
	stateStore := &schedulerState.Store{}
	svc := scheduler.New(db, planner, engine, stateStore, 24*time.Hour, zerolog.Nop())

	a := &API{
		db:        db,
		bus:       events.NewBus(),
		scheduler: svc,
		logger:    zerolog.Nop(),
	}
	return a, db
}

// TestHandleScheduleList_MissingStation verifies 400 is returned when no station_id given.
func TestHandleScheduleList_MissingStation(t *testing.T) {
	a, _ := newScheduleTestAPI(t)

	req := httptest.NewRequest("GET", "/schedule", nil)
	rr := httptest.NewRecorder()
	a.handleScheduleList(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}
}

// TestHandleScheduleList_EmptyReturnsArray verifies that when no entries exist the
// response is a JSON array (not null) so clients can range over it safely.
func TestHandleScheduleList_EmptyReturnsArray(t *testing.T) {
	a, _ := newScheduleTestAPI(t)

	req := httptest.NewRequest("GET", "/?station_id=st-empty", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleScheduleList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("empty list: got %d, want 200", rr.Code)
	}

	var entries []any
	if err := json.NewDecoder(rr.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Must be an array — never null.
	if entries == nil {
		t.Fatal("expected non-nil array, got null")
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

// TestHandleScheduleList_DateFilterReturnsOnlyMatchingEntries verifies that entries
// outside the queried horizon are not returned.
func TestHandleScheduleList_DateFilterReturnsOnlyMatchingEntries(t *testing.T) {
	a, db := newScheduleTestAPI(t)

	// Use local time (not UTC) to match the timezone the handler uses for time.Now()
	// so SQLite string comparisons work correctly.
	now := time.Now()

	// Build the request first so test data is written with the same context
	// the handler will use when querying — required for SQLite single-connection
	// visibility.
	req := httptest.NewRequest("GET", "/?station_id=st-filter", nil)
	req = withAdminClaims(req)
	ctx := req.Context()

	// Entry within the next 6 hours (default horizon) — should be returned.
	entryIn := models.ScheduleEntry{
		ID:         "sched-in-range",
		StationID:  "st-filter",
		StartsAt:   now.Add(2 * time.Hour),
		EndsAt:     now.Add(3 * time.Hour),
		SourceType: "smart_block",
		Metadata:   map[string]any{},
	}
	// Entry that starts in 24 hours — outside the default 6h window, should NOT appear.
	entryOut := models.ScheduleEntry{
		ID:         "sched-out-of-range",
		StationID:  "st-filter",
		StartsAt:   now.Add(24 * time.Hour),
		EndsAt:     now.Add(25 * time.Hour),
		SourceType: "smart_block",
		Metadata:   map[string]any{},
	}
	db.WithContext(ctx).Create(&entryIn)  //nolint:errcheck
	db.WithContext(ctx).Create(&entryOut) //nolint:errcheck
	rr := httptest.NewRecorder()
	a.handleScheduleList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var entries []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry in default 6h window, got %d", len(entries))
	}
	if entries[0]["ID"] != "sched-in-range" {
		t.Fatalf("expected sched-in-range, got %v", entries[0]["ID"])
	}
}

// TestHandleScheduleList_StationScopedDoesNotLeakOtherStation verifies that entries
// from a different station are not returned.
func TestHandleScheduleList_StationScopedDoesNotLeakOtherStation(t *testing.T) {
	a, db := newScheduleTestAPI(t)

	// Use local time to match the handler's time.Now() for SQLite string comparisons.
	now := time.Now()

	// Entry for station A.
	entryA := models.ScheduleEntry{
		ID:         "sched-station-a",
		StationID:  "st-a",
		StartsAt:   now.Add(time.Hour),
		EndsAt:     now.Add(2 * time.Hour),
		SourceType: "smart_block",
		Metadata:   map[string]any{},
	}
	// Entry for station B.
	entryB := models.ScheduleEntry{
		ID:         "sched-station-b",
		StationID:  "st-b",
		StartsAt:   now.Add(time.Hour),
		EndsAt:     now.Add(2 * time.Hour),
		SourceType: "smart_block",
		Metadata:   map[string]any{},
	}
	// Build request before writes so SQLite context visibility is consistent.
	req := httptest.NewRequest("GET", "/?station_id=st-a", nil)
	req = withAdminClaims(req)
	ctx := req.Context()
	db.WithContext(ctx).Create(&entryA) //nolint:errcheck
	db.WithContext(ctx).Create(&entryB) //nolint:errcheck

	// Request station A's schedule — station B's entry must not appear.
	rr := httptest.NewRecorder()
	a.handleScheduleList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}

	var entries []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry for st-a, got %d", len(entries))
	}
	if entries[0]["StationID"] != "st-a" {
		t.Fatalf("leaked entry from wrong station: %v", entries[0]["StationID"])
	}
}

// TestHandleScheduleList_CustomHoursHorizon verifies that the ?hours param is honored.
func TestHandleScheduleList_CustomHoursHorizon(t *testing.T) {
	a, db := newScheduleTestAPI(t)

	// Use local time to match the handler's time.Now() for SQLite string comparisons.
	now := time.Now()

	// Build the 3h request first (the one that will see the entry) so we can
	// seed the DB using that context, ensuring SQLite visibility.
	req3 := httptest.NewRequest("GET", "/?station_id=st-horizon&hours=3", nil)
	req3 = withAdminClaims(req3)
	ctx := req3.Context()

	// Entry at +2h (within 1h window? No. Within 3h window? Yes).
	entry := models.ScheduleEntry{
		ID:         "sched-2h",
		StationID:  "st-horizon",
		StartsAt:   now.Add(2 * time.Hour),
		EndsAt:     now.Add(3 * time.Hour),
		SourceType: "smart_block",
		Metadata:   map[string]any{},
	}
	db.WithContext(ctx).Create(&entry) //nolint:errcheck

	// 1-hour horizon — entry should NOT appear.
	req1 := httptest.NewRequest("GET", "/?station_id=st-horizon&hours=1", nil)
	req1 = withAdminClaims(req1)
	rr := httptest.NewRecorder()
	a.handleScheduleList(rr, req1)

	var entries []map[string]any
	json.NewDecoder(rr.Body).Decode(&entries) //nolint:errcheck
	if len(entries) != 0 {
		t.Fatalf("1h window: expected 0 entries, got %d", len(entries))
	}

	// 3-hour horizon — entry SHOULD appear.
	rr = httptest.NewRecorder()
	a.handleScheduleList(rr, req3)

	json.NewDecoder(rr.Body).Decode(&entries) //nolint:errcheck
	if len(entries) != 1 {
		t.Fatalf("3h window: expected 1 entry, got %d", len(entries))
	}
}

// TestHandleScheduleRefresh_MissingStation verifies 400 for missing station_id.
func TestHandleScheduleRefresh_MissingStation(t *testing.T) {
	a, _ := newScheduleTestAPI(t)

	req := httptest.NewRequest("POST", "/schedule/refresh", bytes.NewReader([]byte(`{}`)))
	rr := httptest.NewRecorder()
	a.handleScheduleRefresh(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}
}

// TestHandleScheduleRefresh_InvalidJSON verifies 400 for malformed body.
func TestHandleScheduleRefresh_InvalidJSON(t *testing.T) {
	a, _ := newScheduleTestAPI(t)

	req := httptest.NewRequest("POST", "/schedule/refresh", bytes.NewReader([]byte(`not-json`)))
	rr := httptest.NewRecorder()
	a.handleScheduleRefresh(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// TestHandleScheduleRefresh_UnauthorizedWithoutClaims verifies that a valid JSON body
// with station_id but no auth claims returns 401.
func TestHandleScheduleRefresh_UnauthorizedWithoutClaims(t *testing.T) {
	a, _ := newScheduleTestAPI(t)

	req := httptest.NewRequest("POST", "/schedule/refresh",
		bytes.NewReader([]byte(`{"station_id":"st-x"}`)))
	// No auth claims attached.
	rr := httptest.NewRecorder()
	a.handleScheduleRefresh(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: got %d, want 401", rr.Code)
	}
}

// TestHandleSmartBlockMaterialize_NotFound verifies 404 when blockID doesn't exist in DB.
// Because the handler loads the block through the scheduler.Materialize → engine.Generate path,
// and our test engine is backed by the same SQLite DB, a missing block returns an error
// that maps to 409 (ErrUnresolved) or 500. We test the pre-materialization DB lookup
// by checking that an unknown blockID after station auth succeeds returns a non-200.
func TestHandleSmartBlockMaterialize_NotFound(t *testing.T) {
	a, _ := newScheduleTestAPI(t)

	body, _ := json.Marshal(map[string]any{
		"station_id":  "st-mat",
		"duration_ms": 60000,
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "blockID", "nonexistent-block-id")
	rr := httptest.NewRecorder()
	a.handleSmartBlockMaterialize(rr, req)

	// Should not be 200 — block doesn't exist so engine will return an error.
	if rr.Code == http.StatusOK {
		t.Fatalf("expected error for nonexistent block, got 200")
	}
}

// TestHandleSmartBlockMaterialize_MissingStation verifies 400 when station_id is absent.
func TestHandleSmartBlockMaterialize_MissingStation(t *testing.T) {
	a, _ := newScheduleTestAPI(t)

	body, _ := json.Marshal(map[string]any{"duration_ms": 60000})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "blockID", "some-block")
	rr := httptest.NewRecorder()
	a.handleSmartBlockMaterialize(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}
}

// TestHandleSmartBlockMaterialize_InvalidJSON verifies 400 for malformed body.
func TestHandleSmartBlockMaterialize_InvalidJSON(t *testing.T) {
	a, _ := newScheduleTestAPI(t)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(`bad json`)))
	req = withAdminClaims(req)
	req = withChiParam(req, "blockID", "some-block")
	rr := httptest.NewRecorder()
	a.handleSmartBlockMaterialize(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// TestHandleSmartBlockMaterialize_UnauthorizedWithoutClaims verifies 401 without auth.
func TestHandleSmartBlockMaterialize_UnauthorizedWithoutClaims(t *testing.T) {
	a, _ := newScheduleTestAPI(t)

	body, _ := json.Marshal(map[string]any{"station_id": "st-x", "duration_ms": 60000})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	// No auth claims.
	req = withChiParam(req, "blockID", "some-block")
	rr := httptest.NewRecorder()
	a.handleSmartBlockMaterialize(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: got %d, want 401", rr.Code)
	}
}

// TestHandleClockSimulate_NotFound verifies 404 when clock doesn't exist.
func TestHandleClockSimulate_NotFound(t *testing.T) {
	a, _ := newScheduleTestAPI(t)

	req := httptest.NewRequest("GET", "/", nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "clockID", "missing-clock-id")
	rr := httptest.NewRecorder()
	a.handleClockSimulate(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("nonexistent clock: got %d, want 404", rr.Code)
	}
}

// TestHandleClockSimulate_EmptyClock verifies that a clock with no slots simulates
// without error and returns a response (empty plan list).
func TestHandleClockSimulate_EmptyClock(t *testing.T) {
	a, db := newScheduleTestAPI(t)

	clock := models.ClockHour{
		ID:        "clock-empty-1",
		StationID: "st-sim",
		Name:      "Empty Clock",
		StartHour: 0,
		EndHour:   24,
		Slots:     []models.ClockSlot{},
	}
	db.Create(&clock) //nolint:errcheck

	req := httptest.NewRequest("GET", "/", nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "clockID", "clock-empty-1")
	rr := httptest.NewRecorder()
	a.handleClockSimulate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("empty clock simulate: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// Body should be a JSON array (may be empty for a clock with no slots).
	var result any
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode simulate response: %v", err)
	}
}

// TestHandleClockSimulate_UnauthorizedWithoutClaims verifies 401 for existing clock without auth.
func TestHandleClockSimulate_UnauthorizedWithoutClaims(t *testing.T) {
	a, db := newScheduleTestAPI(t)

	ch := models.ClockHour{
		ID:        "clock-auth-check",
		StationID: "st-auth",
		Name:      "Auth Check Clock",
		StartHour: 0,
		EndHour:   24,
		Slots:     []models.ClockSlot{},
	}
	db.Create(&ch) //nolint:errcheck

	req := httptest.NewRequest("GET", "/", nil)
	// No auth claims attached.
	req = withChiParam(req, "clockID", "clock-auth-check")
	rr := httptest.NewRecorder()
	a.handleClockSimulate(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: got %d, want 401", rr.Code)
	}
}
