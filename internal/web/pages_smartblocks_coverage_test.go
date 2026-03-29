/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func newSmartBlockTestHandler(t *testing.T) (*Handler, *gorm.DB, models.User, models.Station) {
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
		&models.SmartBlock{},
		&models.LandingPage{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	user := models.User{
		ID:           "u-sb-1",
		Email:        "sbuser@example.com",
		Password:     "x",
		PlatformRole: models.PlatformRoleAdmin,
	}
	station := models.Station{ID: "st-sb-1", Name: "SmartBlock Station", Active: true}

	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.StationUser{
		ID:        "stu-sb-1",
		UserID:    user.ID,
		StationID: station.ID,
		Role:      models.StationRoleManager,
	}).Error; err != nil {
		t.Fatalf("create station user: %v", err)
	}

	h, err := NewHandler(db, []byte("test-secret"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	return h, db, user, station
}

// sbReq builds an HTTP request with the user/station/id context pre-populated.
func sbReq(t *testing.T, method, target string, user *models.User, station *models.Station, blockID string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	ctx := req.Context()
	if user != nil {
		ctx = context.WithValue(ctx, ctxKeyUser, user)
	}
	if station != nil {
		ctx = context.WithValue(ctx, ctxKeyStation, station)
	}
	if blockID != "" {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", blockID)
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	}
	return req.WithContext(ctx)
}

// sbReqWithForm builds an HTTP request with a form body.
func sbReqWithForm(t *testing.T, method, target string, user *models.User, station *models.Station, blockID string, form url.Values) *http.Request {
	t.Helper()
	body := strings.NewReader(form.Encode())
	req := httptest.NewRequest(method, target, body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := req.Context()
	if user != nil {
		ctx = context.WithValue(ctx, ctxKeyUser, user)
	}
	if station != nil {
		ctx = context.WithValue(ctx, ctxKeyStation, station)
	}
	if blockID != "" {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", blockID)
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	}
	return req.WithContext(ctx)
}

// seedSmartBlock creates a SmartBlock in the DB for testing.
func seedSmartBlock(t *testing.T, db *gorm.DB, id, stationID, name string) models.SmartBlock {
	t.Helper()
	block := models.SmartBlock{
		ID:        id,
		StationID: stationID,
		Name:      name,
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("seed smart block %s: %v", id, err)
	}
	return block
}

// ---------------------------------------------------------------------------
// SmartBlockList
// ---------------------------------------------------------------------------

func TestSmartBlockList_NoStation_Redirects(t *testing.T) {
	h, _, user, _ := newSmartBlockTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/smart-blocks", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.SmartBlockList(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect when no station, got %d", rr.Code)
	}
}

func TestSmartBlockList_WithStation_Renders200(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	seedSmartBlock(t, db, "blk-1", station.ID, "Morning Mix")
	seedSmartBlock(t, db, "blk-2", station.ID, "Evening Vibes")

	rr := httptest.NewRecorder()
	h.SmartBlockList(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks", &user, &station, ""))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{"Morning Mix", "Evening Vibes"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q", want)
		}
	}
}

func TestSmartBlockList_EmptyStation_Renders200(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	rr := httptest.NewRecorder()
	h.SmartBlockList(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks", &user, &station, ""))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// SmartBlockNew
// ---------------------------------------------------------------------------

func TestSmartBlockNew_WithStation_Renders200(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	rr := httptest.NewRecorder()
	h.SmartBlockNew(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/new", &user, &station, ""))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockNew_FallbackMode_Renders200(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	req := sbReq(t, http.MethodGet, "/dashboard/smart-blocks/new?mode=fallback", &user, &station, "")
	rr := httptest.NewRecorder()
	h.SmartBlockNew(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockNew_WithMediaAndPlaylists_Renders200(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	// Seed media with genre/artist/mood
	if err := db.Create(&models.MediaItem{
		ID:        "mi-sb-1",
		StationID: station.ID,
		Title:     "Test Track",
		Artist:    "Artist A",
		Genre:     "Rock",
		Mood:      "Energetic",
		Path:      "test.mp3",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}
	if err := db.Create(&models.Playlist{
		ID:        "pl-sb-1",
		StationID: station.ID,
		Name:      "Test Playlist",
	}).Error; err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockNew(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/new", &user, &station, ""))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// SmartBlockCreate
// ---------------------------------------------------------------------------

func TestSmartBlockCreate_NoStation_Returns400(t *testing.T) {
	h, _, user, _ := newSmartBlockTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/smart-blocks", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without station, got %d", rr.Code)
	}
}

func TestSmartBlockCreate_EmptyName_Returns400(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "")
	form.Set("duration_value", "60")
	form.Set("duration_unit", "minutes")

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty name, got %d", rr.Code)
	}
}

func TestSmartBlockCreate_ValidForm_Redirects(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "My Smart Block")
	form.Set("description", "A test block")
	form.Set("duration_value", "60")
	form.Set("duration_unit", "minutes")
	form.Set("sequence_mode", "random")

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect on create, got %d: %s", rr.Code, rr.Body.String())
	}

	var count int64
	db.Model(&models.SmartBlock{}).Where("station_id = ? AND name = ?", station.ID, "My Smart Block").Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 smart block in DB, got %d", count)
	}
}

func TestSmartBlockCreate_HXRequest_SetsHXRedirect(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "HTMX Block")
	form.Set("duration_value", "30")
	form.Set("duration_unit", "minutes")

	req := sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form)
	req.Header.Set("HX-Request", "true")

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HX-Request, got %d", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("HX-Redirect"), "/dashboard/smart-blocks/") {
		t.Fatalf("expected HX-Redirect header, got %q", rr.Header().Get("HX-Redirect"))
	}
}

