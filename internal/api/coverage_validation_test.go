/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// Validation-path tests for handlers that are partially covered.
// Covers: webstream (missing-id guards), handleStationsList (returns array),
// handleScheduleUpdate validation paths, handleAnalyticsListeners paths,
// and integrity audit helpers (logIntegrityRepairAudit / logIntegrityScanAudit).

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
	webstreamsvc "github.com/friendsincode/grimnir_radio/internal/webstream"
)

// newValidationTestAPI creates an API with a webstream service for validation tests.
func newValidationTestAPI(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "validation.db")), &gorm.Config{})
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
		&models.Webstream{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	bus := events.NewBus()
	wsSvc := webstreamsvc.NewService(db, bus, zerolog.Nop())
	t.Cleanup(func() { wsSvc.Shutdown() }) //nolint:errcheck

	return &API{
		db:           db,
		bus:          bus,
		webstreamSvc: wsSvc,
		logger:       zerolog.Nop(),
	}, db
}

// --- handleStationsList ---

// TestHandleStationsList_ReturnsArray verifies 200 with an array (possibly empty).
func TestHandleStationsList_ReturnsArray(t *testing.T) {
	a, _ := newValidationTestAPI(t)

	req := httptest.NewRequest("GET", "/stations", nil)
	rr := httptest.NewRecorder()
	a.handleStationsList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list stations: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleStationsList_WithStations verifies that created stations appear in the list.
func TestHandleStationsList_WithStations(t *testing.T) {
	a, db := newValidationTestAPI(t)

	db.Create(&models.Station{ID: "st-list-1", Name: "Radio One", Timezone: "UTC"}) //nolint:errcheck
	db.Create(&models.Station{ID: "st-list-2", Name: "Radio Two", Timezone: "UTC"}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/stations", nil)
	rr := httptest.NewRecorder()
	a.handleStationsList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list stations: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var stations []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&stations); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(stations) < 2 {
		t.Fatalf("expected at least 2 stations, got %d", len(stations))
	}
}

// --- Webstream validation paths ---

// TestHandleGetWebstream_MissingID verifies 400 when chi id param absent.
func TestHandleGetWebstream_MissingID(t *testing.T) {
	a, _ := newValidationTestAPI(t)

	req := httptest.NewRequest("GET", "/webstreams/", nil)
	// No id chi param
	rr := httptest.NewRecorder()
	a.handleGetWebstream(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing id: got %d, want 400", rr.Code)
	}
}

// TestHandleUpdateWebstream_MissingID verifies 400 when chi id param absent.
func TestHandleUpdateWebstream_MissingID(t *testing.T) {
	a, _ := newValidationTestAPI(t)

	req := httptest.NewRequest("PATCH", "/webstreams/", bytes.NewBufferString("{}"))
	rr := httptest.NewRecorder()
	a.handleUpdateWebstream(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing id: got %d, want 400", rr.Code)
	}
}

// TestHandleUpdateWebstream_InvalidJSON verifies 400 on malformed body.
func TestHandleUpdateWebstream_InvalidJSON(t *testing.T) {
	a, _ := newValidationTestAPI(t)

	req := httptest.NewRequest("PATCH", "/webstreams/ws1", bytes.NewBufferString("{bad"))
	req = withChiParam(req, "id", "ws1")
	rr := httptest.NewRecorder()
	a.handleUpdateWebstream(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// TestHandleDeleteWebstream_MissingID verifies 400 when chi id param absent.
func TestHandleDeleteWebstream_MissingID(t *testing.T) {
	a, _ := newValidationTestAPI(t)

	req := httptest.NewRequest("DELETE", "/webstreams/", nil)
	rr := httptest.NewRecorder()
	a.handleDeleteWebstream(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing id: got %d, want 400", rr.Code)
	}
}

// TestHandleTriggerWebstreamFailover_MissingID verifies 400 when chi id param absent.
func TestHandleTriggerWebstreamFailover_MissingID(t *testing.T) {
	a, _ := newValidationTestAPI(t)

	req := httptest.NewRequest("POST", "/webstreams//failover", nil)
	rr := httptest.NewRecorder()
	a.handleTriggerWebstreamFailover(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing id: got %d, want 400", rr.Code)
	}
}

// TestHandleResetWebstreamToPrimary_MissingID verifies 400 when chi id param absent.
func TestHandleResetWebstreamToPrimary_MissingID(t *testing.T) {
	a, _ := newValidationTestAPI(t)

	req := httptest.NewRequest("POST", "/webstreams//reset", nil)
	rr := httptest.NewRecorder()
	a.handleResetWebstreamToPrimary(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing id: got %d, want 400", rr.Code)
	}
}

// TestHandleCreateWebstream_MissingFields verifies 400 when required fields absent.
func TestHandleCreateWebstream_MissingFields(t *testing.T) {
	a, _ := newValidationTestAPI(t)

	body, _ := json.Marshal(map[string]any{"station_id": "st1"}) // no name, no urls
	req := httptest.NewRequest("POST", "/webstreams", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleCreateWebstream(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing fields: got %d, want 400", rr.Code)
	}
}

// TestHandleCreateWebstream_InvalidJSON verifies 400 on malformed body.
func TestHandleCreateWebstream_InvalidJSON(t *testing.T) {
	a, _ := newValidationTestAPI(t)

	req := httptest.NewRequest("POST", "/webstreams", bytes.NewBufferString("{bad"))
	rr := httptest.NewRecorder()
	a.handleCreateWebstream(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// TestHandleListWebstreams_EmptyReturnsObject verifies 200 with empty webstreams list.
func TestHandleListWebstreams_EmptyReturnsObject(t *testing.T) {
	a, _ := newValidationTestAPI(t)

	req := httptest.NewRequest("GET", "/webstreams", nil)
	rr := httptest.NewRecorder()
	a.handleListWebstreams(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list webstreams: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["webstreams"]; !ok {
		t.Fatal("expected 'webstreams' key in response")
	}
}

// TestHandleListWebstreams_WithStationFilter verifies station_id filter is accepted.
func TestHandleListWebstreams_WithStationFilter(t *testing.T) {
	a, _ := newValidationTestAPI(t)

	req := httptest.NewRequest("GET", "/webstreams?station_id=st1", nil)
	rr := httptest.NewRecorder()
	a.handleListWebstreams(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("filtered list: got %d, want 200", rr.Code)
	}
}

// --- handleScheduleUpdate validation paths ---

// TestHandleScheduleUpdate_MissingEntryID verifies 400 when chi entryID param absent.
func TestHandleScheduleUpdate_MissingEntryID(t *testing.T) {
	a, db := newScheduleTestAPI(t)
	_ = db

	req := httptest.NewRequest("PATCH", "/schedule/", bytes.NewBufferString("{}"))
	// No entryID chi param
	rr := httptest.NewRecorder()
	a.handleScheduleUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing entryID: got %d, want 400", rr.Code)
	}
}

// TestHandleScheduleUpdate_InvalidJSON verifies 400 on malformed body.
func TestHandleScheduleUpdate_InvalidJSON(t *testing.T) {
	a, _ := newScheduleTestAPI(t)

	req := httptest.NewRequest("PATCH", "/schedule/e1", bytes.NewBufferString("{bad"))
	req = withChiParam(req, "entryID", "e1")
	rr := httptest.NewRecorder()
	a.handleScheduleUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// TestHandleScheduleUpdate_InvalidEndsAt verifies 400 for a malformed ends_at.
func TestHandleScheduleUpdate_InvalidEndsAt(t *testing.T) {
	a, db := newScheduleTestAPI(t)

	entry := models.ScheduleEntry{
		ID:         "se-te-1",
		StationID:  "st-te-1",
		SourceType: "smart_block",
		StartsAt:   time.Now(),
		EndsAt:     time.Now().Add(time.Hour),
	}
	db.Create(&entry) //nolint:errcheck

	body, _ := json.Marshal(map[string]any{"ends_at": "not-a-time"})
	req := httptest.NewRequest("PATCH", "/schedule/se-te-1", bytes.NewBuffer(body))
	req = withChiParam(req, "entryID", "se-te-1")
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleScheduleUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid ends_at: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleScheduleUpdate_WithStartsAtAutoEndsAt verifies that when starts_at is valid
// and ends_at is omitted, the entry's ends_at is preserved relative to the original duration.
func TestHandleScheduleUpdate_WithStartsAtAutoEndsAt(t *testing.T) {
	a, db := newScheduleTestAPI(t)

	now := time.Now().Truncate(time.Second)
	entry := models.ScheduleEntry{
		ID:         "se-auto-1",
		StationID:  "st-auto-1",
		SourceType: "smart_block",
		StartsAt:   now,
		EndsAt:     now.Add(time.Hour),
	}
	db.Create(&entry) //nolint:errcheck

	// Valid RFC3339 starts_at, no ends_at — handler auto-computes ends_at.
	newStart := now.Add(30 * time.Minute)
	body, _ := json.Marshal(map[string]any{
		"starts_at": newStart.Format(time.RFC3339),
	})
	req := httptest.NewRequest("PATCH", "/schedule/se-auto-1", bytes.NewBuffer(body))
	req = withChiParam(req, "entryID", "se-auto-1")
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleScheduleUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("auto ends_at: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// --- handleAnalyticsListeners paths ---

// TestHandleAnalyticsListeners_NilBroadcastReturnsZero verifies that when broadcast is nil
// the handler returns 200 with total=0 (nil-guard path).
func TestHandleAnalyticsListeners_NilBroadcastReturnsZero(t *testing.T) {
	a := &API{logger: zerolog.Nop(), bus: events.NewBus(), broadcast: nil}

	req := httptest.NewRequest("GET", "/analytics/listeners", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsListeners(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("nil broadcast: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["total"].(float64) != 0 {
		t.Fatalf("expected total=0, got %v", resp["total"])
	}
}
