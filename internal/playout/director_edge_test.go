package playout

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestResolveRecurringAllTypes(t *testing.T) {
	// Template entry: Monday 2026-02-02 14:00-15:00 UTC
	baseEntry := models.ScheduleEntry{
		ID:       "e-recur",
		StartsAt: time.Date(2026, 2, 2, 14, 0, 0, 0, time.UTC), // Monday
		EndsAt:   time.Date(2026, 2, 2, 15, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name           string
		recurrenceType models.RecurrenceType
		recurrenceDays []int
		day            time.Weekday
		want           bool
	}{
		{"daily matches any day", models.RecurrenceDaily, nil, time.Wednesday, true},
		{"daily matches weekend", models.RecurrenceDaily, nil, time.Saturday, true},
		{"weekdays matches Monday", models.RecurrenceWeekdays, nil, time.Monday, true},
		{"weekdays matches Friday", models.RecurrenceWeekdays, nil, time.Friday, true},
		{"weekdays skips Saturday", models.RecurrenceWeekdays, nil, time.Saturday, false},
		{"weekdays skips Sunday", models.RecurrenceWeekdays, nil, time.Sunday, false},
		{"weekly same weekday", models.RecurrenceWeekly, nil, time.Monday, true},
		{"weekly different weekday", models.RecurrenceWeekly, nil, time.Wednesday, false},
		{"custom matches Wed", models.RecurrenceCustom, []int{1, 3, 5}, time.Wednesday, true},
		{"custom skips Tue", models.RecurrenceCustom, []int{1, 3, 5}, time.Tuesday, false},
		{"none returns false", models.RecurrenceNone, nil, time.Monday, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := baseEntry
			entry.RecurrenceType = tt.recurrenceType
			entry.RecurrenceDays = tt.recurrenceDays
			got := matchesRecurringDay(entry, tt.day)
			if got != tt.want {
				t.Errorf("matchesRecurringDay(%s, %s) = %v, want %v",
					tt.recurrenceType, tt.day, got, tt.want)
			}
		})
	}
}

func TestRecurrenceEndDateBoundary(t *testing.T) {
	today := time.Date(2026, 3, 2, 14, 0, 1, 0, time.UTC)
	todayDate := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	yesterdayDate := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	baseEntry := models.ScheduleEntry{
		ID:             "e-end",
		StartsAt:       time.Date(2026, 2, 1, 14, 0, 0, 0, time.UTC),
		EndsAt:         time.Date(2026, 2, 1, 15, 0, 0, 0, time.UTC),
		RecurrenceType: models.RecurrenceDaily,
	}

	t.Run("EndDate=today plays", func(t *testing.T) {
		entry := baseEntry
		entry.RecurrenceEndDate = &todayDate
		_, _, ok := resolveRecurringOccurrenceWindow(entry, today)
		if !ok {
			t.Fatal("expected occurrence when EndDate=today")
		}
	})

	t.Run("EndDate=yesterday stops", func(t *testing.T) {
		entry := baseEntry
		entry.RecurrenceEndDate = &yesterdayDate
		_, _, ok := resolveRecurringOccurrenceWindow(entry, today)
		if ok {
			t.Fatal("expected no occurrence when EndDate=yesterday")
		}
	})

	t.Run("EndDate=nil plays forever", func(t *testing.T) {
		entry := baseEntry
		entry.RecurrenceEndDate = nil
		_, _, ok := resolveRecurringOccurrenceWindow(entry, today)
		if !ok {
			t.Fatal("expected occurrence when EndDate=nil")
		}
	})
}

func TestResolveRecurringWeekendSkip(t *testing.T) {
	// Saturday 2026-03-07
	now := time.Date(2026, 3, 7, 14, 0, 1, 0, time.UTC)
	entry := models.ScheduleEntry{
		ID:             "e-weekday",
		StartsAt:       time.Date(2026, 2, 2, 14, 0, 0, 0, time.UTC), // Monday
		EndsAt:         time.Date(2026, 2, 2, 15, 0, 0, 0, time.UTC),
		RecurrenceType: models.RecurrenceWeekdays,
	}

	_, _, ok := resolveRecurringOccurrenceWindow(entry, now)
	if ok {
		t.Fatal("expected no occurrence on Saturday for weekdays recurrence")
	}
}

func TestResolveRecurringZeroDuration(t *testing.T) {
	now := time.Date(2026, 3, 2, 14, 0, 1, 0, time.UTC)
	entry := models.ScheduleEntry{
		ID:             "e-zero",
		StartsAt:       time.Date(2026, 2, 1, 14, 0, 0, 0, time.UTC),
		EndsAt:         time.Date(2026, 2, 1, 14, 0, 0, 0, time.UTC), // same as start
		RecurrenceType: models.RecurrenceDaily,
	}

	_, _, ok := resolveRecurringOccurrenceWindow(entry, now)
	if ok {
		t.Fatal("expected no occurrence when EndsAt == StartsAt (zero duration)")
	}
}

func TestResolveEntryForNowTimeWindows(t *testing.T) {
	base := time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"1s after start resolves", base.Add(1 * time.Second), true},
		{"3s after EndsAt is past window", time.Date(2026, 3, 2, 15, 0, 3, 0, time.UTC), false},
		{"3s before start is too early", base.Add(-3 * time.Second), false},
		{"exactly at start resolves", base, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := models.ScheduleEntry{
				ID:       "e-window",
				StartsAt: base,
				EndsAt:   time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC),
			}
			_, _, _, ok := resolveEntryForNow(entry, tt.now)
			if ok != tt.want {
				t.Errorf("resolveEntryForNow(now=%v) ok = %v, want %v", tt.now, ok, tt.want)
			}
		})
	}
}

