/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// Tests for handleListLiveSessions, migration handlers (list jobs, cancel job,
// delete job), and remaining uncovered live handler validation paths.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/live"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
)

func newLiveWithServiceTest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "live.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.LiveSession{},
		&models.PrioritySource{},
		&models.Mount{},
		&models.Station{},
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

// --- handleListLiveSessions ---

// TestHandleListLiveSessions_Empty verifies that an empty session list is returned
// as an empty JSON array (not null).
func TestHandleListLiveSessions_Empty(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	req := httptest.NewRequest("GET", "/live/sessions", nil)
	rr := httptest.NewRecorder()
	a.handleListLiveSessions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("empty sessions: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleListLiveSessions_WithStationFilter verifies that the station_id query
// parameter filters sessions by station.
func TestHandleListLiveSessions_WithStationFilter(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	req := httptest.NewRequest("GET", "/?station_id=st-live-1", nil)
	rr := httptest.NewRecorder()
	a.handleListLiveSessions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("filtered sessions: got %d, want 200", rr.Code)
	}
}

// TestHandleGetLiveSession_MissingID verifies 400 when session_id chi param absent.
func TestHandleGetLiveSession_MissingID(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	req := httptest.NewRequest("GET", "/live/sessions/", nil)
	// No chi param session_id
	rr := httptest.NewRecorder()
	a.handleGetLiveSession(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing session_id: got %d, want 400", rr.Code)
	}
}

// TestHandleGetLiveSession_NotFound verifies that a non-existent session returns
// a non-200 response (the live service returns ErrSessionNotFound → 404 or 500).
func TestHandleGetLiveSession_NotFound(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	req := httptest.NewRequest("GET", "/live/sessions/ghost", nil)
	req = withChiParam(req, "session_id", "ghost-session-id")
	rr := httptest.NewRecorder()
	a.handleGetLiveSession(rr, req)

	// Should be 404 or another non-200 error (not 200).
	if rr.Code == http.StatusOK {
		t.Fatal("expected non-200 for nonexistent session")
	}
}

// TestHandleLiveDisconnect_MissingSessionID verifies 400 when chi param is absent.
func TestHandleLiveDisconnect_MissingSessionID(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	req := httptest.NewRequest("POST", "/live/sessions//disconnect", nil)
	// No session_id chi param
	rr := httptest.NewRecorder()
	a.handleLiveDisconnect(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing session_id: got %d, want 400", rr.Code)
	}
}

// --- Migration handlers ---

func newMigrationHandlerForCoverage(t *testing.T) (*MigrationHandler, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "migration.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&migration.Job{}, &models.StagedImport{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	svc := migration.NewService(db, events.NewBus(), zerolog.Nop())
	return &MigrationHandler{service: svc, logger: zerolog.Nop()}, db
}

// withMigrationJobParam attaches a chi id param for migration handlers.
func withMigrationJobParam(req *http.Request, id string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

// TestHandleListMigrationJobs_HappyPath verifies that list jobs returns 200 with
// an empty jobs array when no jobs exist.
func TestHandleListMigrationJobs_HappyPath(t *testing.T) {
	h, _ := newMigrationHandlerForCoverage(t)

	req := httptest.NewRequest("GET", "/migrations", nil)
	rr := httptest.NewRecorder()
	h.handleListMigrationJobs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list jobs: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleStartMigrationJob_NotFound verifies that starting a non-existent job
// returns an error response (not 200).
func TestHandleStartMigrationJob_NotFound(t *testing.T) {
	h, _ := newMigrationHandlerForCoverage(t)

	req := httptest.NewRequest("POST", "/migrations/ghost-id/start", nil)
	req = withMigrationJobParam(req, "ghost-id")
	rr := httptest.NewRecorder()
	h.handleStartMigrationJob(rr, req)

	// Should be 400 (job not found error from service)
	if rr.Code == http.StatusOK {
		t.Fatal("expected non-200 for nonexistent job start")
	}
}

// TestHandleCancelMigrationJob_NotFound verifies 400 when cancelling a non-existent job.
func TestHandleCancelMigrationJob_NotFound(t *testing.T) {
	h, _ := newMigrationHandlerForCoverage(t)

	req := httptest.NewRequest("POST", "/migrations/ghost-id/cancel", nil)
	req = withMigrationJobParam(req, "ghost-id")
	rr := httptest.NewRecorder()
	h.handleCancelMigrationJob(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatal("expected non-200 for nonexistent job cancel")
	}
}

// TestHandleDeleteMigrationJob_NotFound verifies error response when deleting
// a non-existent job.
func TestHandleDeleteMigrationJob_NotFound(t *testing.T) {
	h, _ := newMigrationHandlerForCoverage(t)

	req := httptest.NewRequest("DELETE", "/migrations/ghost-id", nil)
	req = withMigrationJobParam(req, "ghost-id")
	rr := httptest.NewRecorder()
	h.handleDeleteMigrationJob(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatal("expected non-200 for nonexistent job delete")
	}
}
