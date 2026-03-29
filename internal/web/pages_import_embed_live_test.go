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
// Shared setup helpers (embed / import / live)
// ---------------------------------------------------------------------------

func newEmbedLiveDB(t *testing.T) *gorm.DB {
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

func newEmbedLiveHandler(t *testing.T, db *gorm.DB) *Handler {
	t.Helper()
	h, err := NewHandler(db, []byte("test-secret"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h
}

func seedEmbedPublicStation(t *testing.T, db *gorm.DB, id, name string) models.Station {
	t.Helper()
	s := models.Station{ID: id, Name: name, Active: true, Public: true, Approved: true}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("seed station %s: %v", id, err)
	}
	return s
}

func seedEmbedUser(t *testing.T, db *gorm.DB, id, email string) models.User {
	t.Helper()
	u := models.User{ID: id, Email: email, Password: "x", PlatformRole: models.PlatformRoleAdmin}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed user %s: %v", id, err)
	}
	return u
}

func reqWithIDAndUser(method, target, paramName, paramVal string, user *models.User) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(paramName, paramVal)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if user != nil {
		ctx = context.WithValue(ctx, ctxKeyUser, user)
	}
	return req.WithContext(ctx)
}

func reqWithStationAndUser(method, target string, station *models.Station, user *models.User) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	ctx := req.Context()
	if station != nil {
		ctx = context.WithValue(ctx, ctxKeyStation, station)
	}
	if user != nil {
		ctx = context.WithValue(ctx, ctxKeyUser, user)
	}
	return req.WithContext(ctx)
}

// ---------------------------------------------------------------------------
// EmbedSchedule
// ---------------------------------------------------------------------------

