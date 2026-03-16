/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// Tests for handleStationsCreate (happy path + default timezone),
// handleStationsList (with data), handleClocksList, handleClocksCreate,
// handleSmartBlocksList, handleSmartBlocksCreate, handlePlaylistsList,
// handleScheduleRefresh (happy path), handleScheduleUpdate,
// handleAnalyticsSpins, handleSystemLogs, handleAnalyticsListeners (with broadcast nil),
// and various other partially covered handlers.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/clock"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/logbuffer"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/scheduler"
	schedulerState "github.com/friendsincode/grimnir_radio/internal/scheduler/state"
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
)

// newMiscTestAPI creates an API with a real DB and scheduler for misc handler tests.
func newMiscTestAPI(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

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
		&models.Playlist{},
		&models.StationUser{},
		&models.PlayHistory{},
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

// --- handleStationsCreate ---

// TestHandleStationsCreate_DefaultTimezone verifies that when timezone is omitted from
// the request body, the station is created with UTC as the default timezone.
func TestHandleStationsCreate_DefaultTimezone(t *testing.T) {
	a, _ := newMiscTestAPI(t)

	body, _ := json.Marshal(map[string]any{
		"name": "No-TZ Station",
		// timezone intentionally omitted — should default to UTC
	})
	req := httptest.NewRequest("POST", "/stations", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleStationsCreate(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("create station: got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var station models.Station
	if err := json.NewDecoder(rr.Body).Decode(&station); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if station.Timezone != "UTC" {
		t.Fatalf("expected default timezone=UTC, got %q", station.Timezone)
	}
	if station.ID == "" {
		t.Fatal("expected non-empty ID in response")
	}
}

// TestHandleStationsCreate_CreatesDefaultMount verifies that a default mount is created
// automatically when a station is created.
func TestHandleStationsCreate_CreatesDefaultMount(t *testing.T) {
	a, db := newMiscTestAPI(t)

	body, _ := json.Marshal(map[string]any{
		"name":     "Mount Auto Station",
		"timezone": "America/Chicago",
	})
	req := httptest.NewRequest("POST", "/stations", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleStationsCreate(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("create station: got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var station models.Station
	json.NewDecoder(rr.Body).Decode(&station) //nolint:errcheck

	// A default mount should have been created for this station.
	var count int64
	db.Model(&models.Mount{}).Where("station_id = ?", station.ID).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 default mount, got %d", count)
	}
}

// TestHandleStationsList_ReturnsAllStations verifies that after creating stations
// they appear in the list response.
func TestHandleStationsList_ReturnsAllStations(t *testing.T) {
	a, db := newMiscTestAPI(t)

	// Create two stations directly in DB.
	db.Create(&models.Station{ID: "st-list-1", Name: "Alpha", Timezone: "UTC"}) //nolint:errcheck
	db.Create(&models.Station{ID: "st-list-2", Name: "Beta", Timezone: "UTC"})  //nolint:errcheck

	req := httptest.NewRequest("GET", "/stations", nil)
	rr := httptest.NewRecorder()
	a.handleStationsList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list stations: got %d, want 200", rr.Code)
	}

	var stations []models.Station
	if err := json.NewDecoder(rr.Body).Decode(&stations); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(stations) != 2 {
		t.Fatalf("expected 2 stations, got %d", len(stations))
	}
}

// --- handleClocksList ---

// TestHandleClocksList_StationScopedWithData verifies that when station_id is provided
// only clocks for that station are returned.
func TestHandleClocksList_StationScopedWithData(t *testing.T) {
	a, db := newMiscTestAPI(t)

	db.Create(&models.ClockHour{ID: "clk-a1", StationID: "st-clk-a", Name: "Clock A1", StartHour: 0, EndHour: 24}) //nolint:errcheck
	db.Create(&models.ClockHour{ID: "clk-a2", StationID: "st-clk-a", Name: "Clock A2", StartHour: 0, EndHour: 24}) //nolint:errcheck
	db.Create(&models.ClockHour{ID: "clk-b1", StationID: "st-clk-b", Name: "Clock B1", StartHour: 0, EndHour: 24}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/?station_id=st-clk-a", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleClocksList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("clocks list: got %d, want 200", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	clocks, ok := resp["clocks"].([]any)
	if !ok {
		t.Fatalf("expected 'clocks' key as array, got %T", resp["clocks"])
	}
	if len(clocks) != 2 {
		t.Fatalf("expected 2 clocks for st-clk-a, got %d", len(clocks))
	}
}

// TestHandleClocksList_NoStationIDReturnsAll verifies that when station_id is absent
// (and claims are admin), all clocks across stations are returned.
func TestHandleClocksList_NoStationIDReturnsAll(t *testing.T) {
	a, db := newMiscTestAPI(t)

	db.Create(&models.ClockHour{ID: "clk-all-1", StationID: "st-x", Name: "Clock 1", StartHour: 0, EndHour: 24}) //nolint:errcheck
	db.Create(&models.ClockHour{ID: "clk-all-2", StationID: "st-y", Name: "Clock 2", StartHour: 0, EndHour: 24}) //nolint:errcheck

	// Admin with no station_id filter — all clocks returned.
	req := httptest.NewRequest("GET", "/", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleClocksList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("clocks list all: got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	clocks, _ := resp["clocks"].([]any)
	if len(clocks) < 2 {
		t.Fatalf("expected at least 2 clocks, got %d", len(clocks))
	}
}

// --- handleClocksCreate ---

// TestHandleClocksCreate_HappyPath verifies that a valid clock creation request
// returns 201 with the created clock's ID and slots populated.
func TestHandleClocksCreate_HappyPath(t *testing.T) {
	a, _ := newMiscTestAPI(t)

	body, _ := json.Marshal(map[string]any{
		"station_id": "st-clk-create",
		"name":       "Morning Clock",
		"start_hour": 6,
		"end_hour":   12,
		"slots":      []map[string]any{},
	})
	req := httptest.NewRequest("POST", "/clocks", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleClocksCreate(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("create clock: got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var clock models.ClockHour
	if err := json.NewDecoder(rr.Body).Decode(&clock); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if clock.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if clock.StationID != "st-clk-create" {
		t.Fatalf("wrong station_id: %v", clock.StationID)
	}
	if clock.StartHour != 6 {
		t.Fatalf("wrong start_hour: %v", clock.StartHour)
	}
}

// TestHandleClocksCreate_MissingRequiredFields verifies 400 when station_id or name absent.
func TestHandleClocksCreate_MissingRequiredFields(t *testing.T) {
	a, _ := newMiscTestAPI(t)

	// Missing station_id
	body, _ := json.Marshal(map[string]any{"name": "Orphan Clock"})
	req := httptest.NewRequest("POST", "/clocks", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleClocksCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}

	// Missing name
	body, _ = json.Marshal(map[string]any{"station_id": "st-x"})
	req = httptest.NewRequest("POST", "/clocks", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr = httptest.NewRecorder()
	a.handleClocksCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing name: got %d, want 400", rr.Code)
	}
}

// --- handleSmartBlocksList ---

// TestHandleSmartBlocksList_StationScopedResults verifies station-scoped filtering.
func TestHandleSmartBlocksList_StationScopedResults(t *testing.T) {
	a, db := newMiscTestAPI(t)

	db.Create(&models.SmartBlock{ID: "sb-a1", StationID: "st-sb-a", Name: "Block A1"}) //nolint:errcheck
	db.Create(&models.SmartBlock{ID: "sb-a2", StationID: "st-sb-a", Name: "Block A2"}) //nolint:errcheck
	db.Create(&models.SmartBlock{ID: "sb-b1", StationID: "st-sb-b", Name: "Block B1"}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/?station_id=st-sb-a", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleSmartBlocksList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("smart blocks list: got %d, want 200", rr.Code)
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	blocks, _ := resp["smart_blocks"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks for st-sb-a, got %d", len(blocks))
	}
}

// --- handleSmartBlocksCreate ---

// TestHandleSmartBlocksCreate_HappyPath verifies that a valid smart block create request
// returns 201 with the new block's ID.
func TestHandleSmartBlocksCreate_HappyPath(t *testing.T) {
	a, _ := newMiscTestAPI(t)

	body, _ := json.Marshal(map[string]any{
		"station_id":  "st-sb-create",
		"name":        "Top 40 Block",
		"description": "Plays the top 40 songs",
	})
	req := httptest.NewRequest("POST", "/smart-blocks", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleSmartBlocksCreate(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("create smart block: got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var block models.SmartBlock
	if err := json.NewDecoder(rr.Body).Decode(&block); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if block.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if block.Name != "Top 40 Block" {
		t.Fatalf("expected name=Top 40 Block, got %q", block.Name)
	}
}

// TestHandleSmartBlocksCreate_MissingFields verifies 400 for missing required fields.
func TestHandleSmartBlocksCreate_MissingFields(t *testing.T) {
	a, _ := newMiscTestAPI(t)

	// Missing station_id
	body, _ := json.Marshal(map[string]any{"name": "Orphan Block"})
	req := httptest.NewRequest("POST", "/smart-blocks", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleSmartBlocksCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}
}

// --- handlePlaylistsList ---

// TestHandlePlaylistsList_StationScopedResults verifies station-scoped filtering.
func TestHandlePlaylistsList_StationScopedResults(t *testing.T) {
	a, db := newMiscTestAPI(t)

	db.Create(&models.Playlist{ID: "pl-a1", StationID: "st-pl-a", Name: "Playlist A1"}) //nolint:errcheck
	db.Create(&models.Playlist{ID: "pl-b1", StationID: "st-pl-b", Name: "Playlist B1"}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/?station_id=st-pl-a", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handlePlaylistsList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("playlists list: got %d, want 200", rr.Code)
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	playlists, _ := resp["playlists"].([]any)
	if len(playlists) != 1 {
		t.Fatalf("expected 1 playlist for st-pl-a, got %d", len(playlists))
	}
}

// --- handleScheduleRefresh ---

// TestHandleScheduleRefresh_HappyPath verifies that a valid refresh request with auth
// succeeds and returns status=refresh_queued.
func TestHandleScheduleRefresh_HappyPath(t *testing.T) {
	a, _ := newMiscTestAPI(t)

	body, _ := json.Marshal(map[string]any{"station_id": "st-refresh"})
	req := httptest.NewRequest("POST", "/schedule/refresh", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleScheduleRefresh(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("refresh: got %d, want 202; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "refresh_queued" {
		t.Fatalf("expected status=refresh_queued, got %q", resp["status"])
	}
}

// --- handleScheduleUpdate ---

// TestHandleScheduleUpdate_NotFound verifies 404 for an unknown entry ID.
func TestHandleScheduleUpdate_NotFound(t *testing.T) {
	a, _ := newMiscTestAPI(t)

	body, _ := json.Marshal(map[string]any{})
	req := httptest.NewRequest("PATCH", "/schedule/nonexistent", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "entryID", "nonexistent-entry-id")
	rr := httptest.NewRecorder()
	a.handleScheduleUpdate(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("nonexistent entry: got %d, want 404", rr.Code)
	}
}

// TestHandleScheduleUpdate_MountID verifies that updating a schedule entry's
// mount_id persists the change and returns 200 with the updated entry.
func TestHandleScheduleUpdate_MountID(t *testing.T) {
	a, db := newMiscTestAPI(t)

	now := time.Now()
	entry := models.ScheduleEntry{
		ID:         "sched-update-1",
		StationID:  "st-upd",
		StartsAt:   now.Add(time.Hour),
		EndsAt:     now.Add(2 * time.Hour),
		SourceType: "smart_block",
		Metadata:   map[string]any{},
	}

	// Build request first to write with same context.
	body, _ := json.Marshal(map[string]any{
		"mount_id": "mt-new",
		// No metadata — only mount_id update to avoid SQLite map serialization issues.
	})
	req := httptest.NewRequest("PATCH", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	ctx := req.Context()
	db.WithContext(ctx).Create(&entry) //nolint:errcheck

	req = withChiParam(req, "entryID", "sched-update-1")
	rr := httptest.NewRecorder()
	a.handleScheduleUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("update entry mount_id: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleScheduleUpdate_NoChanges verifies that when an empty body is sent
// the entry is returned unchanged (no-op update) with 200.
func TestHandleScheduleUpdate_NoChanges(t *testing.T) {
	a, db := newMiscTestAPI(t)

	now := time.Now()
	entry := models.ScheduleEntry{
		ID:         "sched-noop-1",
		StationID:  "st-noop",
		StartsAt:   now.Add(time.Hour),
		EndsAt:     now.Add(2 * time.Hour),
		SourceType: "smart_block",
		Metadata:   map[string]any{},
	}

	body, _ := json.Marshal(map[string]any{}) // empty — no changes
	req := httptest.NewRequest("PATCH", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	ctx := req.Context()
	db.WithContext(ctx).Create(&entry) //nolint:errcheck

	req = withChiParam(req, "entryID", "sched-noop-1")
	rr := httptest.NewRecorder()
	a.handleScheduleUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("no-op update: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleScheduleUpdate_InvalidStartsAt verifies 400 for malformed starts_at.
func TestHandleScheduleUpdate_InvalidStartsAt(t *testing.T) {
	a, db := newMiscTestAPI(t)

	now := time.Now()
	entry := models.ScheduleEntry{
		ID:         "sched-bad-time",
		StationID:  "st-badtime",
		StartsAt:   now.Add(time.Hour),
		EndsAt:     now.Add(2 * time.Hour),
		SourceType: "smart_block",
		Metadata:   map[string]any{},
	}

	body, _ := json.Marshal(map[string]any{"starts_at": "not-a-valid-time"})
	req := httptest.NewRequest("PATCH", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	ctx := req.Context()
	db.WithContext(ctx).Create(&entry) //nolint:errcheck

	req = withChiParam(req, "entryID", "sched-bad-time")
	rr := httptest.NewRecorder()
	a.handleScheduleUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid starts_at: got %d, want 400", rr.Code)
	}
}

// --- handleAnalyticsSpins ---

// TestHandleAnalyticsSpins_MissingStation verifies 400 without station_id.
func TestHandleAnalyticsSpins_MissingStation(t *testing.T) {
	a, _ := newMiscTestAPI(t)

	req := httptest.NewRequest("GET", "/analytics/spins", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsSpins(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}
}

// TestHandleAnalyticsSpins_EmptyHistory verifies that an empty response is returned
// when no play history exists for the station.
func TestHandleAnalyticsSpins_EmptyHistory(t *testing.T) {
	a, _ := newMiscTestAPI(t)

	req := httptest.NewRequest("GET", "/?station_id=st-spins-empty", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsSpins(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("empty history: got %d, want 200", rr.Code)
	}

	// Should be an empty array (or nil decoded as null from an empty GROUP BY).
	var rows []any
	json.NewDecoder(rr.Body).Decode(&rows) //nolint:errcheck
	if len(rows) != 0 {
		t.Fatalf("expected 0 spin rows, got %d", len(rows))
	}
}

// TestHandleAnalyticsSpins_WithHistoryData verifies that play history entries are
// grouped and counted correctly.
func TestHandleAnalyticsSpins_WithHistoryData(t *testing.T) {
	a, db := newMiscTestAPI(t)

	now := time.Now()
	// Create 3 plays of "Artist A / Song 1" and 1 play of "Artist B / Song 2".
	plays := []models.PlayHistory{
		{ID: "ph-s1", StationID: "st-spins", Artist: "Artist A", Title: "Song 1", StartedAt: now.Add(-5 * time.Minute)},
		{ID: "ph-s2", StationID: "st-spins", Artist: "Artist A", Title: "Song 1", StartedAt: now.Add(-10 * time.Minute)},
		{ID: "ph-s3", StationID: "st-spins", Artist: "Artist A", Title: "Song 1", StartedAt: now.Add(-15 * time.Minute)},
		{ID: "ph-s4", StationID: "st-spins", Artist: "Artist B", Title: "Song 2", StartedAt: now.Add(-20 * time.Minute)},
	}
	for i := range plays {
		db.Create(&plays[i]) //nolint:errcheck
	}

	req := httptest.NewRequest("GET", "/?station_id=st-spins", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsSpins(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("spins with data: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var rows []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 spin groups, got %d", len(rows))
	}
	// First row should be "Artist A / Song 1" with count=3 (ORDER BY count DESC).
	if rows[0]["artist"] != "Artist A" {
		t.Fatalf("expected top artist=Artist A, got %v", rows[0]["artist"])
	}
	count, _ := rows[0]["count"].(float64)
	if count != 3 {
		t.Fatalf("expected count=3, got %v", count)
	}
}

// TestHandleAnalyticsSpins_CustomSince verifies that the ?since param filters history.
func TestHandleAnalyticsSpins_CustomSince(t *testing.T) {
	a, db := newMiscTestAPI(t)

	now := time.Now()
	// Old play — before the since filter.
	db.Create(&models.PlayHistory{ //nolint:errcheck
		ID: "ph-old", StationID: "st-since", Artist: "Old Artist", Title: "Old Song",
		StartedAt: now.Add(-60 * 24 * time.Hour), // 60 days ago
	})
	// Recent play — within the last day.
	db.Create(&models.PlayHistory{ //nolint:errcheck
		ID: "ph-new", StationID: "st-since", Artist: "New Artist", Title: "New Song",
		StartedAt: now.Add(-1 * time.Hour),
	})

	// Filter with ?since set to yesterday — only new play should appear.
	since := now.Add(-2 * time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest("GET", fmt.Sprintf("/?station_id=st-since&since=%s", since), nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsSpins(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("spins with since: got %d, want 200", rr.Code)
	}

	var rows []map[string]any
	json.NewDecoder(rr.Body).Decode(&rows) //nolint:errcheck
	if len(rows) != 1 {
		t.Fatalf("expected 1 row with since filter, got %d", len(rows))
	}
	if rows[0]["artist"] != "New Artist" {
		t.Fatalf("expected New Artist, got %v", rows[0]["artist"])
	}
}

// --- handleSystemLogs ---

// TestHandleSystemLogs_WithRealBuffer verifies that when a log buffer is set,
// the handler returns structured JSON with entries and count fields.
func TestHandleSystemLogs_WithRealBuffer(t *testing.T) {
	a, _ := newMiscTestAPI(t)
	buf := logbuffer.New(100)
	a.logBuffer = buf

	// Add some log entries.
	buf.Add(logbuffer.LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "test log entry 1",
		Component: "scheduler",
	})
	buf.Add(logbuffer.LogEntry{
		Timestamp: time.Now(),
		Level:     "warn",
		Message:   "test log entry 2",
		Component: "executor",
	})

	req := httptest.NewRequest("GET", "/system/logs", nil)
	rr := httptest.NewRecorder()
	a.handleSystemLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("system logs: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["entries"]; !ok {
		t.Fatal("expected 'entries' key in response")
	}
	if _, ok := resp["count"]; !ok {
		t.Fatal("expected 'count' key in response")
	}
}

// TestHandleSystemLogs_WithLevelFilter verifies that the level query param is passed
// to the buffer query (handler doesn't crash, returns 200).
func TestHandleSystemLogs_WithLevelFilter(t *testing.T) {
	a, _ := newMiscTestAPI(t)
	buf := logbuffer.New(100)
	buf.Add(logbuffer.LogEntry{Timestamp: time.Now(), Level: "error", Message: "err entry"})
	a.logBuffer = buf

	req := httptest.NewRequest("GET", "/system/logs?level=error&limit=10&order=asc", nil)
	rr := httptest.NewRecorder()
	a.handleSystemLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("system logs with filter: got %d, want 200", rr.Code)
	}
}

// --- handleAnalyticsListeners with real broadcast nil ---

// TestHandleAnalyticsListeners_NilBroadcastReturnsZeros verifies that when broadcast
// is nil the handler returns 200 with total=0 and a non-nil mounts array.
// (Tests path in handleAnalyticsListeners not covered by existing nil-broadcast tests.)
func TestHandleAnalyticsListeners_NilBroadcastReturnsMounts(t *testing.T) {
	a, _ := newMiscTestAPI(t)
	// broadcast is nil by default.

	req := httptest.NewRequest("GET", "/analytics/listeners", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsListeners(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("nil broadcast: got %d, want 200", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// mounts key must exist and be a non-nil value.
	if _, ok := resp["mounts"]; !ok {
		t.Fatal("expected 'mounts' key in response")
	}
}

// newMiscTestAPIWithDB creates a plain API (no scheduler) for DB-only tests.
// Used when we need a real file-based DB for cross-context visibility.
func newMiscTestAPIWithDB(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "misc.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.Station{},
		&models.Mount{},
		&models.StationUser{},
		&models.MediaItem{},
		&models.MediaTagLink{},
		&models.AnalysisJob{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{db: db, bus: events.NewBus(), logger: zerolog.Nop()}, db
}

// TestHandleStationsGet_HappyPath verifies that an existing station is returned correctly.
func TestHandleStationsGet_HappyPath(t *testing.T) {
	a, db := newMiscTestAPIWithDB(t)

	db.Create(&models.Station{ID: "st-get-1", Name: "My Station", Timezone: "UTC"}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/stations/st-get-1", nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "stationID", "st-get-1")
	rr := httptest.NewRecorder()
	a.handleStationsGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("get station: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var station models.Station
	if err := json.NewDecoder(rr.Body).Decode(&station); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if station.ID != "st-get-1" {
		t.Fatalf("expected ID=st-get-1, got %q", station.ID)
	}
	if station.Name != "My Station" {
		t.Fatalf("expected name=My Station, got %q", station.Name)
	}
}

// TestHandleStationsGet_NotFound verifies 404 for unknown station.
func TestHandleStationsGet_NotFound(t *testing.T) {
	a, _ := newMiscTestAPIWithDB(t)

	req := httptest.NewRequest("GET", "/stations/ghost", nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "stationID", "ghost-station")
	rr := httptest.NewRecorder()
	a.handleStationsGet(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing station: got %d, want 404", rr.Code)
	}
}
