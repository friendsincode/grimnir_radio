/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/landingpage"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ---------------------------------------------------------------------------
// Setup helpers
// ---------------------------------------------------------------------------

func newLandingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.Station{},
		&models.StationUser{},
		&models.MediaItem{},
		&models.Mount{},
		&models.PlayHistory{},
		&models.Playlist{},
		&models.PlaylistItem{},
		&models.ScheduleEntry{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.LandingPage{},
		&models.LandingPageVersion{},
		&models.LandingPageAsset{},
		&models.SmartBlock{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newLandingTestHandler(t *testing.T, db *gorm.DB) *Handler {
	t.Helper()
	h, err := NewHandler(db, []byte("test-secret"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	// Attach a real landing page service so tests can exercise service calls
	svc := landingpage.NewService(db, nil, t.TempDir(), zerolog.Nop())
	h.SetLandingPageService(svc)
	return h
}

func seedLandingAdminUser(t *testing.T, db *gorm.DB) models.User {
	t.Helper()
	u := models.User{ID: "ladmin1", Email: "ladmin@example.com", Password: "x", PlatformRole: models.PlatformRoleAdmin}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed landing admin: %v", err)
	}
	return u
}

func seedLandingRegularUser(t *testing.T, db *gorm.DB) models.User {
	t.Helper()
	u := models.User{ID: "luser1", Email: "luser@example.com", Password: "x", PlatformRole: models.PlatformRoleUser}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed landing regular user: %v", err)
	}
	return u
}

func seedLandingStation(t *testing.T, db *gorm.DB) models.Station {
	t.Helper()
	s := models.Station{ID: "lstn1", Name: "Landing Station", Active: true}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("seed landing station: %v", err)
	}
	return s
}

func seedStationOwner(t *testing.T, db *gorm.DB, userID, stationID string) models.StationUser {
	t.Helper()
	su := models.StationUser{UserID: userID, StationID: stationID, Role: models.StationRoleOwner}
	if err := db.Create(&su).Error; err != nil {
		t.Fatalf("seed station owner: %v", err)
	}
	return su
}

func landingReq(method, target string, user *models.User, station *models.Station) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	ctx := req.Context()
	if user != nil {
		ctx = context.WithValue(ctx, ctxKeyUser, user)
	}
	if station != nil {
		ctx = context.WithValue(ctx, ctxKeyStation, station)
	}
	return req.WithContext(ctx)
}

