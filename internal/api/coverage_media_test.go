/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// Tests for handleMediaGet, handleMountsList, handleMountsCreate,
// handleAnalyticsListeners, handleReanalyzeMissingArtwork, and
// handleTestMediaEngine — covering the DB-backed and nil-service paths.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// withStationClaims attaches station-scoped (non-admin) claims for the given stationID.
func withStationClaims(req *http.Request, stationID string) *http.Request {
	return req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID:    "u-station",
		StationID: stationID,
		Roles:     []string{"station_admin"},
	}))
}

func newMediaTestAPI(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "media.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// Single connection prevents SQLite WAL isolation issues across HTTP request contexts.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.Station{},
		&models.Mount{},
		&models.MediaItem{},
		&models.MediaTagLink{},
		&models.AnalysisJob{},
		&models.StationUser{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{db: db, bus: events.NewBus(), logger: zerolog.Nop()}, db
}

// --- handleMediaGet ---

// TestHandleMediaGet_NotFound verifies 404 is returned for an unknown media ID.
func TestHandleMediaGet_NotFound(t *testing.T) {
	a, _ := newMediaTestAPI(t)

	req := httptest.NewRequest("GET", "/media/missing", nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "mediaID", "does-not-exist")
	rr := httptest.NewRecorder()
	a.handleMediaGet(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing media: got %d, want 404", rr.Code)
	}
}

// TestHandleMediaGet_ReturnsCorrectFields verifies the happy path returns the item
// with expected field values so callers can rely on the contract.
func TestHandleMediaGet_ReturnsCorrectFields(t *testing.T) {
	a, db := newMediaTestAPI(t)

	item := models.MediaItem{
		ID:            "media-get-1",
		StationID:     "st-mg",
		Title:         "Test Track",
		Artist:        "Test Artist",
		Album:         "Test Album",
		AnalysisState: models.AnalysisComplete,
	}
	db.Create(&item) //nolint:errcheck

	req := httptest.NewRequest("GET", "/media/media-get-1", nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "mediaID", "media-get-1")
	rr := httptest.NewRecorder()
	a.handleMediaGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("found media: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// Decode body as a raw map to inspect field names regardless of json tag conventions.
	var raw json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	body := string(raw)
	if body == "" {
		t.Fatal("empty response body")
	}
	// The response must contain the media item data. Verify by re-decoding into the model.
	var got models.MediaItem
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal into MediaItem: %v", err)
	}
	if got.ID != "media-get-1" {
		t.Fatalf("wrong ID: %q (body: %s)", got.ID, body)
	}
	if got.Title != "Test Track" {
		t.Fatalf("wrong Title: %q", got.Title)
	}
	if got.Artist != "Test Artist" {
		t.Fatalf("wrong Artist: %q", got.Artist)
	}
	if got.StationID != "st-mg" {
		t.Fatalf("wrong StationID: %q", got.StationID)
	}
}

// TestHandleMediaGet_StationAccessDenied verifies that a non-admin user cannot read
// media belonging to a different station.
func TestHandleMediaGet_StationAccessDenied(t *testing.T) {
	a, db := newMediaTestAPI(t)

	item := models.MediaItem{
		ID:            "media-other-st",
		StationID:     "st-other",
		Title:         "Private Track",
		AnalysisState: models.AnalysisComplete,
	}
	db.Create(&item) //nolint:errcheck

	// Attach claims for a different station — should be denied.
	req := httptest.NewRequest("GET", "/media/media-other-st", nil)
	req = req.WithContext(withStationClaims(req, "st-mine").Context())
	req = withChiParam(req, "mediaID", "media-other-st")
	rr := httptest.NewRecorder()
	a.handleMediaGet(rr, req)

	// Should be 403 (station_access_denied) not 200.
	if rr.Code == http.StatusOK {
		t.Fatalf("access to other station's media should be denied, got 200")
	}
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 forbidden, got %d", rr.Code)
	}
}

// --- handleMountsList ---

// TestHandleMountsList_StationScopedResults verifies that only mounts belonging to the
// requested station are returned — not mounts from other stations.
func TestHandleMountsList_StationScopedResults(t *testing.T) {
	a, db := newMediaTestAPI(t)

	// Two mounts for st-a, one for st-b.
	mounts := []models.Mount{
		{ID: "mt-a1", StationID: "st-a", Name: "Stream A1", URL: "/a1.mp3", Format: "mp3"},
		{ID: "mt-a2", StationID: "st-a", Name: "Stream A2", URL: "/a2.mp3", Format: "mp3"},
		{ID: "mt-b1", StationID: "st-b", Name: "Stream B1", URL: "/b1.mp3", Format: "mp3"},
	}
	for i := range mounts {
		db.Create(&mounts[i]) //nolint:errcheck
	}

	req := httptest.NewRequest("GET", "/", nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "stationID", "st-a")
	rr := httptest.NewRecorder()
	a.handleMountsList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("mounts list: got %d, want 200", rr.Code)
	}

	var result []models.Mount
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 mounts for st-a, got %d", len(result))
	}
	for _, m := range result {
		if m.StationID != "st-a" {
			t.Fatalf("leaked mount from wrong station: StationID=%q", m.StationID)
		}
	}
}

