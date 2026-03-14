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

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newMiscAPITest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Station{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.PlayHistory{},
		&models.LiveSession{},
		&models.StationUser{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{db: db, logger: zerolog.Nop(), bus: events.NewBus()}, db
}

// --- parseEventTypes ---

func TestParseEventTypes_Empty(t *testing.T) {
	result := parseEventTypes("")
	if result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}
}

func TestParseEventTypes_Single(t *testing.T) {
	result := parseEventTypes("now_playing")
	if len(result) != 1 || string(result[0]) != "now_playing" {
		t.Fatalf("expected [now_playing], got %v", result)
	}
}

func TestParseEventTypes_Multiple(t *testing.T) {
	result := parseEventTypes("now_playing,health,webstream_health")
	if len(result) != 3 {
		t.Fatalf("expected 3 event types, got %d", len(result))
	}
}

func TestParseEventTypes_SkipsEmpty(t *testing.T) {
	result := parseEventTypes("now_playing,,health")
	if len(result) != 2 {
		t.Fatalf("expected 2 event types (empty skipped), got %d", len(result))
	}
}

// --- handleWebhookTrackStart ---

func TestHandleWebhookTrackStart_InvalidJSON(t *testing.T) {
	a, _ := newMiscAPITest(t)
	req := httptest.NewRequest("POST", "/webhook/track-start", bytes.NewReader([]byte("{")))
	rr := httptest.NewRecorder()
	a.handleWebhookTrackStart(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestHandleWebhookTrackStart_Valid(t *testing.T) {
	a, _ := newMiscAPITest(t)
	body, _ := json.Marshal(map[string]any{"station_id": "s1", "title": "Test Track"})
	req := httptest.NewRequest("POST", "/webhook/track-start", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	a.handleWebhookTrackStart(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", rr.Code)
	}
}

// --- handleTestMediaEngine ---

func TestHandleTestMediaEngine_NilAnalyzer(t *testing.T) {
	a, _ := newMiscAPITest(t)
	// analyzer is nil → 503
	req := httptest.NewRequest("GET", "/system/test-media-engine", nil)
	rr := httptest.NewRecorder()
	a.handleTestMediaEngine(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rr.Code)
	}
}

// --- handleReanalyzeMissingArtwork ---

func TestHandleReanalyzeMissingArtwork_NilAnalyzer(t *testing.T) {
	a, _ := newMiscAPITest(t)
	// analyzer is nil → 503
	req := httptest.NewRequest("POST", "/system/reanalyze-missing-artwork", nil)
	rr := httptest.NewRecorder()
	a.handleReanalyzeMissingArtwork(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rr.Code)
	}
}

// --- handleSystemLogs ---

func TestHandleSystemLogs_NilBuffer(t *testing.T) {
	a, _ := newMiscAPITest(t)
	req := httptest.NewRequest("GET", "/system/logs", nil)
	rr := httptest.NewRecorder()
	a.handleSystemLogs(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rr.Code)
	}
}

// --- handleStationLogs ---

func TestHandleStationLogs_NilBuffer(t *testing.T) {
	a, _ := newMiscAPITest(t)
	req := httptest.NewRequest("GET", "/station/s1/logs", nil)
	req = withChiParam(req, "stationID", "s1")
	rr := httptest.NewRecorder()
	a.handleStationLogs(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rr.Code)
	}
}

// --- handleStationLogComponents ---

func TestHandleStationLogComponents_NilBuffer(t *testing.T) {
	a, _ := newMiscAPITest(t)
	req := httptest.NewRequest("GET", "/station/s1/logs/components", nil)
	req = withChiParam(req, "stationID", "s1")
	rr := httptest.NewRecorder()
	a.handleStationLogComponents(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rr.Code)
	}
}

// --- handleStationLogStats ---

func TestHandleStationLogStats_NilBuffer(t *testing.T) {
	a, _ := newMiscAPITest(t)
	req := httptest.NewRequest("GET", "/station/s1/logs/stats", nil)
	req = withChiParam(req, "stationID", "s1")
	rr := httptest.NewRecorder()
	a.handleStationLogStats(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rr.Code)
	}
}

