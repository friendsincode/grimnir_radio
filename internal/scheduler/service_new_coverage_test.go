/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/clock"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/scheduler/state"
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newFullTestService creates a Service with all tables migrated.
func newFullTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Station{},
		&models.Mount{},
		&models.ScheduleEntry{},
		&models.ScheduleSuppression{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.MediaItem{},
		&models.SmartBlock{},
		&models.Playlist{},
		&models.PlaylistItem{},
		&models.PlayHistory{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	logger := zerolog.Nop()
	planner := clock.NewPlanner(db, logger)
	eng := smartblock.New(db, logger)
	st := state.NewStore()
	svc := New(db, planner, eng, st, 2*time.Hour, logger)
	return svc, db
}

// ── recurringDayMatches ───────────────────────────────────────────────────

func TestRecurringDayMatches_Daily(t *testing.T) {
	entry := models.ScheduleEntry{RecurrenceType: models.RecurrenceDaily}
	loc := time.UTC
	for _, day := range []time.Weekday{
		time.Monday, time.Tuesday, time.Wednesday,
		time.Thursday, time.Friday, time.Saturday, time.Sunday,
	} {
		if !recurringDayMatches(entry, day, loc) {
			t.Errorf("daily: expected match on %v", day)
		}
	}
}

func TestRecurringDayMatches_Weekdays(t *testing.T) {
	entry := models.ScheduleEntry{RecurrenceType: models.RecurrenceWeekdays}
	loc := time.UTC

	weekdays := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	for _, day := range weekdays {
		if !recurringDayMatches(entry, day, loc) {
			t.Errorf("weekdays: expected match on %v", day)
		}
	}

	weekend := []time.Weekday{time.Saturday, time.Sunday}
	for _, day := range weekend {
		if recurringDayMatches(entry, day, loc) {
			t.Errorf("weekdays: expected NO match on %v", day)
		}
	}
}

func TestRecurringDayMatches_Weekly_WithDays(t *testing.T) {
	// Explicit days list: Monday=1, Wednesday=3
	entry := models.ScheduleEntry{
		RecurrenceType: models.RecurrenceWeekly,
		RecurrenceDays: []int{1, 3},
	}
	loc := time.UTC

	if !recurringDayMatches(entry, time.Monday, loc) {
		t.Error("expected match on Monday")
	}
	if !recurringDayMatches(entry, time.Wednesday, loc) {
		t.Error("expected match on Wednesday")
	}
	if recurringDayMatches(entry, time.Tuesday, loc) {
		t.Error("expected NO match on Tuesday")
	}
	if recurringDayMatches(entry, time.Sunday, loc) {
		t.Error("expected NO match on Sunday")
	}
}

func TestRecurringDayMatches_Weekly_Fallback(t *testing.T) {
	// No explicit days — should match original entry's weekday in local time.
	// Entry was created on a Wednesday (UTC).
	startsAt := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC) // Wednesday
	entry := models.ScheduleEntry{
		RecurrenceType: models.RecurrenceWeekly,
		StartsAt:       startsAt,
	}
	loc := time.UTC

	if !recurringDayMatches(entry, time.Wednesday, loc) {
		t.Error("expected match on Wednesday (original weekday)")
	}
	if recurringDayMatches(entry, time.Thursday, loc) {
		t.Error("expected NO match on Thursday")
	}
}

func TestRecurringDayMatches_Custom_WithDays(t *testing.T) {
	// Custom recurrence with explicit days: Friday=5, Sunday=0
	entry := models.ScheduleEntry{
		RecurrenceType: models.RecurrenceCustom,
		RecurrenceDays: []int{5, 0},
	}
	loc := time.UTC

	if !recurringDayMatches(entry, time.Friday, loc) {
		t.Error("expected match on Friday")
	}
	if !recurringDayMatches(entry, time.Sunday, loc) {
		t.Error("expected match on Sunday")
	}
	if recurringDayMatches(entry, time.Monday, loc) {
		t.Error("expected NO match on Monday")
	}
}

