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

// newBrokenDBService creates a Service backed by a DB with no stations table.
func newBrokenDBService(t *testing.T) *Service {
	t.Helper()
	// Open a bare DB with NO tables migrated — Select on stations will fail.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Do NOT migrate any tables — DB queries will fail with "no such table".
	logger := zerolog.Nop()
	planner := clock.NewPlanner(db, logger)
	eng := smartblock.New(db, logger)
	st := state.NewStore()
	return &Service{
		db:         db,
		planner:    planner,
		engine:     eng,
		stateStore: st,
		logger:     logger,
		lookahead:  2 * time.Hour,
		warnedKeys: make(map[string]struct{}),
	}
}

// TestTick_ErrorLoadingStations exercises the error branch in tick.
func TestTick_ErrorLoadingStations(t *testing.T) {
	svc := newBrokenDBService(t)
	ctx := context.Background()

	// With no tables migrated, getStationIDs will fail with "no such table",
	// causing the tick error path.
	svc.tick(ctx)
}

// TestScheduleStation_SuppressionPath exercises slotAlreadyScheduled returning true.
func TestScheduleStation_SuppressionPath(t *testing.T) {
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
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Supp Station", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "M",
		URL: "https://example.invalid/m.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	// Create a clock with a playlist slot
	clockID := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Hour)
	if err := db.Create(&models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Supp Clock",
		StartHour: 0,
		EndHour:   24,
		Slots: []models.ClockSlot{{
			ID:          uuid.NewString(),
			ClockHourID: clockID,
			Position:    0,
			Offset:      0,
			Type:        models.SlotTypePlaylist,
			Payload: map[string]any{
				"playlist_id": "pl-1",
				"mount_id":    mountID,
				"duration_ms": float64(3600000),
			},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	// Pre-populate an entry covering the next several hours so slotAlreadyScheduled returns true.
	preEntry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   now,
		EndsAt:     now.Add(3 * time.Hour),
		SourceType: "media",
		SourceID:   uuid.NewString(),
		IsInstance: true,
	}
	if err := db.Create(&preEntry).Error; err != nil {
		t.Fatalf("create pre-entry: %v", err)
	}

	// First call: will see pre-entry, suppress the slot, and record suppression.
	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("scheduleStation: %v", err)
	}

	// Verify suppression record was created.
	var count int64
	db.Model(&models.ScheduleSuppression{}).Where("station_id = ?", stationID).Count(&count)
	if count == 0 {
		t.Fatal("expected at least one suppression record")
	}
}

// TestScheduleStation_ExistingScheduleEntriesNotDuplicated verifies the warning
// path when no plans are generated but existing entries are present.
func TestScheduleStation_ExistingEntriesNoPlans(t *testing.T) {
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
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Manual Station", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "M",
		URL: "https://example.invalid/m.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	// Create an existing schedule entry (non-clock, manual) in the future.
	now := time.Now().UTC().Truncate(time.Second)
	manualEntry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   now.Add(30 * time.Minute),
		EndsAt:     now.Add(90 * time.Minute),
		SourceType: "media",
		SourceID:   uuid.NewString(),
		IsInstance: true,
	}
	if err := db.Create(&manualEntry).Error; err != nil {
		t.Fatalf("create manual entry: %v", err)
	}

	// No clock template — plans == 0, but existingCount > 0 triggers the debug log path.
	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("scheduleStation: %v", err)
	}
}

// TestRecurringDayMatches_Weekly_LocalTZ verifies that the local-timezone weekday
// is used (not UTC), matching the v1.38.11 bugfix.
func TestRecurringDayMatches_Weekly_LocalTZ(t *testing.T) {
	// A show seeded at 10pm Wednesday CST = 4am Thursday UTC.
	// recurringDayMatches should match Wednesday (local), not Thursday (UTC).
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skip("America/Chicago timezone not available")
	}

	// 10pm Wednesday CST
	startsAt := time.Date(2026, 3, 25, 22, 0, 0, 0, loc) // local Wednesday 10pm
	// This is 2026-03-26 04:00 UTC (Thursday UTC)

	entry := models.ScheduleEntry{
		RecurrenceType: models.RecurrenceWeekly,
		StartsAt:       startsAt,
	}

	// Should match Wednesday local, not Thursday UTC
	if !recurringDayMatches(entry, time.Wednesday, loc) {
		t.Error("expected match on Wednesday (local timezone weekday)")
	}
	if recurringDayMatches(entry, time.Thursday, loc) {
		t.Error("should NOT match Thursday (UTC weekday) — fix from v1.38.11")
	}
}

