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

	start, end, ok := resolveRecurringOccurrenceWindow(entry, now)
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

	start, end, ok := resolveRecurringOccurrenceWindow(entry, now)
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

	_, _, ok := resolveRecurringOccurrenceWindow(entry, now)
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

	resolved, key, until, ok := resolveEntryForNow(entry, now)
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