// TestHandleMountsList_EmptyReturnsArray verifies empty array (not null) for a station
// with no mounts.
func TestHandleMountsList_EmptyReturnsArray(t *testing.T) {
	a, _ := newMediaTestAPI(t)

	req := httptest.NewRequest("GET", "/", nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "stationID", "st-nomounts")
	rr := httptest.NewRecorder()
	a.handleMountsList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("empty mounts: got %d, want 200", rr.Code)
	}

	var result []any
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil array, got null")
	}
}

// --- handleMountsCreate ---

// TestHandleMountsCreate_MissingFormat verifies 400 when format is absent.
func TestHandleMountsCreate_MissingFormat(t *testing.T) {
	a, _ := newMediaTestAPI(t)

	body, _ := json.Marshal(map[string]any{
		"name": "No Format Stream",
		"url":  "/stream.mp3",
		// format intentionally omitted
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "stationID", "st-c")
	rr := httptest.NewRecorder()
	a.handleMountsCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing format: got %d, want 400", rr.Code)
	}
}

// TestHandleMountsCreate_MissingName verifies 400 when name is absent.
func TestHandleMountsCreate_MissingName(t *testing.T) {
	a, _ := newMediaTestAPI(t)

	body, _ := json.Marshal(map[string]any{
		"url":    "/stream.mp3",
		"format": "mp3",
		// name intentionally omitted
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "stationID", "st-c")
	rr := httptest.NewRecorder()
	a.handleMountsCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing name: got %d, want 400", rr.Code)
	}
}

// TestHandleMountsCreate_MissingURL verifies 400 when url is absent.
func TestHandleMountsCreate_MissingURL(t *testing.T) {
	a, _ := newMediaTestAPI(t)

	body, _ := json.Marshal(map[string]any{
		"name":   "No URL Mount",
		"format": "mp3",
		// url intentionally omitted
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "stationID", "st-c")
	rr := httptest.NewRecorder()
	a.handleMountsCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing url: got %d, want 400", rr.Code)
	}
}

// TestHandleMountsCreate_HappyPath verifies that a complete valid request creates
// the mount and returns 201 with the ID set.
func TestHandleMountsCreate_HappyPath(t *testing.T) {
	a, _ := newMediaTestAPI(t)

	body, _ := json.Marshal(map[string]any{
		"name":        "Main MP3 Stream",
		"url":         "/main.mp3",
		"format":      "mp3",
		"bitrate_kbps": 128,
		"channels":    2,
		"sample_rate": 44100,
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "stationID", "st-create")
	rr := httptest.NewRecorder()
	a.handleMountsCreate(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("create mount: got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var mount models.Mount
	if err := json.NewDecoder(rr.Body).Decode(&mount); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if mount.ID == "" {
		t.Fatal("expected non-empty ID in response")
	}
	if mount.StationID != "st-create" {
		t.Fatalf("wrong station_id: %v", mount.StationID)
	}
	if mount.Name != "Main MP3 Stream" {
		t.Fatalf("wrong name: %v", mount.Name)
	}
}

// TestHandleMountsCreate_StationIDFromBody verifies that when stationID chi param is
// absent, the station_id from the JSON body is used.
func TestHandleMountsCreate_StationIDFromBody(t *testing.T) {
	a, _ := newMediaTestAPI(t)

	body, _ := json.Marshal(map[string]any{
		"station_id": "st-from-body",
		"name":       "Body Station Mount",
		"url":        "/body.mp3",
		"format":     "mp3",
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	// No chi stationID param — handler should fall back to body.
	rr := httptest.NewRecorder()
	a.handleMountsCreate(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("body station_id: got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var mount models.Mount
	json.NewDecoder(rr.Body).Decode(&mount) //nolint:errcheck
	if mount.StationID != "st-from-body" {
		t.Fatalf("expected station_id=st-from-body, got %v", mount.StationID)
	}
}

// --- handleAnalyticsListeners ---

// TestHandleAnalyticsListeners_ResponseStructure verifies that when broadcast is nil,
// the handler returns 200 with a structured response containing total and mounts keys
// with correct zero values (extends the nil-broadcast test with body assertions).
func TestHandleAnalyticsListeners_ResponseStructure(t *testing.T) {
	a, _ := newMediaTestAPI(t)
	// broadcast is nil by default in newMediaTestAPI.

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
	// total should be 0.
	total, _ := resp["total"].(float64)
	if total != 0 {
		t.Fatalf("expected total=0, got %v", total)
	}
	// mounts should be an array (not null).
	mounts, ok := resp["mounts"]
	if !ok {
		t.Fatal("expected 'mounts' key in response")
	}
	if mounts == nil {
		t.Fatal("expected non-nil mounts array")
	}
}
