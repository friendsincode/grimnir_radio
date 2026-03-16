/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// coverage_boost3_test.go — Third coverage boost batch.
// Targets:
//   - webdj.go handleListSessions loop body (active session in DB)
//   - webdj.go handleGetSession success path (session in DB)
//   - webdj.go handleEndSession success path (session in DB)
//   - webdj.go handleGoOffAir ErrSessionNotFound path
//   - webdj.go handleGetWaveform ErrMediaNotFound path
//   - handleSystemStatus nil analyzer + nil media branches (already partially covered, verify full path)
//   - handleTestMediaEngine nil analyzer path
//   - handleExecutorStates + handleExecutorHealth with real StateManager
//   - handleLiveStartHandover invalid_priority path (passes validation up to priority check)
//   - handleLiveStartHandover with valid body + session_not_found result → result != nil branch
//   - handleStationsList success path (already at 50%, exercise other branch)

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
	"github.com/friendsincode/grimnir_radio/internal/executor"
	"github.com/friendsincode/grimnir_radio/internal/live"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
	webdjsvc "github.com/friendsincode/grimnir_radio/internal/webdj"
)

// newBoost3WebDJAPI creates a WebDJAPI with a full DB including User table for
// handleStartSession tests, and WebDJSession for session-based tests.
func newBoost3WebDJAPI(t *testing.T) (*WebDJAPI, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "boost3_webdj.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.WebDJSession{},
		&models.WaveformCache{},
		&models.MediaItem{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	bus := events.NewBus()
	svc := webdjsvc.NewService(db, nil, nil, nil, bus, zerolog.Nop())

	// Create a minimal WaveformService with just the DB so GetWaveform can hit
	// the ErrMediaNotFound path when the media item doesn't exist.
	waveformSvc := webdjsvc.NewWaveformService(db, nil, nil, t.TempDir(), zerolog.Nop())

	return &WebDJAPI{
		db:          db,
		webdjSvc:    svc,
		waveformSvc: waveformSvc,
		logger:      zerolog.Nop(),
	}, db
}

// newBoost3ExecutorAPI creates an API with a real executor.StateManager.
func newBoost3ExecutorAPI(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "boost3_executor.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.ExecutorState{},
		&models.Station{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	stateMgr := executor.NewStateManager(db, zerolog.Nop())

	return &API{
		db:               db,
		executorStateMgr: stateMgr,
		logger:           zerolog.Nop(),
	}, db
}

// newBoost3LiveHandoverAPI creates an API with a real live service for
// handleLiveStartHandover tests.
func newBoost3LiveHandoverAPI(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "boost3_live_handover.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.LiveSession{},
		&models.PrioritySource{},
		&models.Station{},
		&models.Mount{},
		&models.AuditLog{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, zerolog.Nop())
	liveSvc := live.NewService(db, prioritySvc, bus, zerolog.Nop())

	return &API{
		db:     db,
		bus:    bus,
		live:   liveSvc,
		logger: zerolog.Nop(),
	}, db
}

// --- handleListSessions loop body ---

// TestHandleWebDJListSessions_WithActiveSession verifies the loop body is exercised
// when an active session exists in the DB.
func TestHandleWebDJListSessions_WithActiveSession(t *testing.T) {
	a, db := newBoost3WebDJAPI(t)

	// Insert a session directly into the DB with active=true
	session := models.WebDJSession{
		ID:        "wdj-list-sess-1",
		StationID: "st-wdj-list-1",
		UserID:    "usr-wdj-list-1",
		Active:    true,
		CreatedAt: time.Now(),
	}
	db.Create(&session) //nolint:errcheck

	req := httptest.NewRequest("GET", "/webdj/sessions", nil)
	rr := httptest.NewRecorder()
	a.handleListSessions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list sessions: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rr.Body.String())
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 session, got %d", len(resp))
	}
	if resp[0]["id"] != "wdj-list-sess-1" {
		t.Fatalf("unexpected session id: %v", resp[0]["id"])
	}
}

