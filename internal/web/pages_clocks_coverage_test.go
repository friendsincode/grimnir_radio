/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func newClocksCoverageDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.Station{},
		&models.StationUser{},
		&models.Mount{},
		&models.MediaItem{},
		&models.Tag{},
		&models.MediaTagLink{},
		&models.Playlist{},
		&models.PlaylistItem{},
		&models.PlayHistory{},
		&models.LandingPage{},
		&models.ScheduleEntry{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.SmartBlock{},
		&models.Webstream{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newClocksCoverageHandler(t *testing.T, db *gorm.DB) *Handler {
	t.Helper()
	h, err := NewHandler(db, []byte("test-secret"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h
}

func clocksStation(t *testing.T, db *gorm.DB) *models.Station {
	t.Helper()
	s := &models.Station{ID: "sCK1", Name: "Clocks Station", Active: true, Timezone: "UTC"}
	if err := db.Create(s).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	return s
}

func clocksUser() *models.User {
	return &models.User{
		ID:           "uCK1",
		Email:        "clocks@example.com",
		PlatformRole: models.PlatformRoleUser,
	}
}

func withClockStation(r *http.Request, station *models.Station) *http.Request {
	ctx := context.WithValue(r.Context(), ctxKeyStation, station)
	ctx = context.WithValue(ctx, ctxKeyUser, clocksUser())
	return r.WithContext(ctx)
}

func withClockID(r *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func seedClock(t *testing.T, db *gorm.DB, stationID string) models.ClockHour {
	t.Helper()
	clock := models.ClockHour{
		ID:        uuid.New().String(),
		StationID: stationID,
		Name:      "Test Clock",
		StartHour: 0,
		EndHour:   24,
	}
	if err := db.Create(&clock).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}
	return clock
}

// ---------------------------------------------------------------------------
// ClockList
// ---------------------------------------------------------------------------

func TestClockList_NoStation_Redirects(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks", nil)
	rr := httptest.NewRecorder()
	h.ClockList(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/dashboard/stations/select" {
		t.Fatalf("expected redirect to station select, got %q", loc)
	}
}

func TestClockList_WithStation_Returns200(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks", nil)
	req = withClockStation(req, station)
	rr := httptest.NewRecorder()
	h.ClockList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestClockList_WithClocks_Returns200(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)
	seedClock(t, db, station.ID)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks", nil)
	req = withClockStation(req, station)
	rr := httptest.NewRecorder()
	h.ClockList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Test Clock") {
		t.Errorf("expected clock name in body")
	}
}

// ---------------------------------------------------------------------------
// ClockNew
// ---------------------------------------------------------------------------

func TestClockNew_WithStation_Returns200(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks/new", nil)
	req = withClockStation(req, station)
	rr := httptest.NewRecorder()
	h.ClockNew(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// ClockCreate
// ---------------------------------------------------------------------------

func TestClockCreate_NoStation_Returns400(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)

	form := url.Values{"name": {"My Clock"}, "start_hour": {"0"}, "end_hour": {"24"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ClockCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestClockCreate_NoName_Returns400(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	form := url.Values{"name": {""}, "start_hour": {"0"}, "end_hour": {"24"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withClockStation(req, station)
	rr := httptest.NewRecorder()
	h.ClockCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestClockCreate_InvalidStartHour_Returns400(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	form := url.Values{"name": {"My Clock"}, "start_hour": {"notanumber"}, "end_hour": {"24"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withClockStation(req, station)
	rr := httptest.NewRecorder()
	h.ClockCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestClockCreate_InvalidEndHour_Returns400(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	form := url.Values{"name": {"My Clock"}, "start_hour": {"0"}, "end_hour": {"badvalue"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withClockStation(req, station)
	rr := httptest.NewRecorder()
	h.ClockCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestClockCreate_InvalidSlotsJSON_Returns400(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	form := url.Values{"name": {"My Clock"}, "start_hour": {"0"}, "end_hour": {"24"}, "slots": {"{invalid json"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withClockStation(req, station)
	rr := httptest.NewRecorder()
	h.ClockCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestClockCreate_Success_Redirects(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	form := url.Values{"name": {"New Clock"}, "start_hour": {"8"}, "end_hour": {"18"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withClockStation(req, station)
	rr := httptest.NewRecorder()
	h.ClockCreate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.HasPrefix(rr.Header().Get("Location"), "/dashboard/clocks/") {
		t.Errorf("expected redirect to clock detail, got %q", rr.Header().Get("Location"))
	}

	// Verify clock was created in DB
	var count int64
	db.Model(&models.ClockHour{}).Where("station_id = ? AND name = ?", station.ID, "New Clock").Count(&count)
	if count != 1 {
		t.Errorf("expected 1 clock in DB, got %d", count)
	}
}

func TestClockCreate_WithValidSlots_Success(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	slots, _ := json.Marshal([]map[string]any{
		{"position": 1, "type": "smart_block", "payload": map[string]any{}},
	})
	form := url.Values{
		"name":       {"Clock With Slots"},
		"start_hour": {"0"},
		"end_hour":   {"24"},
		"slots":      {string(slots)},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withClockStation(req, station)
	rr := httptest.NewRecorder()
	h.ClockCreate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestClockCreate_HtmxRequest_SetsHxRedirect(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	form := url.Values{"name": {"HX Clock"}, "start_hour": {"0"}, "end_hour": {"24"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = withClockStation(req, station)
	rr := httptest.NewRecorder()
	h.ClockCreate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HTMX, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.HasPrefix(rr.Header().Get("HX-Redirect"), "/dashboard/clocks/") {
		t.Errorf("expected HX-Redirect, got %q", rr.Header().Get("HX-Redirect"))
	}
}

// ---------------------------------------------------------------------------
// ClockEdit
// ---------------------------------------------------------------------------

func TestClockEdit_NoStation_Redirects(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks/someID/edit", nil)
	req = withClockID(req, "someID")
	rr := httptest.NewRecorder()
	h.ClockEdit(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestClockEdit_NotFound_Returns404(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks/notexist/edit", nil)
	req = withClockStation(req, station)
	req = withClockID(req, "notexist")
	rr := httptest.NewRecorder()
	h.ClockEdit(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestClockEdit_Found_Returns200(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)
	clock := seedClock(t, db, station.ID)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks/"+clock.ID+"/edit", nil)
	req = withClockStation(req, station)
	req = withClockID(req, clock.ID)
	rr := httptest.NewRecorder()
	h.ClockEdit(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Test Clock") {
		t.Errorf("expected clock name in edit form")
	}
}

// ---------------------------------------------------------------------------
// ClockUpdate
// ---------------------------------------------------------------------------

func TestClockUpdate_NoStation_Returns400(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)

	form := url.Values{"name": {"Updated"}, "start_hour": {"0"}, "end_hour": {"24"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks/someID", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withClockID(req, "someID")
	rr := httptest.NewRecorder()
	h.ClockUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestClockUpdate_NotFound_Returns404(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	form := url.Values{"name": {"Updated"}, "start_hour": {"0"}, "end_hour": {"24"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks/notexist", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withClockStation(req, station)
	req = withClockID(req, "notexist")
	rr := httptest.NewRecorder()
	h.ClockUpdate(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestClockUpdate_InvalidStartHour_Returns400(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)
	clock := seedClock(t, db, station.ID)

	form := url.Values{"name": {"Updated"}, "start_hour": {"bad"}, "end_hour": {"24"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks/"+clock.ID, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withClockStation(req, station)
	req = withClockID(req, clock.ID)
	rr := httptest.NewRecorder()
	h.ClockUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestClockUpdate_InvalidEndHour_Returns400(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)
	clock := seedClock(t, db, station.ID)

	form := url.Values{"name": {"Updated"}, "start_hour": {"0"}, "end_hour": {"bad"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks/"+clock.ID, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withClockStation(req, station)
	req = withClockID(req, clock.ID)
	rr := httptest.NewRecorder()
	h.ClockUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestClockUpdate_InvalidSlotsJSON_Returns400(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)
	clock := seedClock(t, db, station.ID)

	form := url.Values{"name": {"Updated"}, "start_hour": {"0"}, "end_hour": {"24"}, "slots": {"{invalid"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks/"+clock.ID, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withClockStation(req, station)
	req = withClockID(req, clock.ID)
	rr := httptest.NewRecorder()
	h.ClockUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestClockUpdate_Success_Redirects(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)
	clock := seedClock(t, db, station.ID)

	form := url.Values{"name": {"Updated Clock Name"}, "start_hour": {"6"}, "end_hour": {"22"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks/"+clock.ID, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withClockStation(req, station)
	req = withClockID(req, clock.ID)
	rr := httptest.NewRecorder()
	h.ClockUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Location") != "/dashboard/clocks/"+clock.ID {
		t.Errorf("expected redirect to clock, got %q", rr.Header().Get("Location"))
	}

	// Verify DB update
	var updated models.ClockHour
	db.First(&updated, "id = ?", clock.ID)
	if updated.Name != "Updated Clock Name" {
		t.Errorf("expected updated name, got %q", updated.Name)
	}
}

func TestClockUpdate_WithSlots_Success(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)
	clock := seedClock(t, db, station.ID)

	slots, _ := json.Marshal([]map[string]any{
		{"position": 1, "type": "playlist", "payload": map[string]any{"playlist_id": "p1"}},
	})
	form := url.Values{
		"name":       {"Updated With Slots"},
		"start_hour": {"0"},
		"end_hour":   {"24"},
		"slots":      {string(slots)},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks/"+clock.ID, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withClockStation(req, station)
	req = withClockID(req, clock.ID)
	rr := httptest.NewRecorder()
	h.ClockUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestClockUpdate_HtmxRequest_SetsHxRedirect(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)
	clock := seedClock(t, db, station.ID)

	form := url.Values{"name": {"HX Updated"}, "start_hour": {"0"}, "end_hour": {"24"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/clocks/"+clock.ID, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = withClockStation(req, station)
	req = withClockID(req, clock.ID)
	rr := httptest.NewRecorder()
	h.ClockUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HTMX, got %d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("HX-Redirect") != "/dashboard/clocks/"+clock.ID {
		t.Errorf("expected HX-Redirect to clock detail, got %q", rr.Header().Get("HX-Redirect"))
	}
}

// ---------------------------------------------------------------------------
// ClockDelete
// ---------------------------------------------------------------------------

func TestClockDelete_NoStation_Returns400(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)

	req := httptest.NewRequest(http.MethodDelete, "/dashboard/clocks/someID", nil)
	req = withClockID(req, "someID")
	rr := httptest.NewRecorder()
	h.ClockDelete(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestClockDelete_NotFound_Returns404(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	req := httptest.NewRequest(http.MethodDelete, "/dashboard/clocks/notexist", nil)
	req = withClockStation(req, station)
	req = withClockID(req, "notexist")
	rr := httptest.NewRecorder()
	h.ClockDelete(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestClockDelete_Success_Redirects(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)
	clock := seedClock(t, db, station.ID)

	req := httptest.NewRequest(http.MethodDelete, "/dashboard/clocks/"+clock.ID, nil)
	req = withClockStation(req, station)
	req = withClockID(req, clock.ID)
	rr := httptest.NewRecorder()
	h.ClockDelete(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Location") != "/dashboard/clocks" {
		t.Errorf("expected redirect to /dashboard/clocks, got %q", rr.Header().Get("Location"))
	}

	// Verify DB deletion
	var count int64
	db.Model(&models.ClockHour{}).Where("id = ?", clock.ID).Count(&count)
	if count != 0 {
		t.Errorf("expected clock to be deleted from DB")
	}
}

func TestClockDelete_HtmxRequest_SetsHxRedirect(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)
	clock := seedClock(t, db, station.ID)

	req := httptest.NewRequest(http.MethodDelete, "/dashboard/clocks/"+clock.ID, nil)
	req.Header.Set("HX-Request", "true")
	req = withClockStation(req, station)
	req = withClockID(req, clock.ID)
	rr := httptest.NewRecorder()
	h.ClockDelete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HTMX, got %d", rr.Code)
	}
	if rr.Header().Get("HX-Redirect") != "/dashboard/clocks" {
		t.Errorf("expected HX-Redirect to /dashboard/clocks, got %q", rr.Header().Get("HX-Redirect"))
	}
}

// ---------------------------------------------------------------------------
// ClockDetail
// ---------------------------------------------------------------------------

func TestClockDetail_NoStation_Redirects(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks/someID", nil)
	req = withClockID(req, "someID")
	rr := httptest.NewRecorder()
	h.ClockDetail(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestClockDetail_NotFound_Returns404(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks/notexist", nil)
	req = withClockStation(req, station)
	req = withClockID(req, "notexist")
	rr := httptest.NewRecorder()
	h.ClockDetail(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestClockDetail_Found_Returns200(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)
	clock := seedClock(t, db, station.ID)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks/"+clock.ID, nil)
	req = withClockStation(req, station)
	req = withClockID(req, clock.ID)
	rr := httptest.NewRecorder()
	h.ClockDetail(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// ClockSimulate
// ---------------------------------------------------------------------------

func TestClockSimulate_NoStation_Returns400(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks/someID/simulate", nil)
	req = withClockID(req, "someID")
	rr := httptest.NewRecorder()
	h.ClockSimulate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestClockSimulate_NotFound_Returns404(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks/notexist/simulate", nil)
	req = withClockStation(req, station)
	req = withClockID(req, "notexist")
	rr := httptest.NewRecorder()
	h.ClockSimulate(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestClockSimulate_EmptySlots_Returns200(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)
	clock := seedClock(t, db, station.ID)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks/"+clock.ID+"/simulate", nil)
	req = withClockStation(req, station)
	req = withClockID(req, clock.ID)
	rr := httptest.NewRecorder()
	h.ClockSimulate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestClockSimulate_WithWebstreamSlot_Returns200(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	// Create a webstream
	ws := models.Webstream{
		ID:        uuid.New().String(),
		StationID: station.ID,
		Name:      "Test Stream",
	}
	db.Create(&ws)

	// Create clock with webstream slot
	clockID := uuid.New().String()
	clock := models.ClockHour{
		ID:        clockID,
		StationID: station.ID,
		Name:      "WS Clock",
		StartHour: 0,
		EndHour:   24,
		Slots: []models.ClockSlot{
			{
				ID:          uuid.New().String(),
				ClockHourID: clockID,
				Position:    1,
				Type:        models.SlotTypeWebstream,
				Payload:     map[string]any{"webstream_id": ws.ID, "duration_ms": float64(3600000)},
			},
		},
	}
	db.Create(&clock)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks/"+clockID+"/simulate", nil)
	req = withClockStation(req, station)
	req = withClockID(req, clockID)
	rr := httptest.NewRecorder()
	h.ClockSimulate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestClockSimulate_WithStopsetSlot_Returns200(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	clockID := uuid.New().String()
	clock := models.ClockHour{
		ID:        clockID,
		StationID: station.ID,
		Name:      "Stopset Clock",
		StartHour: 0,
		EndHour:   24,
		Slots: []models.ClockSlot{
			{
				ID:          uuid.New().String(),
				ClockHourID: clockID,
				Position:    1,
				Type:        models.SlotTypeStopset,
				Payload:     map[string]any{"duration_ms": float64(60000)},
			},
		},
	}
	db.Create(&clock)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks/"+clockID+"/simulate", nil)
	req = withClockStation(req, station)
	req = withClockID(req, clockID)
	rr := httptest.NewRecorder()
	h.ClockSimulate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestClockSimulate_WithHardItemSlot_Returns200(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	station := clocksStation(t, db)

	// Create a media item
	mediaID := uuid.New().String()
	db.Create(&models.MediaItem{
		ID:            mediaID,
		StationID:     station.ID,
		Title:         "Hard Item Track",
		AnalysisState: "complete",
	})

	clockID := uuid.New().String()
	clock := models.ClockHour{
		ID:        clockID,
		StationID: station.ID,
		Name:      "Hard Clock",
		StartHour: 0,
		EndHour:   24,
		Slots: []models.ClockSlot{
			{
				ID:          uuid.New().String(),
				ClockHourID: clockID,
				Position:    1,
				Type:        models.SlotTypeHardItem,
				Payload:     map[string]any{"media_id": mediaID},
			},
		},
	}
	db.Create(&clock)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/clocks/"+clockID+"/simulate", nil)
	req = withClockStation(req, station)
	req = withClockID(req, clockID)
	rr := httptest.NewRecorder()
	h.ClockSimulate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// parseClockHourField (unit tests)
// ---------------------------------------------------------------------------

func TestParseClockHourField_EmptyValue_ReturnsFallback(t *testing.T) {
	v, err := parseClockHourField("", 0, 23, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 5 {
		t.Errorf("expected fallback 5, got %d", v)
	}
}

func TestParseClockHourField_ValidValue_ReturnsValue(t *testing.T) {
	v, err := parseClockHourField("12", 0, 23, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 12 {
		t.Errorf("expected 12, got %d", v)
	}
}

func TestParseClockHourField_BelowMin_ReturnsError(t *testing.T) {
	_, err := parseClockHourField("-1", 0, 23, 0)
	if err == nil {
		t.Fatal("expected error for below-min value")
	}
}

func TestParseClockHourField_AboveMax_ReturnsError(t *testing.T) {
	_, err := parseClockHourField("25", 0, 24, 24)
	if err == nil {
		t.Fatal("expected error for above-max value")
	}
}

func TestParseClockHourField_NotANumber_ReturnsError(t *testing.T) {
	_, err := parseClockHourField("notanumber", 0, 23, 0)
	if err == nil {
		t.Fatal("expected error for non-numeric value")
	}
}

func TestParseClockHourField_AtMin_OK(t *testing.T) {
	v, err := parseClockHourField("0", 0, 23, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 0 {
		t.Errorf("expected 0, got %d", v)
	}
}

func TestParseClockHourField_AtMax_OK(t *testing.T) {
	v, err := parseClockHourField("24", 1, 24, 24)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 24 {
		t.Errorf("expected 24, got %d", v)
	}
}

// ---------------------------------------------------------------------------
// refreshScheduleForStation
// ---------------------------------------------------------------------------

func TestRefreshScheduleForStation_NilScheduler_NoError(t *testing.T) {
	db := newClocksCoverageDB(t)
	h := newClocksCoverageHandler(t, db)
	// scheduler is nil by default, should not panic
	h.refreshScheduleForStation(context.Background(), "s1")
}