func TestResolveEntryForNowPlaybackKey(t *testing.T) {
	t.Run("recurring key has timestamp", func(t *testing.T) {
		entry := models.ScheduleEntry{
			ID:             "parent-1",
			StartsAt:       time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC),
			EndsAt:         time.Date(2026, 2, 1, 11, 0, 0, 0, time.UTC),
			RecurrenceType: models.RecurrenceDaily,
		}
		now := time.Date(2026, 3, 2, 10, 0, 1, 0, time.UTC)
		_, key, _, ok := resolveEntryForNow(entry, now)
		if !ok {
			t.Fatal("expected resolve")
		}
		want := "parent-1@2026-03-02T10:00:00Z"
		if key != want {
			t.Errorf("key = %q, want %q", key, want)
		}
	})

	t.Run("non-recurring key has original time", func(t *testing.T) {
		startsAt := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
		entry := models.ScheduleEntry{
			ID:       "single-1",
			StartsAt: startsAt,
			EndsAt:   time.Date(2026, 3, 2, 11, 0, 0, 0, time.UTC),
		}
		now := time.Date(2026, 3, 2, 10, 0, 1, 0, time.UTC)
		_, key, _, ok := resolveEntryForNow(entry, now)
		if !ok {
			t.Fatal("expected resolve")
		}
		want := "single-1@2026-03-02T10:00:00Z"
		if key != want {
			t.Errorf("key = %q, want %q", key, want)
		}
	})
}

func TestEffectiveCrossfadeInheritMode(t *testing.T) {
	station := crossfadeConfig{Enabled: true, Duration: 4 * time.Second}
	entry := models.ScheduleEntry{
		Metadata: map[string]any{
			"crossfade": map[string]any{
				"override":    true,
				"enabled":     "inherit",
				"duration_ms": float64(2000),
			},
		},
	}
	got := effectiveCrossfade(entry, station)
	if !got.Enabled {
		t.Error("expected Enabled=true (inherit from station)")
	}
	if got.Duration != 2*time.Second {
		t.Errorf("Duration = %v, want 2s", got.Duration)
	}
}

func TestEffectiveCrossfadeNilMetadata(t *testing.T) {
	station := crossfadeConfig{Enabled: true, Duration: 5 * time.Second}
	entry := models.ScheduleEntry{Metadata: nil}
	got := effectiveCrossfade(entry, station)
	if got != station {
		t.Errorf("got %+v, want station config %+v", got, station)
	}
}

func TestEffectiveCrossfadeDurationCap(t *testing.T) {
	station := crossfadeConfig{Enabled: true, Duration: 3 * time.Second}
	entry := models.ScheduleEntry{
		Metadata: map[string]any{
			"crossfade": map[string]any{
				"override":    true,
				"enabled":     "on",
				"duration_ms": float64(99999),
			},
		},
	}
	got := effectiveCrossfade(entry, station)
	if got.Duration != 30*time.Second {
		t.Errorf("Duration = %v, want 30s (capped)", got.Duration)
	}
}

func TestEffectiveCrossfadeNoOverride(t *testing.T) {
	station := crossfadeConfig{Enabled: false, Duration: 3 * time.Second}
	entry := models.ScheduleEntry{
		Metadata: map[string]any{
			"crossfade": map[string]any{
				"override":    false,
				"enabled":     "on",
				"duration_ms": float64(10000),
			},
		},
	}
	got := effectiveCrossfade(entry, station)
	if got != station {
		t.Errorf("got %+v, want station config %+v (override=false)", got, station)
	}
}

func TestMatchesRecurringDay(t *testing.T) {
	tests := []struct {
		name           string
		recurrenceType models.RecurrenceType
		startsAt       time.Time
		recurrenceDays []int
		day            time.Weekday
		want           bool
	}{
		{"daily Sunday", models.RecurrenceDaily, time.Time{}, nil, time.Sunday, true},
		{"daily Wednesday", models.RecurrenceDaily, time.Time{}, nil, time.Wednesday, true},
		{"weekdays Monday", models.RecurrenceWeekdays, time.Time{}, nil, time.Monday, true},
		{"weekdays Saturday", models.RecurrenceWeekdays, time.Time{}, nil, time.Saturday, false},
		{"weekly match", models.RecurrenceWeekly, time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC), nil, time.Monday, true},
		{"weekly mismatch", models.RecurrenceWeekly, time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC), nil, time.Friday, false},
		{"custom with days", models.RecurrenceCustom, time.Time{}, []int{0, 3, 6}, time.Wednesday, true},
		{"custom miss", models.RecurrenceCustom, time.Time{}, []int{0, 3, 6}, time.Monday, false},
		{"custom empty days", models.RecurrenceCustom, time.Time{}, []int{}, time.Monday, true},
		{"none", models.RecurrenceNone, time.Time{}, nil, time.Monday, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := models.ScheduleEntry{
				RecurrenceType: tt.recurrenceType,
				StartsAt:       tt.startsAt,
				RecurrenceDays: tt.recurrenceDays,
			}
			got := matchesRecurringDay(entry, tt.day)
			if got != tt.want {
				t.Errorf("matchesRecurringDay() = %v, want %v", got, tt.want)
			}
		})
	}
}
