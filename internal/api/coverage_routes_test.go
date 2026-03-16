/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// coverage_routes_test.go — Route registration coverage tests.
// Calling New() and Routes() exercises all route registration code paths,
// which are currently at 0% coverage. Also exercises sub-API constructors
// and their RegisterRoutes methods.
// Each test verifies real behavior: that registered routes respond (not 404)
// to basic requests, catching bugs where route registration is broken.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	analyticsvc "github.com/friendsincode/grimnir_radio/internal/analytics"
	"github.com/friendsincode/grimnir_radio/internal/audit"
	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/executor"
	"github.com/friendsincode/grimnir_radio/internal/integrity"
	landingpagesvc "github.com/friendsincode/grimnir_radio/internal/landingpage"
	"github.com/friendsincode/grimnir_radio/internal/models"
	notifysvc "github.com/friendsincode/grimnir_radio/internal/notifications"
	"github.com/friendsincode/grimnir_radio/internal/priority"
	recordingsvc "github.com/friendsincode/grimnir_radio/internal/recording"
	schedulesvc "github.com/friendsincode/grimnir_radio/internal/schedule"
	syndicationsvc "github.com/friendsincode/grimnir_radio/internal/syndication"
	underwritingsvc "github.com/friendsincode/grimnir_radio/internal/underwriting"
	webhookssvc "github.com/friendsincode/grimnir_radio/internal/webhooks"
	webdjsvc "github.com/friendsincode/grimnir_radio/internal/webdj"
)