func TestSmartBlockCreate_WithAllFilters_Redirects(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "Filtered Block")
	form.Set("duration_value", "2")
	form.Set("duration_unit", "hours")
	form.Set("filter_genre", "Rock")
	form.Set("filter_artist", "The Beatles")
	form.Set("filter_mood", "Energetic")
	form.Set("filter_era", "80s")
	form.Set("filter_language", "en")
	form.Set("filter_exclude_explicit", "on")
	form.Set("filter_bpm_min", "100")
	form.Set("filter_bpm_max", "160")
	form.Set("filter_year_min", "1980")
	form.Set("filter_year_max", "1989")
	form.Set("separation_enabled", "on")
	form.Set("sep_artist", "30")
	form.Set("sep_artist_unit", "minutes")
	form.Set("sep_title", "60")
	form.Set("sep_title_unit", "minutes")
	form.Set("energy_enabled", "on")
	form.Set("energy_curve", "30,50,70,80,60")
	form.Set("loop_to_fill", "on")

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockCreate_WithQuotasAndBoosters_Redirects(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "Quota Block")
	form.Set("duration_value", "60")
	form.Set("duration_unit", "minutes")
	form.Set("quotas_enabled", "on")
	form.Set("quota_0_field", "genre")
	form.Set("quota_0_value", "Rock")
	form.Set("quota_0_min", "30")
	form.Set("quota_0_max", "60")
	form.Set("boosters_enabled", "on")
	form.Set("booster_0_field", "genre")
	form.Set("booster_0_value", "Jazz")
	form.Set("booster_0_weight", "1.5")

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockCreate_WithAds_Redirects(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "Ad Block")
	form.Set("duration_value", "60")
	form.Set("duration_unit", "minutes")
	form.Set("ads_enabled", "on")
	form.Set("ads_every_n", "4")
	form.Set("ads_per_break", "2")
	form.Set("ads_source_type", "genre")
	form.Set("ads_genre", "Commercials")

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockCreate_WithMultiAds_Redirects(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "Multi-Ad Block")
	form.Set("duration_value", "60")
	form.Set("duration_unit", "minutes")
	form.Set("ads_enabled", "on")
	form.Set("ads_logic", "any")
	form["ads_source_type_multi"] = []string{"genre", "playlist"}
	form["ads_genre_multi"] = []string{"Commercials", ""}
	form["ads_playlist_multi"] = []string{"", "pl-ad-1"}
	form["ads_query_multi"] = []string{"", ""}

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockCreate_WithBumpers_Redirects(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "Bumper Block")
	form.Set("duration_value", "60")
	form.Set("duration_unit", "minutes")
	form.Set("bumpers_enabled", "on")
	form.Set("bumpers_source_type", "genre")
	form.Set("bumpers_genre", "Jingles")
	form.Set("bumpers_max_per_gap", "3")

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockCreate_WithFallbacks_Redirects(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	seedSmartBlock(t, db, "fb-blk-1", station.ID, "Fallback Block")

	form := url.Values{}
	form.Set("name", "Fallback Parent Block")
	form.Set("duration_value", "60")
	form.Set("duration_unit", "minutes")
	form.Set("fallbacks_enabled", "on")
	form.Set("fallback_0_block", "fb-blk-1")
	form.Set("fallback_0_limit", "5")

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockCreate_WithAddedDateRange_Redirects(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "Date Range Block")
	form.Set("duration_value", "60")
	form.Set("duration_unit", "minutes")
	form.Set("filter_newer_than", "7")
	form.Set("filter_newer_than_unit", "days")
	form.Set("filter_older_than", "30")
	form.Set("filter_older_than_unit", "days")

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// SmartBlockEdit
// ---------------------------------------------------------------------------

func TestSmartBlockEdit_NoStation_Redirects(t *testing.T) {
	h, _, user, _ := newSmartBlockTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/smart-blocks/blk-x/edit", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "blk-x")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.SmartBlockEdit(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect when no station, got %d", rr.Code)
	}
}

func TestSmartBlockEdit_NotFound_Returns404(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	rr := httptest.NewRecorder()
	h.SmartBlockEdit(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/nonexistent/edit", &user, &station, "nonexistent"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestSmartBlockEdit_Found_Renders200(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	seedSmartBlock(t, db, "blk-edit-1", station.ID, "Edit Me")

	rr := httptest.NewRecorder()
	h.SmartBlockEdit(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-edit-1/edit", &user, &station, "blk-edit-1"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockEdit_WithArchiveEnabled_Renders200(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	block := models.SmartBlock{
		ID:        "blk-arch-1",
		StationID: station.ID,
		Name:      "Archive Block",
		Rules:     map[string]any{"includePublicArchive": true},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockEdit(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-arch-1/edit", &user, &station, "blk-arch-1"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockEdit_WithIncludeArchiveLegacy_Renders200(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	block := models.SmartBlock{
		ID:        "blk-arch-leg-1",
		StationID: station.ID,
		Name:      "Legacy Archive Block",
		Rules:     map[string]any{"include_archive": true},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockEdit(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-arch-leg-1/edit", &user, &station, "blk-arch-leg-1"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// SmartBlockUpdate
// ---------------------------------------------------------------------------

func TestSmartBlockUpdate_NoStation_Returns400(t *testing.T) {
	h, _, user, _ := newSmartBlockTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/smart-blocks/blk-x", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "blk-x")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.SmartBlockUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without station, got %d", rr.Code)
	}
}

func TestSmartBlockUpdate_NotFound_Returns404(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "Updated Name")

	rr := httptest.NewRecorder()
	h.SmartBlockUpdate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks/nonexistent", &user, &station, "nonexistent", form))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestSmartBlockUpdate_Valid_Redirects(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	seedSmartBlock(t, db, "blk-upd-1", station.ID, "Original Name")

	form := url.Values{}
	form.Set("name", "Updated Name")
	form.Set("description", "Updated description")
	form.Set("duration_value", "90")
	form.Set("duration_unit", "minutes")
	form.Set("sequence_mode", "sequential")

	rr := httptest.NewRecorder()
	h.SmartBlockUpdate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks/blk-upd-1", &user, &station, "blk-upd-1", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}

	var updated models.SmartBlock
	db.First(&updated, "id = ?", "blk-upd-1")
	if updated.Name != "Updated Name" {
		t.Fatalf("expected name Updated Name, got %q", updated.Name)
	}
}

func TestSmartBlockUpdate_HXRequest_SetsHXRedirect(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	seedSmartBlock(t, db, "blk-upd-hx", station.ID, "HX Block")

	form := url.Values{}
	form.Set("name", "HX Updated")
	form.Set("duration_value", "60")
	form.Set("duration_unit", "minutes")

	req := sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks/blk-upd-hx", &user, &station, "blk-upd-hx", form)
	req.Header.Set("HX-Request", "true")

	rr := httptest.NewRecorder()
	h.SmartBlockUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HX-Request, got %d", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("HX-Redirect"), "blk-upd-hx") {
		t.Fatalf("expected HX-Redirect header containing block ID, got %q", rr.Header().Get("HX-Redirect"))
	}
}

// ---------------------------------------------------------------------------
// SmartBlockDelete
// ---------------------------------------------------------------------------

func TestSmartBlockDelete_NoStation_Returns400(t *testing.T) {
	h, _, user, _ := newSmartBlockTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/smart-blocks/blk-x/delete", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "blk-x")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.SmartBlockDelete(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without station, got %d", rr.Code)
	}
}

func TestSmartBlockDelete_NotFound_Returns404(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	rr := httptest.NewRecorder()
	h.SmartBlockDelete(rr, sbReq(t, http.MethodPost, "/dashboard/smart-blocks/nonexistent/delete", &user, &station, "nonexistent"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestSmartBlockDelete_Valid_Redirects(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	seedSmartBlock(t, db, "blk-del-1", station.ID, "To Delete")

	rr := httptest.NewRecorder()
	h.SmartBlockDelete(rr, sbReq(t, http.MethodPost, "/dashboard/smart-blocks/blk-del-1/delete", &user, &station, "blk-del-1"))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}

	var count int64
	db.Model(&models.SmartBlock{}).Where("id = ?", "blk-del-1").Count(&count)
	if count != 0 {
		t.Fatalf("expected block to be deleted, still found %d row(s)", count)
	}
}

func TestSmartBlockDelete_HXRequest_SetsHXRedirect(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	seedSmartBlock(t, db, "blk-del-hx", station.ID, "HX Delete")

	req := sbReq(t, http.MethodPost, "/dashboard/smart-blocks/blk-del-hx/delete", &user, &station, "blk-del-hx")
	req.Header.Set("HX-Request", "true")

	rr := httptest.NewRecorder()
	h.SmartBlockDelete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HX-Request, got %d", rr.Code)
	}
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatalf("expected HX-Redirect header")
	}
}

