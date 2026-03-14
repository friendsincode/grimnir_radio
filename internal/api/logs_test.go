/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/logbuffer"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newLogsAPITest(t *testing.T) (*API, *logbuffer.Buffer) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	buf := logbuffer.New(100)
	return &API{db: db, logger: zerolog.Nop(), logBuffer: buf}, buf
}

// --- handleLogComponents ---

func TestHandleLogComponents(t *testing.T) {
	a, _ := newLogsAPITest(t)
	req := httptest.NewRequest("GET", "/logs/components", nil)
	rr := httptest.NewRecorder()
	a.handleLogComponents(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["components"]; !ok {
		t.Fatal("expected components key")
	}
}

// --- handleLogStats ---

func TestHandleLogStats(t *testing.T) {
	a, _ := newLogsAPITest(t)
	req := httptest.NewRequest("GET", "/logs/stats", nil)
	rr := httptest.NewRecorder()
	a.handleLogStats(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
}

// --- handleClearLogs ---

func TestHandleClearLogs(t *testing.T) {
	a, _ := newLogsAPITest(t)
	req := httptest.NewRequest("POST", "/logs/clear", nil)
	rr := httptest.NewRecorder()
	a.handleClearLogs(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["success"] != true {
		t.Fatalf("expected success=true, got %v", resp["success"])
	}
}

// --- handleSystemLogs ---

func TestHandleSystemLogs_WithBuffer(t *testing.T) {
	a, _ := newLogsAPITest(t)

	t.Run("no params defaults", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/logs", nil)
		rr := httptest.NewRecorder()
		a.handleSystemLogs(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
		var resp map[string]any
		json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
		if _, ok := resp["entries"]; !ok {
			t.Fatal("expected entries key")
		}
		if _, ok := resp["count"]; !ok {
			t.Fatal("expected count key")
		}
	})

	t.Run("with query params", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/logs?level=error&component=api&limit=10&order=asc&since=2025-01-01T00:00:00Z", nil)
		rr := httptest.NewRecorder()
		a.handleSystemLogs(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
	})

	t.Run("invalid limit ignored", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/logs?limit=invalid", nil)
		rr := httptest.NewRecorder()
		a.handleSystemLogs(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
	})
}

// --- handleStationLogs ---

func TestHandleStationLogs_WithBuffer(t *testing.T) {
	a, _ := newLogsAPITest(t)

	t.Run("missing station_id chi param", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/stations//logs", nil)
		// no chi param → empty stationID → 400
		rr := httptest.NewRecorder()
		a.handleStationLogs(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("with station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/stations/s1/logs", nil)
		req = withChiParam(req, "stationID", "s1")
		rr := httptest.NewRecorder()
		a.handleStationLogs(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
		var resp map[string]any
		json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
		if _, ok := resp["entries"]; !ok {
			t.Fatal("expected entries key")
		}
		if resp["station_id"] != "s1" {
			t.Fatalf("expected station_id=s1, got %v", resp["station_id"])
		}
	})

	t.Run("with all query params", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/stations/s1/logs?level=warn&component=scheduler&limit=5&order=asc&since=2025-01-01T00:00:00Z", nil)
		req = withChiParam(req, "stationID", "s1")
		rr := httptest.NewRecorder()
		a.handleStationLogs(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
	})
}

// --- handleStationLogComponents ---

func TestHandleStationLogComponents_WithBuffer(t *testing.T) {
	a, _ := newLogsAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/stations//logs/components", nil)
		rr := httptest.NewRecorder()
		a.handleStationLogComponents(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("with station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/stations/s1/logs/components", nil)
		req = withChiParam(req, "stationID", "s1")
		rr := httptest.NewRecorder()
		a.handleStationLogComponents(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
		var resp map[string]any
		json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
		if _, ok := resp["components"]; !ok {
			t.Fatal("expected components key")
		}
	})
}

// --- handleStationLogStats ---

func TestHandleStationLogStats_WithBuffer(t *testing.T) {
	a, _ := newLogsAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/stations//logs/stats", nil)
		rr := httptest.NewRecorder()
		a.handleStationLogStats(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("with station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/stations/s1/logs/stats", nil)
		req = withChiParam(req, "stationID", "s1")
		rr := httptest.NewRecorder()
		a.handleStationLogStats(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
	})
}
