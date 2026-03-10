package playout

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestResolveRecurringOccurrenceWindowDaily(t *testing.T) {
	entry := models.ScheduleEntry{
		ID:             "e1",
		StartsAt:       time.Date(2026, 2, 1, 14, 0, 0, 0, time.UTC),
		EndsAt:         time.Date(2026, 2, 1, 15, 0, 0, 0, time.UTC),
		RecurrenceType: models.RecurrenceDaily,
	}
	now := time.Date(2026, 2, 12, 14, 0, 2, 0, time.UTC)

	start, end, ok := resolveRecurringOccurrenceWindow(entry, now, time.UTC)
	if !ok {
		t.Fatal("expected recurring occurrence to resolve")
	}
	if want := time.Date(2026, 2, 12, 14, 0, 0, 0, time.UTC); !start.Equal(want) {
		t.Fatalf("start = %v, want %v", start, want)
	}
	if want := time.Date(2026, 2, 12, 15, 0, 0, 0, time.UTC); !end.Equal(want) {
		t.Fatalf("end = %v, want %v", end, want)
	}
}

func TestResolveRecurringOccurrenceWindowOvernightUsesPreviousDay(t *testing.T) {
	entry := models.ScheduleEntry{
		ID:             "e2",
		StartsAt:       time.Date(2026, 2, 1, 23, 30, 0, 0, time.UTC),
		EndsAt:         time.Date(2026, 2, 2, 0, 30, 0, 0, time.UTC),
		RecurrenceType: models.RecurrenceDaily,
	}
	now := time.Date(2026, 2, 12, 0, 10, 0, 0, time.UTC)

	start, end, ok := resolveRecurringOccurrenceWindow(entry, now, time.UTC)
	if !ok {
		t.Fatal("expected overnight recurring occurrence to resolve")
	}
	if want := time.Date(2026, 2, 11, 23, 30, 0, 0, time.UTC); !start.Equal(want) {
		t.Fatalf("start = %v, want %v", start, want)
	}
	if want := time.Date(2026, 2, 12, 0, 30, 0, 0, time.UTC); !end.Equal(want) {
		t.Fatalf("end = %v, want %v", end, want)
	}
}

func TestResolveRecurringOccurrenceWindowCustomDayMismatch(t *testing.T) {
	entry := models.ScheduleEntry{
		ID:             "e3",
		StartsAt:       time.Date(2026, 2, 2, 14, 0, 0, 0, time.UTC), // Monday
		EndsAt:         time.Date(2026, 2, 2, 15, 0, 0, 0, time.UTC),
		RecurrenceType: models.RecurrenceCustom,
		RecurrenceDays: []int{1}, // Monday only
	}
	now := time.Date(2026, 2, 12, 14, 0, 0, 0, time.UTC) // Thursday

	_, _, ok := resolveRecurringOccurrenceWindow(entry, now, time.UTC)
	if ok {
		t.Fatal("expected no recurring occurrence on non-matching day")
	}
}

func TestResolveEntryForNowUsesOccurrenceKey(t *testing.T) {
	entry := models.ScheduleEntry{
		ID:             "entry-parent",
		StartsAt:       time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC),
		EndsAt:         time.Date(2026, 2, 1, 11, 0, 0, 0, time.UTC),
		RecurrenceType: models.RecurrenceDaily,
	}
	now := time.Date(2026, 2, 12, 10, 0, 1, 0, time.UTC)

	resolved, key, until, ok := resolveEntryForNow(entry, now, time.UTC)
	if !ok {
		t.Fatal("expected recurring entry to resolve")
	}
	if resolved.ID != entry.ID {
		t.Fatalf("resolved entry id = %q, want %q", resolved.ID, entry.ID)
	}
	if want := "entry-parent@2026-02-12T10:00:00Z"; key != want {
		t.Fatalf("key = %q, want %q", key, want)
	}
	if !until.Equal(time.Date(2026, 2, 12, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("until = %v, want 2026-02-12 11:00:00 +0000 UTC", until)
	}
}

func TestResolveRecurringOccurrenceWindowDST(t *testing.T) {
	// Load America/Chicago to test DST handling.
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skipf("America/Chicago timezone not available: %v", err)
	}

	// Entry created in winter (CST = UTC-6) at "5:00 AM CST" = 11:00 UTC.
	// After DST (spring forward to CDT = UTC-5), the occurrence should still
	// resolve to 5:00 AM CDT = 10:00 UTC, not 6:00 AM CDT = 11:00 UTC.
	entry := models.ScheduleEntry{
		ID:             "dst-test",
		StartsAt:       time.Date(2026, 1, 6, 11, 0, 0, 0, time.UTC), // Tuesday 5 AM CST
		EndsAt:         time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC),
		RecurrenceType: models.RecurrenceCustom,
		RecurrenceDays: []int{2}, // Tuesday
	}

	// Now is a Tuesday in CDT (after DST spring-forward March 8, 2026).
	// 5:00 AM CDT = 10:00 UTC. Occurrence should start at 10:00 UTC.
	now := time.Date(2026, 3, 10, 10, 0, 30, 0, time.UTC) // 5:00:30 AM CDT

	start, end, ok := resolveRecurringOccurrenceWindow(entry, now, chicago)
	if !ok {
		t.Fatal("expected DST-aware recurring occurrence to resolve")
	}
	wantStart := time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC) // 5:00 AM CDT
	wantEnd := time.Date(2026, 3, 10, 11, 0, 0, 0, time.UTC)   // 6:00 AM CDT
	if !start.Equal(wantStart) {
		t.Errorf("start = %v (%v local), want %v (%v local)",
			start, start.In(chicago), wantStart, wantStart.In(chicago))
	}
	if !end.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", end, wantEnd)
	}
}
