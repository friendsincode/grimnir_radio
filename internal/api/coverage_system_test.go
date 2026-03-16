/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// Tests for handleSystemStatus, handleTestMediaEngine, handleReanalyzeMissingArtwork,
// handlePlayoutReload, handlePlayoutSkip, handlePlayoutStop,
// handleAuditList, handleStationAuditList, and handleExecutorHealth.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/audit"
	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/executor"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/playout"
)

// newSystemTestAPI creates a minimal API for system-handler tests.
func newSystemTestAPI(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "system.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.AuditLog{},
		&models.User{},
		&models.MediaItem{},
		&models.AnalysisJob{},
		&models.ExecutorState{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	bus := events.NewBus()
	auditSvc := audit.NewService(db, bus, zerolog.Nop())
	executorMgr := executor.NewStateManager(db, zerolog.Nop())

	return &API{
		db:               db,
		bus:              bus,
		auditSvc:         auditSvc,
		executorStateMgr: executorMgr,
		logger:           zerolog.Nop(),
		// analyzer, media, playout all nil (tests the nil-guard branches)
	}, db
}

// --- handleSystemStatus ---

// TestHandleSystemStatus_NilServices verifies that handleSystemStatus returns 200
// even when analyzer and media services are nil (nil-guard branches exercised).
func TestHandleSystemStatus_NilServices(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	req := httptest.NewRequest("GET", "/system/status", nil)
	rr := httptest.NewRecorder()
	a.handleSystemStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("system status nil services: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Database should show "ok" since SQLite is connected.
	dbStatus, ok := resp["database"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'database' key in response; got %+v", resp)
	}
	if dbStatus["status"] != "ok" {
		t.Fatalf("expected database status=ok, got %v", dbStatus["status"])
	}

	// Media engine and storage should be "unavailable" (nil services).
	meStatus, ok := resp["media_engine"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'media_engine' key in response")
	}
	if meStatus["status"] != "unavailable" {
		t.Fatalf("expected media_engine status=unavailable, got %v", meStatus["status"])
	}

	storageStatus, ok := resp["storage"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'storage' key in response")
	}
	if storageStatus["status"] != "unavailable" {
		t.Fatalf("expected storage status=unavailable, got %v", storageStatus["status"])
	}
}

// --- handleTestMediaEngine ---

// TestHandleTestMediaEngine_NilAnalyzer verifies that the handler returns 503
// when the analyzer service is nil.
func TestHandleTestMediaEngine_NilAnalyzer2(t *testing.T) {
	a, _ := newSystemTestAPI(t)
	// analyzer is nil

	req := httptest.NewRequest("POST", "/system/test-media-engine", nil)
	rr := httptest.NewRecorder()
	a.handleTestMediaEngine(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil analyzer: got %d, want 503; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["success"] != false {
		t.Fatalf("expected success=false, got %v", resp["success"])
	}
}

// --- handleReanalyzeMissingArtwork ---

// TestHandleReanalyzeMissingArtwork_NilAnalyzer verifies that the handler returns
// 503 when the analyzer service is nil.
func TestHandleReanalyzeMissingArtwork_NilAnalyzer2(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	req := httptest.NewRequest("POST", "/system/reanalyze-artwork", nil)
	rr := httptest.NewRecorder()
	a.handleReanalyzeMissingArtwork(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil analyzer: got %d, want 503; body=%s", rr.Code, rr.Body.String())
	}
}

// --- handlePlayoutReload ---

// TestHandlePlayoutReload_InvalidJSON verifies 400 on malformed body.
func TestHandlePlayoutReload_InvalidJSON(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	req := httptest.NewRequest("POST", "/playout/reload", bytes.NewBufferString("{bad"))
	rr := httptest.NewRecorder()
	a.handlePlayoutReload(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// TestHandlePlayoutReload_MissingMountID verifies 400 when mount_id absent.
func TestHandlePlayoutReload_MissingMountID(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	body, _ := json.Marshal(map[string]any{"launch": "auto"})
	req := httptest.NewRequest("POST", "/playout/reload", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handlePlayoutReload(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing mount_id: got %d, want 400", rr.Code)
	}
}

// TestHandlePlayoutReload_MissingLaunch verifies 400 when launch absent.
func TestHandlePlayoutReload_MissingLaunch(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	body, _ := json.Marshal(map[string]any{"mount_id": "mt1"})
	req := httptest.NewRequest("POST", "/playout/reload", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handlePlayoutReload(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing launch: got %d, want 400", rr.Code)
	}
}

// --- handlePlayoutSkip ---

// TestHandlePlayoutSkip_InvalidJSON verifies 400 on malformed body.
func TestHandlePlayoutSkip_InvalidJSON(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	req := httptest.NewRequest("POST", "/playout/skip", bytes.NewBufferString("{bad"))
	rr := httptest.NewRecorder()
	a.handlePlayoutSkip(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// TestHandlePlayoutSkip_MissingMountID verifies 400 when mount_id absent.
func TestHandlePlayoutSkip_MissingMountID(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	body, _ := json.Marshal(map[string]any{"station_id": "st1"}) // no mount_id
	req := httptest.NewRequest("POST", "/playout/skip", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handlePlayoutSkip(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing mount_id: got %d, want 400", rr.Code)
	}
}

// --- handlePlayoutStop ---

// TestHandlePlayoutStop_InvalidJSON verifies 400 on malformed body.
func TestHandlePlayoutStop_InvalidJSON(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	req := httptest.NewRequest("POST", "/playout/stop", bytes.NewBufferString("{bad"))
	rr := httptest.NewRecorder()
	a.handlePlayoutStop(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// TestHandlePlayoutStop_MissingMountID verifies 400 when mount_id absent.
func TestHandlePlayoutStop_MissingMountID(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	body, _ := json.Marshal(map[string]any{})
	req := httptest.NewRequest("POST", "/playout/stop", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handlePlayoutStop(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing mount_id: got %d, want 400", rr.Code)
	}
}

// --- handleAuditList ---

// TestHandleAuditList_EmptyDB verifies 200 with empty audit_logs array when no logs exist.
func TestHandleAuditList_EmptyDB(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	req := httptest.NewRequest("GET", "/audit", nil)
	rr := httptest.NewRecorder()
	a.handleAuditList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("empty audit logs: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["audit_logs"]; !ok {
		t.Fatal("expected 'audit_logs' key in response")
	}
	if resp["total"].(float64) != 0 {
		t.Fatalf("expected total=0, got %v", resp["total"])
	}
}

// TestHandleAuditList_WithFilters verifies that query params are accepted without error.
func TestHandleAuditList_WithFilters(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	req := httptest.NewRequest("GET", "/audit?limit=10&offset=0&action=integrity_scan", nil)
	rr := httptest.NewRecorder()
	a.handleAuditList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("filtered audit: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleAuditList_UserIDFilter verifies that user_id query param is accepted.
func TestHandleAuditList_UserIDFilter(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	req := httptest.NewRequest("GET", "/audit?user_id=u1", nil)
	rr := httptest.NewRecorder()
	a.handleAuditList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("user_id filter: got %d, want 200", rr.Code)
	}
}

// TestHandleAuditList_WithTimeFilters verifies that start_time and end_time params are parsed.
func TestHandleAuditList_WithTimeFilters(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	req := httptest.NewRequest("GET", "/audit?start_time=2026-01-01T00:00:00Z&end_time=2026-12-31T23:59:59Z", nil)
	rr := httptest.NewRecorder()
	a.handleAuditList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("time filter: got %d, want 200", rr.Code)
	}
}

// TestHandleAuditList_ResponseContainsLimitOffset verifies the response includes limit and offset.
func TestHandleAuditList_ResponseContainsLimitOffset(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	req := httptest.NewRequest("GET", "/audit?limit=25&offset=5", nil)
	rr := httptest.NewRecorder()
	a.handleAuditList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["limit"].(float64) != 25 {
		t.Fatalf("expected limit=25, got %v", resp["limit"])
	}
	if resp["offset"].(float64) != 5 {
		t.Fatalf("expected offset=5, got %v", resp["offset"])
	}
}

// --- handleStationAuditList ---

// TestHandleStationAuditList_MissingStationID verifies 400 when chi stationID param absent.
func TestHandleStationAuditList_MissingStationID(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	req := httptest.NewRequest("GET", "/stations//audit", nil)
	// No stationID chi param
	rr := httptest.NewRecorder()
	a.handleStationAuditList(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing stationID: got %d, want 400", rr.Code)
	}
}

// TestHandleStationAuditList_WithStationID verifies 200 when station_id is provided.
func TestHandleStationAuditList_WithStationID(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	req := httptest.NewRequest("GET", "/stations/st1/audit", nil)
	req = withChiParam(req, "stationID", "st1")
	rr := httptest.NewRecorder()
	a.handleStationAuditList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("station audit: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["station_id"] != "st1" {
		t.Fatalf("expected station_id=st1, got %v", resp["station_id"])
	}
}

// --- handleExecutorHealth ---

// TestHandleExecutorHealth_EmptyList verifies 200 with empty health array.
func TestHandleExecutorHealth_EmptyList(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	req := httptest.NewRequest("GET", "/executor/health", nil)
	rr := httptest.NewRecorder()
	a.handleExecutorHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("empty health: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp []any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(resp))
	}
}

// TestHandleExecutorHealth_WithState verifies health response fields when a state exists.
func TestHandleExecutorHealth_WithState(t *testing.T) {
	a, _ := newSystemTestAPI(t)

	// Seed a state via GetState (creates on demand).
	ctx := context.Background()
	_, err := a.executorStateMgr.GetState(ctx, "st-health-1")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}

	req := httptest.NewRequest("GET", "/executor/health", nil)
	rr := httptest.NewRecorder()
	a.handleExecutorHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("health with states: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 state, got %d", len(resp))
	}
	if _, ok := resp[0]["station_id"]; !ok {
		t.Fatal("expected 'station_id' key in health entry")
	}
	if _, ok := resp[0]["healthy"]; !ok {
		t.Fatal("expected 'healthy' key in health entry")
	}
}

// --- handlePlayoutSkip / handlePlayoutStop with real playout manager ---

// newPlayoutTestAPI creates an API with a real playout manager.
func newPlayoutTestAPI(t *testing.T) *API {
	t.Helper()
	mgr := playout.NewManager(&config.Config{}, zerolog.Nop())
	return &API{
		playout: mgr,
		bus:     events.NewBus(),
		logger:  zerolog.Nop(),
	}
}

// TestHandlePlayoutSkip_Success verifies 200 and status=skipped when mount has no active pipeline.
func TestHandlePlayoutSkip_Success(t *testing.T) {
	a := newPlayoutTestAPI(t)

	body, _ := json.Marshal(map[string]any{"mount_id": "mt-skip-1", "station_id": "st1"})
	req := httptest.NewRequest("POST", "/playout/skip", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handlePlayoutSkip(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("skip: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["status"] != "skipped" {
		t.Fatalf("expected status=skipped, got %v", resp["status"])
	}
}

// TestHandlePlayoutStop_Success verifies 200 and status=stopped when mount has no active pipeline.
func TestHandlePlayoutStop_Success(t *testing.T) {
	a := newPlayoutTestAPI(t)

	body, _ := json.Marshal(map[string]any{"mount_id": "mt-stop-1"})
	req := httptest.NewRequest("POST", "/playout/stop", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handlePlayoutStop(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("stop: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["status"] != "stopped" {
		t.Fatalf("expected status=stopped, got %v", resp["status"])
	}
}