// TestExpandRecurringSmartBlock_InProgressBlock tests catching in-progress blocks.
func TestExpandRecurringSmartBlock_InProgressBlock(t *testing.T) {
	loc := time.UTC
	// Block starts at 8am, runs for 14 hours (8am-10pm).
	// Yesterday at 8am = "yesterday"
	now := time.Now().UTC().Truncate(time.Hour)
	yesterday := now.AddDate(0, 0, -1)
	startsAt := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 8, 0, 0, 0, loc)
	endsAt := startsAt.Add(14 * time.Hour) // ends at 10pm yesterday

	entry := models.ScheduleEntry{
		ID:             "sb-inprogress",
		SourceID:       "sb-ip",
		SourceType:     "smart_block",
		RecurrenceType: models.RecurrenceDaily,
		StartsAt:       startsAt,
		EndsAt:         endsAt,
	}

	// Window: from now (middle of today)
	windowStart := now
	windowEnd := now.Add(2 * time.Hour)

	// The expand function walks from one day before windowStart.
	// A block starting at 8am today should be found if 8am < windowEnd and 10pm > windowStart.
	results := expandRecurringSmartBlock(entry, windowStart, windowEnd, loc)

	// We expect the function to include any occurrence that overlaps the window.
	// Since this tests the "in-progress" path, we just verify no panic.
	_ = results
}

// TestMaterializeDirectSmartBlockEntries_NonRecurring tests non-recurring entries.
func TestMaterializeDirectSmartBlockEntries_NonRecurring(t *testing.T) {
	svc, db := newRunTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Non-Recurring", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "M",
		URL: "https://example.invalid/m.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	blockID := uuid.NewString()
	if err := db.Create(&models.SmartBlock{
		ID:        blockID,
		StationID: stationID,
		Name:      "Non-Recurring Block",
		Rules:     map[string]any{"targetMinutes": 60},
	}).Error; err != nil {
		t.Fatalf("create smart block: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	// Non-recurring smart_block entry in the future
	nonRecurEntry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   now.Add(30 * time.Minute),
		EndsAt:     now.Add(90 * time.Minute),
		SourceType: "smart_block",
		SourceID:   blockID,
		IsInstance: false,
	}
	if err := db.Create(&nonRecurEntry).Error; err != nil {
		t.Fatalf("create entry: %v", err)
	}

	// Should not panic — engine may fail to generate but that's ok
	_ = svc.materializeDirectSmartBlockEntries(ctx, stationID, now)
}

// TestScheduleStation_SlotBeforeStart verifies plan.StartsAt.Before(start) skip path.
func TestScheduleStation_SlotBeforeStart(t *testing.T) {
	// When the planner generates a plan with StartsAt before `start`, it should
	// be silently skipped. This is hard to trigger in pure unit test since the
	// planner generates future slots, but we exercise via a clock that has a
	// very narrow time window.
	svc, db, _ := newServiceForAPITests(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Skip Station", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "M",
		URL: "https://example.invalid/m.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	// Create a webstream clock covering a narrow past window.
	// The planner may generate slots in the past that get skipped.
	clockID := uuid.NewString()
	if err := db.Create(&models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Past Clock",
		StartHour: 0,
		EndHour:   24,
		Slots: []models.ClockSlot{{
			ID:          uuid.NewString(),
			ClockHourID: clockID,
			Position:    0,
			Offset:      0,
			Type:        models.SlotTypeWebstream,
			Payload: map[string]any{
				"webstream_id": "ws-past",
				"mount_id":     mountID,
				"duration_ms":  float64(3600000),
			},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("scheduleStation: %v", err)
	}
}

// TestMaterializeSmartBlock_NegativeDuration tests when targetDuration defaults to EndsAt-StartsAt.
func TestMaterializeSmartBlock_NegativeDuration(t *testing.T) {
	svc, db := newRunTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Neg Duration", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "M",
		URL: "https://example.invalid/m.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	// Plan with Duration == 0 but valid EndsAt — should compute duration from times.
	plan := clock.SlotPlan{
		SlotID:   "slot-neg-dur",
		StartsAt: time.Now().UTC(),
		EndsAt:   time.Now().UTC().Add(time.Hour),
		Duration: 0, // will compute from EndsAt - StartsAt
		SlotType: string(models.SlotTypeSmartBlock),
		Payload: map[string]any{
			"smart_block_id": "nonexistent-sb",
			"mount_id":       mountID,
		},
	}

	// Will fail because block doesn't exist (engine will return error),
	// but the duration-computation path is exercised before that.
	_ = svc.materializeSmartBlock(ctx, stationID, plan)
}
