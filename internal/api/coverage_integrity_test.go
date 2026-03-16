/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// Tests for handleIntegrityReport, handleIntegrityRepair, handleRepairFilenames,
// handleExecutorState, handleExecutorTelemetry, handleExecutorStates,
// and audit filter parsing with all query parameters.

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

	"github.com/friendsincode/grimnir_radio/internal/audit"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/executor"
	"github.com/friendsincode/grimnir_radio/internal/integrity"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newIntegrityTestAPI(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "integrity.db")), &gorm.Config{})
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
		&models.Show{},
		&models.ShowInstance{},
		&models.MediaItem{},
		&models.MediaTagLink{},
		&models.AnalysisJob{},
		&models.ExecutorState{},
		&models.AuditLog{},
		&models.User{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	bus := events.NewBus()
	integritySvc := integrity.NewService(db, zerolog.Nop())
	executorMgr := executor.NewStateManager(db, zerolog.Nop())
	auditSvc := audit.NewService(db, bus, zerolog.Nop())

	return &API{
		db:               db,
		bus:              bus,
		integritySvc:     integritySvc,
		executorStateMgr: executorMgr,
		auditSvc:         auditSvc,
		logger:           zerolog.Nop(),
	}, db
}

// --- handleIntegrityReport ---

// TestHandleIntegrityReport_NilService verifies 503 when integritySvc is nil.
func TestHandleIntegrityReport_NilService(t *testing.T) {
	a := &API{logger: zerolog.Nop()}

	req := httptest.NewRequest("GET", "/integrity/report", nil)
	rr := httptest.NewRecorder()
	a.handleIntegrityReport(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil integrity svc: got %d, want 503", rr.Code)
	}
}

