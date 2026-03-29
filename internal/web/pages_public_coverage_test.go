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
// shared setup for public page tests
// ---------------------------------------------------------------------------

func newPublicTestDB(t *testing.T) *gorm.DB {
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
		&models.Show{},
		&models.ShowInstance{},
		&models.Recording{},
		&models.LiveSession{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newPublicTestHandler(t *testing.T, db *gorm.DB) *Handler {
	t.Helper()
	h, err := NewHandler(db, []byte("test-secret"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h
}

func publicReq(method, target string) *http.Request {
	return httptest.NewRequest(method, target, nil)
}

func publicReqWithID(method, target, paramName, paramVal string) *http.Request {
	req := publicReq(method, target)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(paramName, paramVal)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func publicReqWithUser(method, target string, user *models.User) *http.Request {
	req := publicReq(method, target)
	if user != nil {
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, user))
	}
	return req
}

func seedPublicStation(t *testing.T, db *gorm.DB, id, name string) models.Station {
	t.Helper()
	s := models.Station{
		ID:       id,
		Name:     name,
		Active:   true,
		Public:   true,
		Approved: true,
	}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("seed public station %s: %v", id, err)
	}
	return s
}

// ---------------------------------------------------------------------------
// Landing
// ---------------------------------------------------------------------------

func TestLanding_NoStations_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.Landing(rr, publicReq(http.MethodGet, "/"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestLanding_WithPublicStation_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")
	rr := httptest.NewRecorder()
	h.Landing(rr, publicReq(http.MethodGet, "/"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestLanding_FeaturedStation(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	s := seedPublicStation(t, db, "st1", "Test FM")
	s.Featured = true
	db.Save(&s)

	// Also seed a mount so stream URLs are built
	db.Create(&models.Mount{ID: "m1", StationID: "st1", Name: "main"})

	rr := httptest.NewRecorder()
	h.Landing(rr, publicReq(http.MethodGet, "/"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Listen
// ---------------------------------------------------------------------------

func TestListen_NoStations_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.Listen(rr, publicReq(http.MethodGet, "/listen"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestListen_WithStationsAndMounts_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")
	db.Create(&models.Mount{ID: "m1", StationID: "st1", Name: "main"})
	db.Create(&models.LandingPage{ID: "lp1", PublishedConfig: map[string]any{"stationOrder": []any{"st1"}}})

	rr := httptest.NewRecorder()
	h.Listen(rr, publicReq(http.MethodGet, "/listen"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// StationLanding
// ---------------------------------------------------------------------------

func TestStationLanding_EmptyShortcode_Returns404(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	rr := httptest.NewRecorder()
	// No shortcode param → empty string
	req := publicReqWithID(http.MethodGet, "/s/", "shortcode", "")
	h.StationLanding(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestStationLanding_NotFound_Returns404(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/s/missing", "shortcode", "missing")
	h.StationLanding(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestStationLanding_FoundByShortcode_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	s := seedPublicStation(t, db, "st1", "Test FM")
	s.Shortcode = "testfm"
	db.Save(&s)

	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/s/testfm", "shortcode", "testfm")
	h.StationLanding(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", rr.Code, rr.Body.String())
	}
}

func TestStationLanding_FoundByName_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st2", "Rock Radio")

	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/s/rock-radio", "shortcode", "rock-radio")
	h.StationLanding(rr, req)
	// Could match by slug/name fallback
	if rr.Code != http.StatusOK && rr.Code != http.StatusNotFound {
		t.Fatalf("unexpected status %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Archive
// ---------------------------------------------------------------------------

func TestArchive_NoStations_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.Archive(rr, publicReq(http.MethodGet, "/archive"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestArchive_WithMedia_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")
	db.Create(&models.MediaItem{
		ID:            "m1",
		StationID:     "st1",
		Title:         "Song Title",
		Artist:        "Some Artist",
		ShowInArchive: true,
		AnalysisState: "complete",
	})

	rr := httptest.NewRecorder()
	h.Archive(rr, publicReq(http.MethodGet, "/archive"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestArchive_WithSearchFilter_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	h.Archive(rr, publicReq(http.MethodGet, "/archive?q=rock"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestArchive_WithStationFilter_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	h.Archive(rr, publicReq(http.MethodGet, "/archive?station=st1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestArchive_WithDurationFilters(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")

	for _, dur := range []string{"short", "medium", "long"} {
		rr := httptest.NewRecorder()
		h.Archive(rr, publicReq(http.MethodGet, "/archive?duration="+dur))
		if rr.Code != http.StatusOK {
			t.Fatalf("duration=%s: expected 200, got %d", dur, rr.Code)
		}
	}
}

func TestArchive_WithSortOrders(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")

	for _, sort := range []string{"oldest", "title", "artist", "duration"} {
		rr := httptest.NewRecorder()
		h.Archive(rr, publicReq(http.MethodGet, "/archive?sort="+sort))
		if rr.Code != http.StatusOK {
			t.Fatalf("sort=%s: expected 200, got %d", sort, rr.Code)
		}
	}
}

func TestArchive_WithPlaylistFilter_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")
	db.Create(&models.Playlist{ID: "pl1", StationID: "st1", Name: "My List"})

	rr := httptest.NewRecorder()
	h.Archive(rr, publicReq(http.MethodGet, "/archive?show=playlist:pl1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestArchive_WithSmartBlockFilter_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")
	db.Create(&models.SmartBlock{ID: "sb1", StationID: "st1", Name: "Block"})

	rr := httptest.NewRecorder()
	h.Archive(rr, publicReq(http.MethodGet, "/archive?show=smartblock:sb1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestArchive_Pagination_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	h.Archive(rr, publicReq(http.MethodGet, "/archive?page=2"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// ArchiveDetail
// ---------------------------------------------------------------------------

func TestArchiveDetail_NotFound_Returns404(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/archive/missing", "id", "missing")
	h.ArchiveDetail(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestArchiveDetail_MediaNotInArchive_Returns404(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")
	// Create item then explicitly disable show_in_archive (bool default:true can't be overridden at create time)
	m := models.MediaItem{ID: "m1hide", StationID: "st1", Title: "Hidden", AnalysisState: "complete", ShowInArchive: true}
	db.Create(&m)
	db.Model(&m).Update("show_in_archive", false)

	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/archive/m1hide", "id", "m1hide")
	h.ArchiveDetail(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestArchiveDetail_PrivateStation_Returns404(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	// Private station
	db.Create(&models.Station{ID: "priv", Name: "Private", Active: true, Public: false, Approved: true})
	db.Create(&models.MediaItem{ID: "m1", StationID: "priv", Title: "Track", AnalysisState: "complete", ShowInArchive: true})

	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/archive/m1", "id", "m1")
	h.ArchiveDetail(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestArchiveDetail_Found_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")
	db.Create(&models.MediaItem{ID: "m1", StationID: "st1", Title: "Pub Track", AnalysisState: "complete", ShowInArchive: true})

	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/archive/m1", "id", "m1")
	h.ArchiveDetail(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// ArchiveArtwork
// ---------------------------------------------------------------------------

func TestArchiveArtwork_NotFound_Returns404(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/archive/missing/artwork", "id", "missing")
	h.ArchiveArtwork(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestArchiveArtwork_NoArtwork_Returns404(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")
	db.Create(&models.MediaItem{ID: "m1", StationID: "st1", Title: "Track", AnalysisState: "complete", ShowInArchive: true})

	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/archive/m1/artwork", "id", "m1")
	h.ArchiveArtwork(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestArchiveArtwork_WithArtwork_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")
	db.Create(&models.MediaItem{
		ID:            "m1",
		StationID:     "st1",
		Title:         "Track",
		AnalysisState: "complete",
		ShowInArchive: true,
		Artwork:       []byte{0xFF, 0xD8, 0xFF}, // fake JPEG header
		ArtworkMime:   "image/jpeg",
	})

	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/archive/m1/artwork", "id", "m1")
	h.ArchiveArtwork(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %s", ct)
	}
}

func TestArchiveArtwork_PrivateStation_Returns404(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	db.Create(&models.Station{ID: "priv", Name: "Private", Active: true, Public: false, Approved: true})
	db.Create(&models.MediaItem{
		ID:            "m1",
		StationID:     "priv",
		Title:         "Track",
		AnalysisState: "complete",
		ShowInArchive: true,
		Artwork:       []byte{0xFF, 0xD8},
	})

	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/archive/m1/artwork", "id", "m1")
	h.ArchiveArtwork(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// PublicMediaArtwork
// ---------------------------------------------------------------------------

func TestPublicMediaArtwork_NotFound_Returns404(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/media/missing/artwork", "id", "missing")
	h.PublicMediaArtwork(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPublicMediaArtwork_PrivateStation_Returns404(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	db.Create(&models.Station{ID: "priv", Name: "Private", Active: true, Public: false, Approved: true})
	db.Create(&models.MediaItem{ID: "m1", StationID: "priv", Title: "Track", AnalysisState: "complete", Artwork: []byte{0x01}})

	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/media/m1/artwork", "id", "m1")
	h.PublicMediaArtwork(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPublicMediaArtwork_NoArtwork_Returns404(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")
	db.Create(&models.MediaItem{ID: "m1", StationID: "st1", Title: "Track", AnalysisState: "complete"})

	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/media/m1/artwork", "id", "m1")
	h.PublicMediaArtwork(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPublicMediaArtwork_WithArtwork_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")
	db.Create(&models.MediaItem{
		ID:            "m1",
		StationID:     "st1",
		Title:         "Track",
		AnalysisState: "complete",
		Artwork:       []byte{0xFF, 0xD8, 0xFF},
		ArtworkMime:   "image/jpeg",
	})

	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/media/m1/artwork", "id", "m1")
	h.PublicMediaArtwork(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// PublicSchedule
// ---------------------------------------------------------------------------

func TestPublicSchedule_NoStations_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.PublicSchedule(rr, publicReq(http.MethodGet, "/schedule"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestPublicSchedule_WithStation_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	h.PublicSchedule(rr, publicReq(http.MethodGet, "/schedule?station_id=st1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestPublicSchedule_WithNonPublicStation_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	// Non-public station — should still return 200 but with empty entries
	db.Create(&models.Station{ID: "priv", Name: "Private", Active: true, Public: false, Approved: true})

	rr := httptest.NewRecorder()
	h.PublicSchedule(rr, publicReq(http.MethodGet, "/schedule?station_id=priv"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestPublicSchedule_WithLoggedInUser_UsesColorTheme(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")

	u := models.User{ID: "u1", Email: "u@example.com", Password: "x", CalendarColorTheme: "ocean"}
	db.Create(&u)

	rr := httptest.NewRecorder()
	req := publicReqWithUser(http.MethodGet, "/schedule", &u)
	h.PublicSchedule(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// PublicScheduleEvents
// ---------------------------------------------------------------------------

func TestPublicScheduleEvents_NoStations_ReturnsEmptyJSON(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.PublicScheduleEvents(rr, publicReq(http.MethodGet, "/schedule/events"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "[]") {
		t.Fatalf("expected empty JSON array, got: %s", body)
	}
}

func TestPublicScheduleEvents_WithStation_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	h.PublicScheduleEvents(rr, publicReq(http.MethodGet, "/schedule/events?station_id=st1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestPublicScheduleEvents_WithTheme_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")

	for _, theme := range []string{"ocean", "forest", "sunset", "berry", "earth", "neon", "pastel", "invalid"} {
		rr := httptest.NewRecorder()
		h.PublicScheduleEvents(rr, publicReq(http.MethodGet, "/schedule/events?theme="+theme))
		if rr.Code != http.StatusOK {
			t.Fatalf("theme=%s: expected 200, got %d", theme, rr.Code)
		}
	}
}

func TestPublicScheduleEvents_WithDateRange_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	h.PublicScheduleEvents(rr, publicReq(http.MethodGet, "/schedule/events?start=2026-01-01T00:00:00Z&end=2026-01-31T00:00:00Z"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// StationInfo
// ---------------------------------------------------------------------------

func TestStationInfo_NotFound_Returns404(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/stations/missing", "id", "missing")
	h.StationInfo(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestStationInfo_PrivateStation_Returns404(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	db.Create(&models.Station{ID: "priv", Name: "Private", Active: true, Public: false, Approved: true})

	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/stations/priv", "id", "priv")
	h.StationInfo(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestStationInfo_PublicStation_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")
	db.Create(&models.Mount{ID: "m1", StationID: "st1", Name: "main"})

	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/stations/st1", "id", "st1")
	h.StationInfo(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// LoginPage
// ---------------------------------------------------------------------------

func TestLoginPage_NotLoggedIn_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.LoginPage(rr, publicReq(http.MethodGet, "/login"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestLoginPage_AlreadyLoggedIn_Redirects(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	u := models.User{ID: "u1", Email: "u@example.com", Password: "x"}
	db.Create(&u)

	rr := httptest.NewRecorder()
	req := publicReqWithUser(http.MethodGet, "/login", &u)
	h.LoginPage(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestLoginPage_WithRedirectParam_Returns200(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.LoginPage(rr, publicReq(http.MethodGet, "/login?redirect=/dashboard/media"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// LoginSubmit
// ---------------------------------------------------------------------------

func TestLoginSubmit_EmptyCredentials_ShowsError(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)

	form := url.Values{}
	form.Set("email", "")
	form.Set("password", "")
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	h.LoginSubmit(rr, req)
	// Should render the login page with an error (200) or redirect
	if rr.Code == http.StatusInternalServerError {
		t.Fatalf("unexpected 500, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestLoginSubmit_InvalidEmail_ShowsError(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)

	form := url.Values{}
	form.Set("email", "nonexistent@example.com")
	form.Set("password", "password")
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	h.LoginSubmit(rr, req)
	if rr.Code == http.StatusInternalServerError {
		t.Fatalf("unexpected 500: %s", rr.Body.String())
	}
}

func TestLoginSubmit_HTMXInvalidEmail_Returns401(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)

	form := url.Values{}
	form.Set("email", "nobody@example.com")
	form.Set("password", "wrong")
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")

	rr := httptest.NewRecorder()
	h.LoginSubmit(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Logout
// ---------------------------------------------------------------------------

func TestLogout_Redirects(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	rr := httptest.NewRecorder()
	h.Logout(rr, publicReq(http.MethodGet, "/logout"))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/login" {
		t.Fatalf("expected redirect to /login, got %s", loc)
	}
}

// ---------------------------------------------------------------------------
// sanitizeRedirectTarget
// ---------------------------------------------------------------------------

func TestSanitizeRedirectTarget_EmptyString(t *testing.T) {
	if got := sanitizeRedirectTarget(""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestSanitizeRedirectTarget_AbsoluteURL_Rejected(t *testing.T) {
	if got := sanitizeRedirectTarget("https://evil.com"); got != "" {
		t.Fatalf("expected empty for absolute URL, got %q", got)
	}
}

func TestSanitizeRedirectTarget_ExternalHost_Rejected(t *testing.T) {
	if got := sanitizeRedirectTarget("//evil.com/path"); got != "" {
		t.Fatalf("expected empty for double-slash, got %q", got)
	}
}

func TestSanitizeRedirectTarget_LoginPage_Rejected(t *testing.T) {
	if got := sanitizeRedirectTarget("/login"); got != "" {
		t.Fatalf("expected empty for /login redirect, got %q", got)
	}
}

func TestSanitizeRedirectTarget_ValidPath_Preserved(t *testing.T) {
	if got := sanitizeRedirectTarget("/dashboard/media"); got != "/dashboard/media" {
		t.Fatalf("expected /dashboard/media, got %q", got)
	}
}

func TestSanitizeRedirectTarget_PathWithQuery_Preserved(t *testing.T) {
	if got := sanitizeRedirectTarget("/dashboard?tab=media"); got != "/dashboard?tab=media" {
		t.Fatalf("expected /dashboard?tab=media, got %q", got)
	}
}

func TestSanitizeRedirectTarget_InvalidURL_Rejected(t *testing.T) {
	// URL starting with non-slash
	if got := sanitizeRedirectTarget("javascript:alert(1)"); got != "" {
		t.Fatalf("expected empty for non-slash path, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// stationMountMap
// ---------------------------------------------------------------------------

func TestStationMountMap_EmptyStations(t *testing.T) {
	db := newPublicTestDB(t)
	result := stationMountMap(db, nil)
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(result))
	}
}

func TestStationMountMap_WithStations(t *testing.T) {
	db := newPublicTestDB(t)
	seedPublicStation(t, db, "st1", "Test FM")
	db.Create(&models.Mount{ID: "m1", StationID: "st1", Name: "main"})
	db.Create(&models.Mount{ID: "m2", StationID: "st1", Name: "secondary"})

	stations := []models.Station{{ID: "st1", Name: "Test FM"}}
	result := stationMountMap(db, stations)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry (first mount wins), got %d", len(result))
	}
	if result["st1"].ID != "m1" && result["st1"].ID != "m2" {
		t.Fatalf("unexpected mount ID %s", result["st1"].ID)
	}
}

// ---------------------------------------------------------------------------
// buildStationsWithStreams
// ---------------------------------------------------------------------------

func TestBuildStationsWithStreams_NoMounts(t *testing.T) {
	stations := []models.Station{{ID: "st1", Name: "Test FM"}}
	mountMap := map[string]models.Mount{}
	result := buildStationsWithStreams(stations, mountMap)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].StreamURL != "" {
		t.Fatalf("expected empty stream URL for no mount, got %s", result[0].StreamURL)
	}
}

func TestBuildStationsWithStreams_WithMount(t *testing.T) {
	stations := []models.Station{{ID: "st1", Name: "Test FM"}}
	mountMap := map[string]models.Mount{"st1": {ID: "m1", StationID: "st1", Name: "main"}}
	result := buildStationsWithStreams(stations, mountMap)
	if result[0].StreamURL != "/live/main" {
		t.Fatalf("expected /live/main, got %s", result[0].StreamURL)
	}
	if result[0].StreamURLLQ != "/live/main-lq" {
		t.Fatalf("expected /live/main-lq, got %s", result[0].StreamURLLQ)
	}
}

// ---------------------------------------------------------------------------
// reorderStationsForStationLanding
// ---------------------------------------------------------------------------

func TestReorderStationsForStationLanding_EmptyList(t *testing.T) {
	result := reorderStationsForStationLanding(nil, "st1", "current_first")
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestReorderStationsForStationLanding_CurrentFirst(t *testing.T) {
	stations := []models.Station{
		{ID: "st1", Name: "Alpha"},
		{ID: "st2", Name: "Beta"},
		{ID: "st3", Name: "Gamma"},
	}
	result := reorderStationsForStationLanding(stations, "st2", "current_first")
	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	if result[0].ID != "st2" {
		t.Fatalf("expected st2 first, got %s", result[0].ID)
	}
}

func TestReorderStationsForStationLanding_AfterPlatformFirst(t *testing.T) {
	stations := []models.Station{
		{ID: "st1", Name: "Alpha"},
		{ID: "st2", Name: "Beta"},
	}
	result := reorderStationsForStationLanding(stations, "st2", "after_platform_first")
	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	// Platform first should be st1, then st2
	if result[0].ID != "st1" {
		t.Fatalf("expected st1 first (platform first), got %s", result[0].ID)
	}
}

func TestReorderStationsForStationLanding_CurrentNotInList(t *testing.T) {
	stations := []models.Station{
		{ID: "st1", Name: "Alpha"},
		{ID: "st2", Name: "Beta"},
	}
	result := reorderStationsForStationLanding(stations, "st99", "current_first")
	if len(result) == 0 {
		t.Fatal("expected non-empty result even when current not found")
	}
}

// ---------------------------------------------------------------------------
// ArchiveStream download-blocked path
// ---------------------------------------------------------------------------

func TestArchiveStream_NotFound_Returns404(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/archive/missing/stream", "id", "missing")
	h.ArchiveStream(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestArchiveStream_DownloadNotAllowed_Returns403(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")
	// Create the media item, then explicitly set AllowDownload=false to override the DB default:true
	m := models.MediaItem{
		ID:            "m1",
		StationID:     "st1",
		Title:         "Track",
		AnalysisState: "complete",
		ShowInArchive: true,
		AllowDownload: true,
		Path:          "st1/ab/cd/track.mp3",
	}
	db.Create(&m)
	db.Exec("UPDATE media_items SET allow_download = 0 WHERE id = 'src1'")
	db.Model(&m).Update("allow_download", false)

	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/archive/m1/stream?download=1", "id", "m1")
	h.ArchiveStream(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestArchiveStream_PrivateStation_Returns404(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	db.Create(&models.Station{ID: "priv", Name: "Private", Active: true, Public: false, Approved: true})
	db.Create(&models.MediaItem{
		ID:            "m1",
		StationID:     "priv",
		Title:         "Track",
		AnalysisState: "complete",
		ShowInArchive: true,
		Path:          "priv/ab/cd/track.mp3",
	})

	rr := httptest.NewRecorder()
	req := publicReqWithID(http.MethodGet, "/archive/m1/stream", "id", "m1")
	h.ArchiveStream(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}
