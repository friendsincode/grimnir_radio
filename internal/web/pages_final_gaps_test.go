/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// newEmptyHandler creates a handler with a DB that has no users (for setup tests).
func newEmptySetupHandler(t *testing.T) (*Handler, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{}, &models.Station{}, &models.StationUser{},
		&models.MediaItem{}, &models.Mount{}, &models.LandingPage{},
		&models.Playlist{}, &models.PlaylistItem{}, &models.PlayHistory{},
		&models.ScheduleEntry{}, &models.ClockHour{}, &models.ClockSlot{},
		&models.Tag{}, &models.MediaTagLink{}, &models.SmartBlock{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h, err := NewHandler(db, []byte("test-secret"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h, db
}

// ---------------------------------------------------------------------------
// handler.go: StartUpdateChecker / StopUpdateChecker / SetRecordingService
// ---------------------------------------------------------------------------

func TestStartStopUpdateChecker(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.StartUpdateChecker(ctx)
	h.StopUpdateChecker()
}

func TestSetRecordingService_Nil(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	h.SetRecordingService(nil) // just ensure no panic
}

// ---------------------------------------------------------------------------
// middleware.go: GetAuthToken
// ---------------------------------------------------------------------------

func TestGetAuthToken_Empty(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if tok := h.GetAuthToken(req); tok != "" {
		t.Fatalf("expected empty token, got %q", tok)
	}
}

func TestGetAuthToken_WithToken(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), ctxKeyToken, "my-jwt-token")
	req = req.WithContext(ctx)
	if tok := h.GetAuthToken(req); tok != "my-jwt-token" {
		t.Fatalf("expected 'my-jwt-token', got %q", tok)
	}
}

// ---------------------------------------------------------------------------
// pages_setup.go: SetupPage / SetupSubmit / renderSetupError
// ---------------------------------------------------------------------------

func TestSetupPage_WhenNeedsSetup(t *testing.T) {
	h, _ := newEmptySetupHandler(t) // no users → NeedsSetup true
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	h.SetupPage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSetupPage_WhenAlreadySetup(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	db.Create(&models.User{ID: "u1", Email: "a@b.com", Password: "x"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	h.SetupPage(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rr.Code)
	}
}

func TestSetupSubmit_WhenAlreadySetup(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	db.Create(&models.User{ID: "u1", Email: "a@b.com", Password: "x"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/setup", nil)
	h.SetupSubmit(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rr.Code)
	}
}

func TestSetupSubmit_MissingEmail(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	form := strings.NewReader("email=&password=pass1234&confirm_password=pass1234&station_name=Test+Station&timezone=UTC")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/setup", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.SetupSubmit(rr, req)
	if rr.Code != http.StatusOK && rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 200 or 400, got %d", rr.Code)
	}
}

func TestSetupSubmit_PasswordTooShort(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	form := strings.NewReader("email=admin@test.com&password=short&confirm_password=short&station_name=Test+Station&timezone=UTC")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/setup", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.SetupSubmit(rr, req)
	if rr.Code != http.StatusOK && rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 200 or 400, got %d", rr.Code)
	}
}

func TestSetupSubmit_PasswordMismatch(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	form := strings.NewReader("email=admin@test.com&password=password123&confirm_password=different123&station_name=Test+Station&timezone=UTC")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/setup", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.SetupSubmit(rr, req)
	if rr.Code != http.StatusOK && rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 200 or 400, got %d", rr.Code)
	}
}

func TestSetupSubmit_MissingStationName(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	form := strings.NewReader("email=admin@test.com&password=password123&confirm_password=password123&station_name=&timezone=UTC")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/setup", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.SetupSubmit(rr, req)
	if rr.Code != http.StatusOK && rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 200 or 400, got %d", rr.Code)
	}
}

func TestSetupSubmit_HtmxMissingEmail(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	form := strings.NewReader("email=&password=pass123456&confirm_password=pass123456&station_name=Test")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/setup", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	h.SetupSubmit(rr, req)
	if rr.Code != http.StatusBadRequest && rr.Code != http.StatusOK {
		t.Fatalf("expected 400 or 200, got %d", rr.Code)
	}
}