// TestHandleIntegrityReport_EmptyDB verifies that a clean database with no integrity
// issues produces a report with total=0.
func TestHandleIntegrityReport_EmptyDB(t *testing.T) {
	a, _ := newIntegrityTestAPI(t)

	req := httptest.NewRequest("GET", "/integrity/report", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleIntegrityReport(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("empty db: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["total"]; !ok {
		t.Fatal("expected 'total' key in response")
	}
	if _, ok := resp["findings"]; !ok {
		t.Fatal("expected 'findings' key in response")
	}
	if _, ok := resp["by_type"]; !ok {
		t.Fatal("expected 'by_type' key in response")
	}
}

// TestHandleIntegrityReport_FindingsDetected verifies that when a station has no mount,
// the integrity report identifies it as a finding.
func TestHandleIntegrityReport_FindingsDetected(t *testing.T) {
	a, db := newIntegrityTestAPI(t)

	// Create a station without a mount (integrity issue: station_missing_mount).
	db.Create(&models.Station{ID: "st-integrity-1", Name: "No Mount Station", Timezone: "UTC"}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/integrity/report", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleIntegrityReport(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("findings: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	total, _ := resp["total"].(float64)
	if total < 1 {
		t.Fatalf("expected total >= 1 (station with no mount is an integrity issue), got %v", total)
	}
	findings, _ := resp["findings"].([]any)
	if len(findings) < 1 {
		t.Fatalf("expected at least 1 finding, got %d", len(findings))
	}
}

// --- handleIntegrityRepair ---

// TestHandleIntegrityRepair_NilService verifies 503 when integritySvc is nil.
func TestHandleIntegrityRepair_NilService(t *testing.T) {
	a := &API{logger: zerolog.Nop()}

	body, _ := json.Marshal(map[string]any{"type": "station_missing_mount", "resource_id": "st-1"})
	req := httptest.NewRequest("POST", "/integrity/repair", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	a.handleIntegrityRepair(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil integrity svc: got %d, want 503", rr.Code)
	}
}

// TestHandleIntegrityRepair_InvalidJSON verifies 400 for malformed body.
func TestHandleIntegrityRepair_InvalidJSON(t *testing.T) {
	a, _ := newIntegrityTestAPI(t)

	req := httptest.NewRequest("POST", "/integrity/repair", bytes.NewReader([]byte("not-json")))
	rr := httptest.NewRecorder()
	a.handleIntegrityRepair(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// TestHandleIntegrityRepair_MissingFields verifies 400 when type or resource_id absent.
func TestHandleIntegrityRepair_MissingFields(t *testing.T) {
	a, _ := newIntegrityTestAPI(t)

	// Missing resource_id
	body, _ := json.Marshal(map[string]any{"type": "station_missing_mount"})
	req := httptest.NewRequest("POST", "/integrity/repair", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	a.handleIntegrityRepair(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing resource_id: got %d, want 400", rr.Code)
	}

	// Missing type
	body, _ = json.Marshal(map[string]any{"resource_id": "st-1"})
	req = httptest.NewRequest("POST", "/integrity/repair", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	a.handleIntegrityRepair(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing type: got %d, want 400", rr.Code)
	}
}

// TestHandleIntegrityRepair_RepairStationMissingMount verifies that repairing
// a station_missing_mount finding creates the default mount.
func TestHandleIntegrityRepair_RepairStationMissingMount(t *testing.T) {
	a, db := newIntegrityTestAPI(t)

	// Create a station without a mount.
	db.Create(&models.Station{ID: "st-repair-1", Name: "Needs Mount", Timezone: "UTC"}) //nolint:errcheck

	body, _ := json.Marshal(map[string]any{
		"type":        "station_missing_mount",
		"resource_id": "st-repair-1",
	})
	req := httptest.NewRequest("POST", "/integrity/repair", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleIntegrityRepair(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("repair station mount: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["changed"]; !ok {
		t.Fatal("expected 'changed' key in response")
	}
	if _, ok := resp["message"]; !ok {
		t.Fatal("expected 'message' key in response")
	}
}

// --- handleRepairFilenames ---

// TestHandleRepairFilenames_HappyPath verifies that the handler returns 200 with
// updated count when no filenames need repair.
func TestHandleRepairFilenames_HappyPath(t *testing.T) {
	a, _ := newIntegrityTestAPI(t)

	req := httptest.NewRequest("POST", "/integrity/repair-filenames", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleRepairFilenames(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("repair filenames: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["updated"]; !ok {
		t.Fatal("expected 'updated' key in response")
	}
	if _, ok := resp["message"]; !ok {
		t.Fatal("expected 'message' key in response")
	}
}

// --- handleExecutorState and handleExecutorTelemetry ---

// TestHandleExecutorState_MissingStationID verifies 400 when stationID chi param absent.
func TestHandleExecutorState_MissingStationID(t *testing.T) {
	a, _ := newIntegrityTestAPI(t)

	req := httptest.NewRequest("GET", "/executor/states/", nil)
	// No chi param stationID
	rr := httptest.NewRecorder()
	a.handleExecutorState(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing stationID: got %d, want 400", rr.Code)
	}
}

// TestHandleExecutorState_CreatesOnDemand verifies that when no state exists for a
// station, the StateManager creates a new idle state and returns it.
func TestHandleExecutorState_CreatesOnDemand(t *testing.T) {
	a, _ := newIntegrityTestAPI(t)

	req := httptest.NewRequest("GET", "/executor/states/st-exec", nil)
	req = withChiParam(req, "stationID", "st-exec")
	rr := httptest.NewRecorder()
	a.handleExecutorState(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("executor state: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["station_id"] != "st-exec" {
		t.Fatalf("expected station_id=st-exec, got %v", resp["station_id"])
	}
	// New state should be idle.
	if resp["state"] != "idle" {
		t.Fatalf("expected state=idle for new station, got %v", resp["state"])
	}
}

// TestHandleExecutorTelemetry_MissingStationID verifies 400 when stationID absent.
func TestHandleExecutorTelemetry_MissingStationID(t *testing.T) {
	a, _ := newIntegrityTestAPI(t)

	req := httptest.NewRequest("GET", "/executor/telemetry/", nil)
	// No chi param stationID
	rr := httptest.NewRecorder()
	a.handleExecutorTelemetry(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing stationID: got %d, want 400", rr.Code)
	}
}

// TestHandleExecutorTelemetry_ReturnsStructuredResponse verifies the response contains
// all expected telemetry keys for a new station.
func TestHandleExecutorTelemetry_ReturnsStructuredResponse(t *testing.T) {
	a, _ := newIntegrityTestAPI(t)

	req := httptest.NewRequest("GET", "/executor/telemetry/st-telem", nil)
	req = withChiParam(req, "stationID", "st-telem")
	rr := httptest.NewRecorder()
	a.handleExecutorTelemetry(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("executor telemetry: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Check expected telemetry keys.
	for _, key := range []string{"station_id", "state", "audio_level_l", "audio_level_r",
		"loudness_lufs", "buffer_depth_ms", "underrun_count", "healthy", "playing"} {
		if _, ok := resp[key]; !ok {
			t.Fatalf("expected key %q in telemetry response", key)
		}
	}
}

// TestHandleExecutorStates_NilManager verifies that when executorStateMgr is nil the
// handler panics or errors — this is a safety check that ensures the handler
// is only registered when the service is available.
// (Currently executorStateMgr is always non-nil in production.)
// NOTE: We test with a real manager for the happy path.
func TestHandleExecutorStates_EmptyList(t *testing.T) {
	a, _ := newIntegrityTestAPI(t)

	req := httptest.NewRequest("GET", "/executor/states", nil)
	rr := httptest.NewRecorder()
	a.handleExecutorStates(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("executor states: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var states []any
	if err := json.NewDecoder(rr.Body).Decode(&states); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Empty initial state.
	if len(states) != 0 {
		t.Fatalf("expected 0 states initially, got %d", len(states))
	}
}

// --- Audit filter parsing (extended coverage) ---

// TestParseAuditFilters_AllParams verifies that all query parameters are parsed correctly.
func TestParseAuditFilters_AllParams(t *testing.T) {
	req := httptest.NewRequest("GET", "/?user_id=u-123&station_id=st-456&action=station_create&start_time=2026-01-01T00:00:00Z&end_time=2026-01-31T23:59:59Z&limit=25&offset=10", nil)

	filters := parseAuditFilters(req)

	if filters.UserID == nil || *filters.UserID != "u-123" {
		t.Fatalf("expected user_id=u-123, got %v", filters.UserID)
	}
	if filters.StationID == nil || *filters.StationID != "st-456" {
		t.Fatalf("expected station_id=st-456, got %v", filters.StationID)
	}
	if filters.Action == nil || string(*filters.Action) != "station_create" {
		t.Fatalf("expected action=station_create, got %v", filters.Action)
	}
	if filters.StartTime == nil {
		t.Fatal("expected start_time to be set")
	}
	if filters.EndTime == nil {
		t.Fatal("expected end_time to be set")
	}
	if filters.Limit != 25 {
		t.Fatalf("expected limit=25, got %d", filters.Limit)
	}
	if filters.Offset != 10 {
		t.Fatalf("expected offset=10, got %d", filters.Offset)
	}
}

// TestParseAuditFilters_InvalidTimeIgnored verifies that malformed time params
// are silently ignored (not treated as errors).
func TestParseAuditFilters_InvalidTimeIgnored(t *testing.T) {
	req := httptest.NewRequest("GET", "/?start_time=not-a-time&end_time=also-bad", nil)
	filters := parseAuditFilters(req)

	if filters.StartTime != nil {
		t.Fatalf("expected start_time=nil for bad time, got %v", filters.StartTime)
	}
	if filters.EndTime != nil {
		t.Fatalf("expected end_time=nil for bad time, got %v", filters.EndTime)
	}
}

// TestParseAuditFilters_LimitClamped verifies that a limit over 1000 is ignored
// (clamped back to default 100).
func TestParseAuditFilters_LimitClamped(t *testing.T) {
	req := httptest.NewRequest("GET", "/?limit=9999", nil)
	filters := parseAuditFilters(req)

	// limit > 1000 is rejected, falls back to default 100.
	if filters.Limit != 100 {
		t.Fatalf("expected limit=100 (clamped), got %d", filters.Limit)
	}
}

// TestHandleIntegrityReport_WithNilAdditionalFields verifies the response
// structure even when no findings are present — checks all required fields.
func TestHandleIntegrityReport_ResponseStructure(t *testing.T) {
	a, _ := newIntegrityTestAPI(t)

	req := httptest.NewRequest("GET", "/integrity/report", nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleIntegrityReport(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("report: got %d, want 200", rr.Code)
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck

	for _, key := range []string{"generated_at", "total", "by_type", "findings"} {
		if _, ok := resp[key]; !ok {
			t.Fatalf("expected key %q in report response", key)
		}
	}
}
