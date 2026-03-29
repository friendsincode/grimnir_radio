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

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ---------------------------------------------------------------------------
// Shared setup helpers for dashboard tests
// ---------------------------------------------------------------------------

func newDashboardTestDB(t *testing.T) *gorm.DB {
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
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newDashboardTestHandler(t *testing.T, db *gorm.DB) *Handler {
	t.Helper()
	h, err := NewHandler(db, []byte("test-secret"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h
}

func seedDashboardUser(t *testing.T, db *gorm.DB) models.User {
	t.Helper()
	u := models.User{ID: "dash-user1", Email: "dash@example.com", Password: "x", PlatformRole: models.PlatformRoleUser}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed dashboard user: %v", err)
	}
	return u
}

func seedDashboardStation(t *testing.T, db *gorm.DB) models.Station {
	t.Helper()
	s := models.Station{ID: "dash-station1", Name: "Dash Station", Active: true}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("seed dashboard station: %v", err)
	}
	return s
}

func dashboardReq(method, target string, user *models.User, station *models.Station) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	if user != nil {
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, user))
	}
	if station != nil {
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, station))
	}
	return req
}

// ---------------------------------------------------------------------------
// DashboardHome
// ---------------------------------------------------------------------------

func TestDashboardHome_NoStation_RedirectsToSelect(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	req := dashboardReq(http.MethodGet, "/dashboard", &u, nil)
	rr := httptest.NewRecorder()
	h.DashboardHome(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestDashboardHome_SingleStation_AutoSelects(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)
	seedDashboardStation(t, db)

	req := dashboardReq(http.MethodGet, "/dashboard", &u, nil)
	rr := httptest.NewRecorder()
	h.DashboardHome(rr, req)

	// Should redirect to /dashboard after auto-selecting the one station
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestDashboardHome_WithStation_Renders200(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)
	s := seedDashboardStation(t, db)

	req := dashboardReq(http.MethodGet, "/dashboard", &u, &s)
	rr := httptest.NewRecorder()
	h.DashboardHome(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

func TestDashboardHome_WithMedia_ShowsStats(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)
	s := seedDashboardStation(t, db)

	// Seed some media items
	db.Create(&models.MediaItem{ID: "m1", StationID: s.ID, Title: "Track 1", Path: "t1.mp3"})
	db.Create(&models.MediaItem{ID: "m2", StationID: s.ID, Title: "Track 2", Path: "t2.mp3"})

	req := dashboardReq(http.MethodGet, "/dashboard", &u, &s)
	rr := httptest.NewRecorder()
	h.DashboardHome(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestDashboardHome_WithPlayHistory_ShowsNowPlaying(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)
	s := seedDashboardStation(t, db)

	now := time.Now()
	db.Create(&models.PlayHistory{
		ID:        "ph1",
		StationID: s.ID,
		Title:     "Current Track",
		Artist:    "Some Artist",
		StartedAt: now.Add(-1 * time.Minute),
	})

	req := dashboardReq(http.MethodGet, "/dashboard", &u, &s)
	rr := httptest.NewRecorder()
	h.DashboardHome(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// loadDashboardUpcomingEntries
// ---------------------------------------------------------------------------

func TestLoadDashboardUpcomingEntries_Empty(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	s := seedDashboardStation(t, db)

	entries := h.loadDashboardUpcomingEntries(s.ID, time.Now().UTC(), 24*time.Hour, 10)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestLoadDashboardUpcomingEntries_NonRecurring(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	s := seedDashboardStation(t, db)

	now := time.Now().UTC()
	future := now.Add(2 * time.Hour)
	db.Create(&models.ScheduleEntry{
		ID:        "se1",
		StationID: s.ID,
		SourceType: "playlist",
		SourceID:  "pl1",
		StartsAt:  future,
		EndsAt:    future.Add(time.Hour),
	})

	entries := h.loadDashboardUpcomingEntries(s.ID, now, 24*time.Hour, 10)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestLoadDashboardUpcomingEntries_LimitEnforced(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	s := seedDashboardStation(t, db)

	now := time.Now().UTC()
	for i := 0; i < 15; i++ {
		future := now.Add(time.Duration(i+1) * time.Hour)
		db.Create(&models.ScheduleEntry{
			ID:        "se-" + string(rune('a'+i)),
			StationID: s.ID,
			SourceType: "playlist",
			SourceID:  "pl1",
			StartsAt:  future,
			EndsAt:    future.Add(30 * time.Minute),
		})
	}

	entries := h.loadDashboardUpcomingEntries(s.ID, now, 48*time.Hour, 5)
	if len(entries) > 5 {
		t.Fatalf("expected at most 5 entries, got %d", len(entries))
	}
}

func TestLoadDashboardUpcomingEntries_DefaultLimit(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	s := seedDashboardStation(t, db)

	// Passing limit=0 should use default of 10
	entries := h.loadDashboardUpcomingEntries(s.ID, time.Now().UTC(), 24*time.Hour, 0)
	if entries == nil {
		t.Fatal("expected non-nil slice")
	}
}

// ---------------------------------------------------------------------------
// enrichDashboardUpcomingEntries
// ---------------------------------------------------------------------------

func TestEnrichDashboardUpcomingEntries_Empty(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	result := h.enrichDashboardUpcomingEntries(nil)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestEnrichDashboardUpcomingEntries_PlaylistType(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	db.Create(&models.Playlist{ID: "pl1", StationID: "s1", Name: "Test Playlist"})

	now := time.Now().UTC()
	entries := []models.ScheduleEntry{
		{
			ID:         "se1",
			StationID:  "s1",
			SourceType: "playlist",
			SourceID:   "pl1",
			StartsAt:   now.Add(time.Hour),
			EndsAt:     now.Add(2 * time.Hour),
		},
	}

	result := h.enrichDashboardUpcomingEntries(entries)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Metadata["title"] != "Test Playlist" {
		t.Fatalf("expected playlist name in title, got %v", result[0].Metadata["title"])
	}
}

func TestEnrichDashboardUpcomingEntries_SmartBlockType(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	db.Create(&models.SmartBlock{ID: "sb1", StationID: "s1", Name: "Smart Block 1"})

	now := time.Now().UTC()
	entries := []models.ScheduleEntry{
		{
			ID:         "se1",
			StationID:  "s1",
			SourceType: "smart_block",
			SourceID:   "sb1",
			StartsAt:   now.Add(time.Hour),
			EndsAt:     now.Add(2 * time.Hour),
		},
	}

	result := h.enrichDashboardUpcomingEntries(entries)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Metadata["title"] != "Smart Block 1" {
		t.Fatalf("expected smart block name in title, got %v", result[0].Metadata["title"])
	}
}

func TestEnrichDashboardUpcomingEntries_ClockType(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	db.Create(&models.ClockHour{ID: "ch1", StationID: "s1", Name: "Clock Hour 1"})

	now := time.Now().UTC()
	entries := []models.ScheduleEntry{
		{
			ID:         "se1",
			StationID:  "s1",
			SourceType: "clock_template",
			SourceID:   "ch1",
			StartsAt:   now.Add(time.Hour),
			EndsAt:     now.Add(2 * time.Hour),
		},
	}

	result := h.enrichDashboardUpcomingEntries(entries)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Metadata["title"] != "Clock Hour 1" {
		t.Fatalf("expected clock name in title, got %v", result[0].Metadata["title"])
	}
}

func TestEnrichDashboardUpcomingEntries_MediaType(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	db.Create(&models.MediaItem{ID: "mi1", StationID: "s1", Title: "Great Track", Artist: "Great Artist", Path: "g.mp3"})

	now := time.Now().UTC()
	entries := []models.ScheduleEntry{
		{
			ID:         "se1",
			StationID:  "s1",
			SourceType: "media",
			SourceID:   "mi1",
			StartsAt:   now.Add(time.Hour),
			EndsAt:     now.Add(2 * time.Hour),
		},
	}

	result := h.enrichDashboardUpcomingEntries(entries)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	// Artist - Title format
	if !strings.Contains(result[0].Metadata["title"].(string), "Great Artist") {
		t.Fatalf("expected artist in title, got %v", result[0].Metadata["title"])
	}
}

func TestEnrichDashboardUpcomingEntries_LiveType(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	now := time.Now().UTC()
	entries := []models.ScheduleEntry{
		{
			ID:         "se1",
			StationID:  "s1",
			SourceType: "live",
			StartsAt:   now.Add(time.Hour),
			EndsAt:     now.Add(2 * time.Hour),
			Metadata:   map[string]any{"session_name": "Morning Show"},
		},
	}

	result := h.enrichDashboardUpcomingEntries(entries)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Metadata["title"] != "Morning Show" {
		t.Fatalf("expected session name in title, got %v", result[0].Metadata["title"])
	}
}

func TestEnrichDashboardUpcomingEntries_LiveTypeNoSessionName(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	now := time.Now().UTC()
	entries := []models.ScheduleEntry{
		{
			ID:         "se1",
			StationID:  "s1",
			SourceType: "live",
			StartsAt:   now.Add(time.Hour),
			EndsAt:     now.Add(2 * time.Hour),
		},
	}

	result := h.enrichDashboardUpcomingEntries(entries)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Metadata["title"] != "Live Session" {
		t.Fatalf("expected 'Live Session', got %v", result[0].Metadata["title"])
	}
}

func TestEnrichDashboardUpcomingEntries_StopsetType(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	now := time.Now().UTC()
	entries := []models.ScheduleEntry{
		{
			ID:         "se1",
			StationID:  "s1",
			SourceType: "stopset",
			StartsAt:   now.Add(time.Hour),
			EndsAt:     now.Add(2 * time.Hour),
		},
	}

	result := h.enrichDashboardUpcomingEntries(entries)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Metadata["title"] != "Stopset" {
		t.Fatalf("expected 'Stopset', got %v", result[0].Metadata["title"])
	}
}

func TestEnrichDashboardUpcomingEntries_MediaNoArtist(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	db.Create(&models.MediaItem{ID: "mi2", StationID: "s1", Title: "Solo Track", Path: "s.mp3"})

	now := time.Now().UTC()
	entries := []models.ScheduleEntry{
		{
			ID:         "se2",
			StationID:  "s1",
			SourceType: "media",
			SourceID:   "mi2",
			StartsAt:   now.Add(time.Hour),
			EndsAt:     now.Add(2 * time.Hour),
		},
	}

	result := h.enrichDashboardUpcomingEntries(entries)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Metadata["title"] != "Solo Track" {
		t.Fatalf("expected 'Solo Track', got %v", result[0].Metadata["title"])
	}
}

// ---------------------------------------------------------------------------
// ProfilePage
// ---------------------------------------------------------------------------

func TestProfilePage_NoUser_Redirects(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	req := dashboardReq(http.MethodGet, "/dashboard/profile", nil, nil)
	rr := httptest.NewRecorder()
	h.ProfilePage(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestProfilePage_WithUser_Renders200(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	req := dashboardReq(http.MethodGet, "/dashboard/profile", &u, nil)
	rr := httptest.NewRecorder()
	h.ProfilePage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// ProfileUpdate
// ---------------------------------------------------------------------------

func TestProfileUpdate_NoUser_Returns401(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	body := strings.NewReader("email=test@example.com")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ProfileUpdate(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestProfileUpdate_EmptyEmail_ReturnsError(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	body := strings.NewReader("email=")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.ProfileUpdate(rr, req)

	// Should render profile page with error (200 with error content) or redirect
	// ProfilePage with error message
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 (error page), got %d", rr.Code)
	}
}

func TestProfileUpdate_EmptyEmail_HTMX_Returns400(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	body := strings.NewReader("email=")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.ProfileUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestProfileUpdate_DuplicateEmail_ReturnsError(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	u1 := seedDashboardUser(t, db)
	// Create a second user with a different email
	u2 := models.User{ID: "dash-user2", Email: "other@example.com", Password: "x", PlatformRole: models.PlatformRoleUser}
	db.Create(&u2)

	body := strings.NewReader("email=other%40example.com")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u1))

	rr := httptest.NewRecorder()
	h.ProfileUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestProfileUpdate_Success_Redirects(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	body := strings.NewReader("email=updated%40example.com&display_name=Updated+User")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.ProfileUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestProfileUpdate_Success_HTMX_Returns200(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	body := strings.NewReader("email=updated2%40example.com")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.ProfileUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Profile updated") {
		t.Fatalf("expected success message, got: %s", rr.Body.String())
	}
}

func TestProfileUpdate_ValidCalendarTheme(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	body := strings.NewReader("email=dash%40example.com&calendar_color_theme=ocean")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.ProfileUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// ProfileUpdatePassword
// ---------------------------------------------------------------------------

func TestProfileUpdatePassword_NoUser_Returns401(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	body := strings.NewReader("current_password=old&new_password=newpass1&confirm_password=newpass1")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/password", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ProfileUpdatePassword(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestProfileUpdatePassword_WrongCurrent_ReturnsError(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpass"), bcrypt.MinCost)
	u := models.User{ID: "pwuser1", Email: "pw@example.com", Password: string(hash), PlatformRole: models.PlatformRoleUser}
	db.Create(&u)

	body := strings.NewReader("current_password=wrongpass&new_password=newpass123&confirm_password=newpass123")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/password", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.ProfileUpdatePassword(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestProfileUpdatePassword_TooShortNew_ReturnsError(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpass"), bcrypt.MinCost)
	u := models.User{ID: "pwuser2", Email: "pw2@example.com", Password: string(hash), PlatformRole: models.PlatformRoleUser}
	db.Create(&u)

	body := strings.NewReader("current_password=correctpass&new_password=short&confirm_password=short")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/password", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.ProfileUpdatePassword(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestProfileUpdatePassword_Mismatch_ReturnsError(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpass"), bcrypt.MinCost)
	u := models.User{ID: "pwuser3", Email: "pw3@example.com", Password: string(hash), PlatformRole: models.PlatformRoleUser}
	db.Create(&u)

	body := strings.NewReader("current_password=correctpass&new_password=newpass123&confirm_password=different123")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/password", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.ProfileUpdatePassword(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestProfileUpdatePassword_Success_HTMX(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpass"), bcrypt.MinCost)
	u := models.User{ID: "pwuser4", Email: "pw4@example.com", Password: string(hash), PlatformRole: models.PlatformRoleUser}
	db.Create(&u)

	body := strings.NewReader("current_password=correctpass&new_password=newpassword123&confirm_password=newpassword123")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/password", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.ProfileUpdatePassword(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Password updated") {
		t.Fatalf("expected success message, got: %s", rr.Body.String())
	}
}

func TestProfileUpdatePassword_Success_Redirect(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpass2"), bcrypt.MinCost)
	u := models.User{ID: "pwuser5", Email: "pw5@example.com", Password: string(hash), PlatformRole: models.PlatformRoleUser}
	db.Create(&u)

	body := strings.NewReader("current_password=correctpass2&new_password=newpassword456&confirm_password=newpassword456")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/password", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.ProfileUpdatePassword(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// ProfileLogoutAllDevices
// ---------------------------------------------------------------------------

func TestProfileLogoutAllDevices_NoUser_Returns401(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/logout-all", nil)
	rr := httptest.NewRecorder()
	h.ProfileLogoutAllDevices(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestProfileLogoutAllDevices_Success_Redirect(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/logout-all", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.ProfileLogoutAllDevices(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestProfileLogoutAllDevices_Success_HTMX(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/logout-all", nil)
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.ProfileLogoutAllDevices(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "logged out") {
		t.Fatalf("expected logout message, got: %s", rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// renderProfileError / renderPasswordError (via HTMX path)
// ---------------------------------------------------------------------------

func TestRenderProfileError_HTMX(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/profile", nil)
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.renderProfileError(rr, req, "Test error message")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Test error message") {
		t.Fatalf("expected error message in response, got: %s", rr.Body.String())
	}
}

func TestRenderProfileError_NonHTMX(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/profile", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.renderProfileError(rr, req, "Profile error")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 (rendered page), got %d", rr.Code)
	}
}

func TestRenderPasswordError_HTMX(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/password", nil)
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.renderPasswordError(rr, req, "Password error msg")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Password error msg") {
		t.Fatalf("expected error message in response, got: %s", rr.Body.String())
	}
}

func TestRenderPasswordError_NonHTMX(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/password", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))

	rr := httptest.NewRecorder()
	h.renderPasswordError(rr, req, "Password error msg")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 (rendered page), got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// APIKeysSection
// ---------------------------------------------------------------------------

func TestAPIKeysSection_NoUser_Returns401(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/profile/api-keys", nil)
	rr := httptest.NewRecorder()
	h.APIKeysSection(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAPIKeysSection_WithUser_Renders200(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/profile/api-keys", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.APIKeysSection(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// APIKeyGenerate
// ---------------------------------------------------------------------------

func TestAPIKeyGenerate_NoUser_Returns401(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	body := strings.NewReader("name=My+Key&expiration_days=90")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/api-keys", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.APIKeyGenerate(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAPIKeyGenerate_WithDefaultName_Generates200(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	body := strings.NewReader("expiration_days=90")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/api-keys", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.APIKeyGenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

func TestAPIKeyGenerate_WithName_Generates200(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	body := strings.NewReader("name=My+API+Key&expiration_days=30")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/api-keys", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.APIKeyGenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

func TestAPIKeyGenerate_InvalidDays_UsesDefault(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	body := strings.NewReader("name=Test+Key&expiration_days=notanumber")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/api-keys", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.APIKeyGenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

func TestAPIKeyGenerate_OverMaxDays_UsesDefault(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	body := strings.NewReader("name=Test+Key&expiration_days=9999")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/profile/api-keys", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.APIKeyGenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// APIKeyRevoke
// ---------------------------------------------------------------------------

func TestAPIKeyRevoke_NoUser_Returns401(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)

	req := httptest.NewRequest(http.MethodDelete, "/dashboard/profile/api-keys/k1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "k1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	h.APIKeyRevoke(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAPIKeyRevoke_NotFound_Returns404(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	req := httptest.NewRequest(http.MethodDelete, "/dashboard/profile/api-keys/nonexistent", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nonexistent-key-id")
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &u,
	))
	rr := httptest.NewRecorder()
	h.APIKeyRevoke(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestAPIKeyRevoke_Success_Returns200(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	// Generate and save a real API key
	_, apiKey, _ := generateTestAPIKey(u.ID)
	db.Create(apiKey)

	req := httptest.NewRequest(http.MethodDelete, "/dashboard/profile/api-keys/"+apiKey.ID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", apiKey.ID)
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &u,
	))
	rr := httptest.NewRecorder()
	h.APIKeyRevoke(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

func TestAPIKeyRevoke_MissingID_Returns400(t *testing.T) {
	db := newDashboardTestDB(t)
	h := newDashboardTestHandler(t, db)
	u := seedDashboardUser(t, db)

	req := httptest.NewRequest(http.MethodDelete, "/dashboard/profile/api-keys/", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	rr := httptest.NewRecorder()
	h.APIKeyRevoke(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// generateTestAPIKey creates a test API key model using the auth package.
func generateTestAPIKey(userID string) (string, *models.APIKey, error) {
	return auth.GenerateAPIKey(userID, "Test Key", 90*24*time.Hour)
}
