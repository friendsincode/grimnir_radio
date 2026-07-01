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
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// seriesTestDB spins up an in-memory schedule store with a station.
func seriesTestDB(t *testing.T) (*gorm.DB, *models.Station) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}, &models.Mount{}, &models.ScheduleEntry{}, &models.Playlist{}, &models.PlaylistItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	station := &models.Station{ID: "s1", Name: "S1"}
	if err := db.Create(station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: "m1", StationID: station.ID, Name: "main", Format: "mp3", Bitrate: 128}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}
	return db, station
}

func updateEntryReq(t *testing.T, station *models.Station, id string, body map[string]any) *http.Request {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/dashboard/schedule/entries/"+id, bytes.NewReader(raw))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, ctxKeyStation, station)
	return req.WithContext(ctx)
}

// Editing one segment with edit_mode=all carries the playlist change to every
// other segment that shares the series, and leaves unrelated series untouched.
func TestScheduleUpdateAllPropagatesAcrossSeries(t *testing.T) {
	db, station := seriesTestDB(t)
	seriesID := "root"

	root := models.ScheduleEntry{
		ID: "root", StationID: station.ID, MountID: "m1",
		StartsAt: time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 2, 15, 11, 0, 0, 0, time.UTC),
		SourceType: "playlist", SourceID: "p1", RecurrenceType: "weekly", SeriesID: &seriesID,
	}
	child := models.ScheduleEntry{
		ID: "child", StationID: station.ID, MountID: "m1",
		StartsAt: time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 3, 15, 11, 0, 0, 0, time.UTC),
		SourceType: "playlist", SourceID: "p1", RecurrenceType: "weekly", SeriesID: &seriesID,
	}
	other := models.ScheduleEntry{ // a different show; must not change
		ID: "other", StationID: station.ID, MountID: "m1",
		StartsAt: time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 4, 15, 11, 0, 0, 0, time.UTC),
		SourceType: "playlist", SourceID: "p9", RecurrenceType: "weekly", SeriesID: strptr("other"),
	}
	for _, e := range []models.ScheduleEntry{root, child, other} {
		if err := db.Create(&e).Error; err != nil {
			t.Fatalf("seed %s: %v", e.ID, err)
		}
	}

	h := &Handler{db: db, logger: zerolog.Nop()}
	req := updateEntryReq(t, station, "child", map[string]any{
		"starts_at":   child.StartsAt,
		"ends_at":     child.EndsAt,
		"source_type": "playlist",
		"source_id":   "p2",
		"edit_mode":   "all",
	})
	rr := httptest.NewRecorder()
	h.ScheduleUpdateEntry(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	if got := sourceOf(t, db, "root"); got != "p2" {
		t.Errorf("root source = %q, want p2 (change did not propagate to the series)", got)
	}
	if got := sourceOf(t, db, "child"); got != "p2" {
		t.Errorf("child source = %q, want p2", got)
	}
	if got := sourceOf(t, db, "other"); got != "p9" {
		t.Errorf("other source = %q, want p9 (a different series was wrongly touched)", got)
	}
}

// A "this and all following" split keeps the new segment in the same series as
// the segment it split from, so a later "all" edit can span both. Uses a legacy
// parent (SeriesID nil) to exercise the on-split backfill.
func TestScheduleForwardSplitSharesSeries(t *testing.T) {
	db, station := seriesTestDB(t)
	root := models.ScheduleEntry{
		ID: "root", StationID: station.ID, MountID: "m1",
		StartsAt: time.Date(2026, 1, 4, 10, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 1, 4, 11, 0, 0, 0, time.UTC),
		SourceType: "playlist", SourceID: "p1", RecurrenceType: "weekly", RecurrenceDays: []int{0},
	}
	if err := db.Create(&root).Error; err != nil {
		t.Fatalf("seed root: %v", err)
	}

	h := &Handler{db: db, logger: zerolog.Nop()}
	req := updateEntryReq(t, station, "root_20260215", map[string]any{
		"starts_at":   time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC),
		"ends_at":     time.Date(2026, 2, 15, 11, 0, 0, 0, time.UTC),
		"source_type": "playlist",
		"source_id":   "p2",
		"edit_mode":   "forward",
	})
	rr := httptest.NewRecorder()
	h.ScheduleUpdateEntry(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// The legacy parent got a series id equal to its own id.
	var reloaded models.ScheduleEntry
	if err := db.First(&reloaded, "id = ?", "root").Error; err != nil {
		t.Fatalf("reload root: %v", err)
	}
	if reloaded.SeriesID == nil || *reloaded.SeriesID != "root" {
		t.Fatalf("root series id = %v, want root", reloaded.SeriesID)
	}
	// The new forward segment shares that series id.
	var child models.ScheduleEntry
	if err := db.Where("id <> ? AND is_instance = ?", "root", false).First(&child).Error; err != nil {
		t.Fatalf("find forward segment: %v", err)
	}
	if child.SeriesID == nil || *child.SeriesID != "root" {
		t.Errorf("forward segment series id = %v, want root", child.SeriesID)
	}
	if child.SourceID != "p2" {
		t.Errorf("forward segment source = %q, want p2", child.SourceID)
	}
}

