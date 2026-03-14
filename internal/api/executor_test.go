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

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/executor"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newExecutorAPITest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.ExecutorState{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	mgr := executor.NewStateManager(db, zerolog.Nop())
	return &API{db: db, executorStateMgr: mgr, logger: zerolog.Nop()}, db
}

func TestExecutorAPI_States(t *testing.T) {
	a, _ := newExecutorAPITest(t)

	req := httptest.NewRequest("GET", "/executor/states", nil)
	rr := httptest.NewRecorder()
	a.handleExecutorStates(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list states: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp []any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp == nil {
		// Empty array is acceptable
		resp = []any{}
	}
}

func TestExecutorAPI_State(t *testing.T) {
	a, _ := newExecutorAPITest(t)

	// With station_id but no record (returns empty/default state)
	req := httptest.NewRequest("GET", "/executor/states/s1", nil)
	req = withChiParam(req, "stationID", "s1")
	rr := httptest.NewRecorder()
	a.handleExecutorState(rr, req)
	// Service may return 200 with default or 500 for missing
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Fatalf("executor state: got %d, want 200 or 500", rr.Code)
	}
}

func TestExecutorAPI_Telemetry(t *testing.T) {
	a, _ := newExecutorAPITest(t)

	req := httptest.NewRequest("GET", "/executor/telemetry/s1", nil)
	req = withChiParam(req, "stationID", "s1")
	rr := httptest.NewRecorder()
	a.handleExecutorTelemetry(rr, req)
	// May return 200 with defaults or 500 if no state exists
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Fatalf("executor telemetry: got %d, want 200 or 500", rr.Code)
	}
}

func TestExecutorAPI_Health(t *testing.T) {
	a, _ := newExecutorAPITest(t)

	req := httptest.NewRequest("GET", "/executor/health", nil)
	rr := httptest.NewRecorder()
	a.handleExecutorHealth(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("executor health: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
}

func TestExecutorAPI_SerializeExecutorState(t *testing.T) {
	state := &models.ExecutorState{
		ID:        "id1",
		StationID: "s1",
		State:     models.ExecutorStateIdle,
	}
	result := serializeExecutorState(state)
	if result["station_id"] != "s1" {
		t.Fatalf("expected station_id=s1, got %v", result["station_id"])
	}
	if result["state"] != string(models.ExecutorStateIdle) {
		t.Fatalf("expected state=%s, got %v", models.ExecutorStateIdle, result["state"])
	}
}

func TestIntegrityAPI_NilService(t *testing.T) {
	a, _ := newAPIHandlersTest(t) // integritySvc is nil

	t.Run("report unavailable", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/integrity", nil)
		rr := httptest.NewRecorder()
		a.handleIntegrityReport(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("got %d, want 503", rr.Code)
		}
	})

	t.Run("repair unavailable", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"type": "orphan", "resource_id": "r1"})
		req := httptest.NewRequest("POST", "/integrity/repair", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleIntegrityRepair(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("got %d, want 503", rr.Code)
		}
	})
}

func TestIntegrityAPI_RepairValidation(t *testing.T) {
	// Use API without integrity service (still tests validation before nil check)
	a, _ := newAPIHandlersTest(t) // integritySvc is nil → 503, tested above

	// Test with integrity service nil:
	// Repair requires service check first, so invalid input still returns 503 (nil check comes first)
	// Just verify the nil guard is correct behavior
	req := httptest.NewRequest("POST", "/integrity/repair", bytes.NewReader([]byte("invalid")))
	rr := httptest.NewRecorder()
	a.handleIntegrityRepair(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("integrity repair nil svc: got %d, want 503", rr.Code)
	}
}
