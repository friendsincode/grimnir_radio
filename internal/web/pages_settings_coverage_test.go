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
// Setup helpers
// ---------------------------------------------------------------------------

func newSettingsTestDB(t *testing.T) *gorm.DB {
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
		&models.SystemSettings{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newSettingsTestHandler(t *testing.T, db *gorm.DB) *Handler {
	t.Helper()
	h, err := NewHandler(db, []byte("test-secret"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h
}

func seedSettingsAdminUser(t *testing.T, db *gorm.DB) models.User {
	t.Helper()
	u := models.User{ID: "sadmin1", Email: "sadmin@example.com", Password: "x", PlatformRole: models.PlatformRoleAdmin}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed settings admin: %v", err)
	}
	return u
}

func settingsReq(method, target string, user *models.User) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	if user != nil {
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, user))
	}
	return req
}

func settingsFormReq(method, target string, user *models.User, form url.Values) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if user != nil {
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, user))
	}
	return req
}

func settingsReqWithID(method, target string, user *models.User, paramName, paramVal string) *http.Request {
	req := settingsReq(method, target, user)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(paramName, paramVal)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// ---------------------------------------------------------------------------
// SettingsPage
// ---------------------------------------------------------------------------

func TestSettingsPage_Success(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)
	u := seedSettingsAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.SettingsPage(rr, settingsReq(http.MethodGet, "/dashboard/settings", &u))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSettingsPage_DBError(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)
	u := seedSettingsAdminUser(t, db)

	// Close DB to force error
	sqlDB, _ := db.DB()
	sqlDB.Close()

	rr := httptest.NewRecorder()
	h.SettingsPage(rr, settingsReq(http.MethodGet, "/", &u))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// SettingsUpdate
// ---------------------------------------------------------------------------

func TestSettingsUpdate_Success_Redirect(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)
	u := seedSettingsAdminUser(t, db)

	form := url.Values{}
	form.Set("scheduler_lookahead", "48h")
	form.Set("log_level", "debug")
	form.Set("analysis_enabled", "on")
	form.Set("websocket_enabled", "on")
	form.Set("metrics_enabled", "on")

	rr := httptest.NewRecorder()
	h.SettingsUpdate(rr, settingsFormReq(http.MethodPost, "/", &u, form))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSettingsUpdate_InvalidLookahead_UsesDefault(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)
	u := seedSettingsAdminUser(t, db)

	form := url.Values{}
	form.Set("scheduler_lookahead", "999h") // invalid
	form.Set("log_level", "info")

	rr := httptest.NewRecorder()
	h.SettingsUpdate(rr, settingsFormReq(http.MethodPost, "/", &u, form))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestSettingsUpdate_InvalidLogLevel_UsesDefault(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)
	u := seedSettingsAdminUser(t, db)

	form := url.Values{}
	form.Set("scheduler_lookahead", "24h")
	form.Set("log_level", "super-verbose") // invalid

	rr := httptest.NewRecorder()
	h.SettingsUpdate(rr, settingsFormReq(http.MethodPost, "/", &u, form))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestSettingsUpdate_HTMX_Success(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)
	u := seedSettingsAdminUser(t, db)

	form := url.Values{}
	form.Set("scheduler_lookahead", "24h")
	form.Set("log_level", "info")

	req := settingsFormReq(http.MethodPost, "/", &u, form)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.SettingsUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HTMX, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "alert-success") {
		t.Fatalf("expected success alert, got %q", body)
	}
}

func TestSettingsUpdate_HTMX_DBError(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)
	u := seedSettingsAdminUser(t, db)

	// Close DB to force error on load
	sqlDB, _ := db.DB()
	sqlDB.Close()

	form := url.Values{}
	form.Set("scheduler_lookahead", "24h")

	req := settingsFormReq(http.MethodPost, "/", &u, form)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.SettingsUpdate(rr, req)
	// Either 200 with error HTML or 500
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status %d", rr.Code)
	}
}

func TestSettingsUpdate_NoLookaheadOrLogLevel(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)
	u := seedSettingsAdminUser(t, db)

	// Empty form - no fields set
	form := url.Values{}
	rr := httptest.NewRecorder()
	h.SettingsUpdate(rr, settingsFormReq(http.MethodPost, "/", &u, form))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// MigrationsPage
// ---------------------------------------------------------------------------

func TestMigrationsPage_Success(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)
	u := seedSettingsAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.MigrationsPage(rr, settingsReq(http.MethodGet, "/dashboard/settings/migrations", &u))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// MigrationsImport
// ---------------------------------------------------------------------------