func TestRecurringDayMatches_Custom_NoDays(t *testing.T) {
	// Custom with no days means match every day.
	entry := models.ScheduleEntry{
		RecurrenceType: models.RecurrenceCustom,
		RecurrenceDays: []int{},
	}
	loc := time.UTC

	for _, day := range []time.Weekday{time.Monday, time.Sunday, time.Saturday} {
		if !recurringDayMatches(entry, day, loc) {
			t.Errorf("custom/no-days: expected match on %v", day)
		}
	}
}

func TestRecurringDayMatches_UnknownType(t *testing.T) {
	entry := models.ScheduleEntry{RecurrenceType: "unknown_type"}
	loc := time.UTC
	if recurringDayMatches(entry, time.Monday, loc) {
		t.Error("unknown recurrence type should not match")
	}
}

// ── expandRecurringSmartBlock ─────────────────────────────────────────────

func TestExpandRecurringSmartBlock_Daily(t *testing.T) {
	// A daily block starting on Monday at 10:00, runs for 1h.
	loc := time.UTC
	startsAt := time.Date(2026, 3, 23, 10, 0, 0, 0, loc) // Monday
	endsAt := startsAt.Add(time.Hour)

	entry := models.ScheduleEntry{
		ID:             "sb-daily",
		SourceID:       "sb-1",
		SourceType:     "smart_block",
		RecurrenceType: models.RecurrenceDaily,
		StartsAt:       startsAt,
		EndsAt:         endsAt,
	}

	// Ask for 3 days starting Monday
	windowStart := startsAt
	windowEnd := startsAt.AddDate(0, 0, 3)

	results := expandRecurringSmartBlock(entry, windowStart, windowEnd, loc)
	if len(results) == 0 {
		t.Fatal("expected occurrences for daily block")
	}
	// Each result should start at 10:00 and last 1h
	for _, r := range results {
		h, m, s := r.StartsAt.Clock()
		if h != 10 || m != 0 || s != 0 {
			t.Errorf("occurrence start time = %02d:%02d:%02d, want 10:00:00", h, m, s)
		}
		if r.EndsAt.Sub(r.StartsAt) != time.Hour {
			t.Errorf("occurrence duration = %v, want 1h", r.EndsAt.Sub(r.StartsAt))
		}
	}
}

func TestExpandRecurringSmartBlock_WeeklyOnSpecificDay(t *testing.T) {
	loc := time.UTC
	// Entry created on Wednesday 2026-03-25
	startsAt := time.Date(2026, 3, 25, 9, 0, 0, 0, loc)
	endsAt := startsAt.Add(30 * time.Minute)

	entry := models.ScheduleEntry{
		ID:             "sb-weekly",
		SourceID:       "sb-2",
		SourceType:     "smart_block",
		RecurrenceType: models.RecurrenceWeekly,
		StartsAt:       startsAt,
		EndsAt:         endsAt,
	}

	// Window: 2 weeks after creation
	windowStart := startsAt.AddDate(0, 0, 7)
	windowEnd := windowStart.AddDate(0, 0, 7)

	results := expandRecurringSmartBlock(entry, windowStart, windowEnd, loc)
	if len(results) == 0 {
		t.Fatal("expected at least one weekly occurrence")
	}
	for _, r := range results {
		if r.StartsAt.Weekday() != time.Wednesday {
			t.Errorf("occurrence weekday = %v, want Wednesday", r.StartsAt.Weekday())
		}
	}
}

func TestExpandRecurringSmartBlock_ZeroDuration(t *testing.T) {
	// Entry with no duration should return nil.
	loc := time.UTC
	startsAt := time.Date(2026, 3, 23, 10, 0, 0, 0, loc)

	entry := models.ScheduleEntry{
		RecurrenceType: models.RecurrenceDaily,
		StartsAt:       startsAt,
		EndsAt:         startsAt, // same time => 0 duration
	}

	results := expandRecurringSmartBlock(entry, startsAt, startsAt.AddDate(0, 0, 2), loc)
	if len(results) != 0 {
		t.Fatalf("expected nil for zero-duration entry, got %d results", len(results))
	}
}

