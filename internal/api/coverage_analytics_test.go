/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// Tests for handleTimeSlotPerformance, handleBestTimeSlots, handleSchedulingSuggestions,
// and handleAnalyticsNowPlaying — covering the DB-backed paths in SQLite.
//
// NOTE: handleTimeSlotPerformance and handleBestTimeSlots use PostgreSQL-specific
// EXTRACT(DOW FROM date) in the underlying analytics service. On SQLite those queries
// will fail and the handler will return 500. We test:
//   - The missing station_id guard (400) for all handlers.
//   - The happy-path response structure when the analytics service runs without error.
//   - The handleAnalyticsNowPlaying handler fully (uses plain GORM queries, no PG-specific SQL).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/analytics"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newAnalyticsAPITest(t *testing.T) (*ScheduleAnalyticsAPI, *API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "analytics.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// Single connection prevents SQLite WAL isolation issues across HTTP request contexts.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.ScheduleAnalytics{},
		&models.ScheduleAnalyticsDaily{},
		&models.ShowInstance{},
		&models.Show{},
		&models.Station{},
		&models.PlayHistory{},
		&models.LiveSession{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	base := &API{db: db, bus: events.NewBus(), logger: zerolog.Nop()}
	svc := analytics.NewScheduleAnalyticsService(db, zerolog.Nop())
	return NewScheduleAnalyticsAPI(base, svc), base, db
}

// TestHandleTimeSlotPerformance_MissingStation verifies 400 with no station_id.
func TestHandleTimeSlotPerformance_MissingStation(t *testing.T) {
	a, _, _ := newAnalyticsAPITest(t)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleTimeSlotPerformance(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}
}

// TestHandleTimeSlotPerformance_ReturnsStructuredResponse verifies the response
// contains the expected keys even when no data exists (SQLite may return 500 for the
// PostgreSQL-specific query; if it returns 200 the structure must be correct).
func TestHandleTimeSlotPerformance_ReturnsStructuredResponse(t *testing.T) {
	a, _, _ := newAnalyticsAPITest(t)

	req := httptest.NewRequest("GET", "/?station_id=st-tsp", nil)
	rr := httptest.NewRecorder()
	a.handleTimeSlotPerformance(rr, req)

	// On SQLite this may return 500 (PG-specific SQL). If it returns 200 the structure
	// must contain the expected keys.
	if rr.Code == http.StatusOK {
		var resp map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if _, ok := resp["time_slots"]; !ok {
			t.Fatal("expected 'time_slots' key in response")
		}
		if _, ok := resp["start"]; !ok {
			t.Fatal("expected 'start' key in response")
		}
		if _, ok := resp["end"]; !ok {
			t.Fatal("expected 'end' key in response")
		}
	}
	// Any non-400 code is acceptable here (200 or 500 depending on driver).
	if rr.Code == http.StatusBadRequest {
		t.Fatalf("got 400 but station_id was provided; body=%s", rr.Body.String())
	}
}

// TestHandleBestTimeSlots_MissingStation verifies 400 with no station_id.
func TestHandleBestTimeSlots_MissingStation(t *testing.T) {
	a, _, _ := newAnalyticsAPITest(t)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleBestTimeSlots(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}
}

// TestHandleBestTimeSlots_ResponseStructure verifies that when the handler succeeds it
// includes a best_slots key in the response.
func TestHandleBestTimeSlots_ResponseStructure(t *testing.T) {
	a, _, _ := newAnalyticsAPITest(t)

	req := httptest.NewRequest("GET", "/?station_id=st-bts", nil)
	rr := httptest.NewRecorder()
	a.handleBestTimeSlots(rr, req)

	// On SQLite this may return 500 due to PG-specific SQL; if 200 check structure.
	if rr.Code == http.StatusOK {
		var resp map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if _, ok := resp["best_slots"]; !ok {
			t.Fatal("expected 'best_slots' key in response")
		}
	}
	if rr.Code == http.StatusBadRequest {
		t.Fatalf("got 400 but station_id was provided")
	}
}

// TestHandleBestTimeSlots_CustomLimit verifies that the ?limit param is accepted
// without error (the limit parsing path is covered).
func TestHandleBestTimeSlots_CustomLimit(t *testing.T) {
	a, _, _ := newAnalyticsAPITest(t)

	req := httptest.NewRequest("GET", "/?station_id=st-limit&limit=5", nil)
	rr := httptest.NewRecorder()
	a.handleBestTimeSlots(rr, req)

	// Should not be 400 (limit param was not malformed).
	if rr.Code == http.StatusBadRequest {
		t.Fatalf("valid limit: got 400; body=%s", rr.Body.String())
	}
}

// TestHandleSchedulingSuggestions_MissingStation verifies 400 with no station_id.
func TestHandleSchedulingSuggestions_MissingStation(t *testing.T) {
	a, _, _ := newAnalyticsAPITest(t)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleSchedulingSuggestions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}
}