// TestHandleWebDJListSessions_StationFilter verifies station_id filter with an active session.
func TestHandleWebDJListSessions_StationFilter(t *testing.T) {
	a, db := newBoost3WebDJAPI(t)

	// Two sessions from different stations
	db.Create(&models.WebDJSession{
		ID: "wdj-sf-1", StationID: "st-sf-a", UserID: "u1", Active: true, CreatedAt: time.Now(),
	}) //nolint:errcheck
	db.Create(&models.WebDJSession{
		ID: "wdj-sf-2", StationID: "st-sf-b", UserID: "u2", Active: true, CreatedAt: time.Now(),
	}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/webdj/sessions?station_id=st-sf-a", nil)
	rr := httptest.NewRecorder()
	a.handleListSessions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("filtered list: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 session for st-sf-a, got %d", len(resp))
	}
	if resp[0]["station_id"] != "st-sf-a" {
		t.Fatalf("unexpected station_id: %v", resp[0]["station_id"])
	}
}

// --- handleGetSession success path ---

// TestHandleWebDJGetSession_Found verifies 200 with session data when the session exists in DB.
func TestHandleWebDJGetSession_Found(t *testing.T) {
	a, db := newBoost3WebDJAPI(t)

	session := models.WebDJSession{
		ID:        "wdj-get-sess-1",
		StationID: "st-wdj-get-1",
		UserID:    "usr-wdj-get-1",
		Active:    true,
		CreatedAt: time.Now(),
	}
	db.Create(&session) //nolint:errcheck

	req := httptest.NewRequest("GET", "/webdj/sessions/wdj-get-sess-1", nil)
	req = withChiParam(req, "id", "wdj-get-sess-1")
	rr := httptest.NewRecorder()
	a.handleGetSession(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("get session: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["id"] != "wdj-get-sess-1" {
		t.Fatalf("unexpected session id: %v", resp["id"])
	}
}

// TestHandleWebDJGetSession_NotFound verifies 404 when session is not in DB.
func TestHandleWebDJGetSession_NotFound(t *testing.T) {
	a, _ := newBoost3WebDJAPI(t)

	req := httptest.NewRequest("GET", "/webdj/sessions/no-such-session", nil)
	req = withChiParam(req, "id", "no-such-session")
	rr := httptest.NewRecorder()
	a.handleGetSession(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("not found: got %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

// --- handleEndSession success path ---

// TestHandleWebDJEndSession_Found verifies 200 with status="ended" when session exists.
func TestHandleWebDJEndSession_Found(t *testing.T) {
	a, db := newBoost3WebDJAPI(t)

	session := models.WebDJSession{
		ID:        "wdj-end-sess-1",
		StationID: "st-wdj-end-1",
		UserID:    "usr-wdj-end-1",
		Active:    true,
		CreatedAt: time.Now(),
	}
	db.Create(&session) //nolint:errcheck

	req := httptest.NewRequest("DELETE", "/webdj/sessions/wdj-end-sess-1", nil)
	req = withChiParam(req, "id", "wdj-end-sess-1")
	rr := httptest.NewRecorder()
	a.handleEndSession(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("end session: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ended" {
		t.Fatalf("expected status=ended, got %v", resp["status"])
	}
}

// TestHandleWebDJEndSession_NotFound verifies 404 when session does not exist.
func TestHandleWebDJEndSession_NotFound(t *testing.T) {
	a, _ := newBoost3WebDJAPI(t)

	req := httptest.NewRequest("DELETE", "/webdj/sessions/no-such", nil)
	req = withChiParam(req, "id", "no-such")
	rr := httptest.NewRecorder()
	a.handleEndSession(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("not found: got %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

// --- handleGoOffAir paths ---

// TestHandleWebDJGoOffAir_SessionNotFoundBoost3 verifies 404 when session doesn't exist.
func TestHandleWebDJGoOffAir_SessionNotFoundBoost3(t *testing.T) {
	a, _ := newBoost3WebDJAPI(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/no-such/off-air", nil)
	req = withChiParam(req, "id", "no-such")
	rr := httptest.NewRecorder()
	a.handleGoOffAir(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("session not found: got %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleWebDJGoOffAir_NotLive verifies 400 when session exists but is not live.
// We insert a session as active but not in the live state.
func TestHandleWebDJGoOffAir_NotLive(t *testing.T) {
	a, db := newBoost3WebDJAPI(t)

	// Insert active session (not broadcasting live)
	session := models.WebDJSession{
		ID:        "wdj-offair-notlive",
		StationID: "st-offair-1",
		UserID:    "usr-offair-1",
		Active:    true,
		CreatedAt: time.Now(),
	}
	db.Create(&session) //nolint:errcheck

	req := httptest.NewRequest("POST", "/webdj/sessions/wdj-offair-notlive/off-air", nil)
	req = withChiParam(req, "id", "wdj-offair-notlive")
	rr := httptest.NewRecorder()
	a.handleGoOffAir(rr, req)

	// The session exists in DB but the in-memory service doesn't have it marked as live,
	// so GoOffAir will return ErrNotLive → 400
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("not live: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["error"] != "not_live" {
		t.Fatalf("expected error=not_live, got %v", resp["error"])
	}
}

// --- handleGetWaveform ErrMediaNotFound path ---

// TestHandleWebDJGetWaveform_MediaNotFound verifies 404 when media item is not in DB.
func TestHandleWebDJGetWaveform_MediaNotFound(t *testing.T) {
	a, _ := newBoost3WebDJAPI(t)

	req := httptest.NewRequest("GET", "/webdj/library/no-such-media/waveform", nil)
	req = withChiParam(req, "id", "no-such-media")
	rr := httptest.NewRecorder()
	a.handleGetWaveform(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("media not found: got %d, want 404; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["error"] != "media_not_found" {
		t.Fatalf("expected error=media_not_found, got %v", resp["error"])
	}
}

// --- handleSystemStatus paths ---

// TestHandleSystemStatus_NilAnalyzerNilMedia verifies the nil-guard branches for
// analyzer and media (the "unavailable" status paths that are currently uncovered).
func TestHandleSystemStatus_NilAnalyzerNilMedia(t *testing.T) {
	a, db := newBoost3ExecutorAPI(t)
	// a.analyzer == nil, a.media == nil — exercises the else-branches in handleSystemStatus
	_ = db

	req := httptest.NewRequest("GET", "/system/status", nil)
	rr := httptest.NewRecorder()
	a.handleSystemStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("system status: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Both media_engine and storage should be "unavailable"
	mediaEngine, _ := resp["media_engine"].(map[string]any)
	if mediaEngine["status"] != "unavailable" {
		t.Fatalf("expected media_engine=unavailable, got %v", mediaEngine["status"])
	}
	storage, _ := resp["storage"].(map[string]any)
	if storage["status"] != "unavailable" {
		t.Fatalf("expected storage=unavailable, got %v", storage["status"])
	}
}

// --- handleTestMediaEngine nil analyzer path ---

// TestHandleTestMediaEngine_NilAnalyzerBoost3 verifies 503 with success=false when analyzer is nil.
func TestHandleTestMediaEngine_NilAnalyzerBoost3(t *testing.T) {
	a := &API{logger: zerolog.Nop(), analyzer: nil}

	req := httptest.NewRequest("POST", "/system/test-media-engine", nil)
	rr := httptest.NewRecorder()
	a.handleTestMediaEngine(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil analyzer: got %d, want 503; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["success"] != false {
		t.Fatalf("expected success=false, got %v", resp["success"])
	}
}

// --- handleExecutorStates + handleExecutorHealth ---

// TestHandleExecutorStates_EmptyListBoost3 verifies 200 with empty array when no executor states.
func TestHandleExecutorStates_EmptyListBoost3(t *testing.T) {
	a, _ := newBoost3ExecutorAPI(t)

	req := httptest.NewRequest("GET", "/executor/states", nil)
	rr := httptest.NewRecorder()
	a.handleExecutorStates(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("executor states: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp []any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rr.Body.String())
	}
	if len(resp) != 0 {
		t.Fatalf("expected empty list, got %d", len(resp))
	}
}

// TestHandleExecutorHealth_EmptyListBoost3 verifies 200 with empty array from handleExecutorHealth.
func TestHandleExecutorHealth_EmptyListBoost3(t *testing.T) {
	a, _ := newBoost3ExecutorAPI(t)

	req := httptest.NewRequest("GET", "/executor/health", nil)
	rr := httptest.NewRecorder()
	a.handleExecutorHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("executor health: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp []any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rr.Body.String())
	}
	if len(resp) != 0 {
		t.Fatalf("expected empty list, got %d", len(resp))
	}
}

// --- handleLiveStartHandover paths ---

// TestHandleLiveStartHandover_InvalidJSONBoost3 verifies 400 on malformed body.
func TestHandleLiveStartHandover_InvalidJSONBoost3(t *testing.T) {
	a, _ := newBoost3LiveHandoverAPI(t)

	req := httptest.NewRequest("POST", "/live/handover", bytes.NewBufferString("{bad"))
	rr := httptest.NewRecorder()
	a.handleLiveStartHandover(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleLiveStartHandover_MissingFields verifies 400 when required fields are absent.
func TestHandleLiveStartHandover_MissingFields(t *testing.T) {
	a, _ := newBoost3LiveHandoverAPI(t)

	body, _ := json.Marshal(map[string]any{
		"session_id": "sess-1",
		// station_id, mount_id, user_id missing
	})
	req := httptest.NewRequest("POST", "/live/handover", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleLiveStartHandover(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing fields: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleLiveStartHandover_InvalidPriorityBoost3 verifies 400 when priority is not 1 or 2.
func TestHandleLiveStartHandover_InvalidPriorityBoost3(t *testing.T) {
	a, _ := newBoost3LiveHandoverAPI(t)

	body, _ := json.Marshal(map[string]any{
		"session_id": "sess-1",
		"station_id": "st-1",
		"mount_id":   "mt-1",
		"user_id":    "usr-1",
		"priority":   99, // invalid priority
	})
	req := httptest.NewRequest("POST", "/live/handover", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleLiveStartHandover(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid priority: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["error"] != "invalid_priority" {
		t.Fatalf("expected error=invalid_priority, got %v", resp["error"])
	}
}

// TestHandleLiveStartHandover_SessionNotFound verifies the result != nil branch when
// the live service returns an error with a non-nil HandoverResult (session not found).
func TestHandleLiveStartHandover_SessionNotFound(t *testing.T) {
	a, _ := newBoost3LiveHandoverAPI(t)

	// Valid priority (1 = LiveOverride), but session doesn't exist in DB.
	// live.StartHandover will return a non-nil result with an error.
	body, _ := json.Marshal(map[string]any{
		"session_id": "nonexistent-session",
		"station_id": "st-1",
		"mount_id":   "mt-1",
		"user_id":    "usr-1",
		"priority":   int(models.PriorityLiveOverride), // 1
	})
	req := httptest.NewRequest("POST", "/live/handover", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleLiveStartHandover(rr, req)

	// Should be 500 with a JSON body (the result != nil branch)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("session not found result: got %d, want 500; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rr.Body.String())
	}
	// The response should have success=false from the HandoverResult
	if resp["success"] != false {
		t.Fatalf("expected success=false, got %v", resp["success"])
	}
}

// TestHandleLiveStartHandover_RollbackFalse verifies that rollback_on_error=false
// takes the rollbackOnError=false branch (passes validation, still hits session not found).
func TestHandleLiveStartHandover_RollbackFalse(t *testing.T) {
	a, _ := newBoost3LiveHandoverAPI(t)

	body, _ := json.Marshal(map[string]any{
		"session_id":        "nonexistent-session-2",
		"station_id":        "st-2",
		"mount_id":          "mt-2",
		"user_id":           "usr-2",
		"priority":          int(models.PriorityLiveScheduled), // 2
		"rollback_on_error": false,
	})
	req := httptest.NewRequest("POST", "/live/handover", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleLiveStartHandover(rr, req)

	// Will fail at the live service level (session not found) → 500 with result body
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("rollback_false path: got %d, want 500; body=%s", rr.Code, rr.Body.String())
	}
}