// A single-occurrence override inherits the parent's series (legacy parent path).
func TestScheduleSingleOverrideInheritsSeries(t *testing.T) {
	db, station := seriesTestDB(t)
	root := models.ScheduleEntry{
		ID: "root", StationID: station.ID, MountID: "m1",
		StartsAt: time.Date(2026, 1, 4, 10, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 1, 4, 11, 0, 0, 0, time.UTC),
		SourceType: "playlist", SourceID: "p1", RecurrenceType: "weekly", RecurrenceDays: []int{0},
	}
	if err := db.Create(&root).Error; err != nil {
		t.Fatalf("seed root: %v", err)
	}

	h := &Handler{db: db, logger: zerolog.Nop()}
	req := updateEntryReq(t, station, "root_20260215", map[string]any{
		"starts_at":   time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC),
		"ends_at":     time.Date(2026, 2, 15, 11, 0, 0, 0, time.UTC),
		"source_type": "playlist",
		"source_id":   "p2",
		"edit_mode":   "single",
	})
	rr := httptest.NewRecorder()
	h.ScheduleUpdateEntry(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var override models.ScheduleEntry
	if err := db.Where("is_instance = ?", true).First(&override).Error; err != nil {
		t.Fatalf("find override: %v", err)
	}
	if override.SeriesID == nil || *override.SeriesID != "root" {
		t.Errorf("override series id = %v, want root", override.SeriesID)
	}
	// The series content is untouched by a single override.
	if got := sourceOf(t, db, "root"); got != "p1" {
		t.Errorf("root source = %q, want p1 (single override must not change the series)", got)
	}
}

// Deleting a playlist a recurring show still points at is refused, and nothing
// in the schedule is removed.
func TestPlaylistDeleteBlockedByRecurringShow(t *testing.T) {
	db, station := seriesTestDB(t)
	if err := db.Create(&models.Playlist{ID: "pl1", StationID: station.ID, Name: "P"}).Error; err != nil {
		t.Fatalf("seed playlist: %v", err)
	}
	entry := models.ScheduleEntry{
		ID: "e1", StationID: station.ID, MountID: "m1",
		StartsAt: time.Date(2026, 1, 4, 10, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 1, 4, 11, 0, 0, 0, time.UTC),
		SourceType: "playlist", SourceID: "pl1", RecurrenceType: "weekly",
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	h := &Handler{db: db, logger: zerolog.Nop()}
	req := httptest.NewRequest(http.MethodDelete, "/dashboard/playlists/pl1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "pl1")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, ctxKeyStation, station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.PlaylistDelete(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rr.Code, rr.Body.String())
	}
	var plCount, entCount int64
	db.Model(&models.Playlist{}).Where("id = ?", "pl1").Count(&plCount)
	db.Model(&models.ScheduleEntry{}).Where("id = ?", "e1").Count(&entCount)
	if plCount != 1 {
		t.Errorf("playlist was deleted despite recurring reference")
	}
	if entCount != 1 {
		t.Errorf("schedule entry was removed; the guard must not delete anything")
	}
}

func strptr(s string) *string { return &s }

func sourceOf(t *testing.T, db *gorm.DB, id string) string {
	t.Helper()
	var e models.ScheduleEntry
	if err := db.First(&e, "id = ?", id).Error; err != nil {
		t.Fatalf("reload %s: %v", id, err)
	}
	return e.SourceID
}
