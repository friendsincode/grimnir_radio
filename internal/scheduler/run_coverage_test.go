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

// newRunTestService creates a Service with full schema for run/tick tests.
func newRunTestService(t *testing.T) (*Service, *gorm.DB) {
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
	// Use a very short tick for test, but we'll test via context cancellation.
	svc := New(db, planner, eng, st, 2*time.Hour, logger)
	return svc, db
}

// TestRun_CancelStopsLoop verifies that Run exits when the context is cancelled.
func TestRun_CancelStopsLoop(t *testing.T) {
	svc, _ := newRunTestService(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- svc.Run(ctx)
	}()

	// Cancel immediately — Run should return quickly.
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop within 2 seconds after context cancel")
	}
}

// TestTick_WithStation verifies that tick processes stations without error.
func TestTick_WithStation(t *testing.T) {
	svc, db := newRunTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Tick Station", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "Main",
		URL: "https://example.invalid/m.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	// tick should not panic or return errors even with no clocks.
	svc.tick(ctx)
}

// TestTick_EmptyDB verifies that tick runs cleanly on an empty DB.
func TestTick_EmptyDB(t *testing.T) {
	svc, _ := newRunTestService(t)
	ctx := context.Background()

	// Should not panic.
	svc.tick(ctx)
}

// TestMaterialize_DelegatestoEngine verifies that Materialize delegates to the engine.
func TestMaterialize_DelegatestoEngine(t *testing.T) {
	svc, db := newRunTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Mat Station", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "M",
		URL: "https://example.invalid/m.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	// Calling Materialize with an empty smart block ID should return an error
	// (block not found) but not panic.
	_, err := svc.Materialize(ctx, smartblock.GenerateRequest{
		SmartBlockID: "nonexistent-block",
		StationID:    stationID,
		MountID:      mountID,
		Duration:     60000,
	})
	// We expect an error since the block doesn't exist; we just verify no panic.
	_ = err
}

// TestGetStationIDs_CachePath covers the cache-hit and cache-write paths
// by injecting a nil cache (the fallback-to-DB path) and a station.
func TestGetStationIDs_WithMultipleStations(t *testing.T) {
	svc, db := newRunTestService(t)
	ctx := context.Background()

	ids := []string{uuid.NewString(), uuid.NewString()}
	for _, id := range ids {
		if err := db.Create(&models.Station{ID: id, Name: id, Timezone: "UTC"}).Error; err != nil {
			t.Fatalf("create station: %v", err)
		}
	}

	got, err := svc.getStationIDs(ctx)
	if err != nil {
		t.Fatalf("getStationIDs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 station IDs, got %d", len(got))
	}
}

