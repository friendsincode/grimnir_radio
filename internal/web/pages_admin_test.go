/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ---------------------------------------------------------------------------
// Test setup helpers
// ---------------------------------------------------------------------------

func newAdminTestDB(t *testing.T) *gorm.DB {
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
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newAdminTestHandler(t *testing.T, db *gorm.DB) *Handler {
	t.Helper()
	h, err := NewHandler(db, []byte("test-secret"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h
}

func adminReq(method, target string, user *models.User) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	if user != nil {
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, user))
	}
	return req
}

func adminReqWithID(method, target string, user *models.User, paramName, paramVal string) *http.Request {
	req := adminReq(method, target, user)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(paramName, paramVal)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func seedAdminUser(t *testing.T, db *gorm.DB) models.User {
	t.Helper()
	u := models.User{ID: "admin1", Email: "admin@example.com", Password: "x", PlatformRole: models.PlatformRoleAdmin}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed admin user: %v", err)
	}
	return u
}

func seedRegularUser(t *testing.T, db *gorm.DB) models.User {
	t.Helper()
	u := models.User{ID: "user1", Email: "user@example.com", Password: "x", PlatformRole: models.PlatformRoleUser}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed regular user: %v", err)
	}
	return u
}

func seedStation(t *testing.T, db *gorm.DB, id, name string) models.Station {
	t.Helper()
	s := models.Station{ID: id, Name: name, Active: true}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("seed station %s: %v", id, err)
	}
	return s
}

// ---------------------------------------------------------------------------
// Access denied tests (non-admin user → 403 for all admin endpoints)
// ---------------------------------------------------------------------------

