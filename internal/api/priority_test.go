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

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
)

func newPriorityAPITest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.PrioritySource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	bus := events.NewBus()
	svc := priority.NewService(db, bus, zerolog.Nop())
	return &API{db: db, bus: bus, prioritySvc: svc, logger: zerolog.Nop()}, db
}

func TestPriorityAPI_EmergencyValidation(t *testing.T) {
	a, _ := newPriorityAPITest(t)

	// Invalid JSON → 400
	req := httptest.NewRequest("POST", "/priority/emergency", bytes.NewReader([]byte("invalid")))
	rr := httptest.NewRecorder()
	a.handlePriorityEmergency(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}

	// Missing station_id → 400
	body, _ := json.Marshal(map[string]any{"media_id": "m1"})
	req = httptest.NewRequest("POST", "/priority/emergency", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	a.handlePriorityEmergency(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}

	// Missing media_id → 400
	body, _ = json.Marshal(map[string]any{"station_id": "s1"})
	req = httptest.NewRequest("POST", "/priority/emergency", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	a.handlePriorityEmergency(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing media_id: got %d, want 400", rr.Code)
	}
}

func TestPriorityAPI_OverrideValidation(t *testing.T) {
	a, _ := newPriorityAPITest(t)

	// Invalid JSON → 400
	req := httptest.NewRequest("POST", "/priority/override", bytes.NewReader([]byte("invalid")))
	rr := httptest.NewRecorder()
	a.handlePriorityOverride(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}

	// Missing station_id → 400
	body, _ := json.Marshal(map[string]any{"source_id": "src1", "source_type": "live"})
	req = httptest.NewRequest("POST", "/priority/override", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	a.handlePriorityOverride(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}

	// Invalid source_type → 400
	body, _ = json.Marshal(map[string]any{"station_id": "s1", "source_id": "src1", "source_type": "unknown"})
	req = httptest.NewRequest("POST", "/priority/override", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	a.handlePriorityOverride(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid source_type: got %d, want 400", rr.Code)
	}
}

func TestPriorityAPI_ReleaseValidation(t *testing.T) {
	a, _ := newPriorityAPITest(t)

	// Missing station_id in body → 400
	body, _ := json.Marshal(map[string]any{})
	req := httptest.NewRequest("DELETE", "/priority/src1", bytes.NewReader(body))
	req = withChiParam(req, "sourceID", "src1")
	rr := httptest.NewRecorder()
	a.handlePriorityRelease(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}

	// Invalid JSON → 400
	req = httptest.NewRequest("DELETE", "/priority/src1", bytes.NewReader([]byte("invalid")))
	req = withChiParam(req, "sourceID", "src1")
	rr = httptest.NewRecorder()
	a.handlePriorityRelease(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

func TestPriorityAPI_CurrentValidation(t *testing.T) {
	a, _ := newPriorityAPITest(t)

	// Missing station_id → 400
	req := httptest.NewRequest("GET", "/priority/current", nil)
	rr := httptest.NewRecorder()
	a.handlePriorityCurrent(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}

	// With station_id but no active source → service returns error → 500
	req = httptest.NewRequest("GET", "/priority/current?station_id=s1", nil)
	rr = httptest.NewRecorder()
	a.handlePriorityCurrent(rr, req)
	// 200 if automation source exists; 500 if DB is empty — both are valid responses
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Fatalf("current: got %d, want 200 or 500", rr.Code)
	}
}

func TestPriorityAPI_ActiveValidation(t *testing.T) {
	a, _ := newPriorityAPITest(t)

	// Missing station_id → 400
	req := httptest.NewRequest("GET", "/priority/active", nil)
	rr := httptest.NewRecorder()
	a.handlePriorityActive(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}

	// With station_id → 200 with empty active sources
	req = httptest.NewRequest("GET", "/priority/active?station_id=s1", nil)
	rr = httptest.NewRecorder()
	a.handlePriorityActive(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("active: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
}
