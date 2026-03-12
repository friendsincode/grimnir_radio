/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

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

func newAPIHandlersTest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Station{},
		&models.Mount{},
		&models.Playlist{},
		&models.SmartBlock{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.ScheduleEntry{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{db: db, bus: events.NewBus(), logger: zerolog.Nop()}, db
}

// --- Stations ---

func TestAPIHandlers_Stations(t *testing.T) {
	a, _ := newAPIHandlersTest(t)

	// List stations (empty)
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleStationsList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list stations: got %d, want 200", rr.Code)
	}
	var stationList []any
	json.NewDecoder(rr.Body).Decode(&stationList) //nolint:errcheck
	if len(stationList) != 0 {
		t.Fatalf("expected 0 stations, got %d", len(stationList))
	}

	// Create station (missing name → 400)
	body, _ := json.Marshal(map[string]any{"timezone": "UTC"})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	a.handleStationsCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create station no name: got %d, want 400", rr.Code)
	}

	// Create station (valid)
	body, _ = json.Marshal(map[string]any{
		"name":     "Test Station",
		"timezone": "America/New_York",
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr = httptest.NewRecorder()
	a.handleStationsCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create station: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var created models.Station
	json.NewDecoder(rr.Body).Decode(&created) //nolint:errcheck
	if created.ID == "" {
		t.Fatal("expected station id in response")
	}

	// List stations (should have 1)
	req = httptest.NewRequest("GET", "/", nil)
	rr = httptest.NewRecorder()
	a.handleStationsList(rr, req)
	json.NewDecoder(rr.Body).Decode(&stationList) //nolint:errcheck
	if len(stationList) != 1 {
		t.Fatalf("expected 1 station, got %d", len(stationList))
	}

	// Get station
	req = httptest.NewRequest("GET", "/"+created.ID, nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "stationID", created.ID)
	rr = httptest.NewRecorder()
	a.handleStationsGet(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get station: got %d, want 200", rr.Code)
	}

	// Get non-existent station
	req = httptest.NewRequest("GET", "/missing", nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "stationID", "nonexistent")
	rr = httptest.NewRecorder()
	a.handleStationsGet(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get missing station: got %d, want 404", rr.Code)
	}
}

// --- Mounts ---

func TestAPIHandlers_Mounts(t *testing.T) {
	a, _ := newAPIHandlersTest(t)

	stationID := "st-mount-test"

	// List mounts (empty)
	req := httptest.NewRequest("GET", "/", nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "stationID", stationID)
	rr := httptest.NewRecorder()
	a.handleMountsList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list mounts: got %d, want 200", rr.Code)
	}
	var mounts []any
	json.NewDecoder(rr.Body).Decode(&mounts) //nolint:errcheck
	if len(mounts) != 0 {
		t.Fatalf("expected 0 mounts, got %d", len(mounts))
	}

	// Create mount (missing required fields → 400)
	body, _ := json.Marshal(map[string]any{"name": "no-url"})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "stationID", stationID)
	rr = httptest.NewRecorder()
	a.handleMountsCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create mount missing fields: got %d, want 400", rr.Code)
	}

	// Create mount (valid)
	body, _ = json.Marshal(map[string]any{
		"name":       "Main Stream",
		"url":        "/stream.mp3",
		"format":     "mp3",
		"bitrate":    128,
		"channels":   2,
		"sampleRate": 44100,
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "stationID", stationID)
	rr = httptest.NewRecorder()
	a.handleMountsCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create mount: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var mount models.Mount
	json.NewDecoder(rr.Body).Decode(&mount) //nolint:errcheck
	if mount.ID == "" {
		t.Fatal("expected mount id in response")
	}

	// List mounts (should have 1)
	req = httptest.NewRequest("GET", "/", nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "stationID", stationID)
	rr = httptest.NewRecorder()
	a.handleMountsList(rr, req)
	json.NewDecoder(rr.Body).Decode(&mounts) //nolint:errcheck
	if len(mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(mounts))
	}
}

