/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package scheduler

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// A deleted recurring occurrence is recorded as an exception date on the parent
// (EXDATE). The rebuild expander must skip those dates, or the occurrence
// reappears on the next materialization (#50).
func TestExpandRecurringSmartBlock_SkipsExceptions(t *testing.T) {
	loc := time.UTC
	startsAt := time.Date(2026, 3, 23, 8, 0, 0, 0, loc) // Monday
	entry := models.ScheduleEntry{
		ID:                   "sb-exc",
		SourceID:             "sb",
		SourceType:           "smart_block",
		RecurrenceType:       models.RecurrenceDaily,
		StartsAt:             startsAt,
		EndsAt:               startsAt.Add(30 * time.Minute),
		RecurrenceExceptions: []string{"2026-03-25"}, // skip the Wednesday occurrence
	}

	results := expandRecurringSmartBlock(entry, startsAt, startsAt.AddDate(0, 0, 7), loc)
	if len(results) == 0 {
		t.Fatal("expected daily occurrences")
	}
	for _, r := range results {
		if r.StartsAt.In(loc).Format("2006-01-02") == "2026-03-25" {
			t.Fatalf("excepted date 2026-03-25 should be skipped, got occurrence at %v", r.StartsAt)
		}
	}
	// Sanity: a non-excepted day is still present.
	var has24 bool
	for _, r := range results {
		if r.StartsAt.In(loc).Format("2006-01-02") == "2026-03-24" {
			has24 = true
		}
	}
	if !has24 {
		t.Fatal("expected the 2026-03-24 occurrence to remain")
	}
}
