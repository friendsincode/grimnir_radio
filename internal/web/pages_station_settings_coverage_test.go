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

func newSettingsDB(t *testing.T) *gorm.DB {
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

func newSettingsHandler(t *testing.T, db *gorm.DB) *Handler {
	t.Helper()
	h, err := NewHandler(db, []byte("test-secret"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h
}

func settingsStation(t *testing.T, db *gorm.DB) *models.Station {
	t.Helper()
	s := &models.Station{ID: "sST1", Name: "Settings Station", Active: true, Timezone: "UTC"}
	if err := db.Create(s).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	return s
}

func settingsAdminUser(t *testing.T, db *gorm.DB) *models.User {
	t.Helper()
	u := &models.User{ID: "uST-admin", Email: "settings-admin@example.com", Password: "x", PlatformRole: models.PlatformRoleAdmin}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	return u
}

func settingsOwnerUser(t *testing.T, db *gorm.DB, stationID string) *models.User {
	t.Helper()
	u := &models.User{ID: "uST-owner", Email: "settings-owner@example.com", Password: "x", PlatformRole: models.PlatformRoleUser}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("create owner user: %v", err)
	}
	su := &models.StationUser{
		ID:        "su-owner",
		UserID:    u.ID,
		StationID: stationID,
		Role:      models.StationRoleOwner,
	}
	if err := db.Create(su).Error; err != nil {
		t.Fatalf("create station user: %v", err)
	}
	return u
}

func settingsRegularUser(t *testing.T, db *gorm.DB) *models.User {
	t.Helper()
	u := &models.User{ID: "uST-dj", Email: "settings-dj@example.com", Password: "x", PlatformRole: models.PlatformRoleUser}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("create regular user: %v", err)
	}
	return u
}

func withSettingsContext(r *http.Request, station *models.Station, user *models.User) *http.Request {
	ctx := context.WithValue(r.Context(), ctxKeyStation, station)
	ctx = context.WithValue(ctx, ctxKeyUser, user)
	return r.WithContext(ctx)
}

func settingsFormRequest(t *testing.T, method, target string, form url.Values) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// ---------------------------------------------------------------------------
// StationSettings (GET)
// ---------------------------------------------------------------------------

func TestStationSettings_NoStation_Redirects(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/station/settings", nil)
	rr := httptest.NewRecorder()
	h.StationSettings(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/dashboard/stations/select" {
		t.Fatalf("expected redirect to station select, got %q", loc)
	}
}

func TestStationSettings_NilUser_Returns403(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/station/settings", nil)
	ctx := context.WithValue(req.Context(), ctxKeyStation, station)
	// No user in context
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.StationSettings(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestStationSettings_RegularUser_NoRole_Returns403(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	user := settingsRegularUser(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/station/settings", nil)
	req = withSettingsContext(req, station, user)
	rr := httptest.NewRecorder()
	h.StationSettings(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestStationSettings_AdminUser_Returns200(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	admin := settingsAdminUser(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/station/settings", nil)
	req = withSettingsContext(req, station, admin)
	rr := httptest.NewRecorder()
	h.StationSettings(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Settings Station") {
		t.Errorf("expected station name in page")
	}
}

func TestStationSettings_OwnerUser_Returns200(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	owner := settingsOwnerUser(t, db, station.ID)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/station/settings", nil)
	req = withSettingsContext(req, station, owner)
	rr := httptest.NewRecorder()
	h.StationSettings(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// StationSettingsUpdate (POST)
// ---------------------------------------------------------------------------

func TestStationSettingsUpdate_NoStation_Returns400(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)

	form := url.Values{"name": {"Updated Station"}, "timezone": {"UTC"}}
	req := settingsFormRequest(t, http.MethodPost, "/dashboard/station/settings", form)
	rr := httptest.NewRecorder()
	h.StationSettingsUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestStationSettingsUpdate_NilUser_Returns403(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)

	form := url.Values{"name": {"Updated Station"}}
	req := settingsFormRequest(t, http.MethodPost, "/dashboard/station/settings", form)
	ctx := context.WithValue(req.Context(), ctxKeyStation, station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.StationSettingsUpdate(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestStationSettingsUpdate_RegularUser_Returns403(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	user := settingsRegularUser(t, db)

	form := url.Values{"name": {"Updated Station"}}
	req := settingsFormRequest(t, http.MethodPost, "/dashboard/station/settings", form)
	req = withSettingsContext(req, station, user)
	rr := httptest.NewRecorder()
	h.StationSettingsUpdate(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestStationSettingsUpdate_EmptyName_Returns400(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	admin := settingsAdminUser(t, db)

	form := url.Values{"name": {""}, "timezone": {"UTC"}}
	req := settingsFormRequest(t, http.MethodPost, "/dashboard/station/settings", form)
	req = withSettingsContext(req, station, admin)
	rr := httptest.NewRecorder()
	h.StationSettingsUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty name, got %d", rr.Code)
	}
}

func TestStationSettingsUpdate_ValidData_Success_Redirects(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	admin := settingsAdminUser(t, db)

	form := url.Values{
		"name":                          {"New Station Name"},
		"timezone":                      {"America/New_York"},
		"description":                   {"A test station"},
		"schedule_boundary_mode":        {"soft"},
		"schedule_soft_overrun_minutes": {"5"},
		"recording_default_format":      {"flac"},
		"recording_quota_gb":            {"10"},
		"crossfade_duration_ms":         {"2000"},
	}
	req := settingsFormRequest(t, http.MethodPost, "/dashboard/station/settings", form)
	req = withSettingsContext(req, station, admin)
	rr := httptest.NewRecorder()
	h.StationSettingsUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Location") != "/dashboard/station/settings" {
		t.Errorf("expected redirect to settings, got %q", rr.Header().Get("Location"))
	}

	// Verify DB update
	var updated models.Station
	db.First(&updated, "id = ?", station.ID)
	if updated.Name != "New Station Name" {
		t.Errorf("expected name 'New Station Name', got %q", updated.Name)
	}
	if updated.Timezone != "America/New_York" {
		t.Errorf("expected timezone 'America/New_York', got %q", updated.Timezone)
	}
}

func TestStationSettingsUpdate_SoftBoundaryMode(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	admin := settingsAdminUser(t, db)

	form := url.Values{
		"name":                          {"Station Soft"},
		"timezone":                      {"UTC"},
		"schedule_boundary_mode":        {"soft"},
		"schedule_soft_overrun_minutes": {"10"},
	}
	req := settingsFormRequest(t, http.MethodPost, "/dashboard/station/settings", form)
	req = withSettingsContext(req, station, admin)
	rr := httptest.NewRecorder()
	h.StationSettingsUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}

	var updated models.Station
	db.First(&updated, "id = ?", station.ID)
	if updated.ScheduleBoundaryMode != "soft" {
		t.Errorf("expected soft boundary mode, got %q", updated.ScheduleBoundaryMode)
	}
	if updated.ScheduleSoftOverrunSeconds != 600 {
		t.Errorf("expected 600s overrun, got %d", updated.ScheduleSoftOverrunSeconds)
	}
}

func TestStationSettingsUpdate_HardBoundaryMode_Default(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	admin := settingsAdminUser(t, db)

	form := url.Values{
		"name":                   {"Station Hard"},
		"timezone":               {"UTC"},
		"schedule_boundary_mode": {"hard"},
	}
	req := settingsFormRequest(t, http.MethodPost, "/dashboard/station/settings", form)
	req = withSettingsContext(req, station, admin)
	rr := httptest.NewRecorder()
	h.StationSettingsUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}

	var updated models.Station
	db.First(&updated, "id = ?", station.ID)
	if updated.ScheduleBoundaryMode != "hard" {
		t.Errorf("expected hard boundary mode, got %q", updated.ScheduleBoundaryMode)
	}
}

func TestStationSettingsUpdate_RecordingSettings(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	admin := settingsAdminUser(t, db)

	form := url.Values{
		"name":                     {"Station Rec"},
		"timezone":                 {"UTC"},
		"recording_auto_record":    {"on"},
		"recording_default_format": {"opus"},
		"recording_quota_gb":       {"5"},
	}
	req := settingsFormRequest(t, http.MethodPost, "/dashboard/station/settings", form)
	req = withSettingsContext(req, station, admin)
	rr := httptest.NewRecorder()
	h.StationSettingsUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}

	var updated models.Station
	db.First(&updated, "id = ?", station.ID)
	if !updated.RecordingAutoRecord {
		t.Errorf("expected RecordingAutoRecord=true")
	}
	if updated.RecordingDefaultFormat != "opus" {
		t.Errorf("expected opus format, got %q", updated.RecordingDefaultFormat)
	}
	if updated.RecordingQuotaBytes != 5*1073741824 {
		t.Errorf("expected 5GB quota, got %d", updated.RecordingQuotaBytes)
	}
}

func TestStationSettingsUpdate_CrossfadeSettings(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	admin := settingsAdminUser(t, db)

	form := url.Values{
		"name":                  {"Station CF"},
		"timezone":              {"UTC"},
		"crossfade_enabled":     {"on"},
		"crossfade_duration_ms": {"3000"},
	}
	req := settingsFormRequest(t, http.MethodPost, "/dashboard/station/settings", form)
	req = withSettingsContext(req, station, admin)
	rr := httptest.NewRecorder()
	h.StationSettingsUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}

	var updated models.Station
	db.First(&updated, "id = ?", station.ID)
	if !updated.CrossfadeEnabled {
		t.Errorf("expected CrossfadeEnabled=true")
	}
	if updated.CrossfadeDurationMs != 3000 {
		t.Errorf("expected 3000ms crossfade, got %d", updated.CrossfadeDurationMs)
	}
}

func TestStationSettingsUpdate_CrossfadeCappedAt30s(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	admin := settingsAdminUser(t, db)

	form := url.Values{
		"name":                  {"Station CF Cap"},
		"timezone":              {"UTC"},
		"crossfade_enabled":     {"on"},
		"crossfade_duration_ms": {"99999"},
	}
	req := settingsFormRequest(t, http.MethodPost, "/dashboard/station/settings", form)
	req = withSettingsContext(req, station, admin)
	rr := httptest.NewRecorder()
	h.StationSettingsUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}

	var updated models.Station
	db.First(&updated, "id = ?", station.ID)
	if updated.CrossfadeDurationMs != 30000 {
		t.Errorf("expected capped at 30000ms, got %d", updated.CrossfadeDurationMs)
	}
}

func TestStationSettingsUpdate_SoftOverrunCapped(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	admin := settingsAdminUser(t, db)

	// Exceeds 7*24*60 = 10080 minutes cap
	form := url.Values{
		"name":                          {"Station Overrun Cap"},
		"timezone":                      {"UTC"},
		"schedule_boundary_mode":        {"soft"},
		"schedule_soft_overrun_minutes": {"99999"},
	}
	req := settingsFormRequest(t, http.MethodPost, "/dashboard/station/settings", form)
	req = withSettingsContext(req, station, admin)
	rr := httptest.NewRecorder()
	h.StationSettingsUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}

	var updated models.Station
	db.First(&updated, "id = ?", station.ID)
	maxSec := 7 * 24 * 60 * 60
	if updated.ScheduleSoftOverrunSeconds != maxSec {
		t.Errorf("expected overrun capped at %d, got %d", maxSec, updated.ScheduleSoftOverrunSeconds)
	}
}

func TestStationSettingsUpdate_DefaultShowInArchiveAndDownload(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	admin := settingsAdminUser(t, db)

	form := url.Values{
		"name":                    {"Station Archive"},
		"timezone":                {"UTC"},
		"default_show_in_archive": {"on"},
		"default_allow_download":  {"on"},
	}
	req := settingsFormRequest(t, http.MethodPost, "/dashboard/station/settings", form)
	req = withSettingsContext(req, station, admin)
	rr := httptest.NewRecorder()
	h.StationSettingsUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}

	var updated models.Station
	db.First(&updated, "id = ?", station.ID)
	if !updated.DefaultShowInArchive {
		t.Errorf("expected DefaultShowInArchive=true")
	}
	if !updated.DefaultAllowDownload {
		t.Errorf("expected DefaultAllowDownload=true")
	}
}

func TestStationSettingsUpdate_HtmxEmptyName_Returns200WithAlert(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	admin := settingsAdminUser(t, db)

	form := url.Values{"name": {""}, "timezone": {"UTC"}}
	req := settingsFormRequest(t, http.MethodPost, "/dashboard/station/settings", form)
	req.Header.Set("HX-Request", "true")
	req = withSettingsContext(req, station, admin)
	rr := httptest.NewRecorder()
	h.StationSettingsUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HTMX error, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "alert-danger") {
		t.Errorf("expected danger alert in HTMX response")
	}
}

func TestStationSettingsUpdate_HtmxSuccess_Returns200WithTrigger(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	admin := settingsAdminUser(t, db)

	form := url.Values{"name": {"HTMX Updated"}, "timezone": {"UTC"}}
	req := settingsFormRequest(t, http.MethodPost, "/dashboard/station/settings", form)
	req.Header.Set("HX-Request", "true")
	req = withSettingsContext(req, station, admin)
	rr := httptest.NewRecorder()
	h.StationSettingsUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HTMX success, got %d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("HX-Trigger") != "settingsUpdated" {
		t.Errorf("expected HX-Trigger=settingsUpdated, got %q", rr.Header().Get("HX-Trigger"))
	}
	if !strings.Contains(rr.Body.String(), "alert-success") {
		t.Errorf("expected success alert in HTMX response")
	}
}

// ---------------------------------------------------------------------------
// validateStationDescription (unit tests)
// ---------------------------------------------------------------------------

func TestValidateStationDescription_Valid_NoError(t *testing.T) {
	if err := validateStationDescription("A normal description with text."); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateStationDescription_Empty_NoError(t *testing.T) {
	if err := validateStationDescription(""); err != nil {
		t.Errorf("expected no error for empty description, got %v", err)
	}
}

func TestValidateStationDescription_TooLong_ReturnsError(t *testing.T) {
	long := strings.Repeat("a", maxStationDescriptionChars+1)
	if err := validateStationDescription(long); err == nil {
		t.Error("expected error for too-long description")
	}
}

func TestValidateStationDescription_ExactlyAtLimit_NoError(t *testing.T) {
	at := strings.Repeat("a", maxStationDescriptionChars)
	if err := validateStationDescription(at); err != nil {
		t.Errorf("expected no error at limit, got %v", err)
	}
}

func TestValidateStationDescription_WithNullByte_ReturnsError(t *testing.T) {
	if err := validateStationDescription("hello\x00world"); err == nil {
		t.Error("expected error for null byte in description")
	}
}

func TestValidateStationDescription_WithSemicolon_NoError(t *testing.T) {
	// Semicolons are valid characters in descriptions
	if err := validateStationDescription("Mix; it up; and; more"); err != nil {
		t.Errorf("expected no error for description with semicolons, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// parseURLFormSemicolonTolerant (unit tests)
// ---------------------------------------------------------------------------

func TestParseURLFormSemicolonTolerant_NilRequest_ReturnsError(t *testing.T) {
	if err := parseURLFormSemicolonTolerant(nil); err == nil {
		t.Error("expected error for nil request")
	}
}

func TestParseURLFormSemicolonTolerant_GetRequest_UsesStandardParse(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?name=test", nil)
	if err := parseURLFormSemicolonTolerant(req); err != nil {
		t.Errorf("expected no error for GET request, got %v", err)
	}
}

func TestParseURLFormSemicolonTolerant_PostRequest_PreservesSemicolon(t *testing.T) {
	body := "description=hello%3Bworld&name=Station"
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if err := parseURLFormSemicolonTolerant(req); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// The semicolon should have been preserved in the description
	desc := req.FormValue("description")
	if !strings.Contains(desc, ";") {
		t.Errorf("expected semicolon preserved in description, got %q", desc)
	}
}

func TestParseURLFormSemicolonTolerant_PostWithRawSemicolon(t *testing.T) {
	// Raw semicolons in body
	body := "name=My+Station&description=A;B;C"
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if err := parseURLFormSemicolonTolerant(req); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	name := req.FormValue("name")
	if name != "My Station" {
		t.Errorf("expected 'My Station', got %q", name)
	}
}

// ---------------------------------------------------------------------------
// canManageStationSettings (unit tests)
// ---------------------------------------------------------------------------

func TestCanManageStationSettings_NilUser_ReturnsFalse(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := &models.Station{ID: "s1"}

	if h.canManageStationSettings(nil, station) {
		t.Error("expected false for nil user")
	}
}

func TestCanManageStationSettings_NilStation_ReturnsFalse(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	user := &models.User{ID: "u1", PlatformRole: models.PlatformRoleAdmin}

	if h.canManageStationSettings(user, nil) {
		t.Error("expected false for nil station")
	}
}

func TestCanManageStationSettings_PlatformAdmin_ReturnsTrue(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	user := &models.User{ID: "u1", PlatformRole: models.PlatformRoleAdmin}
	station := &models.Station{ID: "s1"}

	if !h.canManageStationSettings(user, station) {
		t.Error("expected true for platform admin")
	}
}

func TestCanManageStationSettings_OwnerRole_ReturnsTrue(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	owner := settingsOwnerUser(t, db, station.ID)

	if !h.canManageStationSettings(owner, station) {
		t.Error("expected true for station owner")
	}
}

func TestCanManageStationSettings_AdminRole_ReturnsTrue(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)

	user := &models.User{ID: "uST-admin2", Email: "admin2@example.com", Password: "x", PlatformRole: models.PlatformRoleUser}
	db.Create(user)
	su := &models.StationUser{ID: "su-admin2", UserID: user.ID, StationID: station.ID, Role: models.StationRoleAdmin}
	db.Create(su)

	if !h.canManageStationSettings(user, station) {
		t.Error("expected true for station admin")
	}
}

func TestCanManageStationSettings_DJRole_ReturnsFalse(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)

	user := &models.User{ID: "uST-dj2", Email: "dj2@example.com", Password: "x", PlatformRole: models.PlatformRoleUser}
	db.Create(user)
	su := &models.StationUser{ID: "su-dj2", UserID: user.ID, StationID: station.ID, Role: models.StationRoleDJ}
	db.Create(su)

	if h.canManageStationSettings(user, station) {
		t.Error("expected false for DJ role")
	}
}

func TestCanManageStationSettings_NoStationRole_ReturnsFalse(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	user := settingsRegularUser(t, db)

	if h.canManageStationSettings(user, station) {
		t.Error("expected false for user with no station role")
	}
}

// ---------------------------------------------------------------------------
// StationStopPlayout
// ---------------------------------------------------------------------------

func TestStationStopPlayout_NoStation_Returns400(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/station/stop-playout", nil)
	rr := httptest.NewRecorder()
	h.StationStopPlayout(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestStationStopPlayout_NoPermission_Returns403(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	user := settingsRegularUser(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/station/stop-playout", nil)
	req = withSettingsContext(req, station, user)
	rr := httptest.NewRecorder()
	h.StationStopPlayout(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestStationStopPlayout_NoDirector_Returns500(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	admin := settingsAdminUser(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/station/stop-playout", nil)
	req = withSettingsContext(req, station, admin)
	rr := httptest.NewRecorder()
	h.StationStopPlayout(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (no director), got %d", rr.Code)
	}
}

func TestStationStopPlayout_NoDirector_HtmxReturns500(t *testing.T) {
	db := newSettingsDB(t)
	h := newSettingsHandler(t, db)
	station := settingsStation(t, db)
	admin := settingsAdminUser(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/station/stop-playout", nil)
	req.Header.Set("HX-Request", "true")
	req = withSettingsContext(req, station, admin)
	rr := httptest.NewRecorder()
	h.StationStopPlayout(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "alert-danger") {
		t.Errorf("expected danger alert in HTMX response")
	}
}