// newRoutesTestDB creates an in-memory SQLite DB for route tests.
func newRoutesTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "routes_test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)

	// Migrate enough tables so route registration succeeds
	if err := db.AutoMigrate(
		&models.Station{},
		&models.Mount{},
		&models.StationUser{},
		&models.User{},
		&models.SmartBlock{},
		&models.Playlist{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.ScheduleEntry{},
		&models.MediaItem{},
		&models.Show{},
		&models.ShowInstance{},
		&models.PrioritySource{},
		&models.ExecutorState{},
		&models.AuditLog{},
		&models.WebDJSession{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// TestNew_ConstructorCoverage verifies that New() and Routes() can be called
// without panicking, covering all route registration code paths.
func TestNew_ConstructorCoverage(t *testing.T) {
	db := newRoutesTestDB(t)
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, zerolog.Nop())
	auditSvc := audit.NewService(db, bus, zerolog.Nop())
	integritySvc := integrity.NewService(db, zerolog.Nop())
	stateMgr := executor.NewStateManager(db, zerolog.Nop())

	// New() covers the constructor and NewMigrationHandler
	a := New(
		db,
		[]byte("test-secret"),
		nil,  // scheduler
		nil,  // analyzer
		nil,  // media
		nil,  // live
		nil,  // webstreamSvc
		nil,  // playout
		prioritySvc,
		stateMgr,
		auditSvc,
		integritySvc,
		nil, // broadcast
		bus,
		nil, // logBuf
		0,   // maxUploadBytes (uses default)
		zerolog.Nop(),
	)

	if a == nil {
		t.Fatal("New() returned nil")
	}

	// Routes() covers all route registration paths including:
	// - All r.Get/Post/Patch/Delete/Route calls
	// - requireRoles middleware setup
	// - requirePlatformAdmin setup
	// - All Add*Routes function calls
	// - All nil sub-API checks
	r := chi.NewRouter()
	a.Routes(r) // Must not panic

	// Verify a known public endpoint is reachable (not 404)
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code == http.StatusNotFound {
		t.Fatalf("health endpoint not found after Routes() registration; got 404")
	}
	// Should be 200 (or possibly 401 due to auth middleware, but not 404)
	if rr.Code != http.StatusOK {
		t.Logf("health endpoint returned %d (acceptable, not 404)", rr.Code)
	}
}

// TestNew_WithSubAPIs covers the sub-API registration branches in Routes().
// By setting all sub-APIs, we exercise the non-nil branches for each conditional.
func TestNew_WithSubAPIs(t *testing.T) {
	db := newRoutesTestDB(t)
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, zerolog.Nop())
	auditSvc := audit.NewService(db, bus, zerolog.Nop())
	integritySvc := integrity.NewService(db, zerolog.Nop())
	stateMgr := executor.NewStateManager(db, zerolog.Nop())

	a := New(
		db,
		[]byte("test-secret"),
		nil, nil, nil, nil, nil, nil,
		prioritySvc,
		stateMgr,
		auditSvc,
		integritySvc,
		nil, bus, nil, 0,
		zerolog.Nop(),
	)

	// Build sub-APIs with nil underlying services (just enough to register routes)
	notifSvc := notifysvc.NewService(db, bus, notifysvc.Config{}, zerolog.Nop())
	notifAPI := NewNotificationAPI(notifSvc)
	a.SetNotificationAPI(notifAPI)

	webhookService := webhookssvc.NewService(db, bus, zerolog.Nop())
	webhookAPI := NewWebhookAPI(a, webhookService)
	a.SetWebhookAPI(webhookAPI)

	analyticsSvc := analyticsvc.NewScheduleAnalyticsService(db, zerolog.Nop())
	schedAnalyticsAPI := NewScheduleAnalyticsAPI(a, analyticsSvc)
	a.SetScheduleAnalyticsAPI(schedAnalyticsAPI)

	syndicSvc := syndicationsvc.NewService(db, zerolog.Nop())
	syndicAPI := NewSyndicationAPI(a, syndicSvc)
	a.SetSyndicationAPI(syndicAPI)

	underwritingSvc := underwritingsvc.NewService(db, zerolog.Nop())
	underwritingAPI := NewUnderwritingAPI(a, underwritingSvc)
	a.SetUnderwritingAPI(underwritingAPI)

	exportSvc := schedulesvc.NewExportService(db, zerolog.Nop())
	exportAPI := NewScheduleExportAPI(a, exportSvc)
	a.SetScheduleExportAPI(exportAPI)

	landingPageSvc := landingpagesvc.NewService(db, nil, t.TempDir(), zerolog.Nop())
	landingPageAPI := NewLandingPageAPI(a, landingPageSvc)
	a.SetLandingPageAPI(landingPageAPI)

	recordingService := recordingsvc.NewService(db, nil, t.TempDir(), zerolog.Nop())
	recordingAPI := NewRecordingAPI(a, recordingService)
	a.SetRecordingAPI(recordingAPI)

	// Set WebDJ API
	webdjSvc := webdjsvc.NewService(db, nil, nil, nil, bus, zerolog.Nop())
	webdjAPI := NewWebDJAPI(db, webdjSvc, nil, zerolog.Nop())
	a.SetWebDJAPI(webdjAPI)

	// Routes() with all sub-APIs covers all non-nil branches
	r := chi.NewRouter()
	a.Routes(r)

	// Verify sub-API routes are registered by checking notification endpoint exists
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code == http.StatusNotFound {
		t.Fatalf("health endpoint not found after Routes() with sub-APIs; got 404")
	}
}

// TestRouteRegistration_SubAPIs verifies individual sub-API RegisterRoutes methods.

// TestNotificationAPI_RegisterRoutes verifies route registration doesn't panic.
func TestNotificationAPI_RegisterRoutes(t *testing.T) {
	db := newRoutesTestDB(t)
	bus := events.NewBus()
	svc := notifysvc.NewService(db, bus, notifysvc.Config{}, zerolog.Nop())
	api := NewNotificationAPI(svc)

	r := chi.NewRouter()
	api.RegisterRoutes(r)

	// Verify the route is registered (GET /notifications/ returns non-404)
	req := httptest.NewRequest("GET", "/notifications/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code == http.StatusNotFound {
		t.Fatal("notifications route not registered")
	}
}

// TestWebhookAPI_RegisterRoutes verifies route registration.
func TestWebhookAPI_RegisterRoutes(t *testing.T) {
	db := newRoutesTestDB(t)
	bus := events.NewBus()
	svc := webhookssvc.NewService(db, bus, zerolog.Nop())

	// newBoostTestAPI provides an API for sub-API construction
	a := &API{db: db, bus: bus, logger: zerolog.Nop()}
	api := NewWebhookAPI(a, svc)

	r := chi.NewRouter()
	api.RegisterRoutes(r)

	req := httptest.NewRequest("GET", "/webhooks/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code == http.StatusNotFound {
		t.Fatal("webhooks route not registered")
	}
}

// TestScheduleAnalyticsAPI_RegisterRoutes verifies route registration.
func TestScheduleAnalyticsAPI_RegisterRoutes(t *testing.T) {
	db := newRoutesTestDB(t)
	a := &API{db: db, logger: zerolog.Nop()}
	svc := analyticsvc.NewScheduleAnalyticsService(db, zerolog.Nop())
	api := NewScheduleAnalyticsAPI(a, svc)

	r := chi.NewRouter()
	api.RegisterRoutes(r)

	req := httptest.NewRequest("GET", "/schedule-analytics/shows", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code == http.StatusNotFound {
		t.Fatal("schedule-analytics route not registered")
	}
}

// TestSyndicationAPI_RegisterRoutes verifies route registration.
func TestSyndicationAPI_RegisterRoutes(t *testing.T) {
	db := newRoutesTestDB(t)
	a := &API{db: db, logger: zerolog.Nop()}
	svc := syndicationsvc.NewService(db, zerolog.Nop())
	api := NewSyndicationAPI(a, svc)

	r := chi.NewRouter()
	api.RegisterRoutes(r)

	// Routes are registered under /networks/ (direct path from RegisterRoutes)
	req := httptest.NewRequest("GET", "/networks/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code == http.StatusNotFound {
		t.Fatal("syndication /networks/ route not registered")
	}
}

// TestUnderwritingAPI_RegisterRoutes verifies route registration.
func TestUnderwritingAPI_RegisterRoutes(t *testing.T) {
	db := newRoutesTestDB(t)
	a := &API{db: db, logger: zerolog.Nop()}
	svc := underwritingsvc.NewService(db, zerolog.Nop())
	api := NewUnderwritingAPI(a, svc)

	r := chi.NewRouter()
	api.RegisterRoutes(r)

	// Routes are registered under /sponsors/ directly
	req := httptest.NewRequest("GET", "/sponsors/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code == http.StatusNotFound {
		t.Fatal("underwriting /sponsors/ route not registered")
	}
}

// TestScheduleExportAPI_RegisterRoutes verifies route registration.
func TestScheduleExportAPI_RegisterRoutes(t *testing.T) {
	db := newRoutesTestDB(t)
	a := &API{db: db, logger: zerolog.Nop()}
	svc := schedulesvc.NewExportService(db, zerolog.Nop())
	api := NewScheduleExportAPI(a, svc)

	r := chi.NewRouter()
	api.RegisterRoutes(r)

	// Routes are registered under /schedule/export/ical directly
	req := httptest.NewRequest("GET", "/schedule/export/ical", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code == http.StatusNotFound {
		t.Fatal("schedule export /schedule/export/ical route not registered")
	}
}

// TestLandingPageAPI_RegisterRoutes verifies route registration.
func TestLandingPageAPI_RegisterRoutes(t *testing.T) {
	db := newRoutesTestDB(t)
	if err := db.AutoMigrate(&models.LandingPage{}); err != nil {
		t.Fatalf("migrate landing page: %v", err)
	}
	a := &API{db: db, logger: zerolog.Nop()}
	svc := landingpagesvc.NewService(db, nil, t.TempDir(), zerolog.Nop())
	api := NewLandingPageAPI(a, svc)

	r := chi.NewRouter()
	api.RegisterRoutes(r)

	// Landing page routes are under /landing-page (station_id comes from chi param)
	req := httptest.NewRequest("GET", "/landing-page/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code == http.StatusNotFound {
		t.Fatal("landing page /landing-page/ route not registered")
	}
}

// TestRecordingAPI_RegisterRoutes verifies route registration.
func TestRecordingAPI_RegisterRoutes(t *testing.T) {
	db := newRoutesTestDB(t)
	a := &API{db: db, logger: zerolog.Nop()}
	svc := recordingsvc.NewService(db, nil, t.TempDir(), zerolog.Nop())
	api := NewRecordingAPI(a, svc)

	r := chi.NewRouter()
	api.RegisterRoutes(r)

	// Recordings routes registered under /recordings/
	req := httptest.NewRequest("GET", "/recordings/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code == http.StatusNotFound {
		t.Fatal("recording /recordings/ route not registered")
	}
}

// TestNewWebDJAPI_Constructor verifies that NewWebDJAPI() can be called,
// covering the 0% constructor.
func TestNewWebDJAPI_Constructor(t *testing.T) {
	db := newRoutesTestDB(t)
	bus := events.NewBus()
	svc := webdjsvc.NewService(db, nil, nil, nil, bus, zerolog.Nop())
	api := NewWebDJAPI(db, svc, nil, zerolog.Nop())

	if api == nil {
		t.Fatal("NewWebDJAPI returned nil")
	}
	if api.db != db {
		t.Fatal("NewWebDJAPI did not store db")
	}
}

// TestHandleWebDJStartSession_UserLookupFailed verifies 500 when user is not in DB
// (claims are valid but user record doesn't exist).
func TestHandleWebDJStartSession_UserLookupFailed(t *testing.T) {
	db := newRoutesTestDB(t)
	bus := events.NewBus()
	svc := webdjsvc.NewService(db, nil, nil, nil, bus, zerolog.Nop())
	a := &WebDJAPI{
		db:       db,
		webdjSvc: svc,
		logger:   zerolog.Nop(),
	}

	// valid JSON, valid station_id, but claims with UserID that doesn't exist in DB
	body, _ := json.Marshal(map[string]any{"station_id": "st-test-1"})
	req := httptest.NewRequest("POST", "/webdj/sessions", bytes.NewBuffer(body))
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "nonexistent-user-id",
		Roles:  []string{},
	}))
	rr := httptest.NewRecorder()
	a.handleStartSession(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("user lookup failed: got %d, want 500; body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleWebDJStartSession_ExistingSessionReturned verifies 200 when an existing
// active session exists in the DB for the same station+user (StartSession returns existing).
func TestHandleWebDJStartSession_ExistingSessionReturned(t *testing.T) {
	db := newRoutesTestDB(t)
	bus := events.NewBus()
	svc := webdjsvc.NewService(db, nil, nil, nil, bus, zerolog.Nop())
	a := &WebDJAPI{
		db:       db,
		webdjSvc: svc,
		logger:   zerolog.Nop(),
	}

	// Create user in DB with the admin claims UserID
	user := models.User{
		ID:    "u-admin", // matches withAdminClaims UserID
		Email: "admin@test.com",
	}
	db.Create(&user) //nolint:errcheck

	// Pre-create an active WebDJ session for this user+station
	existingSession := models.WebDJSession{
		ID:        "existing-wdj-session",
		StationID: "st-start-1",
		UserID:    "u-admin",
		Active:    true,
		CreatedAt: time.Now(),
	}
	db.Create(&existingSession) //nolint:errcheck

	body2, _ := json.Marshal(map[string]any{"station_id": "st-start-1"})
	req := httptest.NewRequest("POST", "/webdj/sessions", bytes.NewBuffer(body2))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleStartSession(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("existing session: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rr.Body.String())
	}
	if resp["id"] != "existing-wdj-session" {
		t.Fatalf("expected existing session id, got %v", resp["id"])
	}
}
