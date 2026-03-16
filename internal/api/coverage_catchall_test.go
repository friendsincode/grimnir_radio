/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// Catch-all tests for remaining low-coverage validation paths.
// Each test hits a specific branch that was uncovered to push coverage closer to 80%.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// newCatchAllAPI creates a minimal API for validation paths not needing external services.
func newCatchAllAPI(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "catchall.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.Station{},
		&models.Mount{},
		&models.StationUser{},
		&models.SmartBlock{},
		&models.Playlist{},
		&models.Tag{},
		&models.MediaItem{},
		&models.MediaTagLink{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.ScheduleEntry{},
		&models.PlayHistory{},
		&models.LiveSession{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{
		db:     db,
		bus:    events.NewBus(),
		logger: zerolog.Nop(),
	}, db
}

// --- handleMountsList ---

// TestHandleMountsList_MissingStationID verifies 400 when chi stationID param absent.
func TestHandleMountsList_MissingStationID(t *testing.T) {
	a, _ := newCatchAllAPI(t)

	req := httptest.NewRequest("GET", "/mounts", nil)
	rr := httptest.NewRecorder()
	a.handleMountsList(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing stationID: got %d, want 400", rr.Code)
	}
}

// TestHandleMountsList_NoAuthReturns401 verifies 401 when no claims on request with station_id.
func TestHandleMountsList_NoAuthReturns401(t *testing.T) {
	a, _ := newCatchAllAPI(t)

	req := httptest.NewRequest("GET", "/stations/st1/mounts", nil)
	req = withChiParam(req, "stationID", "st1")
	// No auth claims
	rr := httptest.NewRecorder()
	a.handleMountsList(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: got %d, want 401", rr.Code)
	}
}

// --- handleSmartBlocksList ---

// TestHandleSmartBlocksList_EmptyNoFilter verifies 200 empty result with no station_id.
func TestHandleSmartBlocksList_EmptyNoFilter(t *testing.T) {
	a, _ := newCatchAllAPI(t)

	req := httptest.NewRequest("GET", "/smart-blocks", nil)
	req = withAdminClaims(req) // Platform admin sees all blocks
	rr := httptest.NewRecorder()
	a.handleSmartBlocksList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("empty list: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["smart_blocks"]; !ok {
		t.Fatal("expected 'smart_blocks' key")
	}
}

// TestHandleSmartBlocksList_StationFilter verifies station_id filter is applied.
func TestHandleSmartBlocksList_StationFilter(t *testing.T) {
	a, db := newCatchAllAPI(t)

	db.Create(&models.SmartBlock{ID: "sb-1", StationID: "st-filter", Name: "Block A"}) //nolint:errcheck
	db.Create(&models.SmartBlock{ID: "sb-2", StationID: "st-other", Name: "Block B"})  //nolint:errcheck

	req := httptest.NewRequest("GET", "/smart-blocks?station_id=st-filter", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleSmartBlocksList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("filter: got %d, want 200", rr.Code)
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	blocks := resp["smart_blocks"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block for station filter, got %d", len(blocks))
	}
}

// --- handlePlaylistsList ---

// TestHandlePlaylistsList_AdminSeesAll verifies platform admin sees all playlists.
func TestHandlePlaylistsList_AdminSeesAll(t *testing.T) {
	a, db := newCatchAllAPI(t)

	db.Create(&models.Playlist{ID: "pl-1", StationID: "st-1", Name: "Pop Hour"})  //nolint:errcheck
	db.Create(&models.Playlist{ID: "pl-2", StationID: "st-2", Name: "Rock Hour"}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/playlists", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handlePlaylistsList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("admin list: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	playlists := resp["playlists"].([]any)
	if len(playlists) < 2 {
		t.Fatalf("expected 2 playlists, got %d", len(playlists))
	}
}

// TestHandlePlaylistsList_StationFilter verifies station_id filter works.
func TestHandlePlaylistsList_StationFilter(t *testing.T) {
	a, db := newCatchAllAPI(t)

	db.Create(&models.Playlist{ID: "pl-f1", StationID: "st-abc", Name: "Filtered"}) //nolint:errcheck
	db.Create(&models.Playlist{ID: "pl-f2", StationID: "st-xyz", Name: "Other"})    //nolint:errcheck

	req := httptest.NewRequest("GET", "/playlists?station_id=st-abc", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handlePlaylistsList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("filter: got %d, want 200", rr.Code)
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	playlists := resp["playlists"].([]any)
	if len(playlists) != 1 {
		t.Fatalf("expected 1 playlist, got %d", len(playlists))
	}
}

// --- handleStationsGet ---

// TestHandleStationsGet_MissingStationID verifies 400 when chi stationID param absent.
func TestHandleStationsGet_MissingStationID(t *testing.T) {
	a, _ := newCatchAllAPI(t)

	req := httptest.NewRequest("GET", "/stations/", nil)
	// No stationID chi param
	rr := httptest.NewRecorder()
	a.handleStationsGet(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing stationID: got %d, want 400", rr.Code)
	}
}

// TestHandleStationsGet_NonExistent verifies 404 for non-existent station.
func TestHandleStationsGet_NonExistent(t *testing.T) {
	a, _ := newCatchAllAPI(t)

	req := httptest.NewRequest("GET", "/stations/ghost-station", nil)
	req = withChiParam(req, "stationID", "ghost-station-id")
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleStationsGet(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("not found: got %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleStationsGet_Found verifies 200 with station data when station exists.
func TestHandleStationsGet_Found(t *testing.T) {
	a, db := newCatchAllAPI(t)

	db.Create(&models.Station{ID: "st-get-1", Name: "Get Station", Timezone: "UTC"}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/stations/st-get-1", nil)
	req = withChiParam(req, "stationID", "st-get-1")
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleStationsGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("found: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var station map[string]any
	json.NewDecoder(rr.Body).Decode(&station) //nolint:errcheck
	// Station struct has no json tags; Go serializes "ID" field as "ID" (uppercase).
	if station["ID"] != "st-get-1" {
		t.Fatalf("expected ID=st-get-1, got %v; body=%s", station["ID"], rr.Body.String())
	}
}

// --- handleMediaGet ---

// TestHandleMediaGet_FoundWithClaims verifies 200 with item data for existing media.
func TestHandleMediaGet_FoundWithClaims(t *testing.T) {
	a, db := newCatchAllAPI(t)

	db.Create(&models.MediaItem{
		ID:               "mi-get-1",
		StationID:        "st-media-1",
		OriginalFilename: "track.mp3",
		AnalysisState:    models.AnalysisComplete,
		Duration:         180.0,
	}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/media/mi-get-1", nil)
	req = withChiParam(req, "mediaID", "mi-get-1")
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleMediaGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("found: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var item map[string]any
	json.NewDecoder(rr.Body).Decode(&item) //nolint:errcheck
	// MediaItem struct has no json tags; Go serializes "ID" field as "ID" (uppercase).
	if item["ID"] != "mi-get-1" {
		t.Fatalf("expected ID=mi-get-1, got %v; body=%s", item["ID"], rr.Body.String())
	}
}

// --- handleAnalyticsNowPlaying live DJ detection ---

// TestHandleAnalyticsNowPlaying_LiveDJByMetadata verifies that when play history has
// type=live metadata, the response indicates is_live_dj=true.
func TestHandleAnalyticsNowPlaying_LiveDJByMetadata(t *testing.T) {
	a, db := newCatchAllAPI(t)

	db.Create(&models.PlayHistory{
		ID:        "ph-live-1",
		StationID: "st-livecheck",
		Title:     "Live DJ",
		StartedAt: time.Now().Add(-5 * time.Minute),
		Metadata:  map[string]any{"type": "live"},
	}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/?station_id=st-livecheck", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsNowPlaying(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("live dj by metadata: got %d; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["is_live_dj"] != true {
		t.Fatalf("expected is_live_dj=true, got %v", resp["is_live_dj"])
	}
	if resp["source_type"] != "live" {
		t.Fatalf("expected source_type=live, got %v", resp["source_type"])
	}
}

// TestHandleAnalyticsNowPlaying_LiveDJTitleDetection verifies that when the title
// is exactly "Live DJ", source_type is set to live.
func TestHandleAnalyticsNowPlaying_LiveDJTitleDetection(t *testing.T) {
	a, db := newCatchAllAPI(t)

	db.Create(&models.PlayHistory{
		ID:        "ph-livejd-2",
		StationID: "st-livejd-2",
		Title:     "Live DJ", // lowercase check in handler uses EqualFold
		StartedAt: time.Now().Add(-2 * time.Minute),
	}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/?station_id=st-livejd-2", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsNowPlaying(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("live title: got %d; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["source_type"] != "live" {
		t.Fatalf("expected source_type=live from title detection, got %v", resp["source_type"])
	}
}

// TestHandleAnalyticsNowPlaying_NowPlayingWithEndsAtInFuture verifies that a track
// whose EndedAt is in the future is considered "playing".
func TestHandleAnalyticsNowPlaying_NowPlayingWithEndsAtInFuture(t *testing.T) {
	a, db := newCatchAllAPI(t)

	db.Create(&models.PlayHistory{
		ID:        "ph-future-1",
		StationID: "st-future",
		Artist:    "Future Artist",
		Title:     "Still Going",
		StartedAt: time.Now().Add(-1 * time.Minute),
		EndedAt:   time.Now().Add(5 * time.Minute), // ends in the future
	}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/?station_id=st-future", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsNowPlaying(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("future ends_at: got %d; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["status"] != "playing" {
		t.Fatalf("expected status=playing for future ends_at, got %v", resp["status"])
	}
}

// --- handleScheduleList requireStationAccess path ---

// TestHandleScheduleList_NoAuthReturns401 verifies 401 when no claims are present.
func TestHandleScheduleList_NoAuthReturns401(t *testing.T) {
	a, _ := newScheduleTestAPI(t)

	req := httptest.NewRequest("GET", "/schedule?station_id=st1", nil)
	// No auth claims — should fail requireStationAccess
	rr := httptest.NewRecorder()
	a.handleScheduleList(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: got %d, want 401", rr.Code)
	}
}

// TestHandleScheduleList_AdminEmptyResult verifies 200 with empty array for admin
// requesting a station with no schedule entries.
func TestHandleScheduleList_AdminEmptyResult(t *testing.T) {
	a, _ := newScheduleTestAPI(t)

	req := httptest.NewRequest("GET", "/schedule?station_id=st-no-entries", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleScheduleList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("admin empty: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// --- handleClocksCreate ---

// TestHandleClocksCreate_InvalidJSON verifies 400 for malformed body.
func TestHandleClocksCreate_InvalidJSON(t *testing.T) {
	a, _ := newCatchAllAPI(t)
	a.scheduler = nil // not needed for validation

	req := httptest.NewRequest("POST", "/clocks", bytes.NewBufferString("{bad"))
	rr := httptest.NewRecorder()
	a.handleClocksCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// TestHandleClocksCreate_MissingFields verifies 400 when required fields absent.
func TestHandleClocksCreate_MissingFields(t *testing.T) {
	a, _ := newCatchAllAPI(t)

	body, _ := json.Marshal(map[string]any{"station_id": "st1"}) // no name
	req := httptest.NewRequest("POST", "/clocks", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleClocksCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing fields: got %d, want 400", rr.Code)
	}
}

// --- handleSmartBlocksCreate ---

// TestHandleSmartBlocksCreate_NoAuth verifies 401 when no claims present.
func TestHandleSmartBlocksCreate_NoAuth(t *testing.T) {
	a, _ := newCatchAllAPI(t)

	body, _ := json.Marshal(map[string]any{"station_id": "st1", "name": "Block"})
	req := httptest.NewRequest("POST", "/smart-blocks", bytes.NewBuffer(body))
	// no auth claims
	rr := httptest.NewRecorder()
	a.handleSmartBlocksCreate(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: got %d, want 401", rr.Code)
	}
}

// --- handleClockSimulate ---

// TestHandleClockSimulate_MissingClockID verifies 400 when chi clockID param absent.
func TestHandleClockSimulate_MissingClockID(t *testing.T) {
	a, _ := newScheduleTestAPI(t)

	req := httptest.NewRequest("POST", "/clocks//simulate", nil)
	// No clockID chi param
	rr := httptest.NewRecorder()
	a.handleClockSimulate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing clockID: got %d, want 400", rr.Code)
	}
}

// TestHandleClockSimulate_NonExistentClock verifies 404 when clock does not exist.
func TestHandleClockSimulate_NonExistentClock(t *testing.T) {
	a, _ := newScheduleTestAPI(t)

	req := httptest.NewRequest("POST", "/clocks/nonexistent/simulate", nil)
	req = withChiParam(req, "clockID", "nonexistent-clock-id")
	rr := httptest.NewRecorder()
	a.handleClockSimulate(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("not found: got %d, want 404", rr.Code)
	}
}

// --- handleMountsCreate no auth ---

// TestHandleMountsCreate_NoAuth verifies 401 when no claims present.
func TestHandleMountsCreate_NoAuth(t *testing.T) {
	a, _ := newCatchAllAPI(t)

	body, _ := json.Marshal(map[string]any{
		"station_id": "st1", "name": "test", "url": "/test", "format": "mp3",
	})
	req := httptest.NewRequest("POST", "/mounts", bytes.NewBuffer(body))
	// No auth claims
	rr := httptest.NewRecorder()
	a.handleMountsCreate(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: got %d, want 401", rr.Code)
	}
}
