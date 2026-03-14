/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

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

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newAnalyticsTest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Station{},
		&models.Mount{},
		&models.PlayHistory{},
		&models.LiveSession{},
		&models.User{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{db: db, bus: events.NewBus(), logger: zerolog.Nop()}, db
}

func TestAPIHandlers_Health(t *testing.T) {
	a, _ := newAnalyticsTest(t)

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	a.handleHealth(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("health: got %d, want 200", rr.Code)
	}
	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["status"] != "ok" {
		t.Fatalf("expected status=ok, got %q", resp["status"])
	}
}

func TestAPIHandlers_PublicStations(t *testing.T) {
	a, db := newAnalyticsTest(t)

	// Empty list
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handlePublicStations(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("public stations empty: got %d, want 200", rr.Code)
	}
	var result []any
	json.NewDecoder(rr.Body).Decode(&result) //nolint:errcheck
	if len(result) != 0 {
		t.Fatalf("expected 0 public stations, got %d", len(result))
	}

	// Seed a public+active+approved station with a mount
	station := models.Station{
		ID:       "st-pub",
		Name:     "Public Station",
		Active:   true,
		Public:   true,
		Approved: true,
	}
	db.Create(&station) //nolint:errcheck
	mount := models.Mount{
		ID:        "mt-pub",
		StationID: station.ID,
		Name:      "main",
		Format:    "mp3",
		Bitrate:   128,
	}
	db.Create(&mount) //nolint:errcheck

	// Seed a private station (should not appear)
	db.Create(&models.Station{ID: "st-priv", Name: "Private", Active: true, Public: false, Approved: true}) //nolint:errcheck

	req = httptest.NewRequest("GET", "/", nil)
	rr = httptest.NewRecorder()
	a.handlePublicStations(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("public stations: got %d, want 200", rr.Code)
	}
	json.NewDecoder(rr.Body).Decode(&result) //nolint:errcheck
	if len(result) != 1 {
		t.Fatalf("expected 1 public station, got %d", len(result))
	}
	stMap, _ := result[0].(map[string]any)
	mounts, _ := stMap["mounts"].([]any)
	if len(mounts) != 1 {
		t.Fatalf("expected 1 mount in response, got %d", len(mounts))
	}
}

func TestAPIHandlers_AnalyticsNowPlaying(t *testing.T) {
	a, db := newAnalyticsTest(t)

	// Missing station_id → 400
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsNowPlaying(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}

	// No history → status=idle
	req = httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr = httptest.NewRecorder()
	a.handleAnalyticsNowPlaying(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("no history: got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["status"] != "idle" {
		t.Fatalf("expected status=idle, got %q", resp["status"])
	}

	// Seed play history
	history := models.PlayHistory{
		ID:        "ph-1",
		StationID: "s1",
		MountID:   "mt-1",
		Artist:    "Miles Davis",
		Title:     "So What",
		StartedAt: time.Now().Add(-5 * time.Minute),
		// EndedAt zero = still playing
	}
	db.Create(&history) //nolint:errcheck

	req = httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr = httptest.NewRecorder()
	a.handleAnalyticsNowPlaying(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("with history: got %d, want 200", rr.Code)
	}
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["status"] != "playing" {
		t.Fatalf("expected status=playing, got %q", resp["status"])
	}
	if resp["artist"] != "Miles Davis" {
		t.Fatalf("expected artist=Miles Davis, got %q", resp["artist"])
	}
}

func TestAPIHandlers_AnalyticsSpins(t *testing.T) {
	a, db := newAnalyticsTest(t)

	// Missing station_id → 400
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsSpins(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}

	// Empty spins
	req = httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr = httptest.NewRecorder()
	a.handleAnalyticsSpins(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("empty spins: got %d, want 200", rr.Code)
	}
	var spins []any
	json.NewDecoder(rr.Body).Decode(&spins) //nolint:errcheck
	if len(spins) != 0 {
		t.Fatalf("expected 0 spins, got %d", len(spins))
	}

	// Seed play history
	for i := 0; i < 3; i++ {
		db.Create(&models.PlayHistory{ //nolint:errcheck
			ID:        "ph-spin-" + string(rune('a'+i)),
			StationID: "s1",
			Artist:    "Miles Davis",
			Title:     "So What",
			StartedAt: time.Now().Add(-time.Duration(i) * time.Hour),
		})
	}
	db.Create(&models.PlayHistory{ //nolint:errcheck
		ID:        "ph-spin-d",
		StationID: "s1",
		Artist:    "Coltrane",
		Title:     "Giant Steps",
		StartedAt: time.Now().Add(-2 * time.Hour),
	})

	req = httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr = httptest.NewRecorder()
	a.handleAnalyticsSpins(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("spins: got %d, want 200", rr.Code)
	}
	json.NewDecoder(rr.Body).Decode(&spins) //nolint:errcheck
	if len(spins) != 2 {
		t.Fatalf("expected 2 unique spin entries, got %d", len(spins))
	}
	// First entry should be Miles Davis (count=3)
	first, _ := spins[0].(map[string]any)
	if first["artist"] != "Miles Davis" {
		t.Fatalf("expected top artist=Miles Davis, got %v", first["artist"])
	}
}

func TestAPIHandlers_AnalyticsListeners(t *testing.T) {
	a, _ := newAnalyticsTest(t)

	// With nil broadcast → returns 0 total, empty mounts
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleAnalyticsListeners(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("listeners: got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	total, _ := resp["total"].(float64)
	if total != 0 {
		t.Fatalf("expected total=0, got %v", total)
	}
}

func TestSplitNowPlayingArtistTitle(t *testing.T) {
	cases := []struct {
		artist, title string
		wantA, wantT  string
	}{
		// Artist provided: return as-is
		{"Miles Davis", "So What", "Miles Davis", "So What"},
		// Empty artist, no separator: returns "" and full title
		{"", "Just a Title", "", "Just a Title"},
		// Combined in title
		{"", "Coltrane - Giant Steps", "Coltrane", "Giant Steps"},
		// Em dash separator
		{"", "DJ — Live Mix", "DJ", "Live Mix"},
		// En dash separator
		{"", "Miles – So What", "Miles", "So What"},
		// Left part empty after split (should not split)
		{"", " - Track", "", "- Track"},
		// Both empty
		{"", "", "", ""},
	}

	for _, tc := range cases {
		a, t2 := splitNowPlayingArtistTitle(tc.artist, tc.title)
		if a != tc.wantA || t2 != tc.wantT {
			t.Errorf("splitNowPlayingArtistTitle(%q, %q) = (%q, %q), want (%q, %q)",
				tc.artist, tc.title, a, t2, tc.wantA, tc.wantT)
		}
	}
}