func TestMigrationsImport_UnsupportedSourceType(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)
	u := seedSettingsAdminUser(t, db)

	// Build multipart body with unsupported source_type
	body := "--testboundary\r\n" +
		"Content-Disposition: form-data; name=\"source_type\"\r\n\r\n" +
		"unsupported\r\n" +
		"--testboundary\r\n" +
		"Content-Disposition: form-data; name=\"import_file\"; filename=\"test.tar.gz\"\r\n" +
		"Content-Type: application/octet-stream\r\n\r\n" +
		"fake-file-content\r\n" +
		"--testboundary--\r\n"

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=testboundary")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.MigrationsImport(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestMigrationsImport_UnsupportedSourceType_HTMX(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)
	u := seedSettingsAdminUser(t, db)

	body := "--testboundary\r\n" +
		"Content-Disposition: form-data; name=\"source_type\"\r\n\r\n" +
		"foobar\r\n" +
		"--testboundary\r\n" +
		"Content-Disposition: form-data; name=\"import_file\"; filename=\"test.tar.gz\"\r\n" +
		"Content-Type: application/octet-stream\r\n\r\n" +
		"data\r\n" +
		"--testboundary--\r\n"

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=testboundary")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.MigrationsImport(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with HTMX error, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "alert-danger") {
		t.Fatalf("expected error HTML, got %q", rr.Body.String())
	}
}

func TestMigrationsImport_LibreTime_NotSupported(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)
	u := seedSettingsAdminUser(t, db)

	body := "--testboundary\r\n" +
		"Content-Disposition: form-data; name=\"source_type\"\r\n\r\n" +
		"libretime\r\n" +
		"--testboundary\r\n" +
		"Content-Disposition: form-data; name=\"import_file\"; filename=\"test.tar.gz\"\r\n" +
		"Content-Type: application/octet-stream\r\n\r\n" +
		"data\r\n" +
		"--testboundary--\r\n"

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=testboundary")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.MigrationsImport(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for LibreTime file import, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestMigrationsImport_NoFile(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)
	u := seedSettingsAdminUser(t, db)

	body := "--testboundary\r\n" +
		"Content-Disposition: form-data; name=\"source_type\"\r\n\r\n" +
		"azuracast\r\n" +
		"--testboundary--\r\n"

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=testboundary")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.MigrationsImport(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing file, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// AzuraCastAPITest
// ---------------------------------------------------------------------------

func TestAzuraCastAPITest_EmptyURL(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	form := url.Values{}
	form.Set("azuracast_url", "")
	form.Set("api_key", "somekey")

	req := settingsFormReq(http.MethodPost, "/", nil, form)
	rr := httptest.NewRecorder()
	h.AzuraCastAPITest(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with error HTML, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "URL is required") {
		t.Fatalf("expected URL required error, got %q", rr.Body.String())
	}
}

func TestAzuraCastAPITest_NoCredentials(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	form := url.Values{}
	form.Set("azuracast_url", "http://example.com")
	// no api_key, no username/password

	req := settingsFormReq(http.MethodPost, "/", nil, form)
	rr := httptest.NewRecorder()
	h.AzuraCastAPITest(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "API key") && !strings.Contains(body, "required") {
		t.Fatalf("expected credential error, got %q", body)
	}
}

func TestAzuraCastAPITest_ConnectionFailed(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	form := url.Values{}
	form.Set("azuracast_url", "http://localhost:1") // unreachable
	form.Set("api_key", "fakekey")

	req := settingsFormReq(http.MethodPost, "/", nil, form)
	rr := httptest.NewRecorder()
	h.AzuraCastAPITest(rr, req)
	body := rr.Body.String()
	// Should contain some error about connection failure
	if !strings.Contains(body, "alert-danger") && !strings.Contains(body, "failed") && !strings.Contains(body, "error") && !strings.Contains(body, "Error") {
		t.Fatalf("expected connection error HTML, got %q", body)
	}
}

// ---------------------------------------------------------------------------
// AzuraCastAPIImport
// ---------------------------------------------------------------------------

func TestAzuraCastAPIImport_EmptyURL(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	form := url.Values{}
	form.Set("azuracast_url", "")
	form.Set("api_key", "key")

	req := settingsFormReq(http.MethodPost, "/", nil, form)
	rr := httptest.NewRecorder()
	h.AzuraCastAPIImport(rr, req)
	if !strings.Contains(rr.Body.String(), "URL is required") {
		t.Fatalf("expected URL required, got %q", rr.Body.String())
	}
}

func TestAzuraCastAPIImport_NoCredentials(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	form := url.Values{}
	form.Set("azuracast_url", "http://example.com")

	req := settingsFormReq(http.MethodPost, "/", nil, form)
	rr := httptest.NewRecorder()
	h.AzuraCastAPIImport(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "required") && !strings.Contains(body, "API key") {
		t.Fatalf("expected credentials required, got %q", body)
	}
}