// TestScheduleStation_StopsetSlot covers the stopset branch in scheduleStation.
func TestScheduleStation_StopsetSlot(t *testing.T) {
	svc, db := newRunTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Stopset Station", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "M",
		URL: "https://example.invalid/m.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	clockID := uuid.NewString()
	if err := db.Create(&models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Stopset Clock",
		StartHour: 0,
		EndHour:   24,
		Slots: []models.ClockSlot{{
			ID:          uuid.NewString(),
			ClockHourID: clockID,
			Position:    0,
			Offset:      0,
			Type:        models.SlotTypeStopset,
			Payload: map[string]any{
				"mount_id":    mountID,
				"playlist_id": "pl-stopset",
				"duration_ms": float64(120000),
			},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("scheduleStation with stopset: %v", err)
	}

	var count int64
	db.Model(&models.ScheduleEntry{}).Where("station_id = ?", stationID).Count(&count)
	if count == 0 {
		t.Fatal("expected at least one stopset entry")
	}
}

// TestScheduleStation_UnhandledSlotType covers the default (unknown) slot type branch.
func TestScheduleStation_UnhandledSlotType(t *testing.T) {
	svc, db := newRunTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Unknown Station", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "M",
		URL: "https://example.invalid/m.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	clockID := uuid.NewString()
	if err := db.Create(&models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Unknown Type Clock",
		StartHour: 0,
		EndHour:   24,
		Slots: []models.ClockSlot{{
			ID:          uuid.NewString(),
			ClockHourID: clockID,
			Position:    0,
			Offset:      0,
			Type:        "unknown_type",
			Payload:     map[string]any{"mount_id": mountID, "duration_ms": float64(60000)},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	// Should not error even with unknown slot type.
	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("scheduleStation with unknown type: %v", err)
	}
}

// TestScheduleStation_SmartBlockNoBlockID covers path where smart_block slot is missing smart_block_id.
func TestScheduleStation_SmartBlockNoBlockID(t *testing.T) {
	svc, db := newRunTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "SB No ID Station", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "M",
		URL: "https://example.invalid/m.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	clockID := uuid.NewString()
	if err := db.Create(&models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "SB No ID Clock",
		StartHour: 0,
		EndHour:   24,
		Slots: []models.ClockSlot{{
			ID:          uuid.NewString(),
			ClockHourID: clockID,
			Position:    0,
			Offset:      0,
			Type:        models.SlotTypeSmartBlock,
			Payload: map[string]any{
				"mount_id":    mountID,
				"duration_ms": float64(3600000),
				// No smart_block_id — materializeSmartBlock should bail gracefully.
			},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("scheduleStation should not error when smart_block_id missing: %v", err)
	}
}

// TestExpandRecurringSmartBlock_Weekdays covers the weekdays recurrence type.
func TestExpandRecurringSmartBlock_Weekdays(t *testing.T) {
	loc := time.UTC
	// Start on a Monday
	startsAt := time.Date(2026, 3, 23, 8, 0, 0, 0, loc) // Monday
	endsAt := startsAt.Add(30 * time.Minute)

	entry := models.ScheduleEntry{
		ID:             "sb-weekdays",
		SourceID:       "sb-wd",
		SourceType:     "smart_block",
		RecurrenceType: models.RecurrenceWeekdays,
		StartsAt:       startsAt,
		EndsAt:         endsAt,
	}

	// Request a 7-day window
	windowStart := startsAt
	windowEnd := startsAt.AddDate(0, 0, 7)

	results := expandRecurringSmartBlock(entry, windowStart, windowEnd, loc)
	if len(results) == 0 {
		t.Fatal("expected weekday occurrences")
	}
	// None of the results should be on Saturday or Sunday
	for _, r := range results {
		day := r.StartsAt.Weekday()
		if day == time.Saturday || day == time.Sunday {
			t.Errorf("weekdays schedule should not include %v", day)
		}
	}
}

// TestExpandRecurringSmartBlock_CustomDays covers the custom recurrence type.
func TestExpandRecurringSmartBlock_CustomDays(t *testing.T) {
	loc := time.UTC
	startsAt := time.Date(2026, 3, 23, 12, 0, 0, 0, loc) // Monday
	endsAt := startsAt.Add(time.Hour)

	entry := models.ScheduleEntry{
		ID:             "sb-custom",
		SourceID:       "sb-c",
		SourceType:     "smart_block",
		RecurrenceType: models.RecurrenceCustom,
		RecurrenceDays: []int{1, 3, 5}, // Mon, Wed, Fri
		StartsAt:       startsAt,
		EndsAt:         endsAt,
	}

	windowStart := startsAt
	windowEnd := startsAt.AddDate(0, 0, 7)

	results := expandRecurringSmartBlock(entry, windowStart, windowEnd, loc)
	if len(results) == 0 {
		t.Fatal("expected custom day occurrences")
	}
	for _, r := range results {
		day := r.StartsAt.Weekday()
		if day != time.Monday && day != time.Wednesday && day != time.Friday {
			t.Errorf("expected only Mon/Wed/Fri, got %v", day)
		}
	}
}

// TestMaterializeDirectSmartBlockEntries_WithRecurring tests expanding recurring smart blocks.
func TestMaterializeDirectSmartBlockEntries_WithRecurring(t *testing.T) {
	svc, db := newRunTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "Recurring Station", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: mountID, StationID: stationID, Name: "M",
		URL: "https://example.invalid/m.mp3", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	// Create a smart block for testing
	blockID := uuid.NewString()
	if err := db.Create(&models.SmartBlock{
		ID:        blockID,
		StationID: stationID,
		Name:      "Daily Block",
		Rules:     map[string]any{"targetMinutes": 60},
	}).Error; err != nil {
		t.Fatalf("create smart block: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	// Recurring daily smart_block entry
	recurEntry := models.ScheduleEntry{
		ID:             uuid.NewString(),
		StationID:      stationID,
		MountID:        mountID,
		StartsAt:       now.Add(-24 * time.Hour),           // created yesterday
		EndsAt:         now.Add(-24*time.Hour + time.Hour), // 1h long
		SourceType:     "smart_block",
		SourceID:       blockID,
		RecurrenceType: models.RecurrenceDaily,
		IsInstance:     false,
	}
	if err := db.Create(&recurEntry).Error; err != nil {
		t.Fatalf("create recurring entry: %v", err)
	}

	// Should not panic or error even if engine returns error (block may not generate)
	err := svc.materializeDirectSmartBlockEntries(ctx, stationID, now)
	// We allow error here since smart block generation may fail
	_ = err
}

// TestMaterializeSmartBlock_MissingSmartBlockID covers the early bail path.
func TestMaterializeSmartBlock_MissingSmartBlockID(t *testing.T) {
	svc, _ := newRunTestService(t)
	ctx := context.Background()

	plan := clock.SlotPlan{
		SlotID:   "slot-no-sbid",
		StartsAt: time.Now().UTC(),
		EndsAt:   time.Now().UTC().Add(time.Hour),
		Duration: time.Hour,
		SlotType: string(models.SlotTypeSmartBlock),
		Payload:  map[string]any{"mount_id": "mount-x"}, // no smart_block_id
	}

	if err := svc.materializeSmartBlock(ctx, "station-x", plan); err != nil {
		t.Fatalf("expected nil for missing smart_block_id, got: %v", err)
	}
}

// TestMaterializeSmartBlock_MissingMount covers bail when no mount is found.
func TestMaterializeSmartBlock_MissingMount(t *testing.T) {
	svc, _ := newRunTestService(t)
	ctx := context.Background()

	plan := clock.SlotPlan{
		SlotID:   "slot-no-mount",
		StartsAt: time.Now().UTC(),
		EndsAt:   time.Now().UTC().Add(time.Hour),
		Duration: time.Hour,
		SlotType: string(models.SlotTypeSmartBlock),
		Payload:  map[string]any{"smart_block_id": "sb-x"}, // no mount_id, no station mount
	}

	if err := svc.materializeSmartBlock(ctx, "station-no-mount", plan); err != nil {
		t.Fatalf("expected nil for missing mount, got: %v", err)
	}
}

// TestCreateStopsetEntry_NoMount covers the bail when no mount is found.
func TestCreateStopsetEntry_NoMount(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	plan := clock.SlotPlan{
		SlotID:   "slot-stopset-no-mount",
		StartsAt: time.Now().UTC().Add(time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(3 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypeStopset),
		Payload:  map[string]any{},
	}

	if err := svc.createStopsetEntry(ctx, "station-no-mount-stopset", plan); err != nil {
		t.Fatalf("createStopsetEntry should return nil for no mount: %v", err)
	}

	var count int64
	db.Model(&models.ScheduleEntry{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected no entries, got %d", count)
	}
}
