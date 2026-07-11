/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// A recurring occurrence is addressed by a virtual id ({parentUUID}_{YYYYMMDD}).
// The read endpoints must resolve that to a concrete row before querying the uuid
// id column; passing the virtual id straight in is a 404 on sqlite and a 22P02 on
// Postgres (issue #49). With no per-occurrence override, it resolves to the parent.
func TestScheduleEntryDetails_VirtualRecurringID_ResolvesToParent(t *testing.T) {
	db, station := newScheduleEdgeTestDB(t)

	parent := models.ScheduleEntry{
		ID:             "11111111-1111-1111-1111-111111111111",
		StationID:      station.ID,
		MountID:        "m1",
		StartsAt:       time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC),
		EndsAt:         time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
		SourceType:     "playlist",
		SourceID:       "playlist-1",
		RecurrenceType: models.RecurrenceWeekly,
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("create parent: %v", err)
	}

	h := &Handler{db: db, logger: zerolog.Nop()}

	occ := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	virtualID := recurrenceInstanceKey(parent.ID, occ, time.UTC)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/schedule/entries/"+virtualID+"/details", nil)
	req = withScheduleRouteID(req, virtualID)
	rec := httptest.NewRecorder()

	h.ScheduleEntryDetails(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("virtual recurring id should resolve, got status %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["source_type"] != "playlist" || resp["source_id"] != "playlist-1" {
		t.Fatalf("expected parent source playlist/playlist-1, got %v/%v", resp["source_type"], resp["source_id"])
	}
}

// When the occurrence has its own concrete override row, the read resolves to that
// override, not the parent, so the modal shows the per-occurrence edit.
func TestScheduleEntryDetails_VirtualRecurringID_PrefersOverride(t *testing.T) {
	db, station := newScheduleEdgeTestDB(t)

	parentID := "22222222-2222-2222-2222-222222222222"
	parent := models.ScheduleEntry{
		ID:             parentID,
		StationID:      station.ID,
		MountID:        "m1",
		StartsAt:       time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC),
		EndsAt:         time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
		SourceType:     "playlist",
		SourceID:       "playlist-1",
		RecurrenceType: models.RecurrenceWeekly,
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("create parent: %v", err)
	}

	occ := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	override := models.ScheduleEntry{
		ID:                 "33333333-3333-3333-3333-333333333333",
		StationID:          station.ID,
		MountID:            "m1",
		StartsAt:           occ,
		EndsAt:             occ.Add(time.Hour),
		SourceType:         "media",
		SourceID:           "media-override",
		RecurrenceParentID: &parentID,
	}
	if err := db.Create(&override).Error; err != nil {
		t.Fatalf("create override: %v", err)
	}

	h := &Handler{db: db, logger: zerolog.Nop()}
	virtualID := recurrenceInstanceKey(parentID, occ, time.UTC)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/schedule/entries/"+virtualID+"/details", nil)
	req = withScheduleRouteID(req, virtualID)
	rec := httptest.NewRecorder()

	h.ScheduleEntryDetails(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("virtual recurring id should resolve, got status %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["source_type"] != "media" || resp["source_id"] != "media-override" {
		t.Fatalf("expected override source media/media-override, got %v/%v", resp["source_type"], resp["source_id"])
	}
}

// The shared resolver both read handlers use: a plain uuid passes straight
// through, and an unknown id surfaces an error rather than being swallowed.
func TestResolveScheduleEntry_PlainAndMissing(t *testing.T) {
	db, station := newScheduleEdgeTestDB(t)

	entry := models.ScheduleEntry{
		ID:         "44444444-4444-4444-4444-444444444444",
		StationID:  station.ID,
		MountID:    "m1",
		StartsAt:   time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC),
		EndsAt:     time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
		SourceType: "playlist",
		SourceID:   "playlist-1",
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatalf("create entry: %v", err)
	}

	h := &Handler{db: db, logger: zerolog.Nop()}

	got, err := h.resolveScheduleEntry(entry.ID)
	if err != nil {
		t.Fatalf("plain uuid should resolve: %v", err)
	}
	if got.ID != entry.ID {
		t.Fatalf("plain uuid resolved to %s, want %s", got.ID, entry.ID)
	}

	if _, err := h.resolveScheduleEntry("55555555-5555-5555-5555-555555555555"); err == nil {
		t.Fatal("unknown id should return an error, got nil")
	}
}