// --- Playlists ---

func TestAPIHandlers_Playlists(t *testing.T) {
	a, db := newAPIHandlersTest(t)

	// List playlists (empty)
	req := httptest.NewRequest("GET", "/?station_id=s1", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handlePlaylistsList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list playlists: got %d, want 200", rr.Code)
	}
	var listResp map[string]any
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	playlists, _ := listResp["playlists"].([]any)
	if len(playlists) != 0 {
		t.Fatalf("expected 0 playlists, got %d", len(playlists))
	}

	// Seed a playlist directly
	pl := models.Playlist{
		ID:        "pl-1",
		StationID: "s1",
		Name:      "Test Playlist",
	}
	db.Create(&pl) //nolint:errcheck

	// List playlists (should have 1)
	req = httptest.NewRequest("GET", "/?station_id=s1", nil)
	req = withAdminClaims(req)
	rr = httptest.NewRecorder()
	a.handlePlaylistsList(rr, req)
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	playlists, _ = listResp["playlists"].([]any)
	if len(playlists) != 1 {
		t.Fatalf("expected 1 playlist, got %d", len(playlists))
	}
}

// --- Smart Blocks ---

func TestAPIHandlers_SmartBlocks(t *testing.T) {
	a, _ := newAPIHandlersTest(t)

	// List smart blocks (empty)
	req := httptest.NewRequest("GET", "/?station_id=s1", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleSmartBlocksList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list smart blocks: got %d, want 200", rr.Code)
	}
	var listResp map[string]any
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	blocks, _ := listResp["smart_blocks"].([]any)
	if len(blocks) != 0 {
		t.Fatalf("expected 0 smart blocks, got %d", len(blocks))
	}

	// Create smart block (missing fields → 400)
	body, _ := json.Marshal(map[string]any{"station_id": "s1"}) // no name
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr = httptest.NewRecorder()
	a.handleSmartBlocksCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create smart block missing name: got %d, want 400", rr.Code)
	}

	// Create smart block (valid)
	body, _ = json.Marshal(map[string]any{
		"station_id":  "s1",
		"name":        "Jazz Block",
		"description": "All jazz, all day",
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr = httptest.NewRecorder()
	a.handleSmartBlocksCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create smart block: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var block models.SmartBlock
	json.NewDecoder(rr.Body).Decode(&block) //nolint:errcheck
	if block.ID == "" {
		t.Fatal("expected smart block id in response")
	}

	// List smart blocks (should have 1)
	req = httptest.NewRequest("GET", "/?station_id=s1", nil)
	req = withAdminClaims(req)
	rr = httptest.NewRecorder()
	a.handleSmartBlocksList(rr, req)
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	blocks, _ = listResp["smart_blocks"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 smart block, got %d", len(blocks))
	}
}

// --- Clocks ---

func TestAPIHandlers_Clocks(t *testing.T) {
	a, _ := newAPIHandlersTest(t)

	// List clocks (empty)
	req := httptest.NewRequest("GET", "/?station_id=s1", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleClocksList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list clocks: got %d, want 200", rr.Code)
	}
	var listResp map[string]any
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	clocks, _ := listResp["clocks"].([]any)
	if len(clocks) != 0 {
		t.Fatalf("expected 0 clocks, got %d", len(clocks))
	}

	// Create clock (missing required fields → 400)
	body, _ := json.Marshal(map[string]any{"station_id": "s1"}) // no name
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr = httptest.NewRecorder()
	a.handleClocksCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create clock missing name: got %d, want 400", rr.Code)
	}

	// Create clock (valid, with a slot)
	body, _ = json.Marshal(map[string]any{
		"station_id": "s1",
		"name":       "Morning Clock",
		"start_hour": 6,
		"end_hour":   12,
		"slots": []map[string]any{
			{
				"position":  1,
				"offset_ms": 0,
				"type":      "smart_block",
				"payload":   map[string]any{"block_id": "sb-1"},
			},
		},
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr = httptest.NewRecorder()
	a.handleClocksCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create clock: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var clock models.ClockHour
	json.NewDecoder(rr.Body).Decode(&clock) //nolint:errcheck
	if clock.ID == "" {
		t.Fatal("expected clock id in response")
	}
	if len(clock.Slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(clock.Slots))
	}

	// List clocks (should have 1)
	req = httptest.NewRequest("GET", "/?station_id=s1", nil)
	req = withAdminClaims(req)
	rr = httptest.NewRecorder()
	a.handleClocksList(rr, req)
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	clocks, _ = listResp["clocks"].([]any)
	if len(clocks) != 1 {
		t.Fatalf("expected 1 clock, got %d", len(clocks))
	}
}

// --- Schedule Entry Update ---

func TestAPIHandlers_ScheduleUpdate(t *testing.T) {
	a, db := newAPIHandlersTest(t)

	// Get non-existent entry → 404
	req := httptest.NewRequest("PUT", "/missing", bytes.NewReader([]byte("{}")))
	req = withAdminClaims(req)
	req = withChiParam(req, "entryID", "nonexistent-id")
	rr := httptest.NewRecorder()
	a.handleScheduleUpdate(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("update missing entry: got %d, want 404", rr.Code)
	}

	// Seed an entry
	now := time.Now().UTC().Truncate(time.Second)
	entry := models.ScheduleEntry{
		ID:         "entry-upd-1",
		StationID:  "s1",
		MountID:    "mt-1",
		StartsAt:   now,
		EndsAt:     now.Add(time.Hour),
		SourceType: "smart_block",
		SourceID:   "sb-1",
		Metadata:   map[string]any{},
	}
	db.Create(&entry) //nolint:errcheck

	// Update starts_at only — ends_at should be preserved relative
	newStart := now.Add(30 * time.Minute).Format(time.RFC3339)
	body, _ := json.Marshal(map[string]any{"starts_at": newStart})
	req = httptest.NewRequest("PUT", "/entry-upd-1", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "entryID", "entry-upd-1")
	rr = httptest.NewRecorder()
	a.handleScheduleUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update entry: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}

	// No-op update (empty body — no changes)
	body, _ = json.Marshal(map[string]any{})
	req = httptest.NewRequest("PUT", "/entry-upd-1", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "entryID", "entry-upd-1")
	rr = httptest.NewRecorder()
	a.handleScheduleUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("no-op update entry: got %d, want 200", rr.Code)
	}
}

