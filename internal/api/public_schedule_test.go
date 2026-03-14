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
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newPublicScheduleTest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Station{},
		&models.Show{},
		&models.ShowInstance{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{db: db, logger: zerolog.Nop()}, db
}

func seedPublicStation(t *testing.T, db *gorm.DB, id, name string) models.Station {
	t.Helper()
	station := models.Station{ID: id, Name: name, Public: true}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("seed public station: %v", err)
	}
	return station
}

func seedShowForPublic(t *testing.T, db *gorm.DB, id, stationID, name string) models.Show {
	t.Helper()
	show := models.Show{
		ID: id, StationID: stationID, Name: name,
		DTStart: time.Now(), Timezone: "UTC",
	}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	return show
}

func seedShowInstanceForPublic(t *testing.T, db *gorm.DB, id string, show models.Show, startsAt, endsAt time.Time) {
	t.Helper()
	inst := models.ShowInstance{
		ID: id, ShowID: show.ID, StationID: show.StationID,
		StartsAt: startsAt, EndsAt: endsAt,
		Status: models.ShowInstanceScheduled,
	}
	if err := db.Create(&inst).Error; err != nil {
		t.Fatalf("seed show instance: %v", err)
	}
}

func TestPublicScheduleAPI_PublicSchedule(t *testing.T) {
	a, db := newPublicScheduleTest(t)

	// Missing station_id → 400
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handlePublicSchedule(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}

	// Non-existent station → 404
	req = httptest.NewRequest("GET", "/?station_id=nonexistent", nil)
	rr = httptest.NewRecorder()
	a.handlePublicSchedule(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("nonexistent station: got %d, want 404", rr.Code)
	}

	// Private station → 404
	db.Create(&models.Station{ID: "st-priv", Name: "Private", Public: false}) //nolint:errcheck
	req = httptest.NewRequest("GET", "/?station_id=st-priv", nil)
	rr = httptest.NewRecorder()
	a.handlePublicSchedule(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("private station: got %d, want 404", rr.Code)
	}

	// Public station, empty schedule
	station := seedPublicStation(t, db, "st-pub", "Public Radio")
	req = httptest.NewRequest("GET", "/?station_id=st-pub", nil)
	rr = httptest.NewRecorder()
	a.handlePublicSchedule(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("public schedule empty: got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	schedule, _ := resp["schedule"].([]any)
	if len(schedule) != 0 {
		t.Fatalf("expected 0 scheduled instances, got %d", len(schedule))
	}

	// Seed a show and instance in the default range (next 7 days)
	show := seedShowForPublic(t, db, "show-1", station.ID, "Morning Jazz")
	now := time.Now()
	seedShowInstanceForPublic(t, db, "inst-1", show, now.Add(time.Hour), now.Add(2*time.Hour))

	req = httptest.NewRequest("GET", "/?station_id=st-pub", nil)
	rr = httptest.NewRecorder()
	a.handlePublicSchedule(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("public schedule with instance: got %d, want 200", rr.Code)
	}
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	schedule, _ = resp["schedule"].([]any)
	if len(schedule) != 1 {
		t.Fatalf("expected 1 scheduled instance, got %d", len(schedule))
	}
}

func TestPublicScheduleAPI_NowPlaying(t *testing.T) {
	a, db := newPublicScheduleTest(t)

	// Missing station_id → 400
	req := httptest.NewRequest("GET", "/now", nil)
	rr := httptest.NewRecorder()
	a.handlePublicNowPlaying(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}

	// Public station, no current show → 200 no current/next
	station := seedPublicStation(t, db, "st-now", "Now Playing Radio")
	req = httptest.NewRequest("GET", "/?station_id=st-now", nil)
	rr = httptest.NewRecorder()
	a.handlePublicNowPlaying(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("now playing empty: got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["current"]; ok {
		t.Fatal("expected no current show")
	}

	// Seed a currently-playing show and next show
	show := seedShowForPublic(t, db, "show-now", station.ID, "Live Jazz")
	now := time.Now()
	seedShowInstanceForPublic(t, db, "inst-now", show, now.Add(-2*time.Hour), now.Add(2*time.Hour))

	show2 := seedShowForPublic(t, db, "show-next", station.ID, "Evening News")
	seedShowInstanceForPublic(t, db, "inst-next", show2, now.Add(3*time.Hour), now.Add(4*time.Hour))

	req = httptest.NewRequest("GET", "/?station_id=st-now", nil)
	rr = httptest.NewRecorder()
	a.handlePublicNowPlaying(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("now playing with show: got %d, want 200", rr.Code)
	}
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["current"]; !ok {
		t.Fatal("expected current show in response")
	}
	if _, ok := resp["next"]; !ok {
		t.Fatal("expected next show in response")
	}
}

func TestPublicScheduleAPI_ICal(t *testing.T) {
	a, db := newPublicScheduleTest(t)

	// Missing station_id → 400
	req := httptest.NewRequest("GET", "/ical", nil)
	rr := httptest.NewRecorder()
	a.handlePublicScheduleICal(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}

	// Public station with a show instance
	station := seedPublicStation(t, db, "st-ical", "iCal Station")
	show := seedShowForPublic(t, db, "show-ical", station.ID, "Tech Talk")
	now := time.Now()
	seedShowInstanceForPublic(t, db, "inst-ical", show, now.Add(time.Hour), now.Add(2*time.Hour))

	req = httptest.NewRequest("GET", "/?station_id=st-ical", nil)
	rr = httptest.NewRecorder()
	a.handlePublicScheduleICal(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ical: got %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "BEGIN:VCALENDAR") {
		t.Fatalf("expected BEGIN:VCALENDAR in iCal output, got:\n%s", body)
	}
	if !strings.Contains(body, "Tech Talk") {
		t.Fatalf("expected show name in iCal output, got:\n%s", body)
	}
}

func TestPublicScheduleAPI_RSS(t *testing.T) {
	a, db := newPublicScheduleTest(t)

	// Missing station_id → 400
	req := httptest.NewRequest("GET", "/rss", nil)
	rr := httptest.NewRecorder()
	a.handlePublicScheduleRSS(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}

	// Public station with a show instance
	station := seedPublicStation(t, db, "st-rss", "RSS Radio")
	show := seedShowForPublic(t, db, "show-rss", station.ID, "Weekend Beats")
	now := time.Now()
	seedShowInstanceForPublic(t, db, "inst-rss", show, now.Add(2*time.Hour), now.Add(3*time.Hour))

	req = httptest.NewRequest("GET", "/?station_id=st-rss", nil)
	rr = httptest.NewRecorder()
	a.handlePublicScheduleRSS(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("rss: got %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "<rss") {
		t.Fatalf("expected <rss in RSS output, got:\n%s", body)
	}
	if !strings.Contains(body, "Weekend Beats") {
		t.Fatalf("expected show name in RSS output, got:\n%s", body)
	}
}