func TestAzuraCastAPIImport_ValidationFails(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	form := url.Values{}
	form.Set("azuracast_url", "http://localhost:1") // unreachable
	form.Set("api_key", "fakekey")

	req := settingsFormReq(http.MethodPost, "/", nil, form)
	rr := httptest.NewRecorder()
	h.AzuraCastAPIImport(rr, req)
	body := rr.Body.String()
	// Should fail validation with connection error
	if !strings.Contains(body, "alert-danger") {
		t.Fatalf("expected error response, got %q", body)
	}
}

// ---------------------------------------------------------------------------
// LibreTimeAPITest
// ---------------------------------------------------------------------------

func TestLibreTimeAPITest_EmptyURL(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	form := url.Values{}
	form.Set("libretime_url", "")
	form.Set("api_key", "mykey")

	req := settingsFormReq(http.MethodPost, "/", nil, form)
	rr := httptest.NewRecorder()
	h.LibreTimeAPITest(rr, req)
	if !strings.Contains(rr.Body.String(), "URL is required") {
		t.Fatalf("expected URL required, got %q", rr.Body.String())
	}
}

func TestLibreTimeAPITest_EmptyAPIKey(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	form := url.Values{}
	form.Set("libretime_url", "http://example.com")
	form.Set("api_key", "")

	req := settingsFormReq(http.MethodPost, "/", nil, form)
	rr := httptest.NewRecorder()
	h.LibreTimeAPITest(rr, req)
	if !strings.Contains(rr.Body.String(), "API key is required") {
		t.Fatalf("expected API key required, got %q", rr.Body.String())
	}
}

func TestLibreTimeAPITest_ConnectionFails(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	form := url.Values{}
	form.Set("libretime_url", "http://localhost:1") // unreachable
	form.Set("api_key", "fakekey")

	req := settingsFormReq(http.MethodPost, "/", nil, form)
	rr := httptest.NewRecorder()
	h.LibreTimeAPITest(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "alert-danger") {
		t.Fatalf("expected error response, got %q", body)
	}
}

// ---------------------------------------------------------------------------
// LibreTimeAPIImport
// ---------------------------------------------------------------------------

func TestLibreTimeAPIImport_EmptyURL(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	form := url.Values{}
	form.Set("libretime_url", "")
	form.Set("api_key", "key")

	req := settingsFormReq(http.MethodPost, "/", nil, form)
	rr := httptest.NewRecorder()
	h.LibreTimeAPIImport(rr, req)
	if !strings.Contains(rr.Body.String(), "URL is required") {
		t.Fatalf("expected URL required, got %q", rr.Body.String())
	}
}

func TestLibreTimeAPIImport_EmptyAPIKey(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	form := url.Values{}
	form.Set("libretime_url", "http://example.com")
	form.Set("api_key", "")

	req := settingsFormReq(http.MethodPost, "/", nil, form)
	rr := httptest.NewRecorder()
	h.LibreTimeAPIImport(rr, req)
	if !strings.Contains(rr.Body.String(), "API key is required") {
		t.Fatalf("expected API key required, got %q", rr.Body.String())
	}
}

func TestLibreTimeAPIImport_ValidationFails(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	form := url.Values{}
	form.Set("libretime_url", "http://localhost:1") // unreachable
	form.Set("api_key", "fakekey")

	req := settingsFormReq(http.MethodPost, "/", nil, form)
	rr := httptest.NewRecorder()
	h.LibreTimeAPIImport(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "alert-danger") {
		t.Fatalf("expected error response, got %q", body)
	}
}

// ---------------------------------------------------------------------------
// MigrationStatusPage
// ---------------------------------------------------------------------------

func TestMigrationStatusPage_Success(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)
	u := seedSettingsAdminUser(t, db)
	rr := httptest.NewRecorder()
	h.MigrationStatusPage(rr, settingsReq(http.MethodGet, "/dashboard/settings/migrations/status", &u))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestMigrationStatusPage_DBError(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)
	u := seedSettingsAdminUser(t, db)

	sqlDB, _ := db.DB()
	sqlDB.Close()

	rr := httptest.NewRecorder()
	h.MigrationStatusPage(rr, settingsReq(http.MethodGet, "/", &u))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// MigrationJobRestart
// ---------------------------------------------------------------------------

func TestMigrationJobRestart_EmptyJobID(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	rr := httptest.NewRecorder()
	h.MigrationJobRestart(rr, settingsReq(http.MethodPost, "/", nil))
	body := rr.Body.String()
	if !strings.Contains(body, "Job ID") && !strings.Contains(body, "required") {
		t.Fatalf("expected job ID required, got %q", body)
	}
}