// --- Auth / Error edge cases ---

func TestAPIHandlers_Errors(t *testing.T) {
	a, _ := newAPIHandlersTest(t)

	t.Run("get station requires auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/s1", nil)
		// No auth claims
		req = withChiParam(req, "stationID", "s1")
		rr := httptest.NewRecorder()
		a.handleStationsGet(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("list mounts requires auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		// No auth claims
		req = withChiParam(req, "stationID", "s1")
		rr := httptest.NewRecorder()
		a.handleMountsList(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("create smart block requires station_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "No Station"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withAdminClaims(req)
		rr := httptest.NewRecorder()
		a.handleSmartBlocksCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("create clock requires station_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "Clock Without Station"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withAdminClaims(req)
		rr := httptest.NewRecorder()
		a.handleClocksCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("update schedule entry with invalid starts_at", func(t *testing.T) {
		// Need a real entry in DB
		entry := models.ScheduleEntry{
			ID:         "entry-err-1",
			StationID:  "s1",
			StartsAt:   time.Now().UTC(),
			EndsAt:     time.Now().UTC().Add(time.Hour),
			SourceType: "smart_block",
		}
		a.db.Create(&entry) //nolint:errcheck
		body, _ := json.Marshal(map[string]any{"starts_at": "not-a-time"})
		req := httptest.NewRequest("PUT", "/entry-err-1", bytes.NewReader(body))
		req = withAdminClaims(req)
		req = withChiParam(req, "entryID", "entry-err-1")
		rr := httptest.NewRecorder()
		a.handleScheduleUpdate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})
}
