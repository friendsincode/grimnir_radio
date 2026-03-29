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
// Shared helpers
// ---------------------------------------------------------------------------

func newMiscTestDB(t *testing.T) *gorm.DB {
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
		&models.Tag{},
		&models.MediaTagLink{},
		&models.PlayHistory{},
		&models.Playlist{},
		&models.PlaylistItem{},
		&models.ScheduleEntry{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.LandingPage{},
		&models.SmartBlock{},
		&models.MountPlayoutState{},
		&models.DJAvailability{},
		&models.ScheduleRequest{},
		&models.Show{},
		&models.ShowInstance{},
		&models.Recording{},
		&models.RecordingChapter{},
		&models.WebDJSession{},
		&models.LiveSession{},
		&models.AuditLog{},
		&models.Notification{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newMiscHandler(t *testing.T, db *gorm.DB) *Handler {
	t.Helper()
	h, err := NewHandler(db, []byte("test-secret"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h
}

func miscAdminUser(t *testing.T, db *gorm.DB) models.User {
	t.Helper()
	u := models.User{ID: "misc-admin-1", Email: "misc-admin@example.com", Password: "x", PlatformRole: models.PlatformRoleAdmin}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	return u
}

func miscRegularUser(t *testing.T, db *gorm.DB) models.User {
	t.Helper()
	u := models.User{ID: "misc-user-1", Email: "misc-user@example.com", Password: "x", PlatformRole: models.PlatformRoleUser}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u
}

func miscStation(t *testing.T, db *gorm.DB) models.Station {
	t.Helper()
	s := models.Station{ID: "misc-station-1", Name: "Misc Station", Active: true}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	return s
}

// reqWithUser creates a test request with optional user in context.
func reqWithUser(method, target string, user *models.User) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	if user != nil {
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, user))
	}
	return req
}

// reqWithUserAndStation creates a request with both user and station in context.
func reqWithUserAndStation(method, target string, user *models.User, station *models.Station) *http.Request {
	req := reqWithUser(method, target, user)
	if station != nil {
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, station))
	}
	return req
}