func TestSmartBlockDelete_RemovesFallbackRefs(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	// Create a block that references another block in its fallback rules
	targetBlock := seedSmartBlock(t, db, "blk-target", station.ID, "Target")
	refBlock := models.SmartBlock{
		ID:        "blk-ref-1",
		StationID: station.ID,
		Name:      "Referencing Block",
		Rules: map[string]any{
			"fallbacks": []any{
				map[string]any{"smart_block_id": "blk-target", "limit": 5},
			},
		},
	}
	if err := db.Create(&refBlock).Error; err != nil {
		t.Fatalf("create ref block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockDelete(rr, sbReq(t, http.MethodPost, "/dashboard/smart-blocks/blk-target/delete", &user, &station, targetBlock.ID))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// SmartBlockDuplicate
// ---------------------------------------------------------------------------

func TestSmartBlockDuplicate_NoStation_Returns400(t *testing.T) {
	h, _, user, _ := newSmartBlockTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/smart-blocks/blk-x/duplicate", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "blk-x")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.SmartBlockDuplicate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestSmartBlockDuplicate_NotFound_Returns404(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	rr := httptest.NewRecorder()
	h.SmartBlockDuplicate(rr, sbReq(t, http.MethodPost, "/dashboard/smart-blocks/nonexistent/duplicate", &user, &station, "nonexistent"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestSmartBlockDuplicate_Valid_Redirects(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	seedSmartBlock(t, db, "blk-dup-src", station.ID, "Original Block")

	rr := httptest.NewRecorder()
	h.SmartBlockDuplicate(rr, sbReq(t, http.MethodPost, "/dashboard/smart-blocks/blk-dup-src/duplicate", &user, &station, "blk-dup-src"))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}

	var count int64
	db.Model(&models.SmartBlock{}).Where("station_id = ? AND name LIKE ?", station.ID, "Original Block%").Count(&count)
	if count != 2 {
		t.Fatalf("expected 2 blocks (original + copy), got %d", count)
	}
}

func TestSmartBlockDuplicate_HXRequest_SetsHXRedirect(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	seedSmartBlock(t, db, "blk-dup-hx", station.ID, "HX Dup")

	req := sbReq(t, http.MethodPost, "/dashboard/smart-blocks/blk-dup-hx/duplicate", &user, &station, "blk-dup-hx")
	req.Header.Set("HX-Request", "true")

	rr := httptest.NewRecorder()
	h.SmartBlockDuplicate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HX-Request, got %d", rr.Code)
	}
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatalf("expected HX-Redirect header")
	}
}

// ---------------------------------------------------------------------------
// SmartBlockDetail
// ---------------------------------------------------------------------------

func TestSmartBlockDetail_NoStation_Redirects(t *testing.T) {
	h, _, user, _ := newSmartBlockTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/smart-blocks/blk-x", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "blk-x")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.SmartBlockDetail(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestSmartBlockDetail_NotFound_Returns404(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	rr := httptest.NewRecorder()
	h.SmartBlockDetail(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/nonexistent", &user, &station, "nonexistent"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestSmartBlockDetail_Found_Renders200(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	seedSmartBlock(t, db, "blk-detail-1", station.ID, "Detail Block")

	rr := httptest.NewRecorder()
	h.SmartBlockDetail(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-detail-1", &user, &station, "blk-detail-1"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// SmartBlockPreview
// ---------------------------------------------------------------------------

func TestSmartBlockPreview_NoStation_Returns400(t *testing.T) {
	h, _, user, _ := newSmartBlockTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/smart-blocks/blk-x/preview", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "blk-x")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestSmartBlockPreview_NotFound_Returns404(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/nonexistent/preview", &user, &station, "nonexistent"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestSmartBlockPreview_EmptyBlock_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	seedSmartBlock(t, db, "blk-prev-1", station.ID, "Preview Block")

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-prev-1/preview", &user, &station, "blk-prev-1"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockPreview_WithMedia_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	seedSmartBlock(t, db, "blk-prev-med", station.ID, "Media Preview Block")

	// Seed media
	for i := 1; i <= 5; i++ {
		if err := db.Create(&models.MediaItem{
			ID:            fmt.Sprintf("mi-prev-%d", i),
			StationID:     station.ID,
			Title:         fmt.Sprintf("Track %d", i),
			Artist:        "Preview Artist",
			Genre:         "Rock",
			Path:          fmt.Sprintf("track-%d.mp3", i),
			Duration:      time.Duration(3*60+30) * time.Second,
			AnalysisState: "complete",
		}).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}

	req := sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-prev-med/preview?variants=2", &user, &station, "blk-prev-med")
	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockPreview_WithFormValues_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	block := seedSmartBlock(t, db, "blk-prev-form", station.ID, "Form Preview Block")

	form := url.Values{}
	form.Set("filter_genre", "Rock")
	form.Set("separation_enabled", "on")
	form.Set("sep_artist", "30")
	form.Set("variants", "2")
	form.Set("loop", "true")

	body := strings.NewReader(form.Encode())
	req := httptest.NewRequest(http.MethodPost, "/dashboard/smart-blocks/blk-prev-form/preview", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", block.ID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockPreview_VariantsCapped_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	seedSmartBlock(t, db, "blk-prev-cap", station.ID, "Cap Preview Block")

	// Variants > 5 should be capped
	req := sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-prev-cap/preview?variants=10", &user, &station, "blk-prev-cap")
	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestParseInt_ValidInt(t *testing.T) {
	if got := parseInt("42", 0); got != 42 {
		t.Fatalf("parseInt(42) = %d, want 42", got)
	}
}

func TestParseInt_EmptyString_ReturnsDefault(t *testing.T) {
	if got := parseInt("", 5); got != 5 {
		t.Fatalf("parseInt('') = %d, want 5", got)
	}
}

func TestParseInt_InvalidString_ReturnsDefault(t *testing.T) {
	if got := parseInt("not_a_number", 7); got != 7 {
		t.Fatalf("parseInt('not_a_number') = %d, want 7", got)
	}
}

func TestParseFloat_ValidFloat(t *testing.T) {
	if got := parseFloat("3.14", 0); got != 3.14 {
		t.Fatalf("parseFloat(3.14) = %f, want 3.14", got)
	}
}

func TestParseFloat_EmptyString_ReturnsDefault(t *testing.T) {
	if got := parseFloat("", 2.5); got != 2.5 {
		t.Fatalf("parseFloat('') = %f, want 2.5", got)
	}
}

func TestParseFloat_InvalidString_ReturnsDefault(t *testing.T) {
	if got := parseFloat("abc", 1.0); got != 1.0 {
		t.Fatalf("parseFloat('abc') = %f, want 1.0", got)
	}
}

func TestParseSeparationMinutes_Minutes(t *testing.T) {
	if got := parseSeparationMinutes("30", "minutes"); got != 30 {
		t.Fatalf("parseSeparationMinutes(30, minutes) = %d, want 30", got)
	}
}

func TestParseSeparationMinutes_Hours(t *testing.T) {
	if got := parseSeparationMinutes("2", "hours"); got != 120 {
		t.Fatalf("parseSeparationMinutes(2, hours) = %d, want 120", got)
	}
}

func TestParseSeparationMinutes_Days(t *testing.T) {
	if got := parseSeparationMinutes("1", "days"); got != 1440 {
		t.Fatalf("parseSeparationMinutes(1, days) = %d, want 1440", got)
	}
}

func TestParseSeparationMinutes_Weeks(t *testing.T) {
	if got := parseSeparationMinutes("1", "weeks"); got != 10080 {
		t.Fatalf("parseSeparationMinutes(1, weeks) = %d, want 10080", got)
	}
}

func TestParseSeparationMinutes_Zero_ReturnsZero(t *testing.T) {
	if got := parseSeparationMinutes("0", "minutes"); got != 0 {
		t.Fatalf("parseSeparationMinutes(0, ...) = %d, want 0", got)
	}
}

func TestParseSeparationMinutes_Negative_ReturnsZero(t *testing.T) {
	if got := parseSeparationMinutes("-5", "minutes"); got != 0 {
		t.Fatalf("parseSeparationMinutes(-5, ...) = %d, want 0", got)
	}
}

func TestParseTrackYear_FullDate(t *testing.T) {
	if got := parseTrackYear("1985-06-15"); got != 1985 {
		t.Fatalf("parseTrackYear('1985-06-15') = %d, want 1985", got)
	}
}

func TestParseTrackYear_YearOnly(t *testing.T) {
	if got := parseTrackYear("2001"); got != 2001 {
		t.Fatalf("parseTrackYear('2001') = %d, want 2001", got)
	}
}

func TestParseTrackYear_Empty_ReturnsZero(t *testing.T) {
	if got := parseTrackYear(""); got != 0 {
		t.Fatalf("parseTrackYear('') = %d, want 0", got)
	}
}

func TestParseTrackYear_Invalid_ReturnsZero(t *testing.T) {
	if got := parseTrackYear("abcd"); got != 0 {
		t.Fatalf("parseTrackYear('abcd') = %d, want 0", got)
	}
}

func TestEraToYearRange_Current(t *testing.T) {
	min, max, ok := eraToYearRange("current")
	if !ok {
		t.Fatal("eraToYearRange('current') returned ok=false")
	}
	now := time.Now().Year()
	if min != now-2 || max != now {
		t.Fatalf("eraToYearRange('current') = (%d, %d), want (%d, %d)", min, max, now-2, now)
	}
}

func TestEraToYearRange_2010s(t *testing.T) {
	min, max, ok := eraToYearRange("2010s")
	if !ok || min != 2010 || max != 2019 {
		t.Fatalf("eraToYearRange('2010s') = (%d, %d, %v), want (2010, 2019, true)", min, max, ok)
	}
}

func TestEraToYearRange_2000s(t *testing.T) {
	min, max, ok := eraToYearRange("2000s")
	if !ok || min != 2000 || max != 2009 {
		t.Fatalf("eraToYearRange('2000s') = (%d, %d, %v)", min, max, ok)
	}
}

func TestEraToYearRange_90s(t *testing.T) {
	min, max, ok := eraToYearRange("90s")
	if !ok || min != 1990 || max != 1999 {
		t.Fatalf("eraToYearRange('90s') = (%d, %d, %v)", min, max, ok)
	}
}

func TestEraToYearRange_80s(t *testing.T) {
	min, max, ok := eraToYearRange("80s")
	if !ok || min != 1980 || max != 1989 {
		t.Fatalf("eraToYearRange('80s') = (%d, %d, %v)", min, max, ok)
	}
}

func TestEraToYearRange_70s(t *testing.T) {
	min, max, ok := eraToYearRange("70s")
	if !ok || min != 1970 || max != 1979 {
		t.Fatalf("eraToYearRange('70s') = (%d, %d, %v)", min, max, ok)
	}
}

func TestEraToYearRange_Classic(t *testing.T) {
	min, max, ok := eraToYearRange("classic")
	if !ok || min != 0 || max != 1979 {
		t.Fatalf("eraToYearRange('classic') = (%d, %d, %v)", min, max, ok)
	}
}

func TestEraToYearRange_2020s(t *testing.T) {
	min, max, ok := eraToYearRange("2020s")
	if !ok || min != 2020 || max != 2029 {
		t.Fatalf("eraToYearRange('2020s') = (%d, %d, %v)", min, max, ok)
	}
}

func TestEraToYearRange_Unknown_ReturnsFalse(t *testing.T) {
	_, _, ok := eraToYearRange("unknown_era")
	if ok {
		t.Fatal("eraToYearRange('unknown_era') returned ok=true, want false")
	}
}

func TestToStringSlice_StringSlice(t *testing.T) {
	input := []string{"a", "b", "c"}
	result := toStringSlice(input)
	if len(result) != 3 {
		t.Fatalf("toStringSlice([]string) = %v, want 3 elements", result)
	}
}

func TestToStringSlice_AnySlice(t *testing.T) {
	input := []any{"x", "y"}
	result := toStringSlice(input)
	if len(result) != 2 {
		t.Fatalf("toStringSlice([]any) = %v, want 2 elements", result)
	}
}

func TestToStringSlice_EmptyStrings_Filtered(t *testing.T) {
	input := []string{"a", "", "  ", "b"}
	result := toStringSlice(input)
	if len(result) != 2 {
		t.Fatalf("toStringSlice with empty strings = %v, want 2 elements (filtered)", result)
	}
}

func TestToStringSlice_Nil_ReturnsNil(t *testing.T) {
	result := toStringSlice(nil)
	if result != nil {
		t.Fatalf("toStringSlice(nil) = %v, want nil", result)
	}
}

func TestToStringSlice_Int_ReturnsNil(t *testing.T) {
	result := toStringSlice(42)
	if result != nil {
		t.Fatalf("toStringSlice(42) = %v, want nil", result)
	}
}

func TestToString_String(t *testing.T) {
	if got := toString("hello"); got != "hello" {
		t.Fatalf("toString('hello') = %q, want 'hello'", got)
	}
}

func TestToString_NonString_ReturnsEmpty(t *testing.T) {
	if got := toString(42); got != "" {
		t.Fatalf("toString(42) = %q, want ''", got)
	}
}

func TestToString_Nil_ReturnsEmpty(t *testing.T) {
	if got := toString(nil); got != "" {
		t.Fatalf("toString(nil) = %q, want ''", got)
	}
}

func TestToIntFromAny_Float64(t *testing.T) {
	if got := toIntFromAny(float64(3.7)); got != 3 {
		t.Fatalf("toIntFromAny(3.7) = %d, want 3", got)
	}
}

func TestToIntFromAny_Int(t *testing.T) {
	if got := toIntFromAny(int(5)); got != 5 {
		t.Fatalf("toIntFromAny(5) = %d, want 5", got)
	}
}

func TestToIntFromAny_String(t *testing.T) {
	if got := toIntFromAny("42"); got != 42 {
		t.Fatalf("toIntFromAny('42') = %d, want 42", got)
	}
}

func TestToIntFromAny_InvalidString_ReturnsZero(t *testing.T) {
	if got := toIntFromAny("abc"); got != 0 {
		t.Fatalf("toIntFromAny('abc') = %d, want 0", got)
	}
}

func TestToIntFromAny_Nil_ReturnsZero(t *testing.T) {
	if got := toIntFromAny(nil); got != 0 {
		t.Fatalf("toIntFromAny(nil) = %d, want 0", got)
	}
}

func TestSubtractDuration_Days(t *testing.T) {
	now := time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC)
	result := subtractDuration(now, 7, "days")
	expected := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Fatalf("subtractDuration(7 days) = %v, want %v", result, expected)
	}
}

func TestSubtractDuration_Weeks(t *testing.T) {
	now := time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC)
	result := subtractDuration(now, 2, "weeks")
	expected := time.Date(2026, 3, 12, 0, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Fatalf("subtractDuration(2 weeks) = %v, want %v", result, expected)
	}
}

func TestSubtractDuration_Months(t *testing.T) {
	now := time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC)
	result := subtractDuration(now, 1, "months")
	expected := time.Date(2026, 2, 26, 0, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Fatalf("subtractDuration(1 month) = %v, want %v", result, expected)
	}
}

func TestSubtractDuration_DefaultDays(t *testing.T) {
	now := time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC)
	result := subtractDuration(now, 3, "")
	expected := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Fatalf("subtractDuration(3 default) = %v, want %v", result, expected)
	}
}

func TestRemoveSmartBlockFallbackRef_NilRules_ReturnsFalse(t *testing.T) {
	if removeSmartBlockFallbackRef(nil, "some-id") {
		t.Fatal("expected false for nil rules")
	}
}

func TestRemoveSmartBlockFallbackRef_NoFallbacks_ReturnsFalse(t *testing.T) {
	rules := map[string]any{"someOtherKey": "value"}
	if removeSmartBlockFallbackRef(rules, "some-id") {
		t.Fatal("expected false when no fallbacks key")
	}
}

func TestRemoveSmartBlockFallbackRef_MatchingFallback_ReturnsTrue(t *testing.T) {
	rules := map[string]any{
		"fallbacks": []any{
			map[string]any{"smart_block_id": "target-id", "limit": 5},
			map[string]any{"smart_block_id": "other-id", "limit": 3},
		},
	}
	if !removeSmartBlockFallbackRef(rules, "target-id") {
		t.Fatal("expected true when matching fallback removed")
	}
	// Check only one fallback remains
	if fallbacks, ok := rules["fallbacks"].([]any); !ok || len(fallbacks) != 1 {
		t.Fatalf("expected 1 fallback remaining, got %v", rules["fallbacks"])
	}
}

func TestRemoveSmartBlockFallbackRef_LegacyKeys_ReturnsTrue(t *testing.T) {
	rules := map[string]any{
		"fallback_0_block": "target-id",
		"fallback_0_limit": 5,
		"fallback_1_block": "other-id",
	}
	if !removeSmartBlockFallbackRef(rules, "target-id") {
		t.Fatal("expected true when legacy fallback key removed")
	}
	if _, exists := rules["fallback_0_block"]; exists {
		t.Fatal("expected fallback_0_block to be removed")
	}
}

func TestRemoveSmartBlockFallbackRef_NoMatch_ReturnsFalse(t *testing.T) {
	rules := map[string]any{
		"fallbacks": []any{
			map[string]any{"smart_block_id": "other-id", "limit": 3},
		},
	}
	if removeSmartBlockFallbackRef(rules, "nonexistent-id") {
		t.Fatal("expected false when no matching fallback")
	}
}

// ---------------------------------------------------------------------------
// extractPreviewConfig tests
// ---------------------------------------------------------------------------

func TestExtractPreviewConfig_NilRules_ReturnsDefaults(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	cfg := h.extractPreviewConfig(nil, nil)

	if cfg.targetMinutes != 60 {
		t.Fatalf("expected targetMinutes=60 for nil rules, got %d", cfg.targetMinutes)
	}
	if cfg.adsEveryN != 4 {
		t.Fatalf("expected adsEveryN=4 default, got %d", cfg.adsEveryN)
	}
}

func TestExtractPreviewConfig_WithTargetMinutes(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{"targetMinutes": float64(30)}
	cfg := h.extractPreviewConfig(rules, nil)

	if cfg.targetMinutes != 30 {
		t.Fatalf("expected targetMinutes=30, got %d", cfg.targetMinutes)
	}
}

func TestExtractPreviewConfig_WithSeparation(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{
		"separationEnabled": true,
		"separation": map[string]any{
			"artist": float64(30),
			"title":  float64(60),
		},
	}
	cfg := h.extractPreviewConfig(rules, nil)

	if !cfg.separationEnabled {
		t.Fatal("expected separationEnabled=true")
	}
	if cfg.separation["artist"] != 30 {
		t.Fatalf("expected separation.artist=30, got %d", cfg.separation["artist"])
	}
}

func TestExtractPreviewConfig_WithInterstitials(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{
		"interstitials": map[string]any{
			"enabled":    true,
			"every":      float64(3),
			"perBreak":   float64(2),
			"logic":      "all",
			"sourceType": "genre",
			"genre":      "Commercials",
		},
	}
	cfg := h.extractPreviewConfig(rules, nil)

	if !cfg.adsEnabled {
		t.Fatal("expected adsEnabled=true")
	}
	if cfg.adsEveryN != 3 {
		t.Fatalf("expected adsEveryN=3, got %d", cfg.adsEveryN)
	}
	if cfg.adsLogic != "all" {
		t.Fatalf("expected adsLogic=all, got %q", cfg.adsLogic)
	}
}

func TestExtractPreviewConfig_WithInterstitials_MultiSource(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{
		"interstitials": map[string]any{
			"enabled": true,
			"sources": []any{
				map[string]any{"sourceType": "genre", "genre": "Ads"},
				map[string]any{"sourceType": "playlist", "playlistID": "pl-1"},
			},
		},
	}
	cfg := h.extractPreviewConfig(rules, nil)

	if !cfg.adsEnabled {
		t.Fatal("expected adsEnabled=true")
	}
	if len(cfg.adsSources) != 2 {
		t.Fatalf("expected 2 ad sources, got %d", len(cfg.adsSources))
	}
}

func TestExtractPreviewConfig_WithBumpers(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{
		"bumpers": map[string]any{
			"enabled":    true,
			"sourceType": "genre",
			"genre":      "Jingles",
			"maxPerGap":  float64(5),
		},
	}
	cfg := h.extractPreviewConfig(rules, nil)

	if !cfg.bumpersEnabled {
		t.Fatal("expected bumpersEnabled=true")
	}
	if cfg.bumpersGenre != "Jingles" {
		t.Fatalf("expected bumpersGenre=Jingles, got %q", cfg.bumpersGenre)
	}
	if cfg.bumpersMaxPerGap != 5 {
		t.Fatalf("expected bumpersMaxPerGap=5, got %d", cfg.bumpersMaxPerGap)
	}
}

func TestExtractPreviewConfig_WithQuotas(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{
		"quotasEnabled": true,
		"quotas": []any{
			map[string]any{
				"field":  "genre",
				"value":  "Rock",
				"minPct": float64(30),
				"maxPct": float64(60),
			},
		},
	}
	cfg := h.extractPreviewConfig(rules, nil)

	if !cfg.quotasEnabled {
		t.Fatal("expected quotasEnabled=true")
	}
	if len(cfg.quotas) != 1 {
		t.Fatalf("expected 1 quota, got %d", len(cfg.quotas))
	}
	if cfg.quotas[0].field != "genre" || cfg.quotas[0].value != "Rock" {
		t.Fatalf("unexpected quota: %+v", cfg.quotas[0])
	}
}

func TestExtractPreviewConfig_WithFallbacks(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{
		"fallbacksEnabled": true,
		"fallbacks": []any{
			map[string]any{"blockID": "blk-fb-1", "limit": float64(10)},
		},
	}
	cfg := h.extractPreviewConfig(rules, nil)

	if !cfg.fallbacksEnabled {
		t.Fatal("expected fallbacksEnabled=true")
	}
	if len(cfg.fallbacks) != 1 {
		t.Fatalf("expected 1 fallback, got %d", len(cfg.fallbacks))
	}
}

func TestExtractPreviewConfig_WithEnergyCurve(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{}
	sequence := map[string]any{
		"energyEnabled": true,
		"energyCurve":   []any{float64(30), float64(50), float64(80), float64(60)},
	}
	cfg := h.extractPreviewConfig(rules, sequence)

	if !cfg.energyEnabled {
		t.Fatal("expected energyEnabled=true")
	}
	if len(cfg.energyCurve) != 4 {
		t.Fatalf("expected 4 energy curve points, got %d", len(cfg.energyCurve))
	}
}

func TestExtractPreviewConfig_WithDurationAccuracy(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{
		"targetMinutes":    float64(45),
		"durationAccuracy": float64(5),
	}
	cfg := h.extractPreviewConfig(rules, nil)

	if cfg.targetMinutes != 45 {
		t.Fatalf("expected targetMinutes=45, got %d", cfg.targetMinutes)
	}
	if cfg.accuracyMs != 5000 {
		t.Fatalf("expected accuracyMs=5000, got %d", cfg.accuracyMs)
	}
}

// ---------------------------------------------------------------------------
// parseSmartBlockForm tests (via SmartBlockCreate)
// ---------------------------------------------------------------------------

func TestParseSmartBlockForm_DurationHours(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "Hours Block")
	form.Set("duration_value", "2")
	form.Set("duration_unit", "hours")

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestParseSmartBlockForm_DurationDays(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "Days Block")
	form.Set("duration_value", "1")
	form.Set("duration_unit", "days")

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestParseSmartBlockForm_DurationTooSmall_UsesMin1(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "Zero Duration Block")
	form.Set("duration_value", "0")
	form.Set("duration_unit", "minutes")

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestParseSmartBlockForm_SourcePlaylist_Stored(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "Playlist Source Block")
	form.Set("duration_value", "60")
	form.Set("duration_unit", "minutes")
	form["source_playlists"] = []string{"pl-1", "pl-2"}
	form.Set("source_include_archive", "on")

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}

	var block models.SmartBlock
	if err := db.Where("station_id = ? AND name = ?", station.ID, "Playlist Source Block").First(&block).Error; err != nil {
		t.Fatalf("fetch block: %v", err)
	}
	if playlists := toStringSlice(block.Rules["sourcePlaylists"]); len(playlists) != 2 {
		t.Fatalf("expected 2 source playlists, got %v", playlists)
	}
}

