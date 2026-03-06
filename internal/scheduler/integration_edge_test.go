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
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newIntegrationTestService creates a Service with a real Planner and in-memory DB.
// This allows end-to-end tests from clock compilation through entry creation.
func newIntegrationTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Station{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.Mount{},
		&models.ScheduleEntry{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	planner := clock.NewPlanner(db, zerolog.Nop())
	svc := &Service{
		db:         db,
		planner:    planner,
		logger:     zerolog.Nop(),
		lookahead:  2 * time.Hour,
		warnedKeys: make(map[string]struct{}),
	}
	return svc, db
}

func setupIntegrationStation(t *testing.T, db *gorm.DB, stationID, mountID string) {
	t.Helper()
	if err := db.Create(&models.Station{ID: stationID, Name: "Test", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if mountID != "" {
		if err := db.Create(&models.Mount{
			ID: mountID, StationID: stationID, Name: "Main",
			URL: "https://example.invalid/main.mp3", Format: "mp3",
		}).Error; err != nil {
			t.Fatalf("create mount: %v", err)
		}
	}
}

func TestScheduleStationPlaylistEndToEnd(t *testing.T) {
	svc, db := newIntegrationTestService(t)
	ctx := context.Background()

	stationID := "station-int-pl"
	mountID := "mount-int-pl"
	setupIntegrationStation(t, db, stationID, mountID)

	now := time.Now().UTC().Truncate(time.Hour)
	if err := db.Create(&models.ClockHour{
		ID: "clock-int-pl", StationID: stationID, Name: "PL Clock",
		StartHour: 0, EndHour: 24,
		Slots: []models.ClockSlot{{
			ID: "slot-int-pl", ClockHourID: "clock-int-pl", Position: 0, Offset: 0,
			Type: models.SlotTypePlaylist, Payload: map[string]any{
				"playlist_id": "p1", "mount_id": mountID, "duration_ms": float64(3600000),
			},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	// Override lookahead to cover next hour from now
	svc.lookahead = 2 * time.Hour

	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("scheduleStation: %v", err)
	}

	var entries []models.ScheduleEntry
	if err := db.Where("station_id = ?", stationID).Find(&entries).Error; err != nil {
		t.Fatalf("query entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one schedule entry")
	}

	found := false
	for _, e := range entries {
		if e.SourceType == "playlist" {
			found = true
			if e.SourceID != "p1" {
				t.Errorf("source_id = %q, want %q", e.SourceID, "p1")
			}
		}
	}
	if !found {
		t.Fatal("no playlist entry found")
	}
	_ = now
}

func TestScheduleStationHardItemEndToEnd(t *testing.T) {
	svc, db := newIntegrationTestService(t)
	ctx := context.Background()

	stationID := "station-int-hi"
	mountID := "mount-int-hi"
	setupIntegrationStation(t, db, stationID, mountID)

	if err := db.Create(&models.ClockHour{
		ID: "clock-int-hi", StationID: stationID, Name: "HI Clock",
		StartHour: 0, EndHour: 24,
		Slots: []models.ClockSlot{{
			ID: "slot-int-hi", ClockHourID: "clock-int-hi", Position: 0, Offset: 0,
			Type: models.SlotTypeHardItem, Payload: map[string]any{
				"media_id": "m1", "mount_id": mountID, "duration_ms": float64(180000),
			},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("scheduleStation: %v", err)
	}

	var entries []models.ScheduleEntry
	db.Where("station_id = ?", stationID).Find(&entries)
	found := false
	for _, e := range entries {
		if e.SourceType == "media" {
			if v, _ := e.Metadata["slot_type"].(string); v == string(models.SlotTypeHardItem) {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no hard item entry found")
	}
}

func TestScheduleStationWebstreamEndToEnd(t *testing.T) {
	svc, db := newIntegrationTestService(t)
	ctx := context.Background()

	stationID := "station-int-ws"
	mountID := "mount-int-ws"
	setupIntegrationStation(t, db, stationID, mountID)

	if err := db.Create(&models.ClockHour{
		ID: "clock-int-ws", StationID: stationID, Name: "WS Clock",
		StartHour: 0, EndHour: 24,
		Slots: []models.ClockSlot{{
			ID: "slot-int-ws", ClockHourID: "clock-int-ws", Position: 0, Offset: 0,
			Type: models.SlotTypeWebstream, Payload: map[string]any{
				"webstream_id": "ws1", "mount_id": mountID,
			},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("scheduleStation: %v", err)
	}

	var entries []models.ScheduleEntry
	db.Where("station_id = ?", stationID).Find(&entries)
	found := false
	for _, e := range entries {
		if e.SourceType == "webstream" {
			found = true
			if e.SourceID != "ws1" {
				t.Errorf("source_id = %q, want %q", e.SourceID, "ws1")
			}
		}
	}
	if !found {
		t.Fatal("no webstream entry found")
	}
}

func TestScheduleStationNoClocksNoEntries(t *testing.T) {
	svc, db := newIntegrationTestService(t)
	ctx := context.Background()

	stationID := "station-int-noclocks"
	setupIntegrationStation(t, db, stationID, "mount-noclocks")

	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("scheduleStation: %v", err)
	}

	var count int64
	db.Model(&models.ScheduleEntry{}).Where("station_id = ?", stationID).Count(&count)
	if count != 0 {
		t.Fatalf("expected 0 entries, got %d", count)
	}
}

func TestScheduleStationEmptyClocksNoEntries(t *testing.T) {
	svc, db := newIntegrationTestService(t)
	ctx := context.Background()

	stationID := "station-int-empty"
	setupIntegrationStation(t, db, stationID, "mount-empty")

	// Clock with no slots
	if err := db.Create(&models.ClockHour{
		ID: "clock-int-empty", StationID: stationID, Name: "Empty",
		StartHour: 0, EndHour: 24,
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("scheduleStation: %v", err)
	}

	var count int64
	db.Model(&models.ScheduleEntry{}).Where("station_id = ?", stationID).Count(&count)
	if count != 0 {
		t.Fatalf("expected 0 entries, got %d", count)
	}
}

func TestScheduleStationIdempotent(t *testing.T) {
	svc, db := newIntegrationTestService(t)
	ctx := context.Background()

	stationID := "station-int-idem"
	mountID := "mount-int-idem"
	setupIntegrationStation(t, db, stationID, mountID)

	if err := db.Create(&models.ClockHour{
		ID: "clock-int-idem", StationID: stationID, Name: "Idem",
		StartHour: 0, EndHour: 24,
		Slots: []models.ClockSlot{{
			ID: "slot-int-idem", ClockHourID: "clock-int-idem", Position: 0, Offset: 0,
			Type: models.SlotTypePlaylist, Payload: map[string]any{
				"playlist_id": "p1", "mount_id": mountID, "duration_ms": float64(3600000),
			},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("first scheduleStation: %v", err)
	}

	var countFirst int64
	db.Model(&models.ScheduleEntry{}).Where("station_id = ?", stationID).Count(&countFirst)

	// Second call should not create duplicates
	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("second scheduleStation: %v", err)
	}

	var countSecond int64
	db.Model(&models.ScheduleEntry{}).Where("station_id = ?", stationID).Count(&countSecond)

	if countSecond != countFirst {
		t.Fatalf("entry count changed: %d → %d (expected idempotent)", countFirst, countSecond)
	}
}

func TestScheduleStationMultipleClocksDifferentHours(t *testing.T) {
	svc, db := newIntegrationTestService(t)
	ctx := context.Background()

	stationID := "station-int-multi"
	mountID := "mount-int-multi"
	setupIntegrationStation(t, db, stationID, mountID)

	// Morning: playlist
	if err := db.Create(&models.ClockHour{
		ID: "clock-int-morning", StationID: stationID, Name: "Morning",
		StartHour: 6, EndHour: 12,
		Slots: []models.ClockSlot{{
			ID: "slot-int-morning", ClockHourID: "clock-int-morning", Position: 0, Offset: 0,
			Type: models.SlotTypePlaylist, Payload: map[string]any{
				"playlist_id": "morning-pl", "mount_id": mountID, "duration_ms": float64(3600000),
			},
		}},
	}).Error; err != nil {
		t.Fatalf("create morning clock: %v", err)
	}

	// Afternoon: webstream
	if err := db.Create(&models.ClockHour{
		ID: "clock-int-afternoon", StationID: stationID, Name: "Afternoon",
		StartHour: 12, EndHour: 18,
		Slots: []models.ClockSlot{{
			ID: "slot-int-afternoon", ClockHourID: "clock-int-afternoon", Position: 0, Offset: 0,
			Type: models.SlotTypeWebstream, Payload: map[string]any{
				"webstream_id": "afternoon-ws", "mount_id": mountID,
			},
		}},
	}).Error; err != nil {
		t.Fatalf("create afternoon clock: %v", err)
	}

	// Set lookahead to cover 6am-6pm from current time
	svc.lookahead = 24 * time.Hour

	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("scheduleStation: %v", err)
	}

	var entries []models.ScheduleEntry
	db.Where("station_id = ?", stationID).Find(&entries)

	hasPlaylist := false
	hasWebstream := false
	for _, e := range entries {
		if e.SourceType == "playlist" {
			hasPlaylist = true
		}
		if e.SourceType == "webstream" {
			hasWebstream = true
		}
	}

	if !hasPlaylist {
		t.Error("expected at least one playlist entry from morning clock")
	}
	if !hasWebstream {
		t.Error("expected at least one webstream entry from afternoon clock")
	}
}

func TestScheduleStationOvernightClock(t *testing.T) {
	svc, db := newIntegrationTestService(t)
	ctx := context.Background()

	stationID := "station-int-night"
	mountID := "mount-int-night"
	setupIntegrationStation(t, db, stationID, mountID)

	if err := db.Create(&models.ClockHour{
		ID: "clock-int-night", StationID: stationID, Name: "Night",
		StartHour: 22, EndHour: 6,
		Slots: []models.ClockSlot{{
			ID: "slot-int-night", ClockHourID: "clock-int-night", Position: 0, Offset: 0,
			Type: models.SlotTypePlaylist, Payload: map[string]any{
				"playlist_id": "night-pl", "mount_id": mountID, "duration_ms": float64(3600000),
			},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	svc.lookahead = 24 * time.Hour

	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("scheduleStation: %v", err)
	}

	var entries []models.ScheduleEntry
	db.Where("station_id = ?", stationID).Find(&entries)

	// Should have entries (the overnight clock covers some hours from now)
	// We can't predict exact count since it depends on current time, but
	// verify no errors and entries are all playlist type
	for _, e := range entries {
		if e.SourceType != "playlist" {
			t.Errorf("unexpected source_type = %q", e.SourceType)
		}
	}
}

func TestScheduleStationMissingMountGracefulSkip(t *testing.T) {
	svc, db := newIntegrationTestService(t)
	ctx := context.Background()

	stationID := "station-int-nomount"
	// Station without any mounts
	if err := db.Create(&models.Station{ID: stationID, Name: "No Mount", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	if err := db.Create(&models.ClockHour{
		ID: "clock-int-nomount", StationID: stationID, Name: "No Mount Clock",
		StartHour: 0, EndHour: 24,
		Slots: []models.ClockSlot{{
			ID: "slot-int-nomount", ClockHourID: "clock-int-nomount", Position: 0, Offset: 0,
			Type: models.SlotTypePlaylist, Payload: map[string]any{
				"playlist_id": "p1", "duration_ms": float64(3600000),
			},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	// Should not panic
	if err := svc.scheduleStation(ctx, stationID); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	var count int64
	db.Model(&models.ScheduleEntry{}).Where("station_id = ?", stationID).Count(&count)
	if count != 0 {
		t.Fatalf("expected 0 entries (no mount), got %d", count)
	}
}
