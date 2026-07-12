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

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Context-menu Duplicate on a recurring occurrence creates a standalone (non-
// recurring) copy at the clicked occurrence's time, leaving the series intact
// (#58). Virtual ids resolve through the shared helper before copying.
func TestScheduleEntryDuplicate_CreatesStandaloneCopyAtOccurrenceTime(t *testing.T) {
	db, station := newScheduleEdgeTestDB(t)

	parentStart := time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)
	parent := models.ScheduleEntry{
		ID:             "parent-entry",
		StationID:      station.ID,
		MountID:        "m1",
		StartsAt:       parentStart,
		EndsAt:         parentStart.Add(time.Hour),
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
	reqBody, _ := json.Marshal(map[string]any{"starts_at": occ, "ends_at": occ.Add(time.Hour)})
	req := httptest.NewRequest(http.MethodPost, "/dashboard/schedule/entries/"+virtualID+"/duplicate", bytes.NewReader(reqBody))
	req = withScheduleRouteID(req, virtualID)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &station))
	rr := httptest.NewRecorder()

	h.ScheduleEntryDuplicate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var all []models.ScheduleEntry
	if err := db.Order("created_at ASC").Find(&all).Error; err != nil {
		t.Fatalf("load entries: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 entries after duplicate, got %d", len(all))
	}
	dup := all[1]
	if dup.ID == parent.ID {
		t.Fatal("duplicate reused the parent id")
	}
	if dup.RecurrenceType != models.RecurrenceNone {
		t.Fatalf("duplicate should be standalone, got recurrence %q", dup.RecurrenceType)
	}
	if dup.RecurrenceParentID != nil || dup.IsInstance {
		t.Fatal("duplicate should not be a recurrence instance")
	}
	if dup.SourceType != "playlist" || dup.SourceID != "playlist-1" {
		t.Fatalf("duplicate source mismatch: %s/%s", dup.SourceType, dup.SourceID)
	}
	if !dup.StartsAt.Equal(occ) {
		t.Fatalf("duplicate StartsAt=%v want occurrence time %v", dup.StartsAt, occ)
	}

	var reloaded models.ScheduleEntry
	if err := db.First(&reloaded, "id = ?", parent.ID).Error; err != nil {
		t.Fatalf("reload parent: %v", err)
	}
	if reloaded.RecurrenceType != models.RecurrenceWeekly || !reloaded.StartsAt.Equal(parentStart) {
		t.Fatal("parent series was modified by the duplicate")
	}
}