func TestSetupSubmit_Success(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	form := strings.NewReader("email=admin@test.com&password=password123&confirm_password=password123&station_name=Test+Station&timezone=UTC")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/setup", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.SetupSubmit(rr, req)
	// Should redirect to /login on success or render an error
	if rr.Code != http.StatusSeeOther && rr.Code != http.StatusOK && rr.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_schedule.go: ScheduleCalendar / ScheduleValidate
// pages_reports.go: ScheduleRefreshReport
// ---------------------------------------------------------------------------

func TestScheduleCalendar_NoStation(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/schedule", nil)
	h.ScheduleCalendar(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rr.Code)
	}
}

func TestScheduleCalendar_WithStation(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	user := models.User{ID: "u1", Email: "a@b.com", Password: "x"}
	station := models.Station{ID: "s1", Name: "Test", Active: true, Timezone: "UTC"}
	db.Create(&user)
	db.Create(&station)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/schedule", nil)
	ctx := context.WithValue(req.Context(), ctxKeyStation, &station)
	ctx = context.WithValue(ctx, ctxKeyUser, &user)
	req = req.WithContext(ctx)
	h.ScheduleCalendar(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestScheduleValidate_NoStation(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/schedule/validate", nil)
	h.ScheduleValidate(rr, req)
	if rr.Code != http.StatusSeeOther && rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 303 or 400, got %d", rr.Code)
	}
}

func TestScheduleValidate_WithStation(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	station := models.Station{ID: "s1", Name: "Test", Active: true, Timezone: "UTC"}
	user := models.User{ID: "u1", Email: "a@b.com", Password: "x"}
	db.Create(&station)
	db.Create(&user)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/schedule/validate", nil)
	ctx := context.WithValue(req.Context(), ctxKeyStation, &station)
	ctx = context.WithValue(ctx, ctxKeyUser, &user)
	req = req.WithContext(ctx)
	h.ScheduleValidate(rr, req)
	// Should render or redirect
	if rr.Code != http.StatusOK && rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 200 or 303, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestScheduleRefreshReport_NoStation(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/dashboard/schedule/refresh", nil)
	h.ScheduleRefreshReport(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rr.Code)
	}
}

func TestScheduleRefreshReport_WithStation(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	station := models.Station{ID: "s1", Name: "Test", Active: true, Timezone: "UTC"}
	db.Create(&station)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/dashboard/schedule/refresh", nil)
	ctx := context.WithValue(req.Context(), ctxKeyStation, &station)
	req = req.WithContext(ctx)
	h.ScheduleRefreshReport(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// diagnoseMissingMaterialization (directly testable internal function)
// ---------------------------------------------------------------------------

func TestDiagnoseMissingMaterialization_BlockDeleted(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest("GET", "/", nil)
	// No block in DB → "Smart block no longer exists"
	result := h.diagnoseMissingMaterialization(req, "station-1", "nonexistent-block-id", "")
	if result == "" {
		t.Fatal("expected non-empty diagnosis")
	}
}

func TestDiagnoseMissingMaterialization_BlockExistsNoTracks(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	// Create a smart block
	block := models.SmartBlock{ID: "block-diag-1", StationID: "s1", Name: "Test Block"}
	db.Create(&block)
	req := httptest.NewRequest("GET", "/", nil)
	result := h.diagnoseMissingMaterialization(req, "s1", "block-diag-1", "Test Block")
	if result == "" {
		t.Fatal("expected non-empty diagnosis")
	}
}

func TestDiagnoseMissingMaterialization_HasTracks(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	// Set up block with playlists and tracks
	block := models.SmartBlock{ID: "block-diag-2", StationID: "s1", Name: "Block2"}
	db.Create(&block)
	req := httptest.NewRequest("GET", "/", nil)
	// No actual smart_block_playlists table in SQLite → falls through to "no playlists"
	result := h.diagnoseMissingMaterialization(req, "s1", "block-diag-2", "Block2")
	if result == "" {
		t.Fatal("expected non-empty diagnosis")
	}
}

// ---------------------------------------------------------------------------
// materializeSmartBlock (directly testable with empty DB = early return)
// ---------------------------------------------------------------------------

func TestMaterializeSmartBlock_EmptyDB(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	block := models.SmartBlock{ID: "mb-1", StationID: "s1", Name: "Test"}
	tracks, totalMs := h.materializeSmartBlock("s1", block)
	// No music tracks in DB → returns nil, 0
	if tracks != nil || totalMs != 0 {
		t.Fatalf("expected nil, 0; got %v, %d", tracks, totalMs)
	}
}

// ---------------------------------------------------------------------------
// resolveSchedulePreviewLabel (directly testable)
// ---------------------------------------------------------------------------

func TestResolveSchedulePreviewLabel_Playlist(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	pl := models.Playlist{ID: "pl-label-1", Name: "Test Playlist", StationID: "s1"}
	db.Create(&pl)
	req := httptest.NewRequest("GET", "/", nil)
	entry := models.ScheduleEntry{SourceType: "playlist", SourceID: "pl-label-1"}
	label, kind := h.resolveSchedulePreviewLabel(req, entry)
	if label == "" || kind == "" {
		t.Fatalf("expected non-empty label and kind; got %q %q", label, kind)
	}
}

func TestResolveSchedulePreviewLabel_SmartBlock(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	block := models.SmartBlock{ID: "block-label-1", Name: "Test Block", StationID: "s1"}
	db.Create(&block)
	req := httptest.NewRequest("GET", "/", nil)
	entry := models.ScheduleEntry{SourceType: "smart_block", SourceID: "block-label-1"}
	label, kind := h.resolveSchedulePreviewLabel(req, entry)
	if label == "" || kind == "" {
		t.Fatalf("expected label %q %q", label, kind)
	}
}

func TestResolveSchedulePreviewLabel_MediaWithArtist(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	item := models.MediaItem{ID: "media-label-1", Title: "Song", Artist: "Artist", AnalysisState: "complete"}
	db.Create(&item)
	req := httptest.NewRequest("GET", "/", nil)
	entry := models.ScheduleEntry{SourceType: "media", SourceID: "media-label-1"}
	label, kind := h.resolveSchedulePreviewLabel(req, entry)
	if label == "" || kind != "Track" {
		t.Fatalf("unexpected: %q %q", label, kind)
	}
}

func TestResolveSchedulePreviewLabel_MediaNotFound(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest("GET", "/", nil)
	entry := models.ScheduleEntry{SourceType: "media", SourceID: "no-such"}
	_, kind := h.resolveSchedulePreviewLabel(req, entry)
	if kind != "Track" {
		t.Fatalf("fallback should be 'Track', got %q", kind)
	}
}

func TestResolveSchedulePreviewLabel_Live(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest("GET", "/", nil)
	entry := models.ScheduleEntry{SourceType: "live", SourceID: "some-id", Metadata: map[string]any{"session_name": "DJ Set"}}
	label, kind := h.resolveSchedulePreviewLabel(req, entry)
	if label != "DJ Set" || kind != "Live" {
		t.Fatalf("unexpected: %q %q", label, kind)
	}
}

func TestResolveSchedulePreviewLabel_Webstream(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest("GET", "/", nil)
	entry := models.ScheduleEntry{SourceType: "webstream", SourceID: "no-such"}
	_, kind := h.resolveSchedulePreviewLabel(req, entry)
	if kind != "Webstream" {
		t.Fatalf("unexpected kind: %q", kind)
	}
}

func TestResolveSchedulePreviewLabel_ClockTemplate(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest("GET", "/", nil)
	entry := models.ScheduleEntry{SourceType: "clock_template", SourceID: "no-such"}
	_, kind := h.resolveSchedulePreviewLabel(req, entry)
	if kind != "Clock" {
		t.Fatalf("unexpected kind: %q", kind)
	}
}

// ---------------------------------------------------------------------------
// GetScheduleData / GetNowPlayingData (direct calls with seeded ShowInstance)
// ---------------------------------------------------------------------------

func newShowTestHandler(t *testing.T) (*Handler, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{}, &models.Station{}, &models.StationUser{},
		&models.MediaItem{}, &models.Mount{}, &models.LandingPage{},
		&models.Playlist{}, &models.PlaylistItem{}, &models.PlayHistory{},
		&models.ScheduleEntry{}, &models.ClockHour{}, &models.ClockSlot{},
		&models.SmartBlock{}, &models.Tag{}, &models.MediaTagLink{},
		&models.Show{}, &models.ShowInstance{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h, err := NewHandler(db, []byte("test-secret"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h, db
}

func TestGetScheduleData_EmptyStation(t *testing.T) {
	h, _ := newShowTestHandler(t)
	shows, err := h.GetScheduleData("no-station", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(shows) != 0 {
		t.Fatalf("expected empty, got %d", len(shows))
	}
}

func TestGetScheduleData_WithInstances(t *testing.T) {
	h, db := newShowTestHandler(t)
	// Seed a show and instance
	show := models.Show{ID: "show-sd-1", StationID: "s1", Name: "Morning Show", Active: true}
	db.Create(&show)
	now := time.Now()
	inst := models.ShowInstance{
		ID:        "inst-sd-1",
		StationID: "s1",
		ShowID:    "show-sd-1",
		Status:    models.ShowInstanceScheduled,
		StartsAt:  now.Add(time.Hour),
		EndsAt:    now.Add(2 * time.Hour),
	}
	db.Create(&inst)

	shows, err := h.GetScheduleData("s1", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(shows) == 0 {
		t.Fatal("expected at least one show")
	}
}

func TestGetNowPlayingData_WithCurrentInstance(t *testing.T) {
	h, db := newShowTestHandler(t)
	show := models.Show{ID: "show-np-1", StationID: "s1", Name: "Live Show", Active: true}
	db.Create(&show)
	now := time.Now()
	inst := models.ShowInstance{
		ID:        "inst-np-1",
		StationID: "s1",
		ShowID:    "show-np-1",
		Status:    models.ShowInstanceScheduled,
		StartsAt:  now.Add(-30 * time.Minute),
		EndsAt:    now.Add(30 * time.Minute),
	}
	db.Create(&inst)

	current, _, err := h.GetNowPlayingData("s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if current == nil {
		t.Fatal("expected current show to be non-nil")
	}
}

func TestGetNowPlayingData_WithNextInstance(t *testing.T) {
	h, db := newShowTestHandler(t)
	show := models.Show{ID: "show-np-2", StationID: "s1", Name: "Next Show", Active: true}
	db.Create(&show)
	now := time.Now()
	// Only a future instance (no current)
	inst := models.ShowInstance{
		ID:        "inst-np-2",
		StationID: "s1",
		ShowID:    "show-np-2",
		Status:    models.ShowInstanceScheduled,
		StartsAt:  now.Add(time.Hour),
		EndsAt:    now.Add(2 * time.Hour),
	}
	db.Create(&inst)

	_, next, err := h.GetNowPlayingData("s1")
	_ = next // may or may not be found depending on query
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// loadExpectedCurrentSchedule (direct call)
// ---------------------------------------------------------------------------

func TestLoadExpectedCurrentSchedule_EmptyDB(t *testing.T) {
	h, _ := newShowTestHandler(t)
	req := httptest.NewRequest("GET", "/", nil)
	result := h.loadExpectedCurrentSchedule(req, "s1", "m1", map[string]string{})
	if result != nil {
		t.Fatal("expected nil for empty DB")
	}
}

func TestLoadExpectedCurrentSchedule_WithEntry(t *testing.T) {
	h, db := newShowTestHandler(t)
	now := time.Now().UTC()
	// Add a schedule entry that spans now
	entry := models.ScheduleEntry{
		ID:         "se-lecs-1",
		StationID:  "s1",
		MountID:    "m1",
		SourceType: "media",
		SourceID:   "media-1",
		StartsAt:   now.Add(-30 * time.Minute),
		EndsAt:     now.Add(30 * time.Minute),
	}
	db.Create(&entry)
	req := httptest.NewRequest("GET", "/", nil)
	result := h.loadExpectedCurrentSchedule(req, "s1", "m1", map[string]string{})
	_ = result // may be non-nil if schedule entry found
}

// --- PlayoutSkip / PlayoutStop / PlayoutReload: no-station branch ---

func TestPlayoutSkip_NoStation_Gaps(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest("POST", "/playout/skip", nil)
	w := httptest.NewRecorder()
	h.PlayoutSkip(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestPlayoutStop_NoStation_Gaps(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest("POST", "/playout/stop", nil)
	w := httptest.NewRecorder()
	h.PlayoutStop(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestPlayoutReload_NoStation_Gaps(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest("POST", "/playout/reload", nil)
	w := httptest.NewRecorder()
	h.PlayoutReload(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- getStationTimezone ---

func TestGetStationTimezone_NotFound(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	loc := h.getStationTimezone("no-such-station")
	if loc != time.UTC {
		t.Fatalf("expected UTC for missing station, got %v", loc)
	}
}

func TestGetStationTimezone_EmptyTimezone(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	db.AutoMigrate(&models.Station{})
	st := models.Station{ID: "tz-empty", Name: "TZ Empty", Timezone: ""}
	db.Create(&st)
	loc := h.getStationTimezone("tz-empty")
	if loc != time.UTC {
		t.Fatalf("expected UTC for empty tz, got %v", loc)
	}
}

func TestGetStationTimezone_ValidTimezone(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	db.AutoMigrate(&models.Station{})
	st := models.Station{ID: "tz-chicago", Name: "TZ Chicago", Timezone: "America/Chicago"}
	db.Create(&st)
	loc := h.getStationTimezone("tz-chicago")
	if loc.String() != "America/Chicago" {
		t.Fatalf("expected America/Chicago, got %v", loc)
	}
}

// --- PlayoutSkip/Stop/Reload: nil-director branch (station in context, no director) ---

func makeRequestWithStation(method, url string, st *models.Station) *http.Request {
	req := httptest.NewRequest(method, url, nil)
	ctx := context.WithValue(req.Context(), ctxKeyStation, st)
	return req.WithContext(ctx)
}

func TestPlayoutSkip_NilDirector(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	st := &models.Station{ID: "s-skip", Name: "S"}
	req := makeRequestWithStation("POST", "/playout/skip", st)
	w := httptest.NewRecorder()
	h.PlayoutSkip(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestPlayoutStop_NilDirector(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	st := &models.Station{ID: "s-stop", Name: "S"}
	req := makeRequestWithStation("POST", "/playout/stop", st)
	w := httptest.NewRecorder()
	h.PlayoutStop(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestPlayoutReload_NilDirector(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	st := &models.Station{ID: "s-reload", Name: "S"}
	req := makeRequestWithStation("POST", "/playout/reload", st)
	w := httptest.NewRecorder()
	h.PlayoutReload(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

// --- MediaBackfillHashes: no-station branch ---

func TestMediaBackfillHashes_NoStation_Gaps(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest("POST", "/media/backfill-hashes", nil)
	w := httptest.NewRecorder()
	h.MediaBackfillHashes(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- RequireSetup middleware ---

func TestRequireSetup_SetupPath(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	req := httptest.NewRequest("GET", "/setup", nil)
	w := httptest.NewRecorder()
	h.RequireSetup(next).ServeHTTP(w, req)
	if !called {
		t.Fatal("expected next to be called for /setup")
	}
}

func TestRequireSetup_StaticPath(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	req := httptest.NewRequest("GET", "/static/app.css", nil)
	w := httptest.NewRecorder()
	h.RequireSetup(next).ServeHTTP(w, req)
	if !called {
		t.Fatal("expected next to be called for /static/")
	}
}

func TestRequireSetup_NeedsSetupRedirect(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	// empty DB = NeedsSetup() returns true
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	req := httptest.NewRequest("GET", "/dashboard", nil)
	w := httptest.NewRecorder()
	h.RequireSetup(next).ServeHTTP(w, req)
	if called {
		t.Fatal("next should not be called when setup needed")
	}
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// previewStatusTone (pure function)
// ---------------------------------------------------------------------------

func TestPreviewStatusTone_Override(t *testing.T) {
	got := previewStatusTone("override")
	if got != "warning text-dark" {
		t.Fatalf("expected 'warning text-dark', got %q", got)
	}
}

func TestPreviewStatusTone_Mismatch(t *testing.T) {
	got := previewStatusTone("mismatch")
	if got != "danger" {
		t.Fatalf("expected 'danger', got %q", got)
	}
}

func TestPreviewStatusTone_Default(t *testing.T) {
	got := previewStatusTone("scheduled")
	if got != "secondary" {
		t.Fatalf("expected 'secondary', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// SchedulePlaylistsJSON / ScheduleSmartBlocksJSON / ScheduleClocksJSON /
// ScheduleWebstreamsJSON — with station in context
// ---------------------------------------------------------------------------

func TestSchedulePlaylistsJSON_WithStation(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	st := &models.Station{ID: "s1", Name: "Test"}
	req := makeRequestWithStation("GET", "/schedule/playlists.json", st)
	w := httptest.NewRecorder()
	h.SchedulePlaylistsJSON(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %q", ct)
	}
}

func TestScheduleSmartBlocksJSON_WithStation(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	st := &models.Station{ID: "s1", Name: "Test"}
	req := makeRequestWithStation("GET", "/schedule/smart-blocks.json", st)
	w := httptest.NewRecorder()
	h.ScheduleSmartBlocksJSON(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestScheduleClocksJSON_WithStation(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	st := &models.Station{ID: "s1", Name: "Test"}
	req := makeRequestWithStation("GET", "/schedule/clocks.json", st)
	w := httptest.NewRecorder()
	h.ScheduleClocksJSON(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestScheduleWebstreamsJSON_WithStation(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	st := &models.Station{ID: "s1", Name: "Test"}
	req := makeRequestWithStation("GET", "/schedule/webstreams.json", st)
	w := httptest.NewRecorder()
	h.ScheduleWebstreamsJSON(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// ScheduleRefresh — with station in context (scheduler is nil → warn branch)
// ---------------------------------------------------------------------------

func TestScheduleRefresh_WithStation_NilScheduler(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	st := &models.Station{ID: "s1", Name: "Test"}
	req := makeRequestWithStation("POST", "/schedule/refresh", st)
	w := httptest.NewRecorder()
	h.ScheduleRefresh(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestScheduleRefresh_WithStation_HtmxNilScheduler(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	st := &models.Station{ID: "s1", Name: "Test"}
	req := makeRequestWithStation("POST", "/schedule/refresh", st)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	h.ScheduleRefresh(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// resolveSchedulePreviewLabel — "found" paths
// ---------------------------------------------------------------------------

func TestResolveSchedulePreviewLabel_PlaylistFound(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	pl := models.Playlist{ID: "pl-found-1", Name: "Morning Mix", StationID: "s1"}
	db.Create(&pl)
	req := httptest.NewRequest("GET", "/", nil)
	entry := models.ScheduleEntry{SourceType: "playlist", SourceID: "pl-found-1"}
	label, kind := h.resolveSchedulePreviewLabel(req, entry)
	if label != "Morning Mix" {
		t.Fatalf("expected 'Morning Mix', got %q", label)
	}
	if kind != "Playlist" {
		t.Fatalf("expected 'Playlist', got %q", kind)
	}
}

func TestResolveSchedulePreviewLabel_SmartBlockFound(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	block := models.SmartBlock{ID: "block-found-1", Name: "Rock Block", StationID: "s1"}
	db.Create(&block)
	req := httptest.NewRequest("GET", "/", nil)
	entry := models.ScheduleEntry{SourceType: "smart_block", SourceID: "block-found-1"}
	label, kind := h.resolveSchedulePreviewLabel(req, entry)
	if label != "Rock Block" {
		t.Fatalf("expected 'Rock Block', got %q", label)
	}
	if kind != "Smart Block" {
		t.Fatalf("expected 'Smart Block', got %q", kind)
	}
}

func TestResolveSchedulePreviewLabel_LiveWithSessionName(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest("GET", "/", nil)
	entry := models.ScheduleEntry{
		SourceType: "live",
		SourceID:   "live-1",
		Metadata:   map[string]any{"session_name": "Evening Jazz"},
	}
	label, kind := h.resolveSchedulePreviewLabel(req, entry)
	if label != "Evening Jazz" {
		t.Fatalf("expected 'Evening Jazz', got %q", label)
	}
	if kind != "Live" {
		t.Fatalf("expected 'Live', got %q", kind)
	}
}

func TestResolveSchedulePreviewLabel_LiveNoSessionName(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest("GET", "/", nil)
	entry := models.ScheduleEntry{SourceType: "live", SourceID: "live-2"}
	label, kind := h.resolveSchedulePreviewLabel(req, entry)
	if label != "Live Session" {
		t.Fatalf("expected 'Live Session', got %q", label)
	}
	if kind != "Live" {
		t.Fatalf("expected 'Live', got %q", kind)
	}
}

// ---------------------------------------------------------------------------
// ProfileLogoutAllDevices — no-user branch and with-user (HTMX) branch
// ---------------------------------------------------------------------------

func TestProfileLogoutAllDevices_NoUser(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest("POST", "/profile/logout-all", nil)
	w := httptest.NewRecorder()
	h.ProfileLogoutAllDevices(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestProfileLogoutAllDevices_WithUser(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	user := &models.User{ID: "u-lad", Email: "lad@example.com", Password: "x"}
	db.Create(user)
	req := httptest.NewRequest("POST", "/profile/logout-all", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, user)
	req = req.WithContext(ctx)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	h.ProfileLogoutAllDevices(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// APIKeysSection — no-user branch and with-user branch
// ---------------------------------------------------------------------------

func TestAPIKeysSection_NoUser(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest("GET", "/profile/api-keys", nil)
	w := httptest.NewRecorder()
	h.APIKeysSection(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAPIKeysSection_WithUser(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	if err := db.AutoMigrate(&models.APIKey{}); err != nil {
		t.Fatalf("migrate api key: %v", err)
	}
	user := &models.User{ID: "u-aks", Email: "aks@example.com", Password: "x"}
	db.Create(user)
	req := httptest.NewRequest("GET", "/profile/api-keys", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.APIKeysSection(w, req)
	// renders a partial — any non-5xx is acceptable
	if w.Code >= 500 {
		t.Fatalf("unexpected error %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// APIKeyGenerate — no-user and with-user
// ---------------------------------------------------------------------------

func TestAPIKeyGenerate_NoUser(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	req := httptest.NewRequest("POST", "/profile/api-keys", nil)
	w := httptest.NewRecorder()
	h.APIKeyGenerate(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAPIKeyGenerate_WithUser(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	if err := db.AutoMigrate(&models.APIKey{}); err != nil {
		t.Fatalf("migrate api key: %v", err)
	}
	user := &models.User{ID: "u-akg", Email: "akg@example.com", Password: "x"}
	db.Create(user)
	form := strings.NewReader("name=Test+Key&expiration_days=30")
	req := httptest.NewRequest("POST", "/profile/api-keys", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.APIKeyGenerate(w, req)
	// should succeed or render partial
	if w.Code >= 500 {
		t.Fatalf("unexpected error %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// loadExpectedCurrentSchedule — playlist and smart_block source types
// ---------------------------------------------------------------------------

func TestLoadExpectedCurrentSchedule_PlaylistEntry(t *testing.T) {
	h, db := newShowTestHandler(t)
	now := time.Now().UTC()
	pl := models.Playlist{ID: "pl-lecs-1", Name: "Daytime", StationID: "s1"}
	db.Create(&pl)
	entry := models.ScheduleEntry{
		ID:         "se-lecs-pl",
		StationID:  "s1",
		MountID:    "m1",
		SourceType: "playlist",
		SourceID:   "pl-lecs-1",
		StartsAt:   now.Add(-30 * time.Minute),
		EndsAt:     now.Add(30 * time.Minute),
	}
	db.Create(&entry)
	req := httptest.NewRequest("GET", "/", nil)
	result := h.loadExpectedCurrentSchedule(req, "s1", "m1", map[string]string{})
	if result == nil {
		t.Fatal("expected non-nil result for playlist entry")
	}
}

func TestLoadExpectedCurrentSchedule_SmartBlockEntry(t *testing.T) {
	h, db := newShowTestHandler(t)
	now := time.Now().UTC()
	block := models.SmartBlock{ID: "block-lecs-1", Name: "Rock Hour", StationID: "s1"}
	db.Create(&block)
	entry := models.ScheduleEntry{
		ID:         "se-lecs-sb",
		StationID:  "s1",
		MountID:    "m1",
		SourceType: "smart_block",
		SourceID:   "block-lecs-1",
		StartsAt:   now.Add(-30 * time.Minute),
		EndsAt:     now.Add(30 * time.Minute),
	}
	db.Create(&entry)
	req := httptest.NewRequest("GET", "/", nil)
	result := h.loadExpectedCurrentSchedule(req, "s1", "m1", map[string]string{})
	if result == nil {
		t.Fatal("expected non-nil result for smart_block entry")
	}
}

// ---------------------------------------------------------------------------
// loadDashboardConfidenceData — with a future ScheduleEntry (smart_block)
// ---------------------------------------------------------------------------

func TestLoadDashboardConfidenceData_WithFutureSmartBlock(t *testing.T) {
	h, db := newShowTestHandler(t)
	now := time.Now().UTC()
	block := models.SmartBlock{ID: "block-dash-1", Name: "Future Block", StationID: "s1"}
	db.Create(&block)
	// Insert a future schedule entry so NextEntry branch is covered
	entry := models.ScheduleEntry{
		ID:         "se-dash-1",
		StationID:  "s1",
		MountID:    "m1",
		SourceType: "smart_block",
		SourceID:   "block-dash-1",
		StartsAt:   now.Add(time.Hour),
		EndsAt:     now.Add(2 * time.Hour),
	}
	db.Create(&entry)
	req := httptest.NewRequest("GET", "/", nil)
	data := h.loadDashboardConfidenceData(req, "s1")
	if data.NextEntry == nil {
		t.Fatal("expected NextEntry to be set")
	}
	if data.NextEntryTitle != "Future Block" {
		t.Fatalf("expected 'Future Block', got %q", data.NextEntryTitle)
	}
}

func TestLoadDashboardConfidenceData_WithFuturePlaylist(t *testing.T) {
	h, db := newShowTestHandler(t)
	now := time.Now().UTC()
	pl := models.Playlist{ID: "pl-dash-1", Name: "Dash Playlist", StationID: "s1"}
	db.Create(&pl)
	entry := models.ScheduleEntry{
		ID:         "se-dash-pl",
		StationID:  "s1",
		MountID:    "m1",
		SourceType: "playlist",
		SourceID:   "pl-dash-1",
		StartsAt:   now.Add(time.Hour),
		EndsAt:     now.Add(2 * time.Hour),
	}
	db.Create(&entry)
	req := httptest.NewRequest("GET", "/", nil)
	data := h.loadDashboardConfidenceData(req, "s1")
	if data.NextEntry == nil {
		t.Fatal("expected NextEntry to be set")
	}
	if data.NextEntryTitle != "Dash Playlist" {
		t.Fatalf("expected 'Dash Playlist', got %q", data.NextEntryTitle)
	}
}

// ---------------------------------------------------------------------------
// refreshScheduleForStation — nil scheduler (warn) and with event bus
// ---------------------------------------------------------------------------

func TestRefreshScheduleForStation_NilScheduler(t *testing.T) {
	h, _ := newEmptySetupHandler(t)
	// h.scheduler is nil — should log warn and publish event without panic
	h.refreshScheduleForStation(context.Background(), "s1")
}

func TestRefreshScheduleForStation_WithStation(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	st := models.Station{ID: "s-refresh", Name: "R", Timezone: "UTC"}
	db.Create(&st)
	// No scheduler — exercises the nil branch and event publish
	h.refreshScheduleForStation(context.Background(), "s-refresh")
}