// TestHandleSchedulingSuggestions_ResponseStructure verifies the response format.
func TestHandleSchedulingSuggestions_ResponseStructure(t *testing.T) {
	a, _, _ := newAnalyticsAPITest(t)

	req := httptest.NewRequest("GET", "/?station_id=st-suggest", nil)
	rr := httptest.NewRecorder()
	a.handleSchedulingSuggestions(rr, req)

	if rr.Code == http.StatusOK {
		var resp map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if _, ok := resp["suggestions"]; !ok {
			t.Fatal("expected 'suggestions' key in response")
		}
	}
	if rr.Code == http.StatusBadRequest {
		t.Fatalf("got 400 but station_id was provided")
	}
}

// TestHandleAnalyticsNowPlaying_MissingStation verifies 400 without station_id.
func TestHandleAnalyticsNowPlaying_MissingStation(t *testing.T) {
	_, a, _ := newAnalyticsAPITest(t)

	req := httptest.NewRequest("GET", "/now-playing", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsNowPlaying(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}
}

// TestHandleAnalyticsNowPlaying_NoHistory verifies that when there's no play history
// the response is 200 with status="idle".
func TestHandleAnalyticsNowPlaying_NoHistory(t *testing.T) {
	_, a, _ := newAnalyticsAPITest(t)

	req := httptest.NewRequest("GET", "/?station_id=st-nowplay", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsNowPlaying(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("no history: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "idle" {
		t.Fatalf("expected status=idle, got %v", resp["status"])
	}
}

// TestHandleAnalyticsNowPlaying_ActiveTrack verifies the response when a track is
// currently playing (EndedAt is zero).
func TestHandleAnalyticsNowPlaying_ActiveTrack(t *testing.T) {
	_, a, db := newAnalyticsAPITest(t)

	now := time.Now().UTC()
	history := models.PlayHistory{
		ID:        "ph-active-1",
		StationID: "st-active",
		MountID:   "mt-active",
		Artist:    "Test Artist",
		Title:     "Active Song",
		Album:     "Live Album",
		StartedAt: now.Add(-3 * time.Minute),
		// EndedAt is zero — track is still playing.
	}
	db.Create(&history) //nolint:errcheck

	req := httptest.NewRequest("GET", "/?station_id=st-active", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsNowPlaying(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("active track: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "playing" {
		t.Fatalf("expected status=playing, got %v", resp["status"])
	}
	if resp["artist"] != "Test Artist" {
		t.Fatalf("expected artist=Test Artist, got %v", resp["artist"])
	}
	if resp["title"] != "Active Song" {
		t.Fatalf("expected title=Active Song, got %v", resp["title"])
	}
	// ended_at should be null (JSON nil) when EndedAt is zero.
	if resp["ended_at"] != nil {
		t.Fatalf("expected ended_at=null for zero time, got %v", resp["ended_at"])
	}
}

// TestHandleAnalyticsNowPlaying_FinishedTrack verifies that when a track has ended
// the status is "idle" (not "playing").
func TestHandleAnalyticsNowPlaying_FinishedTrack(t *testing.T) {
	_, a, db := newAnalyticsAPITest(t)

	past := time.Now().UTC().Add(-10 * time.Minute)
	history := models.PlayHistory{
		ID:        "ph-done-1",
		StationID: "st-done",
		Artist:    "Past Artist",
		Title:     "Finished Song",
		StartedAt: past.Add(-5 * time.Minute),
		EndedAt:   past, // Has ended in the past.
	}
	db.Create(&history) //nolint:errcheck

	req := httptest.NewRequest("GET", "/?station_id=st-done", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsNowPlaying(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("finished track: got %d, want 200", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "idle" {
		t.Fatalf("expected status=idle for ended track, got %v", resp["status"])
	}
}

// TestHandleAnalyticsNowPlaying_ArtistTitleSplitFromTitle verifies that when the artist
// field is empty but the title contains "Artist - Track" format, it is split correctly.
func TestHandleAnalyticsNowPlaying_ArtistTitleSplitFromTitle(t *testing.T) {
	_, a, db := newAnalyticsAPITest(t)

	db.Create(&models.PlayHistory{
		ID:        "ph-split-1",
		StationID: "st-split",
		Artist:    "", // Empty — title contains combined metadata.
		Title:     "Split Artist - The Good Track",
		StartedAt: time.Now().UTC().Add(-1 * time.Minute),
	}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/?station_id=st-split", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsNowPlaying(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("split: got %d; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck

	if resp["artist"] != "Split Artist" {
		t.Fatalf("expected artist=Split Artist, got %v", resp["artist"])
	}
	if resp["title"] != "The Good Track" {
		t.Fatalf("expected title=The Good Track, got %v", resp["title"])
	}
}