func TestExpandRecurringSmartBlock_RespectsEndDate(t *testing.T) {
	loc := time.UTC
	startsAt := time.Date(2026, 3, 23, 10, 0, 0, 0, loc) // Monday
	endsAt := startsAt.Add(time.Hour)

	endDate := startsAt.AddDate(0, 0, 1) // end after 1 day
	entry := models.ScheduleEntry{
		RecurrenceType:    models.RecurrenceDaily,
		StartsAt:          startsAt,
		EndsAt:            endsAt,
		RecurrenceEndDate: &endDate,
	}

	windowStart := startsAt
	windowEnd := startsAt.AddDate(0, 0, 5)

	results := expandRecurringSmartBlock(entry, windowStart, windowEnd, loc)
	// Should only include occurrences up to and including endDate
	for _, r := range results {
		if r.StartsAt.After(endDate) {
			t.Errorf("occurrence %v is after end date %v", r.StartsAt, endDate)
		}
	}
}

func TestExpandRecurringSmartBlock_WindowBeforeStart(t *testing.T) {
	// Window entirely before the entry starts — should return nil.
	loc := time.UTC
	startsAt := time.Date(2026, 3, 25, 10, 0, 0, 0, loc)
	endsAt := startsAt.Add(time.Hour)

	entry := models.ScheduleEntry{
		RecurrenceType: models.RecurrenceDaily,
		StartsAt:       startsAt,
		EndsAt:         endsAt,
	}

	// Request window a full month before the entry
	windowStart := startsAt.AddDate(0, -1, 0)
	windowEnd := windowStart.AddDate(0, 0, 7)

	results := expandRecurringSmartBlock(entry, windowStart, windowEnd, loc)
	if len(results) != 0 {
		t.Fatalf("expected no occurrences when window is before entry start, got %d", len(results))
	}
}

// ── getStationLocation ────────────────────────────────────────────────────

func TestGetStationLocation_UTC(t *testing.T) {
	svc, db := newFullTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "UTC Station", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	loc := svc.getStationLocation(ctx, stationID)
	if loc != time.UTC {
		t.Errorf("expected UTC, got %v", loc)
	}
}

func TestGetStationLocation_Chicago(t *testing.T) {
	svc, db := newFullTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Chicago Station", Timezone: "America/Chicago"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	loc := svc.getStationLocation(ctx, stationID)
	if loc.String() != "America/Chicago" {
		t.Errorf("expected America/Chicago, got %v", loc)
	}
}

func TestGetStationLocation_EmptyTimezone(t *testing.T) {
	svc, db := newFullTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "No TZ Station", Timezone: ""}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	loc := svc.getStationLocation(ctx, stationID)
	if loc != time.UTC {
		t.Errorf("expected UTC for empty timezone, got %v", loc)
	}
}

func TestGetStationLocation_InvalidTimezone(t *testing.T) {
	svc, db := newFullTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Bad TZ", Timezone: "Invalid/Timezone"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	loc := svc.getStationLocation(ctx, stationID)
	if loc != time.UTC {
		t.Errorf("expected UTC for invalid timezone, got %v", loc)
	}
}

func TestGetStationLocation_NotFound(t *testing.T) {
	svc, _ := newFullTestService(t)
	ctx := context.Background()

	loc := svc.getStationLocation(ctx, "nonexistent-station")
	if loc != time.UTC {
		t.Errorf("expected UTC for missing station, got %v", loc)
	}
}

// ── SetCache ──────────────────────────────────────────────────────────────

func TestSetCache_SetsCache(t *testing.T) {
	svc, _, _ := newServiceForAPITests(t)
	// Verify SetCache doesn't panic and sets the field.
	svc.SetCache(nil) // nil is valid (disables cache)
	if svc.cache != nil {
		t.Error("expected cache to be nil after SetCache(nil)")
	}
}

// newServiceForAPITests returns a service suitable for testing API-level methods.
func newServiceForAPITests(t *testing.T) (*Service, *gorm.DB, *clock.Planner) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Station{},
		&models.Mount{},
		&models.ScheduleEntry{},
		&models.ScheduleSuppression{},
		&models.ClockHour{},
		&models.ClockSlot{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	logger := zerolog.Nop()
	planner := clock.NewPlanner(db, logger)
	eng := smartblock.New(db, logger)
	st := state.NewStore()
	svc := New(db, planner, eng, st, 2*time.Hour, logger)
	return svc, db, planner
}