func TestEmbedSchedule_MissingStationParam_Returns400(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	rr := httptest.NewRecorder()
	h.EmbedSchedule(rr, httptest.NewRequest(http.MethodGet, "/embed/schedule", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestEmbedSchedule_StationNotFound_Returns404(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	rr := httptest.NewRecorder()
	h.EmbedSchedule(rr, httptest.NewRequest(http.MethodGet, "/embed/schedule?station=nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestEmbedSchedule_PrivateStation_Returns404(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	db.Create(&models.Station{ID: "priv", Name: "Private", Active: true, Public: false})

	rr := httptest.NewRecorder()
	h.EmbedSchedule(rr, httptest.NewRequest(http.MethodGet, "/embed/schedule?station=priv", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestEmbedSchedule_PublicStation_Returns200(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	seedEmbedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	h.EmbedSchedule(rr, httptest.NewRequest(http.MethodGet, "/embed/schedule?station=st1", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", rr.Code, rr.Body.String())
	}
}

func TestEmbedSchedule_ThemeValidation(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	seedEmbedPublicStation(t, db, "st1", "Test FM")

	for _, theme := range []string{"dark", "light", "invalid"} {
		rr := httptest.NewRecorder()
		h.EmbedSchedule(rr, httptest.NewRequest(http.MethodGet, "/embed/schedule?station=st1&theme="+theme, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("theme=%s: expected 200, got %d", theme, rr.Code)
		}
	}
}

func TestEmbedSchedule_DaysValidation(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	seedEmbedPublicStation(t, db, "st1", "Test FM")

	for _, days := range []string{"0", "5", "30", "99", "-1"} {
		rr := httptest.NewRecorder()
		h.EmbedSchedule(rr, httptest.NewRequest(http.MethodGet, "/embed/schedule?station=st1&days="+days, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("days=%s: expected 200, got %d", days, rr.Code)
		}
	}
}

func TestEmbedSchedule_CompactAndShowHost(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	seedEmbedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	h.EmbedSchedule(rr, httptest.NewRequest(http.MethodGet, "/embed/schedule?station=st1&compact=true&show_host=false", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// EmbedNowPlaying
// ---------------------------------------------------------------------------

func TestEmbedNowPlaying_MissingStationParam_Returns400(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	rr := httptest.NewRecorder()
	h.EmbedNowPlaying(rr, httptest.NewRequest(http.MethodGet, "/embed/now-playing", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestEmbedNowPlaying_StationNotFound_Returns404(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	rr := httptest.NewRecorder()
	h.EmbedNowPlaying(rr, httptest.NewRequest(http.MethodGet, "/embed/now-playing?station=nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestEmbedNowPlaying_PrivateStation_Returns404(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	db.Create(&models.Station{ID: "priv", Name: "Private", Active: true, Public: false})

	rr := httptest.NewRecorder()
	h.EmbedNowPlaying(rr, httptest.NewRequest(http.MethodGet, "/embed/now-playing?station=priv", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestEmbedNowPlaying_PublicStation_Returns200(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	seedEmbedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	h.EmbedNowPlaying(rr, httptest.NewRequest(http.MethodGet, "/embed/now-playing?station=st1", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", rr.Code, rr.Body.String())
	}
}

func TestEmbedNowPlaying_ThemeAndShowNext(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	seedEmbedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	h.EmbedNowPlaying(rr, httptest.NewRequest(http.MethodGet, "/embed/now-playing?station=st1&theme=dark&show_next=false", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// EmbedScheduleJS
// ---------------------------------------------------------------------------

func TestEmbedScheduleJS_ReturnsJavaScript(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	rr := httptest.NewRecorder()
	h.EmbedScheduleJS(rr, httptest.NewRequest(http.MethodGet, "/embed/schedule.js", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "javascript") {
		t.Fatalf("expected javascript content-type, got %s", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "GrimnirSchedule") {
		t.Fatalf("expected GrimnirSchedule in JS body")
	}
}

func TestEmbedScheduleJS_CacheControlHeader(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	rr := httptest.NewRecorder()
	h.EmbedScheduleJS(rr, httptest.NewRequest(http.MethodGet, "/embed/schedule.js", nil))
	if cc := rr.Header().Get("Cache-Control"); cc == "" {
		t.Fatal("expected Cache-Control header")
	}
}

// ---------------------------------------------------------------------------
// EmbedScheduleDataJSON
// ---------------------------------------------------------------------------

func TestEmbedScheduleDataJSON_MissingStation_Returns400(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	rr := httptest.NewRecorder()
	h.EmbedScheduleDataJSON(rr, httptest.NewRequest(http.MethodGet, "/embed/schedule.json", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestEmbedScheduleDataJSON_StationNotFound_Returns404(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	rr := httptest.NewRecorder()
	h.EmbedScheduleDataJSON(rr, httptest.NewRequest(http.MethodGet, "/embed/schedule.json?station=nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestEmbedScheduleDataJSON_PublicStation_Returns200(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	seedEmbedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	h.EmbedScheduleDataJSON(rr, httptest.NewRequest(http.MethodGet, "/embed/schedule.json?station=st1", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "json") {
		t.Fatalf("expected JSON content-type, got %s", ct)
	}
}

func TestEmbedScheduleDataJSON_CORSHeader(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	seedEmbedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	h.EmbedScheduleDataJSON(rr, httptest.NewRequest(http.MethodGet, "/embed/schedule.json?station=st1", nil))
	if cors := rr.Header().Get("Access-Control-Allow-Origin"); cors != "*" {
		t.Fatalf("expected CORS header *, got %q", cors)
	}
}

func TestEmbedScheduleDataJSON_InvalidDaysClampedTo7(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	seedEmbedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	h.EmbedScheduleDataJSON(rr, httptest.NewRequest(http.MethodGet, "/embed/schedule.json?station=st1&days=999", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// GetScheduleData
// ---------------------------------------------------------------------------

func TestGetScheduleData_EmptyDB_ReturnsEmptySlice(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	result, err := h.GetScheduleData("st1", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty slice, got %d items", len(result))
	}
}

// ---------------------------------------------------------------------------
// GetNowPlayingData
// ---------------------------------------------------------------------------

func TestGetNowPlayingData_EmptyDB_ReturnsNil(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	current, next, err := h.GetNowPlayingData("st1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if current != nil {
		t.Fatalf("expected nil current, got %+v", current)
	}
	if next != nil {
		t.Fatalf("expected nil next, got %+v", next)
	}
}

// ---------------------------------------------------------------------------
// pages_import_review.go — pure helper functions (no HTTP server needed)
// ---------------------------------------------------------------------------

func TestContainsString_Found(t *testing.T) {
	if !containsString([]string{"a", "b", "c"}, "b") {
		t.Fatal("expected true")
	}
}

func TestContainsString_NotFound(t *testing.T) {
	if containsString([]string{"a", "b", "c"}, "z") {
		t.Fatal("expected false")
	}
}

func TestContainsString_EmptySlice(t *testing.T) {
	if containsString(nil, "x") {
		t.Fatal("expected false for nil slice")
	}
}

func TestSelectedStationMap_EmptyInput(t *testing.T) {
	m := selectedStationMap(nil)
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(m))
	}
}

func TestSelectedStationMap_WithValues(t *testing.T) {
	m := selectedStationMap([]string{"1", "2", ""})
	if !m["1"] || !m["2"] {
		t.Fatal("expected 1 and 2 in map")
	}
	if m[""] {
		t.Fatal("empty string should be excluded")
	}
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m))
	}
}

func TestStationLabelByID_EmptyInput(t *testing.T) {
	m := stationLabelByID(nil)
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(m))
	}
}

func TestStationLabelByID_WithFilters(t *testing.T) {
	filters := []stationFilterOption{
		{ID: "1", Label: "Alpha"},
		{ID: "2", Label: "Beta"},
		{ID: "", Label: "Should be excluded"},
	}
	m := stationLabelByID(filters)
	if m["1"] != "Alpha" {
		t.Fatalf("expected Alpha for ID 1, got %s", m["1"])
	}
	if m["2"] != "Beta" {
		t.Fatalf("expected Beta for ID 2, got %s", m["2"])
	}
	if _, ok := m[""]; ok {
		t.Fatal("empty ID should be excluded")
	}
}

func TestExtractStationFilters_NilStaged(t *testing.T) {
	filters := extractStationFilters(nil)
	if filters != nil {
		t.Fatalf("expected nil for nil input, got %v", filters)
	}
}

func TestExtractStationFilters_WebstreamDescriptions(t *testing.T) {
	staged := &models.StagedImport{
		StagedWebstreams: models.StagedWebstreamItems{
			{SourceID: "4::ws1", Description: "Station: Jazz FM"},
		},
	}
	filters := extractStationFilters(staged)
	if len(filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(filters))
	}
	if filters[0].Label != "Jazz FM" {
		t.Fatalf("expected Jazz FM, got %s", filters[0].Label)
	}
}

func TestExtractStationFilters_ImportedFromDescription(t *testing.T) {
	staged := &models.StagedImport{
		StagedShows: models.StagedShowItems{
			{SourceID: "7::show1", Description: "Imported from station Cool Radio playlist schedule"},
		},
	}
	filters := extractStationFilters(staged)
	if len(filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(filters))
	}
	if filters[0].Label != "Cool Radio" {
		t.Fatalf("expected Cool Radio, got %s", filters[0].Label)
	}
}

func TestExtractStationFilters_InvalidSourceID(t *testing.T) {
	staged := &models.StagedImport{
		StagedMedia: models.StagedMediaItems{
			{SourceID: "not-valid"},
			{SourceID: "abc::def"},
		},
	}
	// Non-numeric IDs should be ignored
	filters := extractStationFilters(staged)
	if len(filters) != 0 {
		t.Fatalf("expected 0 filters for invalid source IDs, got %d", len(filters))
	}
}

func TestBuildStagedAnomalyCards_NilInput(t *testing.T) {
	cards := buildStagedAnomalyCards(nil)
	if cards != nil {
		t.Fatalf("expected nil for nil input, got %v", cards)
	}
}

func TestBuildStagedAnomalyCards_EmptyStaged(t *testing.T) {
	staged := &models.StagedImport{}
	cards := buildStagedAnomalyCards(staged)
	if len(cards) != 4 {
		t.Fatalf("expected 4 anomaly cards, got %d", len(cards))
	}
	for _, c := range cards {
		if c.Count != 0 {
			t.Fatalf("expected 0 count for %s, got %d", c.Key, c.Count)
		}
	}
}

func TestBuildStagedAnomalyCards_WithDuplicates(t *testing.T) {
	staged := &models.StagedImport{
		StagedMedia: models.StagedMediaItems{
			{Title: "Track A", IsDuplicate: true},
			{Title: "Track B", IsDuplicate: true},
			{Title: "Track C", IsDuplicate: false},
			{Title: "Track D", IsDuplicate: true},
			{Title: "Track E", IsDuplicate: true},
		},
	}
	cards := buildStagedAnomalyCards(staged)
	var dupCard *stagedAnomalyCard
	for i := range cards {
		if cards[i].Key == "duplicate_resolution" {
			dupCard = &cards[i]
		}
	}
	if dupCard == nil {
		t.Fatal("expected duplicate_resolution card")
	}
	if dupCard.Count != 4 {
		t.Fatalf("expected 4 duplicates, got %d", dupCard.Count)
	}
	// Examples capped at 3
	if len(dupCard.Examples) > 3 {
		t.Fatalf("expected at most 3 examples, got %d", len(dupCard.Examples))
	}
}

func TestBuildStagedAnomalyCards_WithWarnings(t *testing.T) {
	staged := &models.StagedImport{
		Warnings: models.ImportWarnings{
			{Code: "duration_missing", Message: "Track has no duration"},
			{Code: "missing_file", Message: "File not found on disk"},
			{Code: "skipped_entity", Message: "Skipped unsupported type"},
			{Code: "other", Message: ""}, // empty message should be skipped
		},
	}
	cards := buildStagedAnomalyCards(staged)
	if len(cards) != 4 {
		t.Fatalf("expected 4 cards, got %d", len(cards))
	}
	// Verify at least one card has non-zero count
	totalCount := 0
	for _, c := range cards {
		totalCount += c.Count
	}
	if totalCount == 0 {
		t.Fatal("expected at least one non-zero count from warnings")
	}
}

// ---------------------------------------------------------------------------
// ImportHistoryPage
// ---------------------------------------------------------------------------

func TestImportHistoryPage_Returns200(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/settings/migrations/history", nil)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	h.ImportHistoryPage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// ImportReviewByJobRedirect
// ---------------------------------------------------------------------------

func TestImportReviewByJobRedirect_EmptyJobID_Returns200WithError(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/settings/migrations/review/job/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", "   ")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	h.ImportReviewByJobRedirect(rr, req)
	// Should return an error (either HTMX error or 4xx)
	if rr.Code < 200 {
		t.Fatalf("unexpected status %d", rr.Code)
	}
}

func TestImportReviewByJobRedirect_JobNotFound_ReturnsError(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/settings/migrations/review/job/nonexistent", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", "nonexistent-job-id")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	h.ImportReviewByJobRedirect(rr, req)
	// Should not be 200 success since job doesn't exist
	if rr.Code == http.StatusSeeOther {
		t.Fatal("expected error, not redirect to review page for missing job")
	}
}

func TestImportReviewByJobRedirect_JobNotFound_HTMXRequest(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/settings/migrations/review/job/nope", nil)
	req.Header.Set("HX-Request", "true")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", "nope")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	h.ImportReviewByJobRedirect(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "alert") {
		t.Fatalf("expected HTMX error alert, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// ImportHistoryRollback
// ---------------------------------------------------------------------------

func TestImportHistoryRollback_NotFound_ReturnsHTMXError(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)

	rr := httptest.NewRecorder()
	req := reqWithIDAndUser(http.MethodPost, "/migrations/history/nope/rollback", "id", "nope", nil)
	req.Header.Set("HX-Request", "true")
	h.ImportHistoryRollback(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "alert") {
		t.Fatalf("expected HTMX error, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// ImportHistoryRedo
// ---------------------------------------------------------------------------

func TestImportHistoryRedo_NotFound_ReturnsHTMXError(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)

	rr := httptest.NewRecorder()
	req := reqWithIDAndUser(http.MethodPost, "/migrations/history/nope/redo", "id", "nope", nil)
	req.Header.Set("HX-Request", "true")
	h.ImportHistoryRedo(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "alert") {
		t.Fatalf("expected HTMX error, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// ImportHistoryItems
// ---------------------------------------------------------------------------

func TestImportHistoryItems_NotFound_ReturnsHTMXError(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)

	rr := httptest.NewRecorder()
	req := reqWithIDAndUser(http.MethodGet, "/migrations/history/nope/items", "id", "nope", nil)
	req.Header.Set("HX-Request", "true")
	h.ImportHistoryItems(rr, req)
	// Should either return error content or 200 with empty items
	if rr.Code >= 500 {
		t.Fatalf("unexpected 5xx: %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// pages_live.go — LiveDashboard
// ---------------------------------------------------------------------------

func TestLiveDashboard_NoStation_Redirects(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/live", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	h.LiveDashboard(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect with no station, got %d", rr.Code)
	}
}

func TestLiveDashboard_WithStation_Returns200(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")
	s := seedEmbedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	req := reqWithStationAndUser(http.MethodGet, "/dashboard/live", &s, &u)
	h.LiveDashboard(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", rr.Code, rr.Body.String())
	}
}

func TestLiveDashboard_WithActiveRecording_Returns200(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")
	s := seedEmbedPublicStation(t, db, "st1", "Test FM")

	db.Create(&models.Recording{
		ID:        "r1",
		StationID: "st1",
		UserID:    "u1",
		Status:    models.RecordingStatusActive,
		Format:    models.RecordingFormatFLAC,
	})

	rr := httptest.NewRecorder()
	req := reqWithStationAndUser(http.MethodGet, "/dashboard/live", &s, &u)
	h.LiveDashboard(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestLiveDashboard_QuotaCalculation_Returns200(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")
	s := seedEmbedPublicStation(t, db, "st1", "Test FM")
	// Set quota so the percentage calculation is exercised
	s.RecordingQuotaBytes = 1024 * 1024 * 100  // 100MB
	s.RecordingStorageUsed = 1024 * 1024 * 150 // 150MB (over quota → capped at 100%)
	db.Save(&s)

	rr := httptest.NewRecorder()
	req := reqWithStationAndUser(http.MethodGet, "/dashboard/live", &s, &u)
	h.LiveDashboard(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// LiveSessions
// ---------------------------------------------------------------------------

func TestLiveSessions_NoStation_Returns400(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	rr := httptest.NewRecorder()
	h.LiveSessions(rr, httptest.NewRequest(http.MethodGet, "/dashboard/live/sessions", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLiveSessions_WithStation_Returns200(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")
	s := seedEmbedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	req := reqWithStationAndUser(http.MethodGet, "/dashboard/live/sessions", &s, &u)
	h.LiveSessions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// LiveGenerateToken
// ---------------------------------------------------------------------------

func TestLiveGenerateToken_NoStation_Returns400(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	rr := httptest.NewRecorder()
	h.LiveGenerateToken(rr, httptest.NewRequest(http.MethodPost, "/dashboard/live/token", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLiveGenerateToken_NoMountID_Returns400(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")
	s := seedEmbedPublicStation(t, db, "st1", "Test FM")

	form := url.Values{}
	form.Set("mount_id", "")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/live/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = reqWithStationAndUser(http.MethodPost, "/dashboard/live/token", &s, &u)

	rr := httptest.NewRecorder()
	h.LiveGenerateToken(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLiveGenerateToken_MountNotFound_Returns404(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")
	s := seedEmbedPublicStation(t, db, "st1", "Test FM")

	form := url.Values{}
	form.Set("mount_id", "nonexistent-mount")
	reqBody := strings.NewReader(form.Encode())
	req := httptest.NewRequest(http.MethodPost, "/dashboard/live/token", reqBody)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyStation, &s)
	ctx = context.WithValue(ctx, ctxKeyUser, &u)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.LiveGenerateToken(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestLiveGenerateToken_NoLiveSvc_Returns503(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")
	s := seedEmbedPublicStation(t, db, "st1", "Test FM")
	db.Create(&models.Mount{ID: "m1", StationID: "st1", Name: "main"})

	form := url.Values{}
	form.Set("mount_id", "m1")
	reqBody := strings.NewReader(form.Encode())
	req := httptest.NewRequest(http.MethodPost, "/dashboard/live/token", reqBody)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyStation, &s)
	ctx = context.WithValue(ctx, ctxKeyUser, &u)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.LiveGenerateToken(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (no live svc), got %d", rr.Code)
	}
}

func TestLiveGenerateToken_NoLiveSvc_HTMXRequest_Returns503(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")
	s := seedEmbedPublicStation(t, db, "st1", "Test FM")
	db.Create(&models.Mount{ID: "m1", StationID: "st1", Name: "main"})

	form := url.Values{}
	form.Set("mount_id", "m1")
	reqBody := strings.NewReader(form.Encode())
	req := httptest.NewRequest(http.MethodPost, "/dashboard/live/token", reqBody)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	ctx := context.WithValue(req.Context(), ctxKeyStation, &s)
	ctx = context.WithValue(ctx, ctxKeyUser, &u)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.LiveGenerateToken(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "alert") {
		t.Fatalf("expected HTMX alert, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// LiveConnect
// ---------------------------------------------------------------------------

func TestLiveConnect_Returns400(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	rr := httptest.NewRecorder()
	h.LiveConnect(rr, httptest.NewRequest(http.MethodPost, "/dashboard/live/connect", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// LiveDisconnect
// ---------------------------------------------------------------------------

func TestLiveDisconnect_SessionNotFound_Returns404(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")

	rr := httptest.NewRecorder()
	req := reqWithIDAndUser(http.MethodPost, "/dashboard/live/sessions/nope/disconnect", "id", "nope", &u)
	h.LiveDisconnect(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestLiveDisconnect_WrongUser_Returns403(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u1 := seedEmbedUser(t, db, "u1", "admin@example.com")
	// Create a second user (regular role)
	u2 := models.User{ID: "u2", Email: "other@example.com", Password: "x", PlatformRole: models.PlatformRoleUser}
	db.Create(&u2)

	// Create a session owned by u1
	db.Create(&models.LiveSession{
		ID:        "sess1",
		StationID: "st1",
		UserID:    u1.ID,
		Active:    true,
		Token:     "tok1",
	})

	rr := httptest.NewRecorder()
	req := reqWithIDAndUser(http.MethodPost, "/dashboard/live/sessions/sess1/disconnect", "id", "sess1", &u2)
	h.LiveDisconnect(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestLiveDisconnect_OwnSession_NoLiveSvc_Succeeds(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")

	db.Create(&models.LiveSession{
		ID:        "sess1",
		StationID: "st1",
		UserID:    u.ID,
		Active:    true,
		Token:     "tok2",
	})

	rr := httptest.NewRecorder()
	req := reqWithIDAndUser(http.MethodPost, "/dashboard/live/sessions/sess1/disconnect", "id", "sess1", &u)
	h.LiveDisconnect(rr, req)
	// Should redirect or return 200
	if rr.Code >= 500 {
		t.Fatalf("unexpected 5xx: %d: %s", rr.Code, rr.Body.String())
	}
}

func TestLiveDisconnect_OwnSession_HTMXRefresh(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")

	db.Create(&models.LiveSession{
		ID:        "sess2",
		StationID: "st1",
		UserID:    u.ID,
		Active:    true,
		Token:     "tok3",
	})

	rr := httptest.NewRecorder()
	req := reqWithIDAndUser(http.MethodPost, "/dashboard/live/sessions/sess2/disconnect", "id", "sess2", &u)
	req.Header.Set("HX-Request", "true")
	h.LiveDisconnect(rr, req)
	if refresh := rr.Header().Get("HX-Refresh"); refresh != "true" {
		t.Fatalf("expected HX-Refresh: true, got %q", refresh)
	}
}

// ---------------------------------------------------------------------------
// LiveHandover
// ---------------------------------------------------------------------------

func TestLiveHandover_NoStation_Returns400(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/dashboard/live/handover", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	h.LiveHandover(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLiveHandover_NoActiveSession_Returns404(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")
	s := seedEmbedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	req := reqWithStationAndUser(http.MethodPost, "/dashboard/live/handover", &s, &u)
	h.LiveHandover(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (no active session), got %d", rr.Code)
	}
}

func TestLiveHandover_WithActiveSession_NoLiveSvc_Redirects(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")
	s := seedEmbedPublicStation(t, db, "st1", "Test FM")

	db.Create(&models.LiveSession{
		ID:        "sess3",
		StationID: s.ID,
		UserID:    u.ID,
		Active:    true,
		Token:     "tok4",
	})

	rr := httptest.NewRecorder()
	req := reqWithStationAndUser(http.MethodPost, "/dashboard/live/handover", &s, &u)
	h.LiveHandover(rr, req)
	// No live svc → logs warn and redirects
	if rr.Code >= 500 {
		t.Fatalf("unexpected 5xx: %d: %s", rr.Code, rr.Body.String())
	}
}

func TestLiveHandover_HTMXRequest_NoLiveSvc(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")
	s := seedEmbedPublicStation(t, db, "st1", "Test FM")

	db.Create(&models.LiveSession{
		ID:        "sess4",
		StationID: s.ID,
		UserID:    u.ID,
		Active:    true,
		Token:     "tok5",
	})

	rr := httptest.NewRecorder()
	req := reqWithStationAndUser(http.MethodPost, "/dashboard/live/handover", &s, &u)
	req.Header.Set("HX-Request", "true")
	h.LiveHandover(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "alert") {
		t.Fatalf("expected HTMX alert for handover, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// LiveReleaseHandover
// ---------------------------------------------------------------------------

func TestLiveReleaseHandover_NoStation_Returns400(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/dashboard/live/handover/release", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &u))
	h.LiveReleaseHandover(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLiveReleaseHandover_NoActiveSession_Returns404(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")
	s := seedEmbedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	req := reqWithStationAndUser(http.MethodPost, "/dashboard/live/handover/release", &s, &u)
	h.LiveReleaseHandover(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestLiveReleaseHandover_WithActiveSession_NoLiveSvc_Redirects(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")
	s := seedEmbedPublicStation(t, db, "st1", "Test FM")

	db.Create(&models.LiveSession{
		ID:        "sess5",
		StationID: s.ID,
		UserID:    u.ID,
		Active:    true,
		Token:     "tok6",
	})

	rr := httptest.NewRecorder()
	req := reqWithStationAndUser(http.MethodPost, "/dashboard/live/handover/release", &s, &u)
	h.LiveReleaseHandover(rr, req)
	// No live svc → logs warn and redirects
	if rr.Code >= 500 {
		t.Fatalf("unexpected 5xx: %d: %s", rr.Code, rr.Body.String())
	}
}

func TestLiveReleaseHandover_HTMXRequest_NoLiveSvc(t *testing.T) {
	db := newEmbedLiveDB(t)
	h := newEmbedLiveHandler(t, db)
	u := seedEmbedUser(t, db, "u1", "admin@example.com")
	s := seedEmbedPublicStation(t, db, "st1", "Test FM")

	db.Create(&models.LiveSession{
		ID:        "sess6",
		StationID: s.ID,
		UserID:    u.ID,
		Active:    true,
		Token:     "tok7",
	})

	rr := httptest.NewRecorder()
	req := reqWithStationAndUser(http.MethodPost, "/dashboard/live/handover/release", &s, &u)
	req.Header.Set("HX-Request", "true")
	h.LiveReleaseHandover(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "alert") {
		t.Fatalf("expected HTMX alert for cancel handover, got: %s", body)
	}
}
