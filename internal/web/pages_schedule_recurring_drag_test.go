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

// A calendar drag/resize sends {starts_at, ends_at} with no edit_mode. On a
// recurring occurrence that must NOT rewrite the whole series; it defaults to a
// single-occurrence exception so only that occurrence moves (#57).
func TestScheduleUpdateEntry_RecurringNoEditMode_KeepsParentAndOverrides(t *testing.T) {
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
	newStart := occ.Add(30 * time.Minute) // dragged 30 min later
	reqBody, _ := json.Marshal(map[string]any{
		"starts_at": newStart,
		"ends_at":   newStart.Add(time.Hour),
		// no edit_mode: exactly what eventDrop/eventResize sends
	})
	virtualID := recurrenceInstanceKey(parent.ID, occ, time.UTC)
	req := httptest.NewRequest(http.MethodPut, "/dashboard/schedule/entries/"+virtualID, bytes.NewReader(reqBody))
	req = withScheduleRouteID(req, virtualID)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &station))
	rr := httptest.NewRecorder()

	h.ScheduleUpdateEntry(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// The parent series anchor must be untouched.
	var reloaded models.ScheduleEntry
	if err := db.First(&reloaded, "id = ?", parent.ID).Error; err != nil {
		t.Fatalf("reload parent: %v", err)
	}
	if !reloaded.StartsAt.Equal(parentStart) {
		t.Fatalf("parent series was rewritten: StartsAt=%v want %v", reloaded.StartsAt, parentStart)
	}

	// A single-occurrence override should exist for the dragged day.
	var overrides []models.ScheduleEntry
	if err := db.Where("recurrence_parent_id = ?", parent.ID).Find(&overrides).Error; err != nil {
		t.Fatalf("load overrides: %v", err)
	}
	if len(overrides) != 1 {
		t.Fatalf("expected 1 occurrence override, got %d", len(overrides))
	}
	if !overrides[0].StartsAt.Equal(newStart) {
		t.Fatalf("override StartsAt=%v want %v", overrides[0].StartsAt, newStart)
	}
}