// ── Upcoming ──────────────────────────────────────────────────────────────

func TestUpcoming_ReturnsEntriesInWindow(t *testing.T) {
	svc, db, _ := newServiceForAPITests(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "Main",
		URL: "https://example.invalid/s.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	entries := []models.ScheduleEntry{
		{ID: uuid.NewString(), StationID: stationID, MountID: mountID, StartsAt: now.Add(10 * time.Minute), EndsAt: now.Add(13 * time.Minute), SourceType: "media", SourceID: "m1"},
		{ID: uuid.NewString(), StationID: stationID, MountID: mountID, StartsAt: now.Add(1 * time.Hour), EndsAt: now.Add(63 * time.Minute), SourceType: "media", SourceID: "m2"},
		{ID: uuid.NewString(), StationID: stationID, MountID: mountID, StartsAt: now.Add(25 * time.Hour), EndsAt: now.Add(26 * time.Hour), SourceType: "media", SourceID: "m3"},
	}
	if err := db.Create(&entries).Error; err != nil {
		t.Fatalf("create entries: %v", err)
	}

	result, err := svc.Upcoming(ctx, stationID, now, 2*time.Hour)
	if err != nil {
		t.Fatalf("Upcoming: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries within 2h window, got %d", len(result))
	}
}

func TestUpcoming_ZeroHorizonDefaultsTo24h(t *testing.T) {
	svc, db, _ := newServiceForAPITests(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "Main",
		URL: "https://example.invalid/s.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   now.Add(20 * time.Hour),
		EndsAt:     now.Add(21 * time.Hour),
		SourceType: "media",
		SourceID:   "m1",
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatalf("create entry: %v", err)
	}

	// horizon = 0 should default to 24h
	result, err := svc.Upcoming(ctx, stationID, now, 0)
	if err != nil {
		t.Fatalf("Upcoming: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 entry in 24h window, got %d", len(result))
	}
}