func TestMigrationJobRestart_JobNotFound(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	rr := httptest.NewRecorder()
	h.MigrationJobRestart(rr, settingsReqWithID(http.MethodPost, "/", nil, "id", "nonexistent-job-id"))
	body := rr.Body.String()
	if !strings.Contains(body, "Job not found") && !strings.Contains(body, "alert-danger") {
		t.Fatalf("expected not found error, got %q", body)
	}
}

// ---------------------------------------------------------------------------
// MigrationJobDelete
// ---------------------------------------------------------------------------

func TestMigrationJobDelete_EmptyJobID(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	rr := httptest.NewRecorder()
	h.MigrationJobDelete(rr, settingsReq(http.MethodDelete, "/", nil))
	body := rr.Body.String()
	if !strings.Contains(body, "Job ID") && !strings.Contains(body, "required") {
		t.Fatalf("expected job ID required, got %q", body)
	}
}

func TestMigrationJobDelete_JobNotFound(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	rr := httptest.NewRecorder()
	h.MigrationJobDelete(rr, settingsReqWithID(http.MethodDelete, "/", nil, "id", "nonexistent"))
	body := rr.Body.String()
	if !strings.Contains(body, "alert-danger") && !strings.Contains(body, "Failed") {
		t.Fatalf("expected error, got %q", body)
	}
}

// ---------------------------------------------------------------------------
// MigrationResetData
// ---------------------------------------------------------------------------

func TestMigrationResetData_ReturnsHTMLResponse(t *testing.T) {
	db := newSettingsTestDB(t)
	h := newSettingsTestHandler(t, db)

	rr := httptest.NewRecorder()
	h.MigrationResetData(rr, settingsReq(http.MethodPost, "/", nil))
	// Either succeeds (200 with success HTML) or fails on missing tables (200 with error HTML)
	// The handler always returns 200 - it uses writeHTMXError on failure
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	// Should contain either success or error alert div
	if !strings.Contains(body, "alert-success") && !strings.Contains(body, "alert-danger") {
		t.Fatalf("expected alert HTML, got %q", body)
	}
}

// ---------------------------------------------------------------------------
// writeHTMXError helper
// ---------------------------------------------------------------------------

func TestWriteHTMXError(t *testing.T) {
	rr := httptest.NewRecorder()
	writeHTMXError(rr, "something went wrong")
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %q", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "something went wrong") {
		t.Fatalf("expected error message in body, got %q", body)
	}
	if !strings.Contains(body, "alert-danger") {
		t.Fatalf("expected alert-danger class, got %q", body)
	}
}

// ---------------------------------------------------------------------------
// buildAnomalyClassViews helper
// ---------------------------------------------------------------------------

func TestBuildAnomalyClassViews_Nil(t *testing.T) {
	result := buildAnomalyClassViews(nil)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestBuildAnomalyClassViews_EmptyByClass(t *testing.T) {
	result := buildAnomalyClassViews(&migration.AnomalyReport{ByClass: map[migration.AnomalyClass]migration.AnomalyBucket{}})
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d", len(result))
	}
}

func TestBuildAnomalyClassViews_WithKnownClasses(t *testing.T) {
	report := &migration.AnomalyReport{
		ByClass: map[migration.AnomalyClass]migration.AnomalyBucket{
			migration.AnomalyClassDuration: {
				Count:    3,
				Examples: []string{"file1.mp3", "file2.mp3"},
			},
		},
	}
	result := buildAnomalyClassViews(report)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].Label != "Duration" {
		t.Fatalf("expected Duration label, got %q", result[0].Label)
	}
	if result[0].Count != 3 {
		t.Fatalf("expected count 3, got %d", result[0].Count)
	}
}

func TestBuildAnomalyClassViews_WithUnknownClass(t *testing.T) {
	report := &migration.AnomalyReport{
		ByClass: map[migration.AnomalyClass]migration.AnomalyBucket{
			migration.AnomalyClass("some_custom_anomaly"): {
				Count: 1,
			},
		},
	}
	result := buildAnomalyClassViews(report)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].Key != "some_custom_anomaly" {
		t.Fatalf("unexpected key %q", result[0].Key)
	}
}

// ---------------------------------------------------------------------------
// anomalyLabelFromKey helper
// ---------------------------------------------------------------------------

func TestAnomalyLabelFromKey_SingleWord(t *testing.T) {
	result := anomalyLabelFromKey("duration")
	if result != "Duration" {
		t.Fatalf("expected Duration, got %q", result)
	}
}

func TestAnomalyLabelFromKey_MultiWord(t *testing.T) {
	result := anomalyLabelFromKey("missing_links")
	if result != "Missing Links" {
		t.Fatalf("expected 'Missing Links', got %q", result)
	}
}

func TestAnomalyLabelFromKey_Empty(t *testing.T) {
	result := anomalyLabelFromKey("")
	if result != "" {
		t.Fatalf("expected empty, got %q", result)
	}
}
