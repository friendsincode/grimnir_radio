/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestApplyScheduleRequest_Mutations guards the DJ approve flow's side effects.
// The existing tests called applyScheduleRequest but asserted only HTTP 200 (or
// nothing), so a bug swapping starts/ends, writing the wrong column, or no-oping
// the mutation would ship green. Here we assert the instance actually changed.
func TestApplyScheduleRequest_Mutations(t *testing.T) {
	a, stationID := newSerializerPGTest(t)
	ctx := context.Background()
	showID := dbtest.UUID("show")
	if err := a.db.Create(&models.Show{ID: showID, StationID: stationID, Name: "S"}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}

	seedInstance := func(id string) {
		if err := a.db.Create(&models.ShowInstance{
			ID: id, ShowID: showID, StationID: stationID,
			StartsAt: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC),
			EndsAt:   time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
			Status:   models.ShowInstanceScheduled,
		}).Error; err != nil {
			t.Fatalf("seed instance: %v", err)
		}
	}
	sp := func(s string) *string { return &s }

	t.Run("cancel", func(t *testing.T) {
		id := dbtest.UUID("cancel")
		seedInstance(id)
		a.applyScheduleRequest(ctx, &models.ScheduleRequest{RequestType: models.RequestTypeCancel, TargetInstanceID: sp(id)})
		var got models.ShowInstance
		a.db.First(&got, "id = ?", id)
		if got.Status != models.ShowInstanceCancelled {
			t.Fatalf("status = %q, want cancelled", got.Status)
		}
	})

	t.Run("reschedule", func(t *testing.T) {
		id := dbtest.UUID("resched")
		seedInstance(id)
		newStart := time.Date(2026, 3, 1, 14, 0, 0, 0, time.UTC)
		newEnd := time.Date(2026, 3, 1, 15, 0, 0, 0, time.UTC)
		a.applyScheduleRequest(ctx, &models.ScheduleRequest{
			RequestType:      models.RequestTypeReschedule,
			TargetInstanceID: sp(id),
			ProposedData:     map[string]any{"starts_at": newStart.Format(time.RFC3339), "ends_at": newEnd.Format(time.RFC3339)},
		})
		var got models.ShowInstance
		a.db.First(&got, "id = ?", id)
		if !got.StartsAt.Equal(newStart) || !got.EndsAt.Equal(newEnd) {
			t.Fatalf("times not rescheduled: starts=%v ends=%v", got.StartsAt, got.EndsAt)
		}
		if got.ExceptionType != models.ShowExceptionRescheduled {
			t.Fatalf("exception_type = %q, want rescheduled", got.ExceptionType)
		}
	})

	t.Run("swap", func(t *testing.T) {
		id := dbtest.UUID("swap")
		seedInstance(id)
		newHost := dbtest.UUID("newhost")
		if err := a.db.Create(&models.User{ID: newHost, Email: "newhost@t.local"}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		a.applyScheduleRequest(ctx, &models.ScheduleRequest{
			RequestType:      models.RequestTypeSwap,
			TargetInstanceID: sp(id),
			SwapWithUserID:   sp(newHost),
		})
		var got models.ShowInstance
		a.db.First(&got, "id = ?", id)
		if got.HostUserID == nil || *got.HostUserID != newHost {
			t.Fatalf("host not swapped: %v", got.HostUserID)
		}
		if got.ExceptionType != models.ShowExceptionSubstitute {
			t.Fatalf("exception_type = %q, want substitute", got.ExceptionType)
		}
	})
}