// ---------------------------------------------------------------------------
// fetchMusicTracks / fetchAdTracks / fetchBumperTracks (via preview integration)
// ---------------------------------------------------------------------------

func TestSmartBlockPreview_WithSourcePlaylists_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	playlist := models.Playlist{
		ID:        "pl-src-1",
		StationID: station.ID,
		Name:      "Source Playlist",
	}
	if err := db.Create(&playlist).Error; err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	media := models.MediaItem{
		ID:            "mi-pl-1",
		StationID:     station.ID,
		Title:         "Playlist Track",
		Path:          "p-track.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: "complete",
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}
	if err := db.Create(&models.PlaylistItem{
		ID:         "pli-1",
		PlaylistID: playlist.ID,
		MediaID:    media.ID,
		Position:   1,
	}).Error; err != nil {
		t.Fatalf("create playlist item: %v", err)
	}

	block := models.SmartBlock{
		ID:        "blk-pl-src",
		StationID: station.ID,
		Name:      "Playlist Source Block",
		Rules: map[string]any{
			"targetMinutes":  float64(5),
			"sourcePlaylists": []any{"pl-src-1"},
		},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-pl-src/preview", &user, &station, "blk-pl-src"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockPreview_WithYearRange_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	// Seed media with year
	if err := db.Create(&models.MediaItem{
		ID:            "mi-yr-1",
		StationID:     station.ID,
		Title:         "80s Track",
		Year:          "1985",
		Path:          "yr.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: "complete",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	block := models.SmartBlock{
		ID:        "blk-yr",
		StationID: station.ID,
		Name:      "Year Block",
		Rules: map[string]any{
			"targetMinutes": float64(5),
			"yearRange":     map[string]any{"min": float64(1980), "max": float64(1989)},
		},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-yr/preview", &user, &station, "blk-yr"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockPreview_WithEra_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	block := models.SmartBlock{
		ID:        "blk-era",
		StationID: station.ID,
		Name:      "Era Block",
		Rules: map[string]any{
			"targetMinutes": float64(5),
			"era":           "80s",
		},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-era/preview", &user, &station, "blk-era"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockPreview_WithAddedDateRange_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	block := models.SmartBlock{
		ID:        "blk-adr",
		StationID: station.ID,
		Name:      "ADR Block",
		Rules: map[string]any{
			"targetMinutes": float64(5),
			"addedDateRange": map[string]any{
				"newerThan":     float64(7),
				"newerThanUnit": "days",
			},
		},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-adr/preview", &user, &station, "blk-adr"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockPreview_WithBumperTracks_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	// Seed bumper genre media
	for i := 0; i < 3; i++ {
		if err := db.Create(&models.MediaItem{
			ID:            fmt.Sprintf("mi-bumper-%d", i),
			StationID:     station.ID,
			Title:         fmt.Sprintf("Bumper %d", i),
			Genre:         "Jingles",
			Path:          fmt.Sprintf("bumper-%d.mp3", i),
			Duration:      30 * time.Second,
			AnalysisState: "complete",
		}).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}

	block := models.SmartBlock{
		ID:        "blk-bumpers",
		StationID: station.ID,
		Name:      "Bumpers Block",
		Rules: map[string]any{
			"targetMinutes": float64(5),
			"bumpers": map[string]any{
				"enabled":    true,
				"sourceType": "genre",
				"genre":      "Jingles",
				"maxPerGap":  float64(3),
			},
		},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-bumpers/preview", &user, &station, "blk-bumpers"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockPreview_WithAds_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	// Seed main tracks
	for i := 0; i < 8; i++ {
		if err := db.Create(&models.MediaItem{
			ID:            fmt.Sprintf("mi-main-ad-%d", i),
			StationID:     station.ID,
			Title:         fmt.Sprintf("Main %d", i),
			Genre:         "Pop",
			Path:          fmt.Sprintf("main-%d.mp3", i),
			Duration:      3 * time.Minute,
			AnalysisState: "complete",
		}).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}
	// Seed ad tracks
	for i := 0; i < 3; i++ {
		if err := db.Create(&models.MediaItem{
			ID:            fmt.Sprintf("mi-ad-gen-%d", i),
			StationID:     station.ID,
			Title:         fmt.Sprintf("Ad %d", i),
			Genre:         "Commercials",
			Path:          fmt.Sprintf("ad-%d.mp3", i),
			Duration:      30 * time.Second,
			AnalysisState: "complete",
		}).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}

	block := models.SmartBlock{
		ID:        "blk-ads",
		StationID: station.ID,
		Name:      "Ads Block",
		Rules: map[string]any{
			"targetMinutes": float64(15),
			"interstitials": map[string]any{
				"enabled":    true,
				"every":      float64(4),
				"perBreak":   float64(1),
				"sourceType": "genre",
				"genre":      "Commercials",
			},
		},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-ads/preview", &user, &station, "blk-ads"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockPreview_WithFallbackBlock_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	fallbackBlock := seedSmartBlock(t, db, "blk-fb-prev", station.ID, "Fallback Block")

	// Seed fallback tracks
	if err := db.Create(&models.MediaItem{
		ID:            "mi-fb-1",
		StationID:     station.ID,
		Title:         "Fallback Track",
		Path:          "fb-track.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: "complete",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	block := models.SmartBlock{
		ID:        "blk-with-fb",
		StationID: station.ID,
		Name:      "Block With Fallback",
		Rules: map[string]any{
			"targetMinutes":    float64(10),
			"fallbacksEnabled": true,
			"fallbacks": []any{
				map[string]any{"blockID": fallbackBlock.ID, "limit": float64(5)},
			},
		},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-with-fb/preview", &user, &station, "blk-with-fb"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockPreview_WithSeparation_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	// Seed tracks from same artist to test separation
	for i := 0; i < 5; i++ {
		if err := db.Create(&models.MediaItem{
			ID:            fmt.Sprintf("mi-sep-%d", i),
			StationID:     station.ID,
			Title:         fmt.Sprintf("Same Artist Track %d", i),
			Artist:        "Separation Artist",
			Path:          fmt.Sprintf("sep-%d.mp3", i),
			Duration:      3 * time.Minute,
			AnalysisState: "complete",
		}).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}

	block := models.SmartBlock{
		ID:        "blk-sep",
		StationID: station.ID,
		Name:      "Separation Block",
		Rules: map[string]any{
			"targetMinutes":     float64(15),
			"separationEnabled": true,
			"separation": map[string]any{
				"artist": float64(15),
			},
		},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-sep/preview", &user, &station, "blk-sep"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockPreview_LoopEnabled_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	for i := 0; i < 3; i++ {
		if err := db.Create(&models.MediaItem{
			ID:            fmt.Sprintf("mi-loop-%d", i),
			StationID:     station.ID,
			Title:         fmt.Sprintf("Loop Track %d", i),
			Path:          fmt.Sprintf("loop-%d.mp3", i),
			Duration:      3 * time.Minute,
			AnalysisState: "complete",
		}).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}

	block := models.SmartBlock{
		ID:        "blk-loop",
		StationID: station.ID,
		Name:      "Loop Block",
		Rules: map[string]any{
			"targetMinutes": float64(15),
		},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	req := sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-loop/preview?loop=true", &user, &station, "blk-loop")
	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// fetchAdTracks source type coverage
// ---------------------------------------------------------------------------

func TestSmartBlockPreview_AdsByPlaylist_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	adPlaylist := models.Playlist{
		ID:        "pl-ads-1",
		StationID: station.ID,
		Name:      "Ad Playlist",
	}
	if err := db.Create(&adPlaylist).Error; err != nil {
		t.Fatalf("create ad playlist: %v", err)
	}

	block := models.SmartBlock{
		ID:        "blk-ads-pl",
		StationID: station.ID,
		Name:      "Ads by Playlist",
		Rules: map[string]any{
			"targetMinutes": float64(5),
			"interstitials": map[string]any{
				"enabled":    true,
				"every":      float64(2),
				"sourceType": "playlist",
				"playlistID": "pl-ads-1",
			},
		},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-ads-pl/preview", &user, &station, "blk-ads-pl"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockPreview_AdsByTitle_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	block := models.SmartBlock{
		ID:        "blk-ads-title",
		StationID: station.ID,
		Name:      "Ads by Title",
		Rules: map[string]any{
			"targetMinutes": float64(5),
			"interstitials": map[string]any{
				"enabled":    true,
				"every":      float64(2),
				"sourceType": "title",
				"query":      "ad",
			},
		},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-ads-title/preview", &user, &station, "blk-ads-title"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockPreview_MultiSourceAds_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	block := models.SmartBlock{
		ID:        "blk-ads-multi",
		StationID: station.ID,
		Name:      "Multi-Source Ads",
		Rules: map[string]any{
			"targetMinutes": float64(5),
			"interstitials": map[string]any{
				"enabled": true,
				"every":   float64(2),
				"logic":   "any",
				"sources": []any{
					map[string]any{"sourceType": "genre", "genre": "Commercials"},
					map[string]any{"sourceType": "artist", "query": "ad agency"},
					map[string]any{"sourceType": "label", "query": "ACME"},
				},
			},
		},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-ads-multi/preview", &user, &station, "blk-ads-multi"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// fetchBumperTracks source type coverage
// ---------------------------------------------------------------------------

func TestSmartBlockPreview_BumpersByTitle_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	block := models.SmartBlock{
		ID:        "blk-bumpers-title",
		StationID: station.ID,
		Name:      "Bumpers by Title",
		Rules: map[string]any{
			"targetMinutes": float64(5),
			"bumpers": map[string]any{
				"enabled":    true,
				"sourceType": "title",
				"query":      "jingle",
			},
		},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-bumpers-title/preview", &user, &station, "blk-bumpers-title"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockPreview_BumpersByArtist_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	block := models.SmartBlock{
		ID:        "blk-bumpers-artist",
		StationID: station.ID,
		Name:      "Bumpers by Artist",
		Rules: map[string]any{
			"targetMinutes": float64(5),
			"bumpers": map[string]any{
				"enabled":    true,
				"sourceType": "artist",
				"query":      "station voice",
			},
		},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-bumpers-artist/preview", &user, &station, "blk-bumpers-artist"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSmartBlockPreview_BumpersByLabel_Renders(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	block := models.SmartBlock{
		ID:        "blk-bumpers-label",
		StationID: station.ID,
		Name:      "Bumpers by Label",
		Rules: map[string]any{
			"targetMinutes": float64(5),
			"bumpers": map[string]any{
				"enabled":    true,
				"sourceType": "label",
				"query":      "self prod",
			},
		},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockPreview(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-bumpers-label/preview", &user, &station, "blk-bumpers-label"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// SmartBlockUpdate with complex form rules
// ---------------------------------------------------------------------------

func TestSmartBlockUpdate_WithSeparation_Persists(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	seedSmartBlock(t, db, "blk-sep-upd", station.ID, "Sep Block")

	form := url.Values{}
	form.Set("name", "Sep Block Updated")
	form.Set("duration_value", "60")
	form.Set("duration_unit", "minutes")
	form.Set("separation_enabled", "on")
	form.Set("sep_artist", "45")
	form.Set("sep_artist_unit", "minutes")
	form.Set("sep_album", "90")
	form.Set("sep_album_unit", "minutes")
	form.Set("sep_label", "2")
	form.Set("sep_label_unit", "hours")

	rr := httptest.NewRecorder()
	h.SmartBlockUpdate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks/blk-sep-upd", &user, &station, "blk-sep-upd", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}

	var block models.SmartBlock
	db.First(&block, "id = ?", "blk-sep-upd")
	if block.Rules["separationEnabled"] != true {
		t.Fatal("expected separationEnabled=true to be persisted")
	}
}

func TestSmartBlockUpdate_WithEnergyCurve_Persists(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)
	seedSmartBlock(t, db, "blk-energy-upd", station.ID, "Energy Block")

	form := url.Values{}
	form.Set("name", "Energy Block")
	form.Set("duration_value", "60")
	form.Set("duration_unit", "minutes")
	form.Set("energy_enabled", "on")
	form.Set("energy_curve", "20,40,60,80,100,80,60,40,20")

	rr := httptest.NewRecorder()
	h.SmartBlockUpdate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks/blk-energy-upd", &user, &station, "blk-energy-upd", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Error path: different station cannot see another station's block
// ---------------------------------------------------------------------------

func TestSmartBlockEdit_WrongStation_Returns404(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	// Create a second station
	otherStation := models.Station{ID: "st-other", Name: "Other Station", Active: true}
	if err := db.Create(&otherStation).Error; err != nil {
		t.Fatalf("create other station: %v", err)
	}

	// Create a block belonging to the other station
	otherBlock := models.SmartBlock{
		ID:        "blk-other-st",
		StationID: otherStation.ID,
		Name:      "Other Station Block",
	}
	if err := db.Create(&otherBlock).Error; err != nil {
		t.Fatalf("create other block: %v", err)
	}

	// Try to access other station's block via our station context
	rr := httptest.NewRecorder()
	h.SmartBlockEdit(rr, sbReq(t, http.MethodGet, "/dashboard/smart-blocks/blk-other-st/edit", &user, &station, "blk-other-st"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (cross-station access), got %d", rr.Code)
	}
}

func TestSmartBlockDelete_WrongStation_Returns404(t *testing.T) {
	h, db, user, station := newSmartBlockTestHandler(t)

	otherStation := models.Station{ID: "st-other-del", Name: "Other Station Del", Active: true}
	if err := db.Create(&otherStation).Error; err != nil {
		t.Fatalf("create other station: %v", err)
	}
	otherBlock := models.SmartBlock{
		ID:        "blk-other-del",
		StationID: otherStation.ID,
		Name:      "Other Station Block Del",
	}
	if err := db.Create(&otherBlock).Error; err != nil {
		t.Fatalf("create other block: %v", err)
	}

	rr := httptest.NewRecorder()
	h.SmartBlockDelete(rr, sbReq(t, http.MethodPost, "/dashboard/smart-blocks/blk-other-del/delete", &user, &station, "blk-other-del"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (cross-station delete), got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Duration accuracy clamping
// ---------------------------------------------------------------------------

func TestSmartBlockCreate_AccuracyClamped_LowEnd(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "Low Accuracy Block")
	form.Set("duration_value", "60")
	form.Set("duration_unit", "minutes")
	form.Set("duration_accuracy", "0") // below min, should be clamped to 1

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestSmartBlockCreate_AccuracyClamped_HighEnd(t *testing.T) {
	h, _, user, station := newSmartBlockTestHandler(t)

	form := url.Values{}
	form.Set("name", "High Accuracy Block")
	form.Set("duration_value", "60")
	form.Set("duration_unit", "minutes")
	form.Set("duration_accuracy", "9999") // above max, should be clamped to 60

	rr := httptest.NewRecorder()
	h.SmartBlockCreate(rr, sbReqWithForm(t, http.MethodPost, "/dashboard/smart-blocks", &user, &station, "", form))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// extractPreviewConfig edge cases
// ---------------------------------------------------------------------------

func TestExtractPreviewConfig_BumpersMaxPerGapZero_UsesDefault(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{
		"bumpers": map[string]any{
			"enabled":   true,
			"maxPerGap": float64(0), // Should use default of 8
		},
	}
	cfg := h.extractPreviewConfig(rules, nil)
	if cfg.bumpersMaxPerGap != 8 {
		t.Fatalf("expected bumpersMaxPerGap=8 (default), got %d", cfg.bumpersMaxPerGap)
	}
}

func TestExtractPreviewConfig_AdsEveryNZero_UsesDefault(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{
		"interstitials": map[string]any{
			"enabled": true,
			"every":   float64(0), // Should use default of 4
		},
	}
	cfg := h.extractPreviewConfig(rules, nil)
	if cfg.adsEveryN != 4 {
		t.Fatalf("expected adsEveryN=4 (default), got %d", cfg.adsEveryN)
	}
}

func TestExtractPreviewConfig_AdsPerBreakZero_UsesDefault(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{
		"interstitials": map[string]any{
			"enabled":  true,
			"perBreak": float64(0), // Should use default of 1
		},
	}
	cfg := h.extractPreviewConfig(rules, nil)
	if cfg.adsPerBreak != 1 {
		t.Fatalf("expected adsPerBreak=1 (default), got %d", cfg.adsPerBreak)
	}
}

func TestExtractPreviewConfig_InterstitialsLogicAny(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{
		"interstitials": map[string]any{
			"enabled": true,
			"logic":   "random_value",
		},
	}
	cfg := h.extractPreviewConfig(rules, nil)
	if cfg.adsLogic != "any" {
		t.Fatalf("expected adsLogic=any for unknown value, got %q", cfg.adsLogic)
	}
}

func TestExtractPreviewConfig_FallbacksEmptyBlockID_Skipped(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{
		"fallbacksEnabled": true,
		"fallbacks": []any{
			map[string]any{"blockID": "", "limit": float64(5)},        // empty blockID: skip
			map[string]any{"blockID": "valid-id", "limit": float64(0)}, // zero limit: use default 10
		},
	}
	cfg := h.extractPreviewConfig(rules, nil)
	if len(cfg.fallbacks) != 1 {
		t.Fatalf("expected 1 fallback (empty blockID skipped), got %d", len(cfg.fallbacks))
	}
	if cfg.fallbacks[0].limit != 10 {
		t.Fatalf("expected fallback limit=10 (default), got %d", cfg.fallbacks[0].limit)
	}
}

func TestExtractPreviewConfig_BumpersWithArchive(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{
		"bumpers": map[string]any{
			"enabled":              true,
			"sourceType":           "genre",
			"genre":                "Jingles",
			"includePublicArchive": true,
			"query":                "  station id  ",
			"playlistID":           "pl-1",
		},
	}
	cfg := h.extractPreviewConfig(rules, nil)
	if !cfg.bumpersIncludeArchive {
		t.Fatal("expected bumpersIncludeArchive=true")
	}
	if cfg.bumpersQuery != "station id" {
		t.Fatalf("expected bumpersQuery trimmed, got %q", cfg.bumpersQuery)
	}
}

func TestExtractPreviewConfig_InterstitialsWithArchive(t *testing.T) {
	h, _, _, _ := newSmartBlockTestHandler(t)
	rules := map[string]any{
		"interstitials": map[string]any{
			"enabled":              true,
			"includePublicArchive": true,
		},
	}
	cfg := h.extractPreviewConfig(rules, nil)
	if !cfg.adsIncludeArchive {
		t.Fatal("expected adsIncludeArchive=true")
	}
}