// reqWithIDParam adds a chi URL param to the request context.
func reqWithIDParam(req *http.Request, paramName, paramVal string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(paramName, paramVal)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// ---------------------------------------------------------------------------
// pages_audit.go – AdminAudit
// ---------------------------------------------------------------------------

func TestAdminAudit_NoUser_Forbidden(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	rr := httptest.NewRecorder()
	h.AdminAudit(rr, reqWithUser(http.MethodGet, "/", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminAudit_RegularUser_Forbidden(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminAudit(rr, reqWithUser(http.MethodGet, "/", &u))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminAudit_Admin_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminAudit(rr, reqWithUser(http.MethodGet, "/", &u))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_audit.go – StationAudit
// ---------------------------------------------------------------------------

func TestStationAudit_NoStation_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.StationAudit(rr, reqWithUserAndStation(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestStationAudit_NoUser_Unauthorized(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.StationAudit(rr, reqWithUserAndStation(http.MethodGet, "/", nil, &s))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestStationAudit_UserNoRole_Forbidden(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscRegularUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.StationAudit(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestStationAudit_UserDJRole_Forbidden(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscRegularUser(t, db)
	s := miscStation(t, db)
	su := models.StationUser{ID: "su-audit-dj", UserID: u.ID, StationID: s.ID, Role: models.StationRoleDJ}
	db.Create(&su)
	rr := httptest.NewRecorder()
	h.StationAudit(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestStationAudit_Admin_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.StationAudit(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestStationAudit_ManagerRole_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscRegularUser(t, db)
	s := miscStation(t, db)
	su := models.StationUser{ID: "su-audit-mgr", UserID: u.ID, StationID: s.ID, Role: models.StationRoleManager}
	db.Create(&su)
	rr := httptest.NewRecorder()
	h.StationAudit(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_integrity.go – AdminIntegrity
// ---------------------------------------------------------------------------

func TestAdminIntegrity_NoUser_Forbidden(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	rr := httptest.NewRecorder()
	h.AdminIntegrity(rr, reqWithUser(http.MethodGet, "/", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminIntegrity_RegularUser_Forbidden(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscRegularUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminIntegrity(rr, reqWithUser(http.MethodGet, "/", &u))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminIntegrity_Admin_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.AdminIntegrity(rr, reqWithUser(http.MethodGet, "/", &u))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_orphans.go – parsePageSize
// ---------------------------------------------------------------------------

func TestParsePageSize_Default(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := parsePageSize(req); got != 25 {
		t.Fatalf("expected 25, got %d", got)
	}
}

func TestParsePageSize_50(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?per_page=50", nil)
	if got := parsePageSize(req); got != 50 {
		t.Fatalf("expected 50, got %d", got)
	}
}

func TestParsePageSize_100(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?per_page=100", nil)
	if got := parsePageSize(req); got != 100 {
		t.Fatalf("expected 100, got %d", got)
	}
}

func TestParsePageSize_All(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?per_page=all", nil)
	if got := parsePageSize(req); got != 0 {
		t.Fatalf("expected 0 for 'all', got %d", got)
	}
}

func TestParsePageSize_Invalid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?per_page=999", nil)
	if got := parsePageSize(req); got != 25 {
		t.Fatalf("expected default 25 for invalid value, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// pages_orphans.go – OrphansPage (no mediaService → renders gracefully)
// ---------------------------------------------------------------------------

func TestOrphansPage_NoMediaService_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.OrphansPage(rr, reqWithUser(http.MethodGet, "/", &u))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestOrphansPage_WithPagination_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodGet, "/?page=2&per_page=50", &u)
	h.OrphansPage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// pages_orphans.go – OrphansScan (no mediaService → HTMX error)
// ---------------------------------------------------------------------------

func TestOrphansScan_NoMediaService_ReturnsError(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.OrphansScan(rr, reqWithUser(http.MethodPost, "/", &u))
	body := rr.Body.String()
	if !strings.Contains(body, "not available") {
		t.Fatalf("expected 'not available' in body, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// pages_orphans.go – OrphansAdopt (no mediaService → HTMX error)
// ---------------------------------------------------------------------------

func TestOrphansAdopt_NoMediaService_ReturnsError(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)

	form := url.Values{"station_id": {"s1"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	req = reqWithIDParam(req, "id", "orphan-1")

	rr := httptest.NewRecorder()
	h.OrphansAdopt(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "not available") {
		t.Fatalf("expected 'not available' in body, got: %s", body)
	}
}

func TestOrphansAdopt_MissingStationID_ReturnsError(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)

	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	req = reqWithIDParam(req, "id", "orphan-1")

	rr := httptest.NewRecorder()
	h.OrphansAdopt(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "required") {
		t.Fatalf("expected 'required' in body, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// pages_orphans.go – OrphansDelete (no mediaService → HTMX error)
// ---------------------------------------------------------------------------

func TestOrphansDelete_NoMediaService_ReturnsError(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)

	req := reqWithUser(http.MethodDelete, "/?delete_file=false", &u)
	req = reqWithIDParam(req, "id", "orphan-1")

	rr := httptest.NewRecorder()
	h.OrphansDelete(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "not available") {
		t.Fatalf("expected 'not available' in body, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// pages_orphans.go – OrphansBulkAdopt (no mediaService → HTMX error)
// ---------------------------------------------------------------------------

func TestOrphansBulkAdopt_NoMediaService_ReturnsError(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)

	form := url.Values{"station_id": {"s1"}, "orphan_ids": {"id1", "id2"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.OrphansBulkAdopt(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "not available") {
		t.Fatalf("expected 'not available' in body, got: %s", body)
	}
}

func TestOrphansBulkAdopt_MissingStationID_ReturnsError(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)

	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.OrphansBulkAdopt(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "required") {
		t.Fatalf("expected 'required' in body, got: %s", body)
	}
}

func TestOrphansBulkAdopt_NoOrphansSelected_ReturnsError(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)

	// No orphan_ids and no select_all — because mediaService is nil the handler
	// returns the "not available" error before reaching the empty-IDs check.
	form := url.Values{"station_id": {"s1"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.OrphansBulkAdopt(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "not available") {
		t.Fatalf("expected 'not available' in body, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// pages_orphans.go – OrphansBulkDelete (no mediaService → HTMX error)
// ---------------------------------------------------------------------------

func TestOrphansBulkDelete_NoMediaService_ReturnsError(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)

	form := url.Values{"orphan_ids": {"id1"}, "delete_files": {"true"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.OrphansBulkDelete(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "not available") {
		t.Fatalf("expected 'not available' in body, got: %s", body)
	}
}

func TestOrphansBulkDelete_NoOrphansSelected_ReturnsError(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)

	// mediaService is nil so handler returns "not available" before empty-IDs check.
	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.OrphansBulkDelete(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "not available") {
		t.Fatalf("expected 'not available' in body, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// pages_orphans.go – formatBytesHuman
// ---------------------------------------------------------------------------

func TestFormatBytesHuman(t *testing.T) {
	cases := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, c := range cases {
		got := formatBytesHuman(c.input)
		if got != c.want {
			t.Errorf("formatBytesHuman(%d) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// pages_station_logs.go – StationLogs
// ---------------------------------------------------------------------------

func TestStationLogs_NoStation_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.StationLogs(rr, reqWithUserAndStation(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestStationLogs_NoUser_Unauthorized(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.StationLogs(rr, reqWithUserAndStation(http.MethodGet, "/", nil, &s))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestStationLogs_UserNoRole_Forbidden(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscRegularUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.StationLogs(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestStationLogs_Admin_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.StationLogs(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestStationLogs_MemberRole_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscRegularUser(t, db)
	s := miscStation(t, db)
	su := models.StationUser{ID: "su-logs-dj", UserID: u.ID, StationID: s.ID, Role: models.StationRoleDJ}
	db.Create(&su)
	rr := httptest.NewRecorder()
	h.StationLogs(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_recordings.go – RecordingsList
// ---------------------------------------------------------------------------

func TestRecordingsList_NoStation_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.RecordingsList(rr, reqWithUserAndStation(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestRecordingsList_WithStation_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.RecordingsList(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestRecordingsList_CustomLimitOffset_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/?limit=10&offset=5", &u, &s)
	h.RecordingsList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// pages_recordings.go – RecordingDetail
// ---------------------------------------------------------------------------

func TestRecordingDetail_NoStation_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/", &u, nil)
	req = reqWithIDParam(req, "id", "rec-1")
	h.RecordingDetail(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestRecordingDetail_NotFound_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/", &u, &s)
	req = reqWithIDParam(req, "id", "nonexistent")
	h.RecordingDetail(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestRecordingDetail_Found_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)

	rec := models.Recording{
		ID:        "rec-detail-1",
		StationID: s.ID,
		Title:     "My Recording",
		Status:    models.RecordingStatusComplete,
	}
	db.Create(&rec)

	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/", &u, &s)
	req = reqWithIDParam(req, "id", "rec-detail-1")
	h.RecordingDetail(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_recordings.go – RecordingStop
// ---------------------------------------------------------------------------

func TestRecordingStop_NoStation_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, nil)
	req = reqWithIDParam(req, "id", "rec-1")
	h.RecordingStop(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestRecordingStop_RecordingNotFound_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, &s)
	req = reqWithIDParam(req, "id", "nonexistent")
	h.RecordingStop(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestRecordingStop_Found_NoService_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)

	rec := models.Recording{
		ID:        "rec-stop-1",
		StationID: s.ID,
		Title:     "Active Rec",
		Status:    models.RecordingStatusActive,
	}
	db.Create(&rec)

	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, &s)
	req = reqWithIDParam(req, "id", "rec-stop-1")
	h.RecordingStop(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// pages_recordings.go – RecordingDelete
// ---------------------------------------------------------------------------

func TestRecordingDelete_NoStation_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, nil)
	req = reqWithIDParam(req, "id", "rec-1")
	h.RecordingDelete(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestRecordingDelete_NotFound_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, &s)
	req = reqWithIDParam(req, "id", "nonexistent")
	h.RecordingDelete(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestRecordingDelete_ActiveRecording_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)

	rec := models.Recording{
		ID:        "rec-del-active",
		StationID: s.ID,
		Title:     "Active Rec",
		Status:    models.RecordingStatusActive,
	}
	db.Create(&rec)

	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, &s)
	req = reqWithIDParam(req, "id", "rec-del-active")
	h.RecordingDelete(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}

	// Should still exist (not deleted)
	var check models.Recording
	if err := db.First(&check, "id = ?", "rec-del-active").Error; err != nil {
		t.Fatalf("recording should still exist: %v", err)
	}
}

func TestRecordingDelete_CompletedRecording_Deletes(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)

	rec := models.Recording{
		ID:        "rec-del-done",
		StationID: s.ID,
		Title:     "Done Rec",
		Status:    models.RecordingStatusComplete,
	}
	db.Create(&rec)

	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, &s)
	req = reqWithIDParam(req, "id", "rec-del-done")
	h.RecordingDelete(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// pages_recordings.go – RecordingUpdateVisibility
// ---------------------------------------------------------------------------

func TestRecordingUpdateVisibility_NoStation_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, nil)
	req = reqWithIDParam(req, "id", "rec-1")
	h.RecordingUpdateVisibility(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestRecordingUpdateVisibility_WithStation_Returns200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)

	rec := models.Recording{
		ID:        "rec-vis-1",
		StationID: s.ID,
		Title:     "Rec",
		Status:    models.RecordingStatusComplete,
	}
	db.Create(&rec)

	form := url.Values{"visibility": {"public"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &s))
	req = reqWithIDParam(req, "id", "rec-vis-1")

	rr := httptest.NewRecorder()
	h.RecordingUpdateVisibility(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// pages_recordings.go – RecordingStart
// ---------------------------------------------------------------------------

func TestRecordingStart_NoStation_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, nil)
	h.RecordingStart(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestRecordingStart_NoMount_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)

	// No mount in db for this station; should redirect
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, &s)
	h.RecordingStart(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestRecordingStart_HasMount_NoService_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)

	mount := models.Mount{ID: "mnt-1", StationID: s.ID, Name: "Main"}
	db.Create(&mount)

	form := url.Values{"mount_id": {"mnt-1"}, "title": {"Test Recording"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &s))

	rr := httptest.NewRecorder()
	h.RecordingStart(rr, req)
	// recordingSvc is nil, so should redirect
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// pages_users.go – UserList
// ---------------------------------------------------------------------------

func TestUserList_Admin_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.UserList(rr, reqWithUser(http.MethodGet, "/", &u))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUserList_Mod_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := models.User{ID: "mod-1", Email: "mod@example.com", Password: "x", PlatformRole: models.PlatformRoleMod}
	db.Create(&u)
	rr := httptest.NewRecorder()
	h.UserList(rr, reqWithUser(http.MethodGet, "/", &u))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_users.go – UserNew
// ---------------------------------------------------------------------------

func TestUserNew_Admin_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.UserNew(rr, reqWithUser(http.MethodGet, "/", &u))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUserNew_Mod_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := models.User{ID: "mod-2", Email: "mod2@example.com", Password: "x", PlatformRole: models.PlatformRoleMod}
	db.Create(&u)
	rr := httptest.NewRecorder()
	h.UserNew(rr, reqWithUser(http.MethodGet, "/", &u))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_users.go – UserCreate
// ---------------------------------------------------------------------------

func TestUserCreate_MissingFields_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)

	form := url.Values{"email": {""}, "password": {""}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.UserCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestUserCreate_ModCreatingElevatedRole_Forbidden(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	mod := models.User{ID: "mod-3", Email: "mod3@example.com", Password: "x", PlatformRole: models.PlatformRoleMod}
	db.Create(&mod)

	form := url.Values{"email": {"newadmin@example.com"}, "password": {"pass123"}, "platform_role": {"platform_admin"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &mod))

	rr := httptest.NewRecorder()
	h.UserCreate(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestUserCreate_DuplicateEmail_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	miscRegularUser(t, db) // creates misc-user@example.com

	form := url.Values{"email": {"misc-user@example.com"}, "password": {"pass123"}, "platform_role": {"user"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.UserCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestUserCreate_Success_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)

	form := url.Values{"email": {"newuser@example.com"}, "password": {"pass123"}, "platform_role": {"user"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.UserCreate(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestUserCreate_HTMX_SetsRedirectHeader(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)

	form := url.Values{"email": {"htmxuser@example.com"}, "password": {"pass123"}, "platform_role": {"user"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.UserCreate(rr, req)
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatalf("expected HX-Redirect header to be set")
	}
}

// ---------------------------------------------------------------------------
// pages_users.go – UserDetail
// ---------------------------------------------------------------------------

func TestUserDetail_NotFound_Returns404(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodGet, "/", &u)
	req = reqWithIDParam(req, "id", "nonexistent")
	h.UserDetail(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestUserDetail_Found_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	target := miscRegularUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodGet, "/", &u)
	req = reqWithIDParam(req, "id", target.ID)
	h.UserDetail(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_users.go – UserEdit
// ---------------------------------------------------------------------------

func TestUserEdit_NotFound_Returns404(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodGet, "/", &u)
	req = reqWithIDParam(req, "id", "nonexistent")
	h.UserEdit(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestUserEdit_ModEditingAdmin_Forbidden(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	mod := models.User{ID: "mod-4", Email: "mod4@example.com", Password: "x", PlatformRole: models.PlatformRoleMod}
	db.Create(&mod)
	admin := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodGet, "/", &mod)
	req = reqWithIDParam(req, "id", admin.ID)
	h.UserEdit(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestUserEdit_Admin_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	target := miscRegularUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodGet, "/", &u)
	req = reqWithIDParam(req, "id", target.ID)
	h.UserEdit(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUserEdit_Mod_EditRegularUser_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	mod := models.User{ID: "mod-edit-regular", Email: "mod-editreg@example.com", Password: "x", PlatformRole: models.PlatformRoleMod}
	db.Create(&mod)
	target := miscRegularUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodGet, "/", &mod)
	req = reqWithIDParam(req, "id", target.ID)
	h.UserEdit(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for mod editing regular user, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_users.go – UserUpdate
// ---------------------------------------------------------------------------

func TestUserUpdate_NotFound_Returns404(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodPost, "/", &u)
	req = reqWithIDParam(req, "id", "nonexistent")
	h.UserUpdate(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestUserUpdate_ModEditingAdmin_Forbidden(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	mod := models.User{ID: "mod-5", Email: "mod5@example.com", Password: "x", PlatformRole: models.PlatformRoleMod}
	db.Create(&mod)
	admin := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodPost, "/", &mod)
	req = reqWithIDParam(req, "id", admin.ID)
	h.UserUpdate(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestUserUpdate_Success_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	target := miscRegularUser(t, db)

	form := url.Values{"email": {"updated@example.com"}, "platform_role": {"user"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	req = reqWithIDParam(req, "id", target.ID)

	rr := httptest.NewRecorder()
	h.UserUpdate(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestUserUpdate_HTMX_SetsRedirectHeader(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	target := miscRegularUser(t, db)

	form := url.Values{"email": {"updated2@example.com"}, "platform_role": {"user"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	req = reqWithIDParam(req, "id", target.ID)

	rr := httptest.NewRecorder()
	h.UserUpdate(rr, req)
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatalf("expected HX-Redirect header to be set")
	}
}

// ---------------------------------------------------------------------------
// pages_users.go – UserDelete
// ---------------------------------------------------------------------------

func TestUserDelete_NotFound_Returns404(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodPost, "/", &u)
	req = reqWithIDParam(req, "id", "nonexistent")
	h.UserDelete(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestUserDelete_Self_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodPost, "/", &u)
	req = reqWithIDParam(req, "id", u.ID)
	h.UserDelete(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestUserDelete_ModDeletingAdmin_Forbidden(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	mod := models.User{ID: "mod-6", Email: "mod6@example.com", Password: "x", PlatformRole: models.PlatformRoleMod}
	db.Create(&mod)
	admin := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodPost, "/", &mod)
	req = reqWithIDParam(req, "id", admin.ID)
	h.UserDelete(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestUserDelete_Success_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	target := miscRegularUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodPost, "/", &u)
	req = reqWithIDParam(req, "id", target.ID)
	h.UserDelete(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestUserDelete_HTMX_SetsRedirectHeader(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	target := models.User{ID: "to-del-htmx", Email: "todel-htmx@example.com", Password: "x", PlatformRole: models.PlatformRoleUser}
	db.Create(&target)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	req = reqWithIDParam(req, "id", target.ID)

	rr := httptest.NewRecorder()
	h.UserDelete(rr, req)
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatalf("expected HX-Redirect header to be set")
	}
}

// ---------------------------------------------------------------------------
// pages_station_users.go – StationUserList
// ---------------------------------------------------------------------------

func TestStationUserList_NoStation_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.StationUserList(rr, reqWithUserAndStation(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestStationUserList_NoPermission_Forbidden(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscRegularUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.StationUserList(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestStationUserList_Admin_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.StationUserList(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_station_users.go – StationUserInvite
// ---------------------------------------------------------------------------

func TestStationUserInvite_NoStation_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.StationUserInvite(rr, reqWithUserAndStation(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestStationUserInvite_NoPermission_Forbidden(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscRegularUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.StationUserInvite(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestStationUserInvite_Admin_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.StationUserInvite(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestStationUserInvite_WithExistingUsers_CoversNotInBranch(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	// Seed a station user so existingUserIDs is non-empty → covers the NOT IN branch
	db.Create(&models.StationUser{ID: "su-existing-invite", UserID: u.ID, StationID: s.ID, Role: models.StationRoleDJ})
	rr := httptest.NewRecorder()
	h.StationUserInvite(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_station_users.go – StationUserAdd
// ---------------------------------------------------------------------------

func TestStationUserAdd_NoStation_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.StationUserAdd(rr, reqWithUserAndStation(http.MethodPost, "/", &u, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestStationUserAdd_NoPermission_Forbidden(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscRegularUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.StationUserAdd(rr, reqWithUserAndStation(http.MethodPost, "/", &u, &s))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestStationUserAdd_UserNotFound_Returns404(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)

	form := url.Values{"user_id": {"nonexistent"}, "role": {"dj"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &s))

	rr := httptest.NewRecorder()
	h.StationUserAdd(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestStationUserAdd_InvalidRole_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	target := miscRegularUser(t, db)

	form := url.Values{"user_id": {target.ID}, "role": {"invalid_role"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &s))

	rr := httptest.NewRecorder()
	h.StationUserAdd(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestStationUserAdd_AlreadyMember_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	target := miscRegularUser(t, db)

	// Add them first
	existing := models.StationUser{ID: "su-existing", UserID: target.ID, StationID: s.ID, Role: models.StationRoleDJ}
	db.Create(&existing)

	form := url.Values{"user_id": {target.ID}, "role": {"dj"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &s))

	rr := httptest.NewRecorder()
	h.StationUserAdd(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestStationUserAdd_Success_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	target := miscRegularUser(t, db)

	form := url.Values{"user_id": {target.ID}, "role": {"dj"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &s))

	rr := httptest.NewRecorder()
	h.StationUserAdd(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// pages_station_users.go – StationUserEdit
// ---------------------------------------------------------------------------

func TestStationUserEdit_NoStation_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/", &u, nil)
	req = reqWithIDParam(req, "id", "su-1")
	h.StationUserEdit(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestStationUserEdit_NotFound_Returns404(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/", &u, &s)
	req = reqWithIDParam(req, "id", "nonexistent")
	h.StationUserEdit(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestStationUserEdit_WrongStation_Forbidden(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	otherStation := models.Station{ID: "other-st", Name: "Other", Active: true}
	db.Create(&otherStation)
	target := miscRegularUser(t, db)

	su := models.StationUser{ID: "su-wrong-st", UserID: target.ID, StationID: otherStation.ID, Role: models.StationRoleDJ}
	db.Create(&su)

	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/", &u, &s)
	req = reqWithIDParam(req, "id", "su-wrong-st")
	h.StationUserEdit(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestStationUserEdit_Found_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	target := miscRegularUser(t, db)

	su := models.StationUser{ID: "su-edit-ok", UserID: target.ID, StationID: s.ID, Role: models.StationRoleDJ}
	db.Create(&su)

	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/", &u, &s)
	req = reqWithIDParam(req, "id", "su-edit-ok")
	h.StationUserEdit(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_station_users.go – StationUserUpdate
// ---------------------------------------------------------------------------

func TestStationUserUpdate_NoStation_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, nil)
	req = reqWithIDParam(req, "id", "su-1")
	h.StationUserUpdate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestStationUserUpdate_NotFound_Returns404(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, &s)
	req = reqWithIDParam(req, "id", "nonexistent")
	h.StationUserUpdate(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestStationUserUpdate_InvalidRole_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	target := miscRegularUser(t, db)

	su := models.StationUser{ID: "su-upd-inv", UserID: target.ID, StationID: s.ID, Role: models.StationRoleDJ}
	db.Create(&su)

	form := url.Values{"role": {"invalid"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &s))
	req = reqWithIDParam(req, "id", "su-upd-inv")

	rr := httptest.NewRecorder()
	h.StationUserUpdate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestStationUserUpdate_DemoteOwner_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	target := miscRegularUser(t, db)

	su := models.StationUser{ID: "su-owner-demote", UserID: target.ID, StationID: s.ID, Role: models.StationRoleOwner}
	db.Create(&su)

	form := url.Values{"role": {"dj"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &s))
	req = reqWithIDParam(req, "id", "su-owner-demote")

	rr := httptest.NewRecorder()
	h.StationUserUpdate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestStationUserUpdate_Success_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	target := miscRegularUser(t, db)

	su := models.StationUser{ID: "su-upd-ok", UserID: target.ID, StationID: s.ID, Role: models.StationRoleDJ}
	db.Create(&su)

	form := url.Values{"role": {"manager"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &s))
	req = reqWithIDParam(req, "id", "su-upd-ok")

	rr := httptest.NewRecorder()
	h.StationUserUpdate(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_station_users.go – StationUserRemove
// ---------------------------------------------------------------------------

func TestStationUserRemove_NoStation_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, nil)
	req = reqWithIDParam(req, "id", "su-1")
	h.StationUserRemove(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestStationUserRemove_OwnerRole_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	target := miscRegularUser(t, db)

	su := models.StationUser{ID: "su-rm-owner", UserID: target.ID, StationID: s.ID, Role: models.StationRoleOwner}
	db.Create(&su)

	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, &s)
	req = reqWithIDParam(req, "id", "su-rm-owner")
	h.StationUserRemove(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestStationUserRemove_Self_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)

	su := models.StationUser{ID: "su-rm-self", UserID: u.ID, StationID: s.ID, Role: models.StationRoleDJ}
	db.Create(&su)

	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, &s)
	req = reqWithIDParam(req, "id", "su-rm-self")
	h.StationUserRemove(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestStationUserRemove_Success_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	target := miscRegularUser(t, db)

	su := models.StationUser{ID: "su-rm-ok", UserID: target.ID, StationID: s.ID, Role: models.StationRoleDJ}
	db.Create(&su)

	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodPost, "/", &u, &s)
	req = reqWithIDParam(req, "id", "su-rm-ok")
	h.StationUserRemove(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// pages_dj.go – DJAvailability
// ---------------------------------------------------------------------------

func TestDJAvailability_NoStation_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.DJAvailability(rr, reqWithUserAndStation(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestDJAvailability_NoUser_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.DJAvailability(rr, reqWithUserAndStation(http.MethodGet, "/", nil, &s))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestDJAvailability_WithUserAndStation_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.DJAvailability(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_dj.go – DJRequests
// ---------------------------------------------------------------------------

func TestDJRequests_NoStation_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.DJRequests(rr, reqWithUserAndStation(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestDJRequests_NoUser_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.DJRequests(rr, reqWithUserAndStation(http.MethodGet, "/", nil, &s))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestDJRequests_Admin_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.DJRequests(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDJRequests_RegularUser_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscRegularUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.DJRequests(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_dj.go – DJAvailabilityJSON
// ---------------------------------------------------------------------------

func TestDJAvailabilityJSON_NoStation_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.DJAvailabilityJSON(rr, reqWithUserAndStation(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDJAvailabilityJSON_NoUser_Unauthorized(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.DJAvailabilityJSON(rr, reqWithUserAndStation(http.MethodGet, "/", nil, &s))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestDJAvailabilityJSON_WithUserAndStation_ReturnsJSON(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.DJAvailabilityJSON(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON content-type, got %q", ct)
	}
}

// ---------------------------------------------------------------------------
// pages_webdj.go – WebDJConsole
// ---------------------------------------------------------------------------

func TestWebDJConsole_NoStation_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.WebDJConsole(rr, reqWithUserAndStation(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestWebDJConsole_WithStation_Renders200(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.WebDJConsole(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_webdj.go – WebDJLibrarySearch
// ---------------------------------------------------------------------------

func TestWebDJLibrarySearch_NoStation_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.WebDJLibrarySearch(rr, reqWithUserAndStation(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestWebDJLibrarySearch_WithStation_ReturnsJSON(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/?q=test", &u, &s)
	h.WebDJLibrarySearch(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON, got %q", ct)
	}
}

func TestWebDJLibrarySearch_WithGenre_ReturnsJSON(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/?genre=Rock", &u, &s)
	h.WebDJLibrarySearch(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// pages_webdj.go – WebDJGenres
// ---------------------------------------------------------------------------

func TestWebDJGenres_NoStation_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.WebDJGenres(rr, reqWithUserAndStation(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestWebDJGenres_WithStation_ReturnsJSON(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.WebDJGenres(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON, got %q", ct)
	}
}

// ---------------------------------------------------------------------------
// pages_webdj.go – WebDJPlaylists
// ---------------------------------------------------------------------------

func TestWebDJPlaylists_NoStation_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.WebDJPlaylists(rr, reqWithUserAndStation(http.MethodGet, "/", &u, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestWebDJPlaylists_WithStation_ReturnsJSON(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	h.WebDJPlaylists(rr, reqWithUserAndStation(http.MethodGet, "/", &u, &s))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON, got %q", ct)
	}
}

// ---------------------------------------------------------------------------
// pages_webdj.go – WebDJPlaylistItems
// ---------------------------------------------------------------------------

func TestWebDJPlaylistItems_NoStation_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/", &u, nil)
	req = reqWithIDParam(req, "id", "pl-1")
	h.WebDJPlaylistItems(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestWebDJPlaylistItems_PlaylistNotFound_Returns404(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/", &u, &s)
	req = reqWithIDParam(req, "id", "nonexistent")
	h.WebDJPlaylistItems(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestWebDJPlaylistItems_Found_ReturnsJSON(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)

	pl := models.Playlist{ID: "pl-webdj-1", StationID: s.ID, Name: "Test Playlist"}
	db.Create(&pl)

	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/", &u, &s)
	req = reqWithIDParam(req, "id", "pl-webdj-1")
	h.WebDJPlaylistItems(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// pages_webdj.go – WebDJMediaArtwork
// ---------------------------------------------------------------------------

func TestWebDJMediaArtwork_NotFound_Returns404(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodGet, "/", &u)
	req = reqWithIDParam(req, "id", "nonexistent")
	h.WebDJMediaArtwork(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestWebDJMediaArtwork_NoArtwork_ReturnsSVG(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)

	media := models.MediaItem{
		ID:            "mi-artwork-empty",
		StationID:     s.ID,
		Title:         "Track",
		Path:          "path/to/file.mp3",
		AnalysisState: "complete",
	}
	db.Create(&media)

	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodGet, "/", &u)
	req = reqWithIDParam(req, "id", "mi-artwork-empty")
	h.WebDJMediaArtwork(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "svg") {
		t.Fatalf("expected SVG content-type for missing artwork, got %q", ct)
	}
}

func TestWebDJMediaArtwork_WithArtwork_ServesImage(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)

	media := models.MediaItem{
		ID:            "mi-artwork-ok",
		StationID:     s.ID,
		Title:         "Track",
		Path:          "path/to/file.mp3",
		AnalysisState: "complete",
		Artwork:       []byte("fakeimagebytes"),
		ArtworkMime:   "image/jpeg",
	}
	db.Create(&media)

	rr := httptest.NewRecorder()
	req := reqWithUser(http.MethodGet, "/", &u)
	req = reqWithIDParam(req, "id", "mi-artwork-ok")
	h.WebDJMediaArtwork(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %q", ct)
	}
}

// ---------------------------------------------------------------------------
// pages_webdj.go – WebDJMediaStream
// ---------------------------------------------------------------------------

func TestWebDJMediaStream_NoStation_BadRequest(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/", &u, nil)
	req = reqWithIDParam(req, "id", "mi-1")
	h.WebDJMediaStream(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestWebDJMediaStream_NotFound_Returns404(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)
	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/", &u, &s)
	req = reqWithIDParam(req, "id", "nonexistent")
	h.WebDJMediaStream(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestWebDJMediaStream_Found_Redirects(t *testing.T) {
	db := newMiscTestDB(t)
	h := newMiscHandler(t, db)
	u := miscAdminUser(t, db)
	s := miscStation(t, db)

	media := models.MediaItem{
		ID:            "mi-stream-1",
		StationID:     s.ID,
		Title:         "Track",
		Path:          "path/to/file.mp3",
		AnalysisState: "complete",
	}
	db.Create(&media)

	rr := httptest.NewRecorder()
	req := reqWithUserAndStation(http.MethodGet, "/", &u, &s)
	req = reqWithIDParam(req, "id", "mi-stream-1")
	h.WebDJMediaStream(rr, req)
	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected 307, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); !strings.Contains(loc, "mi-stream-1") {
		t.Fatalf("expected redirect to contain media ID, got %q", loc)
	}
}
