/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func deleteReq(t *testing.T, station models.Station, virtualID, scope string) *http.Request {
	t.Helper()
	url := "/dashboard/schedule/entries/" + virtualID
	if scope != "" {
		url += "?scope=" + scope
	}
	req := httptest.NewRequest(http.MethodDelete, url, nil)
	req = withScheduleRouteID(req, virtualID)
	return req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &station))
}

// Deleting a single recurring occurrence records an EXDATE on the parent and
// leaves the series running, instead of ending it. That is what makes the delete
// stick across rebuilds without wiping future occurrences (#50/#52).
func TestScheduleDeleteEntry_Occurrence_AddsExceptionKeepsSeries(t *testing.T) {
	db, station := newScheduleEdgeTestDB(t)
	parentStart := time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)
	parent := models.ScheduleEntry{
		ID: "parent-entry", StationID: station.ID, MountID: "m1",
		StartsAt: parentStart, EndsAt: parentStart.Add(time.Hour),
		SourceType: "playlist", SourceID: "playlist-1",
		RecurrenceType: models.RecurrenceWeekly,
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("create parent: %v", err)
	}
	h := &Handler{db: db, logger: zerolog.Nop()}

	occ := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	virtualID := recurrenceInstanceKey(parent.ID, occ, time.UTC)
	rr := httptest.NewRecorder()
	h.ScheduleDeleteEntry(rr, deleteReq(t, station, virtualID, "")) // default scope = occurrence

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	var reloaded models.ScheduleEntry
	if err := db.First(&reloaded, "id = ?", parent.ID).Error; err != nil {
		t.Fatalf("reload parent: %v", err)
	}
	if reloaded.RecurrenceEndDate != nil {
		t.Fatal("deleting one occurrence must not end the series")
	}
	if !reloaded.IsExceptedOn(occ, time.UTC) {
		t.Fatalf("occurrence date should be an exception, got %v", reloaded.RecurrenceExceptions)
	}
}

// scope=forward truncates the series at the day before the occurrence.
func TestScheduleDeleteEntry_Forward_SetsEndDate(t *testing.T) {
	db, station := newScheduleEdgeTestDB(t)
	parentStart := time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)
	parent := models.ScheduleEntry{
		ID: "parent-fwd", StationID: station.ID, MountID: "m1",
		StartsAt: parentStart, EndsAt: parentStart.Add(time.Hour),
		SourceType: "playlist", SourceID: "playlist-1",
		RecurrenceType: models.RecurrenceWeekly,
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("create parent: %v", err)
	}
	h := &Handler{db: db, logger: zerolog.Nop()}

	occ := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	virtualID := recurrenceInstanceKey(parent.ID, occ, time.UTC)
	rr := httptest.NewRecorder()
	h.ScheduleDeleteEntry(rr, deleteReq(t, station, virtualID, "forward"))

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	var reloaded models.ScheduleEntry
	if err := db.First(&reloaded, "id = ?", parent.ID).Error; err != nil {
		t.Fatalf("reload parent: %v", err)
	}
	if reloaded.RecurrenceEndDate == nil {
		t.Fatal("forward delete should set RecurrenceEndDate")
	}
}

// scope=all removes the parent and every materialized instance/override.
func TestScheduleDeleteEntry_All_RemovesSeries(t *testing.T) {
	db, station := newScheduleEdgeTestDB(t)
	parentStart := time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)
	parentID := "parent-all"
	parent := models.ScheduleEntry{
		ID: parentID, StationID: station.ID, MountID: "m1",
		StartsAt: parentStart, EndsAt: parentStart.Add(time.Hour),
		SourceType: "playlist", SourceID: "playlist-1",
		RecurrenceType: models.RecurrenceWeekly,
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("create parent: %v", err)
	}
	inst := models.ScheduleEntry{
		ID: "inst-1", StationID: station.ID, MountID: "m1",
		StartsAt: parentStart.AddDate(0, 0, 7), EndsAt: parentStart.AddDate(0, 0, 7).Add(time.Hour),
		SourceType: "playlist", SourceID: "playlist-1",
		IsInstance: true, RecurrenceParentID: &parentID,
	}
	if err := db.Create(&inst).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}
	h := &Handler{db: db, logger: zerolog.Nop()}

	occ := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	virtualID := recurrenceInstanceKey(parentID, occ, time.UTC)
	rr := httptest.NewRecorder()
	h.ScheduleDeleteEntry(rr, deleteReq(t, station, virtualID, "all"))

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	var count int64
	db.Model(&models.ScheduleEntry{}).Count(&count)
	if count != 0 {
		t.Fatalf("scope=all should remove parent and instances, %d rows remain", count)
	}
}