// --- handleScheduleList ---

func TestHandleScheduleList_MissingStationID(t *testing.T) {
	a, _ := newMiscAPITest(t)
	req := httptest.NewRequest("GET", "/schedule", nil)
	rr := httptest.NewRecorder()
	a.handleScheduleList(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

// --- handleScheduleRefresh ---

func TestHandleScheduleRefresh(t *testing.T) {
	a, _ := newMiscAPITest(t)

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/schedule/refresh", bytes.NewReader([]byte("{")))
		rr := httptest.NewRecorder()
		a.handleScheduleRefresh(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing station_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{})
		req := httptest.NewRequest("POST", "/schedule/refresh", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleScheduleRefresh(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}

// --- handleScheduleUpdate ---

func TestHandleScheduleUpdate(t *testing.T) {
	a, _ := newMiscAPITest(t)

	t.Run("missing entry_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"title": "New Title"})
		req := httptest.NewRequest("PUT", "/schedule/entry/", bytes.NewReader(body))
		// no chi param → empty entryID → 400
		rr := httptest.NewRecorder()
		a.handleScheduleUpdate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/schedule/entry/e1", bytes.NewReader([]byte("{")))
		req = withChiParam(req, "entryID", "e1")
		rr := httptest.NewRecorder()
		a.handleScheduleUpdate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}

// --- handleClocksList ---

func TestHandleClocksList(t *testing.T) {
	a, _ := newMiscAPITest(t)

	req := httptest.NewRequest("GET", "/clocks", nil)
	rr := httptest.NewRecorder()
	a.handleClocksList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["clocks"]; !ok {
		t.Fatal("expected clocks key in response")
	}
}

// --- handleClocksCreate ---

func TestHandleClocksCreate(t *testing.T) {
	a, _ := newMiscAPITest(t)

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/clocks", bytes.NewReader([]byte("{")))
		rr := httptest.NewRecorder()
		a.handleClocksCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "Test Clock"})
		req := httptest.NewRequest("POST", "/clocks", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleClocksCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("valid creation with admin", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"station_id": "s1",
			"name":       "Test Clock",
			"start_hour": 0,
			"end_hour":   1,
		})
		req := httptest.NewRequest("POST", "/clocks", bytes.NewReader(body))
		req = withAdminClaims(req)
		rr := httptest.NewRecorder()
		a.handleClocksCreate(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("got %d, want 201, body=%s", rr.Code, rr.Body.String())
		}
	})
}

// --- handleClockSimulate ---

func TestHandleClockSimulate(t *testing.T) {
	a, _ := newMiscAPITest(t)

	t.Run("missing clock_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/clocks//simulate", nil)
		// no chi param → empty clockID → 400
		rr := httptest.NewRecorder()
		a.handleClockSimulate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("nonexistent clock", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/clocks/nonexistent/simulate", nil)
		req = withChiParam(req, "clockID", "nonexistent")
		req = withAdminClaims(req)
		rr := httptest.NewRecorder()
		a.handleClockSimulate(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})
}

// --- handleAnalyticsNowPlaying ---

func TestHandleAnalyticsNowPlaying(t *testing.T) {
	a, _ := newMiscAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/analytics/now-playing", nil)
		rr := httptest.NewRecorder()
		a.handleAnalyticsNowPlaying(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("with station_id no history", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/analytics/now-playing?station_id=s1", nil)
		req = withAdminClaims(req)
		rr := httptest.NewRecorder()
		a.handleAnalyticsNowPlaying(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
		var resp map[string]any
		json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
		if resp["status"] != "idle" {
			t.Fatalf("expected status=idle, got %v", resp["status"])
		}
	})
}

// --- handleSystemStatus ---

func TestHandleSystemStatus(t *testing.T) {
	a, _ := newMiscAPITest(t)
	req := httptest.NewRequest("GET", "/system/status", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u1",
		Roles:  []string{string(models.PlatformRoleAdmin)},
	}))
	rr := httptest.NewRecorder()
	a.handleSystemStatus(rr, req)
	// Returns 200 with system status or 500 if some service unavailable
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 200 or 500", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["database"]; !ok {
		t.Fatal("expected database key in response")
	}
}

// --- handleAnalyticsListeners ---

func TestHandleAnalyticsListeners_NilBroadcast(t *testing.T) {
	a, _ := newMiscAPITest(t)
	req := httptest.NewRequest("GET", "/analytics/listeners", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsListeners(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["total"] == nil {
		t.Fatal("expected total key in response")
	}
}

// --- handleAnalyticsSpins ---

func TestHandleAnalyticsSpins(t *testing.T) {
	a, _ := newMiscAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/analytics/spins", nil)
		rr := httptest.NewRecorder()
		a.handleAnalyticsSpins(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("with station_id default range", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/analytics/spins?station_id=s1", nil)
		rr := httptest.NewRecorder()
		a.handleAnalyticsSpins(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("with station_id and since param", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/analytics/spins?station_id=s1&since=2025-01-01T00:00:00Z", nil)
		rr := httptest.NewRecorder()
		a.handleAnalyticsSpins(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

// --- handleAnalyticsNowPlaying with history ---

func TestHandleAnalyticsNowPlaying_WithHistory(t *testing.T) {
	a, db := newMiscAPITest(t)
	if err := db.AutoMigrate(&models.PlayHistory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Insert a play history record
	history := models.PlayHistory{
		ID:        "ph-1",
		StationID: "s1",
		Artist:    "Test Artist",
		Title:     "Test Title",
		StartedAt: time.Now(),
	}
	if err := db.Create(&history).Error; err != nil {
		t.Fatalf("create history: %v", err)
	}

	req := httptest.NewRequest("GET", "/analytics/now-playing?station_id=s1", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleAnalyticsNowPlaying(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["artist"] == nil {
		t.Fatal("expected artist key in response")
	}
}

// --- parseInt ---

func TestParseInt(t *testing.T) {
	v, err := parseInt("42")
	if err != nil || v != 42 {
		t.Fatalf("expected (42, nil), got (%d, %v)", v, err)
	}

	_, err = parseInt("not-a-number")
	if err == nil {
		t.Fatal("expected error for non-numeric input")
	}
}

// --- requireStationAccess variations ---

func TestRequireStationAccess_NoAuth(t *testing.T) {
	a := newMiddlewareAPITest(t)
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	result := a.requireStationAccess(rr, req, "s1")
	if result {
		t.Fatal("expected false (no auth)")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rr.Code)
	}
}

func TestRequireStationAccess_EmptyStationID(t *testing.T) {
	a := newMiddlewareAPITest(t)
	req := httptest.NewRequest("GET", "/", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	result := a.requireStationAccess(rr, req, "")
	if result {
		t.Fatal("expected false (empty stationID)")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestRequireStationAccess_PlatformAdmin(t *testing.T) {
	a := newMiddlewareAPITest(t)
	req := httptest.NewRequest("GET", "/", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	result := a.requireStationAccess(rr, req, "s1")
	if !result {
		t.Fatal("expected true for platform admin")
	}
}

func TestRequireStationAccess_ClaimsStationID(t *testing.T) {
	a := newMiddlewareAPITest(t)
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID:    "u1",
		StationID: "s1",
	}))
	rr := httptest.NewRecorder()
	result := a.requireStationAccess(rr, req, "s1")
	if !result {
		t.Fatal("expected true when claims.StationID matches")
	}
}

func TestRequireStationAccess_Forbidden(t *testing.T) {
	a := newMiddlewareAPITest(t)
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u1",
		Roles:  []string{string(models.RoleAdmin)},
	}))
	rr := httptest.NewRecorder()
	result := a.requireStationAccess(rr, req, "s1")
	if result {
		t.Fatal("expected false (no station access)")
	}
	if rr.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", rr.Code)
	}
}
