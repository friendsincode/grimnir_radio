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
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ---------------------------------------------------------------------------
// DB / handler factory for wave-2 tests (includes all models needed)
// ---------------------------------------------------------------------------

func newWave2DB(t *testing.T) *gorm.DB {
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
		&models.ScheduleEntry{},
		&models.SmartBlock{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.LandingPage{},
		&models.APIKey{},
		&models.ListenerSample{},
		&models.PlayoutQueueItem{},
		&models.ExecutorState{},
		&models.MountPlayoutState{},
		&models.AuditLog{},
		&models.Show{},
		&models.ShowInstance{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newWave2Handler(t *testing.T, db *gorm.DB) *Handler {
	t.Helper()
	h, err := NewHandler(db, []byte("test-secret"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h
}

func seedWave2AdminUser(t *testing.T, db *gorm.DB) models.User {
	t.Helper()
	u := models.User{ID: "w2-admin1", Email: "w2admin@example.com", Password: "x", PlatformRole: models.PlatformRoleAdmin}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	return u
}

func seedWave2User(t *testing.T, db *gorm.DB) models.User {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("oldpassword"), bcrypt.MinCost)
	u := models.User{ID: "w2-user1", Email: "w2user@example.com", Password: string(hash), PlatformRole: models.PlatformRoleUser}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u
}

func seedWave2Station(t *testing.T, db *gorm.DB) models.Station {
	t.Helper()
	s := models.Station{ID: "w2-station1", Name: "Wave2 Station", Active: true}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	return s
}

func wave2Req(method, target string, user *models.User, station *models.Station) *http.Request {
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

func wave2ReqWithID(method, target string, user *models.User, station *models.Station, paramName, paramVal string) *http.Request {
	req := wave2Req(method, target, user, station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(paramName, paramVal)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// ---------------------------------------------------------------------------
// humanReadableBytes (pages_media.go)
// ---------------------------------------------------------------------------

func TestHumanReadableBytes_Zero(t *testing.T) {
	if got := humanReadableBytes(0); got != "0 B" {
		t.Fatalf("expected '0 B', got %q", got)
	}
}

func TestHumanReadableBytes_Negative(t *testing.T) {
	if got := humanReadableBytes(-1); got != "0 B" {
		t.Fatalf("expected '0 B' for negative, got %q", got)
	}
}

func TestHumanReadableBytes_UnderKilobyte(t *testing.T) {
	if got := humanReadableBytes(512); got != "512 B" {
		t.Fatalf("expected '512 B', got %q", got)
	}
}

func TestHumanReadableBytes_ExactKilobyte(t *testing.T) {
	if got := humanReadableBytes(1024); got != "1.0 KiB" {
		t.Fatalf("expected '1.0 KiB', got %q", got)
	}
}

func TestHumanReadableBytes_Megabyte(t *testing.T) {
	got := humanReadableBytes(1024 * 1024)
	if !strings.HasSuffix(got, "iB") {
		t.Fatalf("expected iB suffix, got %q", got)
	}
}

func TestHumanReadableBytes_Gigabyte(t *testing.T) {
	got := humanReadableBytes(1024 * 1024 * 1024)
	if !strings.Contains(got, "G") {
		t.Fatalf("expected G in output, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// classifyRuntimeMismatch (pages_schedule.go)
// ---------------------------------------------------------------------------

func TestClassifyRuntimeMismatch_W2_NoMismatch(t *testing.T) {
	entry := models.ScheduleEntry{SourceType: "playlist", SourceID: "p1"}
	mount := models.MountPlayoutState{SourceType: "playlist", SourceID: "p1"}
	mismatch, _, _ := classifyRuntimeMismatch(entry, mount)
	if mismatch {
		t.Fatal("expected no mismatch when types and IDs match")
	}
}

func TestClassifyRuntimeMismatch_W2_WrongSourceType(t *testing.T) {
	entry := models.ScheduleEntry{SourceType: "playlist", SourceID: "p1"}
	mount := models.MountPlayoutState{SourceType: "smart_block", SourceID: "p1"}
	mismatch, _, label := classifyRuntimeMismatch(entry, mount)
	if !mismatch {
		t.Fatal("expected mismatch on differing source types")
	}
	if label != "Wrong Source Type" {
		t.Fatalf("expected 'Wrong Source Type', got %q", label)
	}
}

func TestClassifyRuntimeMismatch_W2_WrongSourceID(t *testing.T) {
	entry := models.ScheduleEntry{SourceType: "playlist", SourceID: "p1"}
	mount := models.MountPlayoutState{SourceType: "playlist", SourceID: "p2"}
	mismatch, _, label := classifyRuntimeMismatch(entry, mount)
	if !mismatch {
		t.Fatal("expected mismatch on differing source IDs")
	}
	if label != "Wrong Source" {
		t.Fatalf("expected 'Wrong Source', got %q", label)
	}
}

func TestClassifyRuntimeMismatch_W2_LiveSkipsIDCheck(t *testing.T) {
	entry := models.ScheduleEntry{SourceType: "live", SourceID: "live-id-1"}
	mount := models.MountPlayoutState{SourceType: "live", SourceID: "live-id-2"}
	mismatch, _, _ := classifyRuntimeMismatch(entry, mount)
	if mismatch {
		t.Fatal("expected no mismatch for live entries with different IDs")
	}
}

func TestClassifyRuntimeMismatch_W2_EmptyMountState(t *testing.T) {
	entry := models.ScheduleEntry{SourceType: "playlist", SourceID: "p1"}
	mount := models.MountPlayoutState{}
	mismatch, _, _ := classifyRuntimeMismatch(entry, mount)
	if mismatch {
		t.Fatal("expected no mismatch when mount state is empty")
	}
}

// ---------------------------------------------------------------------------
// nextOccurrenceLocal (pages_schedule.go)
// ---------------------------------------------------------------------------

func TestNextOccurrenceLocal_W2_Daily(t *testing.T) {
	h := &Handler{}
	entry := models.ScheduleEntry{RecurrenceType: models.RecurrenceDaily}
	from := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)
	next := h.nextOccurrenceLocal(entry, from, time.UTC)
	if next.IsZero() {
		t.Fatal("expected non-zero next occurrence for daily")
	}
	if next.Day() != 26 {
		t.Fatalf("expected day 26, got %d", next.Day())
	}
}

func TestNextOccurrenceLocal_W2_Weekly(t *testing.T) {
	h := &Handler{}
	base := time.Date(2026, 3, 23, 9, 0, 0, 0, time.UTC) // Monday
	entry := models.ScheduleEntry{
		RecurrenceType: models.RecurrenceWeekly,
		StartsAt:       base,
	}
	from := time.Date(2026, 3, 25, 9, 0, 0, 0, time.UTC) // Wednesday
	next := h.nextOccurrenceLocal(entry, from, time.UTC)
	if next.IsZero() {
		t.Fatal("expected non-zero next for weekly")
	}
	// Should be 7 days after from
	if next.Sub(from) != 7*24*time.Hour {
		t.Fatalf("expected 7-day offset, got %v", next.Sub(from))
	}
}

func TestNextOccurrenceLocal_W2_Weekdays_SkipsWeekend(t *testing.T) {
	h := &Handler{}
	entry := models.ScheduleEntry{RecurrenceType: models.RecurrenceWeekdays}
	// Friday: next weekday is Monday
	from := time.Date(2026, 3, 27, 9, 0, 0, 0, time.UTC) // Friday
	next := h.nextOccurrenceLocal(entry, from, time.UTC)
	if next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
		t.Fatalf("nextOccurrenceLocal returned weekend day %v", next.Weekday())
	}
}

func TestNextOccurrenceLocal_W2_Custom(t *testing.T) {
	h := &Handler{}
	entry := models.ScheduleEntry{RecurrenceType: models.RecurrenceCustom}
	from := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)
	next := h.nextOccurrenceLocal(entry, from, time.UTC)
	if next.IsZero() {
		t.Fatal("expected non-zero next for custom recurrence")
	}
}

func TestNextOccurrenceLocal_W2_UnknownType_ReturnsZero(t *testing.T) {
	h := &Handler{}
	entry := models.ScheduleEntry{RecurrenceType: "unknown"}
	from := time.Now()
	next := h.nextOccurrenceLocal(entry, from, time.UTC)
	if !next.IsZero() {
		t.Fatal("expected zero time for unknown recurrence type")
	}
}

// ---------------------------------------------------------------------------
// normalizeScheduleSourceID (pages_schedule.go)
// ---------------------------------------------------------------------------

func TestNormalizeScheduleSourceID_W2_NonEmptyReturnsItself(t *testing.T) {
	got := normalizeScheduleSourceID("playlist", "p1", "fallback-uuid")
	if got != "p1" {
		t.Fatalf("expected 'p1', got %q", got)
	}
}

func TestNormalizeScheduleSourceID_W2_LiveEmptyReturnsFallback(t *testing.T) {
	got := normalizeScheduleSourceID("live", "", "fallback-uuid")
	if got != "fallback-uuid" {
		t.Fatalf("expected fallback uuid, got %q", got)
	}
}

func TestNormalizeScheduleSourceID_W2_NonLiveEmptyReturnsEmpty(t *testing.T) {
	got := normalizeScheduleSourceID("playlist", "", "fallback-uuid")
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestNormalizeScheduleSourceID_W2_SpacesOnlyTreatedAsEmpty(t *testing.T) {
	got := normalizeScheduleSourceID("live", "   ", "fallback-uuid")
	if got != "fallback-uuid" {
		t.Fatalf("expected fallback for space-only live, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// StationSelect (pages_dashboard.go)
// ---------------------------------------------------------------------------

func TestStationSelect_SingleStation_HtmxRedirect(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)
	s := seedWave2Station(t, db)
	// give user access to station
	db.Create(&models.StationUser{ID: "su-w2", UserID: u.ID, StationID: s.ID, Role: models.StationRoleManager})

	req := wave2Req(http.MethodGet, "/dashboard/stations/select", &u, nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.StationSelect(rr, req)
	// Single station: should redirect (either HTMX or plain)
	if rr.Code != http.StatusOK && rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 200 (HX-Redirect) or 303, got %d", rr.Code)
	}
}

func TestStationSelect_MultipleStations_Renders200(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2AdminUser(t, db) // admin sees all stations
	db.Create(&models.Station{ID: "ms1", Name: "Multi One", Active: true})
	db.Create(&models.Station{ID: "ms2", Name: "Multi Two", Active: true})

	req := wave2Req(http.MethodGet, "/dashboard/stations/select", &u, nil)
	rr := httptest.NewRecorder()
	h.StationSelect(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestStationSelect_NoStations_Renders200(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	req := wave2Req(http.MethodGet, "/dashboard/stations/select", &u, nil)
	rr := httptest.NewRecorder()
	h.StationSelect(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// StationSelectSubmit (pages_dashboard.go)
// ---------------------------------------------------------------------------

func TestStationSelectSubmit_MissingStationID_Returns400(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/stations/select", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.StationSelectSubmit(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestStationSelectSubmit_StationNotFound_Returns404(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	form := url.Values{"station_id": {"nonexistent-id"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/stations/select", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.StationSelectSubmit(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestStationSelectSubmit_StationExistsButNoAccess_Returns403(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)
	s := seedWave2Station(t, db)
	// do NOT create StationUser — user has no access

	form := url.Values{"station_id": {s.ID}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/stations/select", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.StationSelectSubmit(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestStationSelectSubmit_ValidStation_Redirects(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2AdminUser(t, db) // admin sees all stations
	s := seedWave2Station(t, db)

	form := url.Values{"station_id": {s.ID}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/stations/select", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.StationSelectSubmit(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestStationSelectSubmit_HtmxRequest_SetsHXRedirect(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2AdminUser(t, db)
	s := seedWave2Station(t, db)

	form := url.Values{"station_id": {s.ID}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/stations/select", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.StationSelectSubmit(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HX-Request, got %d", rr.Code)
	}
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatal("expected HX-Redirect header")
	}
}

func TestStationSelectSubmit_OpenRedirectPrevented(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2AdminUser(t, db)
	s := seedWave2Station(t, db)

	form := url.Values{
		"station_id":  {s.ID},
		"redirect_to": {"https://evil.com/steal"},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/stations/select", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.StationSelectSubmit(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/dashboard") {
		t.Fatalf("expected redirect to /dashboard*, got %q", loc)
	}
}

// ---------------------------------------------------------------------------
// ProfileUpdate (pages_dashboard.go)
// ---------------------------------------------------------------------------

func TestProfileUpdate_NoUser_Returns401_Wave2(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile", nil)
	rr := httptest.NewRecorder()
	h.ProfileUpdate(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestProfileUpdate_EmptyEmail_BadRequest(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	form := url.Values{"email": {""}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.ProfileUpdate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty email, got %d", rr.Code)
	}
}

func TestProfileUpdate_ValidEmail_HtmxSuccess(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	form := url.Values{"email": {"newemail@example.com"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.ProfileUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestProfileUpdate_DuplicateEmail_Returns400(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)
	// Seed another user with the email we'll try to use
	db.Create(&models.User{ID: "other-user", Email: "taken@example.com", Password: "x"})

	form := url.Values{"email": {"taken@example.com"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.ProfileUpdate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for duplicate email, got %d", rr.Code)
	}
}

func TestProfileUpdate_ValidCalendarTheme_Saved(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	form := url.Values{
		"email":                {"w2user@example.com"},
		"calendar_color_theme": {"ocean"},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.ProfileUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestProfileUpdate_Redirect_NonHtmx(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	form := url.Values{"email": {"w2user@example.com"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.ProfileUpdate(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// ProfileUpdatePassword (pages_dashboard.go)
// ---------------------------------------------------------------------------

func TestProfileUpdatePassword_NoUser_Returns401_Wave2(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/password", nil)
	rr := httptest.NewRecorder()
	h.ProfileUpdatePassword(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestProfileUpdatePassword_WrongCurrentPassword(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	form := url.Values{
		"current_password": {"wrongpassword"},
		"new_password":     {"newpassword123"},
		"confirm_password": {"newpassword123"},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.ProfileUpdatePassword(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestProfileUpdatePassword_TooShortNewPassword(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	form := url.Values{
		"current_password": {"oldpassword"},
		"new_password":     {"short"},
		"confirm_password": {"short"},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.ProfileUpdatePassword(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestProfileUpdatePassword_PasswordMismatch(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	form := url.Values{
		"current_password": {"oldpassword"},
		"new_password":     {"newpassword123"},
		"confirm_password": {"differentpassword123"},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.ProfileUpdatePassword(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestProfileUpdatePassword_Success_HtmxResponse(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	form := url.Values{
		"current_password": {"oldpassword"},
		"new_password":     {"newpassword123"},
		"confirm_password": {"newpassword123"},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.ProfileUpdatePassword(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// ProfileLogoutAllDevices (pages_dashboard.go)
// ---------------------------------------------------------------------------

func TestProfileLogoutAllDevices_NoUser_Returns401_Wave2(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/logout-all", nil)
	rr := httptest.NewRecorder()
	h.ProfileLogoutAllDevices(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestProfileLogoutAllDevices_HtmxSuccess(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/logout-all", nil)
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.ProfileLogoutAllDevices(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestProfileLogoutAllDevices_NonHtmx_Redirects(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/logout-all", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.ProfileLogoutAllDevices(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// APIKeysSection (pages_dashboard.go)
// ---------------------------------------------------------------------------

func TestAPIKeysSection_NoUser_Returns401_Wave2(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/profile/api-keys", nil)
	rr := httptest.NewRecorder()
	h.APIKeysSection(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAPIKeysSection_WithUser_Returns200(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/profile/api-keys", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.APIKeysSection(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// APIKeyGenerate (pages_dashboard.go)
// ---------------------------------------------------------------------------

func TestAPIKeyGenerate_NoUser_Returns401_Wave2(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/api-keys", nil)
	rr := httptest.NewRecorder()
	h.APIKeyGenerate(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAPIKeyGenerate_WithUser_Creates200(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	form := url.Values{
		"name":            {"My Test Key"},
		"expiration_days": {"30"},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/api-keys", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.APIKeyGenerate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAPIKeyGenerate_DefaultName_WhenEmpty(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/api-keys", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.APIKeyGenerate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// APIKeyRevoke (pages_dashboard.go)
// ---------------------------------------------------------------------------

func TestAPIKeyRevoke_NoUser_Returns401_Wave2(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)

	req := wave2ReqWithID(http.MethodDelete, "/dashboard/profile/api-keys/someid", nil, nil, "id", "someid")
	rr := httptest.NewRecorder()
	h.APIKeyRevoke(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAPIKeyRevoke_NonexistentKey_Returns404(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	req := wave2ReqWithID(http.MethodDelete, "/dashboard/profile/api-keys/nosuchkey", &u, nil, "id", "nosuchkey")
	rr := httptest.NewRecorder()
	h.APIKeyRevoke(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// AdminMediaDuplicates (pages_admin.go)
// ---------------------------------------------------------------------------

func TestAdminMediaDuplicates_NonAdmin_Returns403_Wave2(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	req := wave2Req(http.MethodGet, "/dashboard/admin/media/duplicates", &u, nil)
	rr := httptest.NewRecorder()
	h.AdminMediaDuplicates(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminMediaDuplicates_Admin_NoItems_Returns200(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2AdminUser(t, db)

	req := wave2Req(http.MethodGet, "/dashboard/admin/media/duplicates", &u, nil)
	rr := httptest.NewRecorder()
	h.AdminMediaDuplicates(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// AdminMediaBackfillHashes (pages_admin.go)
// ---------------------------------------------------------------------------

func TestAdminMediaBackfillHashes_NonAdmin_Returns403_Wave2(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	req := wave2Req(http.MethodPost, "/dashboard/admin/media/backfill-hashes", &u, nil)
	rr := httptest.NewRecorder()
	h.AdminMediaBackfillHashes(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminMediaBackfillHashes_Admin_NoMedia_Redirects(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2AdminUser(t, db)

	req := wave2Req(http.MethodPost, "/dashboard/admin/media/backfill-hashes", &u, nil)
	rr := httptest.NewRecorder()
	h.AdminMediaBackfillHashes(rr, req)
	// Should redirect (no media to process)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestAdminMediaBackfillHashes_Admin_HtmxRedirect(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2AdminUser(t, db)

	req := wave2Req(http.MethodPost, "/dashboard/admin/media/backfill-hashes", &u, nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.AdminMediaBackfillHashes(rr, req)
	// HTMX path sets HX-Redirect header
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatal("expected HX-Redirect header for HTMX request")
	}
}

// ---------------------------------------------------------------------------
// AdminMediaBulk – additional action branches (pages_admin.go)
// ---------------------------------------------------------------------------

func TestAdminMediaBulk_MoveToStation_TargetNotFound(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2AdminUser(t, db)
	s := seedWave2Station(t, db)
	m := models.MediaItem{ID: "bulk-m1", StationID: s.ID, Title: "T", Path: "t.mp3", AnalysisState: models.AnalysisComplete}
	db.Create(&m)

	body, _ := json.Marshal(map[string]any{
		"action": "move_to_station",
		"ids":    []string{m.ID},
		"value":  "nonexistent-station",
	})
	req := httptest.NewRequest(http.MethodPost, "/dashboard/admin/media/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.AdminMediaBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for nonexistent target station, got %d", rr.Code)
	}
}

func TestAdminMediaBulk_MakePublic_Success(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2AdminUser(t, db)
	s := seedWave2Station(t, db)
	m := models.MediaItem{ID: "bulk-m2", StationID: s.ID, Title: "T", Path: "t.mp3", AnalysisState: models.AnalysisComplete}
	db.Create(&m)

	body, _ := json.Marshal(map[string]any{
		"action": "make_public",
		"ids":    []string{m.ID},
	})
	req := httptest.NewRequest(http.MethodPost, "/dashboard/admin/media/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.AdminMediaBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestAdminMediaBulk_UnknownAction_Returns400(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2AdminUser(t, db)

	body, _ := json.Marshal(map[string]any{
		"action": "vaporize",
		"ids":    []string{"someid"},
	})
	req := httptest.NewRequest(http.MethodPost, "/dashboard/admin/media/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.AdminMediaBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown action, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// AdminUsersBulk – action branches (pages_admin.go)
// ---------------------------------------------------------------------------

func TestAdminUsersBulk_SetRoleAdmin_Success(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	admin := seedWave2AdminUser(t, db)
	target := seedWave2User(t, db)

	body, _ := json.Marshal(map[string]any{
		"action": "set_role_admin",
		"ids":    []string{target.ID},
	})
	req := httptest.NewRequest(http.MethodPost, "/dashboard/admin/users/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	rr := httptest.NewRecorder()
	h.AdminUsersBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestAdminUsersBulk_SetRoleMod_Success(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	admin := seedWave2AdminUser(t, db)
	target := seedWave2User(t, db)

	body, _ := json.Marshal(map[string]any{
		"action": "set_role_mod",
		"ids":    []string{target.ID},
	})
	req := httptest.NewRequest(http.MethodPost, "/dashboard/admin/users/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	rr := httptest.NewRecorder()
	h.AdminUsersBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestAdminUsersBulk_UnknownAction_Returns400_Wave2(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	admin := seedWave2AdminUser(t, db)
	target := seedWave2User(t, db)

	body, _ := json.Marshal(map[string]any{
		"action": "teleport",
		"ids":    []string{target.ID},
	})
	req := httptest.NewRequest(http.MethodPost, "/dashboard/admin/users/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	rr := httptest.NewRecorder()
	h.AdminUsersBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown action, got %d", rr.Code)
	}
}

func TestAdminUsersBulk_OnlySelf_Returns400(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	admin := seedWave2AdminUser(t, db)

	body, _ := json.Marshal(map[string]any{
		"action": "set_role_user",
		"ids":    []string{admin.ID}, // only self
	})
	req := httptest.NewRequest(http.MethodPost, "/dashboard/admin/users/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	rr := httptest.NewRecorder()
	h.AdminUsersBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when only self in bulk, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// AdminMediaStream (pages_admin.go)
// ---------------------------------------------------------------------------

func TestAdminMediaStream_NonAdmin_Returns403_Wave2(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2User(t, db)

	req := wave2ReqWithID(http.MethodGet, "/dashboard/admin/media/stream/m1", &u, nil, "id", "m1")
	rr := httptest.NewRecorder()
	h.AdminMediaStream(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminMediaStream_MediaNotFound_Returns404(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2AdminUser(t, db)

	req := wave2ReqWithID(http.MethodGet, "/dashboard/admin/media/stream/nonexistent", &u, nil, "id", "nonexistent")
	rr := httptest.NewRecorder()
	h.AdminMediaStream(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestAdminMediaStream_NoPath_Returns404(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	u := seedWave2AdminUser(t, db)
	s := seedWave2Station(t, db)
	// Media item with empty path
	m := models.MediaItem{ID: "stream-nopath", StationID: s.ID, Title: "T", Path: "", AnalysisState: models.AnalysisComplete}
	db.Create(&m)

	req := wave2ReqWithID(http.MethodGet, "/dashboard/admin/media/stream/stream-nopath", &u, nil, "id", "stream-nopath")
	rr := httptest.NewRecorder()
	h.AdminMediaStream(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for empty path, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// MediaStream (pages_media.go)
// ---------------------------------------------------------------------------

func TestMediaStream_NoStation_Returns400(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)

	req := mediaReqWithID(http.MethodGet, "/dashboard/media/stream/m1", &user, nil, "id", "m1")
	rr := httptest.NewRecorder()
	h.MediaStream(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaStream_MediaNotFound_Returns404(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)

	req := mediaReqWithID(http.MethodGet, "/dashboard/media/stream/nonexistent", &user, &station, "id", "nonexistent")
	rr := httptest.NewRecorder()
	h.MediaStream(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// MediaDuplicates (pages_media.go)
// ---------------------------------------------------------------------------

func TestMediaDuplicates_NoStation_Redirects(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)

	req := mediaReq(http.MethodGet, "/dashboard/media/duplicates", &user, nil)
	rr := httptest.NewRecorder()
	h.MediaDuplicates(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestMediaDuplicates_WithStation_Returns200(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)

	req := mediaReq(http.MethodGet, "/dashboard/media/duplicates", &user, &station)
	rr := httptest.NewRecorder()
	h.MediaDuplicates(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// MediaPurgeDuplicates (pages_media.go)
// ---------------------------------------------------------------------------

func TestMediaPurgeDuplicates_NoStation_Returns400(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)

	req := mediaReq(http.MethodPost, "/dashboard/media/duplicates/purge", &user, nil)
	rr := httptest.NewRecorder()
	h.MediaPurgeDuplicates(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaPurgeDuplicates_NoIDs_Redirects(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)

	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/media/duplicates/purge", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaPurgeDuplicates(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// loadMediaUsage (pages_media.go)
// ---------------------------------------------------------------------------

func TestLoadMediaUsage_EmptyIDs_ReturnsEmpty(t *testing.T) {
	h, _, _, station := newMediaDetailTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	result := h.loadMediaUsage(req, station.ID, nil)
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d entries", len(result))
	}
}

func TestLoadMediaUsage_WithMediaIDs_NoUsage(t *testing.T) {
	h, _, _, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "usage-m1", "Song 1")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	result := h.loadMediaUsage(req, station.ID, []string{"usage-m1"})
	// No playlist/schedule usage seeded, should have zero references
	if usage, ok := result["usage-m1"]; ok {
		if len(usage.Playlists) != 0 {
			t.Fatalf("expected no playlist refs, got %d", len(usage.Playlists))
		}
	}
}

// ---------------------------------------------------------------------------
// ShowDelete (pages_shows.go)
// ---------------------------------------------------------------------------

func TestShowDelete_NotFound_Returns404_Wave2(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)

	req := wave2ReqWithID(http.MethodDelete, "/api/shows/nonexistent", nil, nil, "id", "nonexistent")
	rr := httptest.NewRecorder()
	h.ShowDelete(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestShowDelete_ExistingShow_Returns204(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	s := seedWave2Station(t, db)
	show := models.Show{
		ID:                     "del-show1",
		StationID:              s.ID,
		Name:                   "Delete Me",
		DefaultDurationMinutes: 60,
		DTStart:                time.Now().Add(24 * time.Hour),
		Timezone:               "UTC",
		Active:                 true,
	}
	db.Create(&show)

	req := wave2ReqWithID(http.MethodDelete, "/api/shows/del-show1", nil, nil, "id", "del-show1")
	rr := httptest.NewRecorder()
	h.ShowDelete(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// ShowMaterialize (pages_shows.go)
// ---------------------------------------------------------------------------

func TestShowMaterialize_ShowNotFound_Returns404(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)

	body, _ := json.Marshal(map[string]any{
		"start": time.Now().Format(time.RFC3339),
		"end":   time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/shows/nonexistent/materialize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nonexistent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	h.ShowMaterialize(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestShowMaterialize_InvalidJSON_Returns400_Wave2(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	s := seedWave2Station(t, db)
	show := models.Show{
		ID:                     "mat-show1",
		StationID:              s.ID,
		Name:                   "Materialize Me",
		DefaultDurationMinutes: 60,
		DTStart:                time.Now().Add(24 * time.Hour),
		Timezone:               "UTC",
		Active:                 true,
	}
	db.Create(&show)

	req := httptest.NewRequest(http.MethodPost, "/api/shows/mat-show1/materialize", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "mat-show1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	h.ShowMaterialize(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestShowMaterialize_InvalidStart_Returns400_Wave2(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	s := seedWave2Station(t, db)
	show := models.Show{
		ID:                     "mat-show2",
		StationID:              s.ID,
		Name:                   "Materialize Me 2",
		DefaultDurationMinutes: 60,
		DTStart:                time.Now().Add(24 * time.Hour),
		Timezone:               "UTC",
		Active:                 true,
	}
	db.Create(&show)

	body, _ := json.Marshal(map[string]any{
		"start": "not-a-date",
		"end":   time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/shows/mat-show2/materialize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "mat-show2")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	h.ShowMaterialize(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid start, got %d", rr.Code)
	}
}

func TestShowMaterialize_NonRecurringShow_Success(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	s := seedWave2Station(t, db)
	now := time.Now().UTC()
	show := models.Show{
		ID:                     "mat-show3",
		StationID:              s.ID,
		Name:                   "One-Time Show",
		DefaultDurationMinutes: 60,
		DTStart:                now.Add(2 * time.Hour),
		Timezone:               "UTC",
		Active:                 true,
	}
	db.Create(&show)

	body, _ := json.Marshal(map[string]any{
		"start": now.Format(time.RFC3339),
		"end":   now.Add(24 * time.Hour).Format(time.RFC3339),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/shows/mat-show3/materialize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "mat-show3")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	h.ShowMaterialize(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// ShowInstanceCancel (pages_shows.go)
// ---------------------------------------------------------------------------

func TestShowInstanceCancel_RealInstance_NotFound(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)

	req := wave2ReqWithID(http.MethodPost, "/api/shows/instances/nonexistent/cancel", nil, nil, "id", "nonexistent")
	rr := httptest.NewRecorder()
	h.ShowInstanceCancel(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestShowInstanceCancel_RealInstance_Success(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	s := seedWave2Station(t, db)
	show := models.Show{
		ID:                     "cancel-show1",
		StationID:              s.ID,
		Name:                   "Cancel Test",
		DefaultDurationMinutes: 60,
		DTStart:                time.Now().Add(24 * time.Hour),
		Timezone:               "UTC",
		Active:                 true,
	}
	db.Create(&show)
	inst := models.ShowInstance{
		ID:        "cancel-inst1",
		ShowID:    show.ID,
		StationID: s.ID,
		StartsAt:  time.Now().Add(24 * time.Hour),
		EndsAt:    time.Now().Add(25 * time.Hour),
		Status:    models.ShowInstanceScheduled,
	}
	db.Create(&inst)

	req := wave2ReqWithID(http.MethodPost, "/api/shows/instances/cancel-inst1/cancel", nil, &s, "id", "cancel-inst1")
	rr := httptest.NewRecorder()
	h.ShowInstanceCancel(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var result map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("expected JSON response: %v", err)
	}
	if result["status"] != "cancelled" {
		t.Fatalf("expected status=cancelled, got %q", result["status"])
	}
}

// ---------------------------------------------------------------------------
// ShowInstanceUpdate (pages_shows.go) - basic validation paths
// ---------------------------------------------------------------------------

func TestShowInstanceUpdate_NoStation_Returns400_Wave2(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)

	body, _ := json.Marshal(map[string]any{})
	req := httptest.NewRequest(http.MethodPost, "/api/shows/instances/some-id", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "some-id")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	h.ShowInstanceUpdate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (no station), got %d", rr.Code)
	}
}

func TestShowInstanceUpdate_InvalidJSON_Returns400_Wave2(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	s := seedWave2Station(t, db)

	req := httptest.NewRequest(http.MethodPost, "/api/shows/instances/some-id", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "some-id")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, ctxKeyStation, &s)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.ShowInstanceUpdate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d", rr.Code)
	}
}

func TestShowInstanceUpdate_VirtualInvalidID_Returns400(t *testing.T) {
	db := newWave2DB(t)
	h := newWave2Handler(t, db)
	s := seedWave2Station(t, db)

	body, _ := json.Marshal(map[string]any{})
	req := httptest.NewRequest(http.MethodPost, "/api/shows/instances/virtual_badid", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "virtual_badid")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, ctxKeyStation, &s)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.ShowInstanceUpdate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid virtual ID, got %d", rr.Code)
	}
}