func TestAdminStationsList_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminStationsList(rr, adminReq(http.MethodGet, "/", &u))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminStationsList_NoUser_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.AdminStationsList(rr, adminReq(http.MethodGet, "/", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminStationToggleActive_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminStationToggleActive(rr, adminReqWithID(http.MethodPost, "/", &u, "id", "s1"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminStationTogglePublic_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminStationTogglePublic(rr, adminReqWithID(http.MethodPost, "/", &u, "id", "s1"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminStationToggleApproved_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminStationToggleApproved(rr, adminReqWithID(http.MethodPost, "/", &u, "id", "s1"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminStationDelete_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminStationDelete(rr, adminReqWithID(http.MethodPost, "/", &u, "id", "s1"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminUsersList_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminUsersList(rr, adminReq(http.MethodGet, "/", &u))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminUserEdit_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminUserEdit(rr, adminReqWithID(http.MethodGet, "/", &u, "id", "u2"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminUserUpdate_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminUserUpdate(rr, adminReqWithID(http.MethodPost, "/", &u, "id", "u2"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminUserResetPassword_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminUserResetPassword(rr, adminReqWithID(http.MethodPost, "/", &u, "id", "u2"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminUserDelete_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminUserDelete(rr, adminReqWithID(http.MethodPost, "/", &u, "id", "u2"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminStationsBulk_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminStationsBulk(rr, adminReq(http.MethodPost, "/", &u))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminUsersBulk_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminUsersBulk(rr, adminReq(http.MethodPost, "/", &u))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminMediaList_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminMediaList(rr, adminReq(http.MethodGet, "/", &u))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminMediaBulk_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminMediaBulk(rr, adminReq(http.MethodPost, "/", &u))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminMediaDelete_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminMediaDelete(rr, adminReqWithID(http.MethodPost, "/", &u, "id", "m1"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminMediaDuplicates_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminMediaDuplicates(rr, adminReq(http.MethodGet, "/", &u))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminMediaBackfillHashes_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminMediaBackfillHashes(rr, adminReq(http.MethodPost, "/", &u))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminMediaPurgeDuplicates_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminMediaPurgeDuplicates(rr, adminReq(http.MethodPost, "/", &u))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminMediaStream_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminMediaStream(rr, adminReqWithID(http.MethodGet, "/", &u, "id", "m1"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminLogs_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	u := seedRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminLogs(rr, adminReq(http.MethodGet, "/", &u))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Happy path: AdminStationsList
// ---------------------------------------------------------------------------

func TestAdminStationsList_Admin_Renders200(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "s1", "Alpha Station")
	seedStation(t, db, "s2", "Beta Station")

	rr := httptest.NewRecorder()
	h.AdminStationsList(rr, adminReq(http.MethodGet, "/dashboard/admin/stations", &admin))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{"Alpha Station", "Beta Station"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// Happy path: AdminUsersList
// ---------------------------------------------------------------------------

func TestAdminUsersList_Admin_Renders200(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedRegularUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminUsersList(rr, adminReq(http.MethodGet, "/dashboard/admin/users", &admin))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "admin@example.com") {
		t.Fatalf("expected admin email in body")
	}
}

// ---------------------------------------------------------------------------
// Happy path: AdminLogs
// ---------------------------------------------------------------------------

func TestAdminLogs_Admin_Renders200(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminLogs(rr, adminReq(http.MethodGet, "/dashboard/admin/logs", &admin))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Happy path: AdminStationToggleActive
// ---------------------------------------------------------------------------

func TestAdminStationToggleActive_Admin_TogglesAndRedirects(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	s := seedStation(t, db, "s1", "Test Station")
	if !s.Active {
		t.Fatalf("seed station should be active")
	}

	rr := httptest.NewRecorder()
	req := adminReqWithID(http.MethodPost, "/", &admin, "id", "s1")
	h.AdminStationToggleActive(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rr.Code)
	}

	var updated models.Station
	db.First(&updated, "id = ?", "s1")
	if updated.Active {
		t.Fatalf("expected station to be inactive after toggle")
	}
}

func TestAdminStationToggleActive_Admin_StationNotFound_Returns404(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminStationToggleActive(rr, adminReqWithID(http.MethodPost, "/", &admin, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Happy path: AdminStationTogglePublic
// ---------------------------------------------------------------------------

func TestAdminStationTogglePublic_Admin_TogglesAndRedirects(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "s2", "Public Station")

	rr := httptest.NewRecorder()
	h.AdminStationTogglePublic(rr, adminReqWithID(http.MethodPost, "/", &admin, "id", "s2"))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Happy path: AdminStationToggleApproved
// ---------------------------------------------------------------------------

func TestAdminStationToggleApproved_Admin_TogglesAndRedirects(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "s3", "Approved Station")

	rr := httptest.NewRecorder()
	h.AdminStationToggleApproved(rr, adminReqWithID(http.MethodPost, "/", &admin, "id", "s3"))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Happy path: AdminStationsBulk
// ---------------------------------------------------------------------------

func TestAdminStationsBulk_Admin_ActivatesStations(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	s := seedStation(t, db, "sb1", "Bulk Station")
	// Deactivate first
	db.Model(&s).Update("active", false)

	body, _ := json.Marshal(BulkRequest{IDs: []string{"sb1"}, Action: "activate"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminStationsBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var updated models.Station
	db.First(&updated, "id = ?", "sb1")
	if !updated.Active {
		t.Fatalf("expected station to be active")
	}
}

func TestAdminStationsBulk_Admin_EmptyIDs_Returns400(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	body, _ := json.Marshal(BulkRequest{IDs: []string{}, Action: "activate"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminStationsBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestAdminStationsBulk_Admin_UnknownAction_Returns400(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "sb2", "Station")

	body, _ := json.Marshal(BulkRequest{IDs: []string{"sb2"}, Action: "unknown_action"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminStationsBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestAdminStationsBulk_Admin_DeleteStations(t *testing.T) {
	db := newCascadeTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "sb3", "To Delete")

	body, _ := json.Marshal(BulkRequest{IDs: []string{"sb3"}, Action: "delete"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminStationsBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Happy path: AdminStationDelete (not-found path)
// ---------------------------------------------------------------------------

func TestAdminStationDelete_Admin_NotFound_Returns404(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminStationDelete(rr, adminReqWithID(http.MethodPost, "/", &admin, "id", "nope"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Happy path: AdminMediaDuplicates (admin renders page)
// ---------------------------------------------------------------------------

func TestAdminMediaDuplicates_Admin_Renders200(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminMediaDuplicates(rr, adminReq(http.MethodGet, "/dashboard/admin/media/duplicates", &admin))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Happy path: AdminUserEdit (404 on missing user)
// ---------------------------------------------------------------------------

func TestAdminUserEdit_Admin_UserNotFound_Returns404(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminUserEdit(rr, adminReqWithID(http.MethodGet, "/", &admin, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// AdminUsersBulk - self-exclusion
// ---------------------------------------------------------------------------

func TestAdminUsersBulk_Admin_SelfOnly_Returns400(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	// Bulk action only includes the admin themselves — should be rejected
	body, _ := json.Marshal(BulkRequest{IDs: []string{admin.ID}, Action: "set_role_user"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminUsersBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (self-excluded), got %d", rr.Code)
	}
}

func TestAdminUsersBulk_Admin_SetRoleAdmin_Succeeds(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	other := seedRegularUser(t, db)

	body, _ := json.Marshal(BulkRequest{IDs: []string{other.ID}, Action: "set_role_admin"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminUsersBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// getDiskUsage - pure syscall
// ---------------------------------------------------------------------------

func TestGetDiskUsage_EmptyPath_ReturnsNil(t *testing.T) {
	if got := getDiskUsage(""); got != nil {
		t.Fatalf("expected nil for empty path, got %+v", got)
	}
}

func TestGetDiskUsage_ValidPath_ReturnsInfo(t *testing.T) {
	// Use /tmp which always exists on Linux
	info := getDiskUsage("/tmp")
	if info == nil {
		t.Fatalf("expected disk usage info for /tmp")
	}
	if info.Path != "/tmp" {
		t.Fatalf("expected path '/tmp', got %q", info.Path)
	}
	if info.Total == "" {
		t.Fatalf("expected non-empty Total")
	}
}

func TestGetDiskUsage_InvalidPath_ReturnsNil(t *testing.T) {
	// A path that does not exist triggers syscall.Statfs error
	info := getDiskUsage("/nonexistent/path/for/coverage/test")
	if info != nil {
		t.Fatalf("expected nil for invalid path, got %+v", info)
	}
}

// ---------------------------------------------------------------------------
// Cascade delete test
// ---------------------------------------------------------------------------

// newCascadeTestDB creates an in-memory SQLite DB with all models needed by
// cascadeDeleteStation auto-migrated.
func newCascadeTestDB(t *testing.T) *gorm.DB {
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
		&models.LandingPageAsset{},
		&models.LandingPageVersion{},
		&models.ScheduleEntry{},
		&models.ScheduleSuppression{},
		&models.ScheduleRule{},
		&models.ScheduleTemplate{},
		&models.ScheduleVersion{},
		&models.Show{},
		&models.ShowInstance{},
		&models.ScheduleRequest{},
		&models.DJAvailability{},
		&models.ScheduleLock{},
		&models.WebhookTarget{},
		&models.WebhookLog{},
		&models.ListenerSample{},
		&models.ScheduleAnalytics{},
		&models.ScheduleAnalyticsDaily{},
		&models.NetworkSubscription{},
		&models.UnderwritingObligation{},
		&models.UnderwritingSpot{},
		&models.Recording{},
		&models.RecordingChapter{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.Clock{},
		&models.SmartBlock{},
		&models.PlayoutQueueItem{},
		&models.MountPlayoutState{},
		&models.AnalysisJob{},
		&models.PrioritySource{},
		&models.ExecutorState{},
		&models.LiveSession{},
		&models.WebDJSession{},
		&models.Webstream{},
		&models.StationGroup{},
		&models.StationGroupMember{},
		&models.Sponsor{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// TestAdminStationsBulk_Delete_CascadesChildData verifies that the bulk
// "delete" action removes the station AND all child records (no orphans).
func TestAdminStationsBulk_Delete_CascadesChildData(t *testing.T) {
	db := newCascadeTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	station := seedStation(t, db, "sc1", "Cascade Station")

	// Seed a child MediaItem belonging to the station.
	item := models.MediaItem{
		ID:            "mi1",
		StationID:     station.ID,
		Title:         "Test Track",
		Path:          "sc1/ab/cd/test.mp3",
		AnalysisState: "complete",
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("seed media item: %v", err)
	}

	// Confirm station and child both exist before the action.
	var preStation models.Station
	if err := db.First(&preStation, "id = ?", "sc1").Error; err != nil {
		t.Fatalf("station should exist before delete: %v", err)
	}
	var preItem models.MediaItem
	if err := db.First(&preItem, "id = ?", "mi1").Error; err != nil {
		t.Fatalf("media item should exist before delete: %v", err)
	}

	// Send the bulk delete request.
	body, _ := json.Marshal(BulkRequest{IDs: []string{"sc1"}, Action: "delete"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminStationsBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Assert: station row is gone.
	var postStation models.Station
	err := db.Unscoped().First(&postStation, "id = ?", "sc1").Error
	if err == nil {
		t.Fatal("station row still exists after bulk delete — expected it to be gone")
	}

	// Assert: child MediaItem is gone (no orphan).
	var postItem models.MediaItem
	err = db.Unscoped().First(&postItem, "id = ?", "mi1").Error
	if err == nil {
		t.Fatal("media item still exists after bulk delete — orphaned child data not cleaned up")
	}
}

// TestCascadeDeleteStation_CleansScheduleSuppressions verifies that
// cascadeDeleteStation removes all schedule_suppressions records for the deleted station.
func TestCascadeDeleteStation_CleansScheduleSuppressions(t *testing.T) {
	db := newCascadeTestDB(t)
	station := seedStation(t, db, "ss1", "Suppression Test Station")

	// Seed a ScheduleSuppression record.
	suppression := models.ScheduleSuppression{
		ID:        "supp1",
		StationID: station.ID,
		SlotID:    "slot1",
		SlotType:  "smart_block",
		StartsAt:  time.Now(),
		Reason:    "overlapping program",
	}
	if err := db.Create(&suppression).Error; err != nil {
		t.Fatalf("seed schedule suppression: %v", err)
	}

	// Confirm suppression exists before delete.
	var preSupp models.ScheduleSuppression
	if err := db.First(&preSupp, "id = ?", "supp1").Error; err != nil {
		t.Fatalf("schedule suppression should exist before delete: %v", err)
	}

	// Delete the station via cascadeDeleteStation.
	err := db.Transaction(func(tx *gorm.DB) error {
		return cascadeDeleteStation(tx, station.ID, &station)
	})
	if err != nil {
		t.Fatalf("cascadeDeleteStation failed: %v", err)
	}

	// Assert: suppression row is gone.
	var postSupp models.ScheduleSuppression
	err = db.Unscoped().First(&postSupp, "id = ?", "supp1").Error
	if err == nil {
		t.Fatal("schedule suppression still exists after cascadeDeleteStation — expected it to be deleted")
	}
}
