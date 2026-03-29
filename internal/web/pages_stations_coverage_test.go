/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ---------------------------------------------------------------------------
// Test DB + handler helpers (stations tests use their own DB with all needed tables)
// ---------------------------------------------------------------------------

func newStationsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.Station{},
		&models.StationUser{},
		&models.Mount{},
		&models.MediaItem{},
		&models.Playlist{},
		&models.PlaylistItem{},
		&models.PlayHistory{},
		&models.LandingPage{},
		&models.ScheduleEntry{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.SmartBlock{},
		&models.Tag{},
		&models.MediaTagLink{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newStationsTestHandler(t *testing.T, db *gorm.DB) *Handler {
	t.Helper()
	h, err := NewHandler(db, []byte("test-secret"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h
}

// stationsReqWithParams builds a request with chi route params and optional user/station context.
func stationsReqWithID(method, target string, user *models.User, paramName, paramVal string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(paramName, paramVal)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if user != nil {
		ctx = context.WithValue(ctx, ctxKeyUser, user)
	}
	return req.WithContext(ctx)
}

func stationsReqWithTwoIDs(method, target string, user *models.User, p1, v1, p2, v2 string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(p1, v1)
	rctx.URLParams.Add(p2, v2)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if user != nil {
		ctx = context.WithValue(ctx, ctxKeyUser, user)
	}
	return req.WithContext(ctx)
}

func stationsFormReq(method, target string, user *models.User, form url.Values, stationID string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	if stationID != "" {
		rctx.URLParams.Add("id", stationID)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if user != nil {
		ctx = context.WithValue(ctx, ctxKeyUser, user)
	}
	return req.WithContext(ctx)
}

// ---------------------------------------------------------------------------
// defaultMountURL
// ---------------------------------------------------------------------------

func TestDefaultMountURL_HTTP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.Host = "example.com"
	got := defaultMountURL(req, "mystation")
	if !strings.HasPrefix(got, "http://example.com/live/mystation") {
		t.Fatalf("unexpected URL: %s", got)
	}
}

func TestDefaultMountURL_HTTPS_ViaHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	req.Host = "example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	got := defaultMountURL(req, "mystation")
	if !strings.HasPrefix(got, "https://example.com/live/mystation") {
		t.Fatalf("unexpected URL: %s", got)
	}
}

// ---------------------------------------------------------------------------
// parseIntOrDefault
// ---------------------------------------------------------------------------

func TestParseIntOrDefault_ValidInt(t *testing.T) {
	if got := parseIntOrDefault("42", 0); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestParseIntOrDefault_Empty(t *testing.T) {
	if got := parseIntOrDefault("", 7); got != 7 {
		t.Fatalf("expected fallback 7, got %d", got)
	}
}

func TestParseIntOrDefault_Invalid(t *testing.T) {
	if got := parseIntOrDefault("notanumber", 5); got != 5 {
		t.Fatalf("expected fallback 5, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// StationsList
// ---------------------------------------------------------------------------

func TestStationsList_RendersPage(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "st1", "Alpha Station")
	seedStation(t, db, "st2", "Beta Station")

	req := httptest.NewRequest(http.MethodGet, "/dashboard/stations", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))

	rr := httptest.NewRecorder()
	h.StationsList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{"Alpha Station", "Beta Station"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// StationNew
// ---------------------------------------------------------------------------

func TestStationNew_RendersForm(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/stations/new", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))

	rr := httptest.NewRecorder()
	h.StationNew(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// StationCreate
// ---------------------------------------------------------------------------

func TestStationCreate_NoUser_Returns401(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)

	form := url.Values{"name": {"Test"}, "timezone": {"UTC"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/stations", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	h.StationCreate(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestStationCreate_EmptyName_RendersError(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)

	form := url.Values{"name": {""}, "timezone": {"UTC"}}
	req := stationsFormReq(http.MethodPost, "/dashboard/stations", &admin, form, "")

	rr := httptest.NewRecorder()
	h.StationCreate(rr, req)

	// Should either re-render form (200) with error or return 400
	if rr.Code == http.StatusSeeOther {
		t.Fatalf("expected error response, got redirect")
	}
}

func TestStationCreate_ValidStation_RedirectsAdminUser(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)

	form := url.Values{
		"name":       {"New Station"},
		"timezone":   {"UTC"},
		"active":     {"on"},
		"sort_order": {"1"},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/stations", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))

	rr := httptest.NewRecorder()
	h.StationCreate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify station was created
	var count int64
	db.Model(&models.Station{}).Where("name = ?", "New Station").Count(&count)
	if count == 0 {
		t.Fatal("station not created in DB")
	}
}

func TestStationCreate_HXRequest_SetsHXRedirect(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)

	form := url.Values{"name": {"HTMX Station"}, "timezone": {"America/Chicago"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/stations", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))

	rr := httptest.NewRecorder()
	h.StationCreate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HX request, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatal("expected HX-Redirect header")
	}
}

func TestStationCreate_EmptyName_HXRequest_Returns400(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)

	form := url.Values{"name": {""}, "timezone": {"UTC"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/stations", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))

	rr := httptest.NewRecorder()
	h.StationCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for HX error, got %d", rr.Code)
	}
}

func TestStationCreate_NonAdminUser_CreatesStation(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	user := seedRegularUser(t, db)

	form := url.Values{"name": {"User Station"}, "timezone": {"UTC"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/stations", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &user))

	rr := httptest.NewRecorder()
	h.StationCreate(rr, req)

	// Non-admin can create stations too
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// StationEdit
// ---------------------------------------------------------------------------

func TestStationEdit_NotFound_Returns404(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.StationEdit(rr, stationsReqWithID(http.MethodGet, "/", &admin, "id", "nonexistent"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestStationEdit_ExistingStation_RendersForm(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "edit1", "Edit Me")

	rr := httptest.NewRecorder()
	h.StationEdit(rr, stationsReqWithID(http.MethodGet, "/", &admin, "id", "edit1"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Edit Me") {
		t.Fatal("expected station name in body")
	}
}

// ---------------------------------------------------------------------------
// StationUpdate
// ---------------------------------------------------------------------------

func TestStationUpdate_NotFound_Returns404(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)

	form := url.Values{"name": {"Updated"}, "timezone": {"UTC"}}
	req := stationsFormReq(http.MethodPost, "/", &admin, form, "nonexistent")

	rr := httptest.NewRecorder()
	h.StationUpdate(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestStationUpdate_EmptyName_RendersError(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "upd1", "Original Name")

	form := url.Values{"name": {""}, "timezone": {"UTC"}}
	req := stationsFormReq(http.MethodPost, "/", &admin, form, "upd1")

	rr := httptest.NewRecorder()
	h.StationUpdate(rr, req)

	// Should re-render form with error
	if rr.Code == http.StatusSeeOther {
		t.Fatal("expected error response, not redirect")
	}
}

func TestStationUpdate_ValidData_Redirects(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "upd2", "Old Name")

	form := url.Values{"name": {"New Name"}, "timezone": {"UTC"}, "active": {"on"}}
	req := stationsFormReq(http.MethodPost, "/", &admin, form, "upd2")

	rr := httptest.NewRecorder()
	h.StationUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}

	var updated models.Station
	db.First(&updated, "id = ?", "upd2")
	if updated.Name != "New Name" {
		t.Fatalf("expected name 'New Name', got %q", updated.Name)
	}
}

func TestStationUpdate_HXRequest_SetsHXRedirect(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "upd3", "Station HX")

	form := url.Values{"name": {"Station HX Updated"}, "timezone": {"UTC"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "upd3")
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.StationUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatal("expected HX-Redirect header")
	}
}

// ---------------------------------------------------------------------------
// StationDelete
// ---------------------------------------------------------------------------

func TestStationDelete_NotFound_Returns404(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.StationDelete(rr, stationsReqWithID(http.MethodPost, "/", &admin, "id", "nonexistent"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestStationDelete_ExistingStation_Redirects(t *testing.T) {
	db := newCascadeTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "del1", "Delete Me")

	rr := httptest.NewRecorder()
	h.StationDelete(rr, stationsReqWithID(http.MethodPost, "/", &admin, "id", "del1"))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}

	var count int64
	db.Model(&models.Station{}).Where("id = ?", "del1").Count(&count)
	if count != 0 {
		t.Fatal("station should be deleted")
	}
}

func TestStationDelete_HXRequest_SetsHXRefresh(t *testing.T) {
	db := newCascadeTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "del2", "Delete HX")

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("HX-Request", "true")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "del2")
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.StationDelete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("HX-Refresh") != "true" {
		t.Fatal("expected HX-Refresh header")
	}
}

// ---------------------------------------------------------------------------
// MountsList
// ---------------------------------------------------------------------------

func TestMountsList_StationNotFound_Returns404(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.MountsList(rr, stationsReqWithID(http.MethodGet, "/", &admin, "stationID", "nonexistent"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestMountsList_ValidStation_RendersPage(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "mst1", "Mount Station")

	// Add a mount
	if err := db.Create(&models.Mount{
		ID:        "m1",
		StationID: "mst1",
		Name:      "Main Mount",
		Format:    "mp3",
		Bitrate:   128,
	}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	rr := httptest.NewRecorder()
	h.MountsList(rr, stationsReqWithID(http.MethodGet, "/", &admin, "stationID", "mst1"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Main Mount") {
		t.Fatal("expected mount name in body")
	}
}

// ---------------------------------------------------------------------------
// MountNew
// ---------------------------------------------------------------------------

func TestMountNew_StationNotFound_Returns404(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.MountNew(rr, stationsReqWithID(http.MethodGet, "/", &admin, "stationID", "nonexistent"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestMountNew_ValidStation_RendersForm(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "mst2", "Mount Station 2")

	rr := httptest.NewRecorder()
	h.MountNew(rr, stationsReqWithID(http.MethodGet, "/", &admin, "stationID", "mst2"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// MountCreate
// ---------------------------------------------------------------------------

func TestMountCreate_ValidMount_Redirects(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "mst3", "Mount Create Station")

	form := url.Values{
		"name":        {"Live Stream"},
		"format":      {"mp3"},
		"bitrate":     {"128"},
		"channels":    {"2"},
		"sample_rate": {"44100"},
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("stationID", "mst3")
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.MountCreate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}

	var count int64
	db.Model(&models.Mount{}).Where("station_id = ? AND name = ?", "mst3", "Live Stream").Count(&count)
	if count == 0 {
		t.Fatal("mount not created")
	}
}

func TestMountCreate_HXRequest_SetsHXRedirect(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "mst4", "Mount Create HX Station")

	form := url.Values{"name": {"HX Mount"}, "format": {"mp3"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("stationID", "mst4")
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.MountCreate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatal("expected HX-Redirect header")
	}
}

// ---------------------------------------------------------------------------
// MountEdit
// ---------------------------------------------------------------------------

func TestMountEdit_StationNotFound_Returns404(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.MountEdit(rr, stationsReqWithTwoIDs(http.MethodGet, "/", &admin, "stationID", "nonexistent", "id", "m1"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestMountEdit_MountNotFound_Returns404(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "mst5", "Station For MountEdit")

	rr := httptest.NewRecorder()
	h.MountEdit(rr, stationsReqWithTwoIDs(http.MethodGet, "/", &admin, "stationID", "mst5", "id", "nonexistent"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestMountEdit_ValidMount_RendersForm(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "mst6", "Station 6")
	if err := db.Create(&models.Mount{ID: "me1", StationID: "mst6", Name: "Edit Mount", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	rr := httptest.NewRecorder()
	h.MountEdit(rr, stationsReqWithTwoIDs(http.MethodGet, "/", &admin, "stationID", "mst6", "id", "me1"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// MountUpdate
// ---------------------------------------------------------------------------

func TestMountUpdate_MountNotFound_Returns404(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)

	form := url.Values{"name": {"Updated"}, "format": {"mp3"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("stationID", "mst1")
	rctx.URLParams.Add("id", "nonexistent")
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.MountUpdate(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestMountUpdate_ValidData_Redirects(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "mst7", "Station 7")
	if err := db.Create(&models.Mount{ID: "mu1", StationID: "mst7", Name: "Mount Orig", Format: "mp3", Bitrate: 128, Channels: 2, SampleRate: 44100}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	form := url.Values{"name": {"Mount Updated"}, "format": {"ogg"}, "bitrate": {"192"}, "channels": {"2"}, "sample_rate": {"44100"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("stationID", "mst7")
	rctx.URLParams.Add("id", "mu1")
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.MountUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}
	var updated models.Mount
	db.First(&updated, "id = ?", "mu1")
	if updated.Name != "Mount Updated" {
		t.Fatalf("expected 'Mount Updated', got %q", updated.Name)
	}
}

func TestMountUpdate_EmptyURL_UsesDefaultMountURL(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "mst8", "Station 8")
	if err := db.Create(&models.Mount{ID: "mu2", StationID: "mst8", Name: "Mount URL", Format: "mp3", Bitrate: 128, Channels: 2, SampleRate: 44100}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	form := url.Values{"name": {"Mount URL"}, "format": {"mp3"}, "url": {""}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "example.com"
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("stationID", "mst8")
	rctx.URLParams.Add("id", "mu2")
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.MountUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}
	var updated models.Mount
	db.First(&updated, "id = ?", "mu2")
	if updated.URL == "" {
		t.Fatal("expected a default URL to be set")
	}
}

// ---------------------------------------------------------------------------
// MountDelete
// ---------------------------------------------------------------------------

func TestMountDelete_ValidMount_Redirects(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "mst9", "Station 9")
	if err := db.Create(&models.Mount{ID: "md1", StationID: "mst9", Name: "Delete Mount", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	rr := httptest.NewRecorder()
	h.MountDelete(rr, stationsReqWithTwoIDs(http.MethodPost, "/", &admin, "stationID", "mst9", "id", "md1"))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}

	var count int64
	db.Model(&models.Mount{}).Where("id = ?", "md1").Count(&count)
	if count != 0 {
		t.Fatal("mount should be deleted")
	}
}

func TestMountDelete_HXRequest_SetsHXRefresh(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "mst10", "Station 10")
	if err := db.Create(&models.Mount{ID: "md2", StationID: "mst10", Name: "HX Delete Mount", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("HX-Request", "true")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("stationID", "mst10")
	rctx.URLParams.Add("id", "md2")
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.MountDelete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("HX-Refresh") != "true" {
		t.Fatal("expected HX-Refresh header")
	}
}

// ---------------------------------------------------------------------------
// renderStationFormError (indirect via handler calls)
// ---------------------------------------------------------------------------

func TestRenderStationFormError_HXRequest_Returns400WithBody(t *testing.T) {
	db := newStationsTestDB(t)
	h := newStationsTestHandler(t, db)
	admin := seedAdminUser(t, db)

	// Trigger form error via StationCreate with empty name + HX-Request
	form := url.Values{"name": {""}, "timezone": {"UTC"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/stations", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))

	rr := httptest.NewRecorder()
	h.StationCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "alert") {
		t.Fatalf("expected alert in body, got: %s", rr.Body.String())
	}
}