func TestUpcoming_EmptyStation(t *testing.T) {
	svc, _, _ := newServiceForAPITests(t)
	ctx := context.Background()

	result, err := svc.Upcoming(ctx, "nonexistent-station", time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("Upcoming on empty station: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(result))
	}
}

// ── RefreshStation ────────────────────────────────────────────────────────

func TestRefreshStation_NoClock(t *testing.T) {
	svc, db, _ := newServiceForAPITests(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Refresh Station", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	// No clock template — scheduleStation should succeed without entries.
	if err := svc.RefreshStation(ctx, stationID); err != nil {
		t.Fatalf("RefreshStation: %v", err)
	}

	var count int64
	db.Model(&models.ScheduleEntry{}).Where("station_id = ?", stationID).Count(&count)
	if count != 0 {
		t.Fatalf("expected 0 entries, got %d", count)
	}
}

// ── Simulate / SimulateClock ──────────────────────────────────────────────

func TestSimulate_NoClocks(t *testing.T) {
	svc, db, _ := newServiceForAPITests(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Simulate Station", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	plans, err := svc.Simulate(ctx, stationID, time.Now().UTC(), 2*time.Hour)
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	// No clocks means no plans.
	if len(plans) != 0 {
		t.Fatalf("expected 0 plans, got %d", len(plans))
	}
}

func TestSimulate_WithClock(t *testing.T) {
	svc, db, _ := newServiceForAPITests(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Simulate Clock Station", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "Main",
		URL: "https://example.invalid/s.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	clockID := uuid.NewString()
	slotID := uuid.NewString()
	if err := db.Create(&models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Test Clock",
		StartHour: 0,
		EndHour:   24,
		Slots: []models.ClockSlot{{
			ID:          slotID,
			ClockHourID: clockID,
			Position:    0,
			Offset:      0,
			Type:        models.SlotTypePlaylist,
			Payload:     map[string]any{"playlist_id": "p1", "mount_id": mountID, "duration_ms": float64(3600000)},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	plans, err := svc.Simulate(ctx, stationID, time.Now().UTC(), 2*time.Hour)
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if len(plans) == 0 {
		t.Fatal("expected plans from clock, got none")
	}
}

func TestSimulateClock_NonExistentClock(t *testing.T) {
	svc, _, _ := newServiceForAPITests(t)
	ctx := context.Background()

	// A non-existent clock ID returns an error (record not found).
	_, err := svc.SimulateClock(ctx, "nonexistent-clock-id", time.Now().UTC(), 2*time.Hour)
	if err == nil {
		t.Fatal("expected error for nonexistent clock, got nil")
	}
}

// ── getStationIDs ─────────────────────────────────────────────────────────

func TestGetStationIDs_Empty(t *testing.T) {
	svc, _, _ := newServiceForAPITests(t)
	ctx := context.Background()

	ids, err := svc.getStationIDs(ctx)
	if err != nil {
		t.Fatalf("getStationIDs: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected 0 station IDs, got %d", len(ids))
	}
}

func TestGetStationIDs_WithStations(t *testing.T) {
	svc, db, _ := newServiceForAPITests(t)
	ctx := context.Background()

	stationIDs := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for _, id := range stationIDs {
		if err := db.Create(&models.Station{ID: id, Name: id, Timezone: "UTC"}).Error; err != nil {
			t.Fatalf("create station: %v", err)
		}
	}

	ids, err := svc.getStationIDs(ctx)
	if err != nil {
		t.Fatalf("getStationIDs: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 IDs, got %d", len(ids))
	}
}

// ── maybeCleanupOldEntries ────────────────────────────────────────────────

func TestMaybeCleanupOldEntries_DeletesOldInstances(t *testing.T) {
	svc, db := newFullTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "M",
		URL: "https://example.invalid/m.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	now := time.Now().UTC()
	old := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   now.Add(-8 * 24 * time.Hour),
		EndsAt:     now.Add(-7*24*time.Hour - time.Minute),
		SourceType: "media",
		SourceID:   uuid.NewString(),
		IsInstance: true,
	}
	recent := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   now.Add(time.Hour),
		EndsAt:     now.Add(2 * time.Hour),
		SourceType: "media",
		SourceID:   uuid.NewString(),
		IsInstance: true,
	}
	if err := db.Create(&old).Error; err != nil {
		t.Fatalf("create old entry: %v", err)
	}
	if err := db.Create(&recent).Error; err != nil {
		t.Fatalf("create recent entry: %v", err)
	}

	// Force cleanup by zeroing lastCleanup
	svc.lastCleanup = time.Time{}
	svc.maybeCleanupOldEntries(ctx)

	var count int64
	db.Model(&models.ScheduleEntry{}).Where("station_id = ?", stationID).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 entry after cleanup (old should be deleted), got %d", count)
	}
}

func TestMaybeCleanupOldEntries_RateLimited(t *testing.T) {
	svc, db := newFullTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "M",
		URL: "https://example.invalid/m.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	now := time.Now().UTC()
	old := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   now.Add(-8 * 24 * time.Hour),
		EndsAt:     now.Add(-7*24*time.Hour - time.Minute),
		SourceType: "media",
		SourceID:   uuid.NewString(),
		IsInstance: true,
	}
	if err := db.Create(&old).Error; err != nil {
		t.Fatalf("create old entry: %v", err)
	}

	// Set lastCleanup to now — should be rate-limited and not run
	svc.lastCleanup = time.Now()
	svc.maybeCleanupOldEntries(ctx)

	var count int64
	db.Model(&models.ScheduleEntry{}).Where("station_id = ?", stationID).Count(&count)
	if count != 1 {
		t.Fatalf("rate-limited cleanup should not have deleted anything, got count=%d", count)
	}
}

// ── warnOnce ──────────────────────────────────────────────────────────────

func TestWarnOnce_OnlyWarnsOnce(t *testing.T) {
	svc, _, _ := newServiceForAPITests(t)

	callCount := 0
	svc.warnOnce("test-key", func(_ *zerolog.Event) {
		callCount++
	})
	svc.warnOnce("test-key", func(_ *zerolog.Event) {
		callCount++
	})
	svc.warnOnce("test-key", func(_ *zerolog.Event) {
		callCount++
	})

	if callCount != 1 {
		t.Fatalf("warnOnce called log fn %d times, expected exactly 1", callCount)
	}
}

func TestWarnOnce_DifferentKeysBothWarn(t *testing.T) {
	svc, _, _ := newServiceForAPITests(t)

	callCount := 0
	svc.warnOnce("key-a", func(_ *zerolog.Event) { callCount++ })
	svc.warnOnce("key-b", func(_ *zerolog.Event) { callCount++ })
	svc.warnOnce("key-a", func(_ *zerolog.Event) { callCount++ }) // duplicate

	if callCount != 2 {
		t.Fatalf("warnOnce: expected 2 distinct key logs, got %d", callCount)
	}
}

// ── explainNoPlans ────────────────────────────────────────────────────────

func TestExplainNoPlans_NoClockTemplate(t *testing.T) {
	svc, db, _ := newServiceForAPITests(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "No Clock", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	reason, _, _ := svc.explainNoPlans(ctx, stationID)
	if reason != "no_clock_template" {
		t.Errorf("expected no_clock_template, got %q", reason)
	}
}

func TestExplainNoPlans_ClockWithNoSlots(t *testing.T) {
	svc, db, _ := newServiceForAPITests(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Empty Clock", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.ClockHour{
		ID:        uuid.NewString(),
		StationID: stationID,
		Name:      "Empty",
		StartHour: 0,
		EndHour:   24,
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	reason, _, _ := svc.explainNoPlans(ctx, stationID)
	if reason != "clock_has_no_slots" {
		t.Errorf("expected clock_has_no_slots, got %q", reason)
	}
}

func TestExplainNoPlans_ClockWithSlots(t *testing.T) {
	svc, db, _ := newServiceForAPITests(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Has Slots", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	clockID := uuid.NewString()
	if err := db.Create(&models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Has Slots Clock",
		StartHour: 0,
		EndHour:   24,
		Slots: []models.ClockSlot{{
			ID:          uuid.NewString(),
			ClockHourID: clockID,
			Position:    0,
			Offset:      0,
			Type:        models.SlotTypePlaylist,
			Payload:     map[string]any{"playlist_id": "p1"},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock with slots: %v", err)
	}

	reason, _, _ := svc.explainNoPlans(ctx, stationID)
	if reason != "no_slots_generated" {
		t.Errorf("expected no_slots_generated, got %q", reason)
	}
}

// ── createWebstreamEntry - no mount fallback ──────────────────────────────

func TestCreateWebstreamEntry_NoMount(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	plan := clock.SlotPlan{
		SlotID:   "slot-ws-nomount",
		StartsAt: time.Now().UTC().Add(time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypeWebstream),
		Payload:  map[string]any{"webstream_id": "ws-test"},
	}

	if err := svc.createWebstreamEntry(ctx, "station-ws-nomount", plan); err != nil {
		t.Fatalf("createWebstreamEntry should return nil for no mount: %v", err)
	}

	var count int64
	db.Model(&models.ScheduleEntry{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected no entries (no mount available), got %d", count)
	}
}

func TestCreateWebstreamEntry_NoWebstreamID(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := "station-ws-noid"
	mountID := "mount-ws-noid"
	createTestMount(t, db, stationID, mountID)

	plan := clock.SlotPlan{
		SlotID:   "slot-ws-noid",
		StartsAt: time.Now().UTC().Add(time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypeWebstream),
		Payload:  map[string]any{"mount_id": mountID},
	}

	if err := svc.createWebstreamEntry(ctx, stationID, plan); err != nil {
		t.Fatalf("createWebstreamEntry should return nil for missing webstream_id: %v", err)
	}

	var count int64
	db.Model(&models.ScheduleEntry{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected no entries (no webstream_id), got %d", count)
	}
}

// ── materializeDirectSmartBlockEntries - edge cases ───────────────────────

func TestMaterializeDirectSmartBlockEntries_EmptyStation(t *testing.T) {
	svc, db, _ := newServiceForAPITests(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "DS", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	// No entries at all — should succeed without error.
	if err := svc.materializeDirectSmartBlockEntries(ctx, stationID, time.Now().UTC()); err != nil {
		t.Fatalf("materializeDirectSmartBlockEntries: %v", err)
	}
}