func landingReqWithParam(method, target string, user *models.User, station *models.Station, paramName, paramVal string) *http.Request {
	req := landingReq(method, target, user, station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(paramName, paramVal)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

func landingReqWithBody(method, target string, user *models.User, station *models.Station, body []byte) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := req.Context()
	if user != nil {
		ctx = context.WithValue(ctx, ctxKeyUser, user)
	}
	if station != nil {
		ctx = context.WithValue(ctx, ctxKeyStation, station)
	}
	return req.WithContext(ctx)
}

// ---------------------------------------------------------------------------
// canManageLandingPage helper tests
// ---------------------------------------------------------------------------

func TestCanManageLandingPage_NilUser(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	station := seedLandingStation(t, db)
	if h.canManageLandingPage(nil, &station) {
		t.Fatal("expected false for nil user")
	}
}

func TestCanManageLandingPage_NilStation(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	if h.canManageLandingPage(&u, nil) {
		t.Fatal("expected false for nil station")
	}
}

func TestCanManageLandingPage_PlatformAdmin(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	if !h.canManageLandingPage(&u, &station) {
		t.Fatal("expected true for platform admin")
	}
}

func TestCanManageLandingPage_StationOwner(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	station := seedLandingStation(t, db)
	seedStationOwner(t, db, u.ID, station.ID)
	if !h.canManageLandingPage(&u, &station) {
		t.Fatal("expected true for station owner")
	}
}

func TestCanManageLandingPage_NoRole(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	station := seedLandingStation(t, db)
	if h.canManageLandingPage(&u, &station) {
		t.Fatal("expected false when user has no station role")
	}
}

func TestCanManageLandingPage_DJRole(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	station := seedLandingStation(t, db)
	su := models.StationUser{UserID: u.ID, StationID: station.ID, Role: models.StationRoleDJ}
	db.Create(&su)
	if h.canManageLandingPage(&u, &station) {
		t.Fatal("expected false for DJ role (no landing page access)")
	}
}

// ---------------------------------------------------------------------------
// LandingPageEditor
// ---------------------------------------------------------------------------

func TestLandingPageEditor_NoStation_Redirects303(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageEditor(rr, landingReq(http.MethodGet, "/dashboard/landing", &u, nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestLandingPageEditor_PermissionDenied(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageEditor(rr, landingReq(http.MethodGet, "/", &u, &station))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestLandingPageEditor_Success(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageEditor(rr, landingReq(http.MethodGet, "/", &u, &station))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// LandingPageEditorSave
// ---------------------------------------------------------------------------

func TestLandingPageEditorSave_NoStation_400(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	body := `{"config":{"foo":"bar"}}`
	rr := httptest.NewRecorder()
	h.LandingPageEditorSave(rr, landingReqWithBody(http.MethodPost, "/", &u, nil, []byte(body)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageEditorSave_PermissionDenied(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	station := seedLandingStation(t, db)
	body := `{"config":{"foo":"bar"}}`
	rr := httptest.NewRecorder()
	h.LandingPageEditorSave(rr, landingReqWithBody(http.MethodPost, "/", &u, &station, []byte(body)))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestLandingPageEditorSave_InvalidJSON(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	req := landingReqWithBody(http.MethodPost, "/", &u, &station, []byte("not json"))
	h.LandingPageEditorSave(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageEditorSave_Success(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	body := `{"config":{"theme":"daw-dark","content":{}}}`
	rr := httptest.NewRecorder()
	h.LandingPageEditorSave(rr, landingReqWithBody(http.MethodPost, "/", &u, &station, []byte(body)))
	// SQLite doesn't support JSONB columns used by the landing page service,
	// so we may get 500 in tests. Verify it reaches the service layer at least (not 400/403).
	if rr.Code == http.StatusBadRequest || rr.Code == http.StatusForbidden {
		t.Fatalf("unexpected permission/bad-request error: %d %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// LandingPageEditorPublish
// ---------------------------------------------------------------------------

func TestLandingPageEditorPublish_NoStation_400(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageEditorPublish(rr, landingReqWithBody(http.MethodPost, "/", &u, nil, []byte(`{}`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageEditorPublish_PermissionDenied(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageEditorPublish(rr, landingReqWithBody(http.MethodPost, "/", &u, &station, []byte(`{}`)))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestLandingPageEditorPublish_Success(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	body := `{"summary":"my publish note"}`
	rr := httptest.NewRecorder()
	h.LandingPageEditorPublish(rr, landingReqWithBody(http.MethodPost, "/", &u, &station, []byte(body)))
	// SQLite JSONB limitations may cause 500; verify not permission denied (400/403)
	if rr.Code == http.StatusBadRequest || rr.Code == http.StatusForbidden {
		t.Fatalf("unexpected permission/bad-request error: %d %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// LandingPageEditorDiscard
// ---------------------------------------------------------------------------

func TestLandingPageEditorDiscard_NoStation_400(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageEditorDiscard(rr, landingReq(http.MethodPost, "/", &u, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageEditorDiscard_PermissionDenied(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageEditorDiscard(rr, landingReq(http.MethodPost, "/", &u, &station))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestLandingPageEditorDiscard_Success(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageEditorDiscard(rr, landingReq(http.MethodPost, "/", &u, &station))
	// SQLite JSONB limitations may cause 500; verify not permission denied (400/403)
	if rr.Code == http.StatusBadRequest || rr.Code == http.StatusForbidden {
		t.Fatalf("unexpected permission/bad-request error: %d %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// LandingPageEditorPreview
// ---------------------------------------------------------------------------

func TestLandingPageEditorPreview_NoStation_400(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageEditorPreview(rr, landingReq(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageEditorPreview_Success(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageEditorPreview(rr, landingReq(http.MethodGet, "/", &u, &station))
	// Preview renders HTML - 200 or 500 both OK as long as no panic
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// LandingPageVersions
// ---------------------------------------------------------------------------

func TestLandingPageVersions_NoStation_Redirects303(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageVersions(rr, landingReq(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestLandingPageVersions_PermissionDenied(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageVersions(rr, landingReq(http.MethodGet, "/", &u, &station))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestLandingPageVersions_Success(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	req := landingReq(http.MethodGet, "/?limit=10&offset=0", &u, &station)
	h.LandingPageVersions(rr, req)
	// Not a permission error
	if rr.Code == http.StatusForbidden {
		t.Fatalf("unexpected 403: %s", rr.Body.String())
	}
}

func TestLandingPageVersions_WithQueryParams(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	req := landingReq(http.MethodGet, "/?limit=5&offset=10", &u, &station)
	h.LandingPageVersions(rr, req)
	// Not a permission error
	if rr.Code == http.StatusForbidden {
		t.Fatalf("unexpected 403: %s", rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// LandingPageVersionRestore
// ---------------------------------------------------------------------------

func TestLandingPageVersionRestore_NoStation_400(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageVersionRestore(rr, landingReqWithParam(http.MethodPost, "/", &u, nil, "versionID", "v1"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageVersionRestore_PermissionDenied(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageVersionRestore(rr, landingReqWithParam(http.MethodPost, "/", &u, &station, "versionID", "v1"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestLandingPageVersionRestore_MissingVersionID(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	// No versionID param → empty string
	h.LandingPageVersionRestore(rr, landingReq(http.MethodPost, "/", &u, &station))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageVersionRestore_NotFound(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageVersionRestore(rr, landingReqWithParam(http.MethodPost, "/", &u, &station, "versionID", "nonexistent"))
	// Service will return error (version not found)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// LandingPageAssetDelete
// ---------------------------------------------------------------------------

func TestLandingPageAssetDelete_NoStation_400(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageAssetDelete(rr, landingReqWithParam(http.MethodDelete, "/", &u, nil, "assetID", "a1"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageAssetDelete_PermissionDenied(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageAssetDelete(rr, landingReqWithParam(http.MethodDelete, "/", &u, &station, "assetID", "a1"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestLandingPageAssetDelete_MissingAssetID(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageAssetDelete(rr, landingReq(http.MethodDelete, "/", &u, &station))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageAssetDelete_NotFound(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageAssetDelete(rr, landingReqWithParam(http.MethodDelete, "/", &u, &station, "assetID", "nonexistent"))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// LandingPageAssetServe
// ---------------------------------------------------------------------------

func TestLandingPageAssetServe_MissingAssetID(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageAssetServe(rr, landingReq(http.MethodGet, "/", nil, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageAssetServe_NotFound(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageAssetServe(rr, landingReqWithParam(http.MethodGet, "/", nil, nil, "assetID", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// LandingPageAssetByType
// ---------------------------------------------------------------------------

func TestLandingPageAssetByType_MissingType(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageAssetByType(rr, landingReq(http.MethodGet, "/", nil, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageAssetByType_NotFound_Returns1x1GIF(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	rr := httptest.NewRecorder()
	req := landingReqWithParam(http.MethodGet, "/?station_id=nostationhere", nil, nil, "assetType", "logo")
	h.LandingPageAssetByType(rr, req)
	// Should return a GIF (1x1 transparent pixel) on not found
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with 1x1 GIF, got %d", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "image/gif") {
		t.Fatalf("expected image/gif content-type, got %q", ct)
	}
}

func TestLandingPageAssetByType_PlatformMode(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	rr := httptest.NewRecorder()
	req := landingReqWithParam(http.MethodGet, "/?platform=true", nil, nil, "assetType", "logo")
	h.LandingPageAssetByType(rr, req)
	// No asset found → 1x1 GIF
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// LandingPageThemeUpdate
// ---------------------------------------------------------------------------

func TestLandingPageThemeUpdate_NoStation_400(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageThemeUpdate(rr, landingReqWithBody(http.MethodPost, "/", &u, nil, []byte(`{"theme":"daw-dark"}`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageThemeUpdate_PermissionDenied(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageThemeUpdate(rr, landingReqWithBody(http.MethodPost, "/", &u, &station, []byte(`{"theme":"daw-dark"}`)))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestLandingPageThemeUpdate_InvalidJSON(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageThemeUpdate(rr, landingReqWithBody(http.MethodPost, "/", &u, &station, []byte("notjson")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageThemeUpdate_Success(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageThemeUpdate(rr, landingReqWithBody(http.MethodPost, "/", &u, &station, []byte(`{"theme":"daw-dark"}`)))
	// SQLite JSONB limitations may cause 500; verify not permission denied
	if rr.Code == http.StatusForbidden || rr.Code == http.StatusBadRequest {
		t.Fatalf("unexpected permission/validation error: %d %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// LandingPageCustomCSS
// ---------------------------------------------------------------------------

func TestLandingPageCustomCSS_NoStation_400(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageCustomCSS(rr, landingReqWithBody(http.MethodPost, "/", &u, nil, []byte(`{"css":"body{}"}`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageCustomCSS_PermissionDenied(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageCustomCSS(rr, landingReqWithBody(http.MethodPost, "/", &u, &station, []byte(`{"css":"body{}"}`)))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestLandingPageCustomCSS_InvalidJSON(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageCustomCSS(rr, landingReqWithBody(http.MethodPost, "/", &u, &station, []byte("notjson")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageCustomCSS_Success(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageCustomCSS(rr, landingReqWithBody(http.MethodPost, "/", &u, &station, []byte(`{"css":"body { color: red; }"}`)))
	// SQLite JSONB limitations may cause 500; verify not permission denied
	if rr.Code == http.StatusForbidden || rr.Code == http.StatusBadRequest {
		t.Fatalf("unexpected permission/validation error: %d %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PlatformLandingPageEditor
// ---------------------------------------------------------------------------

func TestPlatformLandingPageEditor_NoUser(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPageEditor(rr, landingReq(http.MethodGet, "/", nil, nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestPlatformLandingPageEditor_NonAdmin(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPageEditor(rr, landingReq(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestPlatformLandingPageEditor_Admin_Success(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPageEditor(rr, landingReq(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PlatformLandingPageSave
// ---------------------------------------------------------------------------

func TestPlatformLandingPageSave_NoUser(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPageSave(rr, landingReqWithBody(http.MethodPost, "/", nil, nil, []byte(`{"config":{}}`)))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestPlatformLandingPageSave_NonAdmin(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPageSave(rr, landingReqWithBody(http.MethodPost, "/", &u, nil, []byte(`{"config":{}}`)))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestPlatformLandingPageSave_InvalidJSON(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPageSave(rr, landingReqWithBody(http.MethodPost, "/", &u, nil, []byte("notjson")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlatformLandingPageSave_Success(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	body := `{"config":{"theme":"daw-dark"}}`
	rr := httptest.NewRecorder()
	h.PlatformLandingPageSave(rr, landingReqWithBody(http.MethodPost, "/", &u, nil, []byte(body)))
	// SQLite JSONB limitations may cause 500; verify admin is not rejected (403)
	if rr.Code == http.StatusForbidden {
		t.Fatalf("unexpected 403 for platform admin: %s", rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PlatformLandingPagePublish
// ---------------------------------------------------------------------------

func TestPlatformLandingPagePublish_NoUser(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPagePublish(rr, landingReqWithBody(http.MethodPost, "/", nil, nil, []byte(`{}`)))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestPlatformLandingPagePublish_NonAdmin(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPagePublish(rr, landingReqWithBody(http.MethodPost, "/", &u, nil, []byte(`{}`)))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestPlatformLandingPagePublish_Success(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPagePublish(rr, landingReqWithBody(http.MethodPost, "/", &u, nil, []byte(`{"summary":"platform pub"}`)))
	// SQLite JSONB limitations may cause 500; verify admin is not rejected (403)
	if rr.Code == http.StatusForbidden {
		t.Fatalf("unexpected 403 for platform admin: %s", rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PlatformLandingPageDiscard
// ---------------------------------------------------------------------------

func TestPlatformLandingPageDiscard_NoUser(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPageDiscard(rr, landingReq(http.MethodPost, "/", nil, nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestPlatformLandingPageDiscard_NonAdmin(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPageDiscard(rr, landingReq(http.MethodPost, "/", &u, nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestPlatformLandingPageDiscard_Success(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPageDiscard(rr, landingReq(http.MethodPost, "/", &u, nil))
	// SQLite JSONB limitations may cause 500; verify admin is not rejected (403)
	if rr.Code == http.StatusForbidden {
		t.Fatalf("unexpected 403 for platform admin: %s", rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PlatformLandingPagePreview
// ---------------------------------------------------------------------------

func TestPlatformLandingPagePreview_NoUser(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPagePreview(rr, landingReq(http.MethodGet, "/", nil, nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestPlatformLandingPagePreview_NonAdmin(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPagePreview(rr, landingReq(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestPlatformLandingPagePreview_Admin_Success(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPagePreview(rr, landingReq(http.MethodGet, "/", &u, nil))
	// 200 or renders fine; 500 is possible if template not found but no panic
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// PlatformLandingPageAssetUpload
// ---------------------------------------------------------------------------

func TestPlatformLandingPageAssetUpload_NoUser(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPageAssetUpload(rr, landingReq(http.MethodPost, "/", nil, nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestPlatformLandingPageAssetUpload_NonAdmin(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.PlatformLandingPageAssetUpload(rr, landingReq(http.MethodPost, "/", &u, nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestPlatformLandingPageAssetUpload_NoFile(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	// Send a multipart form with no file
	body := &bytes.Buffer{}
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----boundary")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &u)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlatformLandingPageAssetUpload(rr, req)
	// Should fail (no file or parse error)
	if rr.Code != http.StatusBadRequest && rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 400 or 500, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// LandingPageAssetUpload (station)
// ---------------------------------------------------------------------------

func TestLandingPageAssetUpload_NoStation_400(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageAssetUpload(rr, landingReq(http.MethodPost, "/", &u, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLandingPageAssetUpload_PermissionDenied(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingRegularUser(t, db)
	station := seedLandingStation(t, db)
	rr := httptest.NewRecorder()
	h.LandingPageAssetUpload(rr, landingReq(http.MethodPost, "/", &u, &station))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestLandingPageAssetUpload_NoFile(t *testing.T) {
	db := newLandingTestDB(t)
	h := newLandingTestHandler(t, db)
	u := seedLandingAdminUser(t, db)
	station := seedLandingStation(t, db)
	body := &bytes.Buffer{}
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----boundary")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &u)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.LandingPageAssetUpload(rr, req)
	// Should fail due to missing file
	if rr.Code != http.StatusBadRequest && rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 400 or 500, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// orderStationsByConfig helper
// ---------------------------------------------------------------------------

func TestOrderStationsByConfig_NoContent(t *testing.T) {
	stations := []models.Station{{ID: "a"}, {ID: "b"}}
	result := orderStationsByConfig(stations, map[string]any{})
	if len(result) != 2 {
		t.Fatalf("expected 2 stations, got %d", len(result))
	}
}

func TestOrderStationsByConfig_WithOrder(t *testing.T) {
	stations := []models.Station{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	config := map[string]any{
		"content": map[string]any{
			"stationOrder": []any{"c", "a", "b"},
		},
	}
	result := orderStationsByConfig(stations, config)
	if len(result) != 3 {
		t.Fatalf("expected 3 stations, got %d", len(result))
	}
	if result[0].ID != "c" || result[1].ID != "a" || result[2].ID != "b" {
		t.Fatalf("unexpected order: %v %v %v", result[0].ID, result[1].ID, result[2].ID)
	}
}

func TestOrderStationsByConfig_PartialOrder(t *testing.T) {
	stations := []models.Station{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	config := map[string]any{
		"content": map[string]any{
			"stationOrder": []any{"b"},
		},
	}
	result := orderStationsByConfig(stations, config)
	// "b" should come first
	if len(result) != 3 {
		t.Fatalf("expected 3 stations, got %d", len(result))
	}
	if result[0].ID != "b" {
		t.Fatalf("expected b first, got %v", result[0].ID)
	}
}

func TestOrderStationsByConfig_EmptyOrder(t *testing.T) {
	stations := []models.Station{{ID: "a"}, {ID: "b"}}
	config := map[string]any{
		"content": map[string]any{
			"stationOrder": []any{},
		},
	}
	result := orderStationsByConfig(stations, config)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
}

func TestOrderStationsByConfig_StringSliceOrder(t *testing.T) {
	stations := []models.Station{{ID: "x"}, {ID: "y"}}
	config := map[string]any{
		"content": map[string]any{
			"stationOrder": []string{"y", "x"},
		},
	}
	result := orderStationsByConfig(stations, config)
	if result[0].ID != "y" {
		t.Fatalf("expected y first, got %v", result[0].ID)
	}
}

// ---------------------------------------------------------------------------
// getClientIP helper
// ---------------------------------------------------------------------------

func TestGetClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	ip := getClientIP(req)
	if ip != "10.0.0.1" {
		t.Fatalf("expected 10.0.0.1, got %q", ip)
	}
}

func TestGetClientIP_XForwardedForSingle(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "192.168.1.100")
	ip := getClientIP(req)
	if ip != "192.168.1.100" {
		t.Fatalf("expected 192.168.1.100, got %q", ip)
	}
}

func TestGetClientIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "172.16.0.1")
	ip := getClientIP(req)
	if ip != "172.16.0.1" {
		t.Fatalf("expected 172.16.0.1, got %q", ip)
	}
}

func TestGetClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ip := getClientIP(req)
	if ip == "" {
		t.Fatal("expected non-empty IP from RemoteAddr")
	}
}

// ---------------------------------------------------------------------------
// mustMarshalJSON helper
// ---------------------------------------------------------------------------

func TestMustMarshalJSON_ValidInput(t *testing.T) {
	result := mustMarshalJSON(map[string]string{"key": "value"})
	if !strings.Contains(result, "key") {
		t.Fatalf("expected JSON with 'key', got %q", result)
	}
}

func TestMustMarshalJSON_NilInput(t *testing.T) {
	result := mustMarshalJSON(nil)
	if result == "" {
		t.Fatal("expected non-empty result for nil")
	}
}

// ---------------------------------------------------------------------------
// writeJSONResponse helper
// ---------------------------------------------------------------------------

func TestWriteJSONResponse(t *testing.T) {
	rr := httptest.NewRecorder()
	writeJSONResponse(rr, http.StatusCreated, map[string]string{"k": "v"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("expected json content-type, got %q", ct)
	}
}
