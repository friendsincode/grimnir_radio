/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package clock

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newPlannerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}, &models.ClockHour{}, &models.ClockSlot{}); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	return db
}

func TestCompileSelectsClockByHourWindow(t *testing.T) {
	db := newPlannerTestDB(t)
	planner := NewPlanner(db, zerolog.Nop())

	stationID := "station-1"
	if err := db.Create(&models.Station{ID: stationID, Name: "Test", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	morning := models.ClockHour{
		ID:        "clock-morning",
		StationID: stationID,
		Name:      "Morning",
		StartHour: 6,
		EndHour:   12,
		Slots: []models.ClockSlot{
			{
				ID:          "slot-morning",
				ClockHourID: "clock-morning",
				Position:    0,
				Offset:      0,
				Type:        models.SlotTypePlaylist,
				Payload:     map[string]any{"playlist_id": "p1"},
			},
		},
	}
	afternoon := models.ClockHour{
		ID:        "clock-afternoon",
		StationID: stationID,
		Name:      "Afternoon",
		StartHour: 12,
		EndHour:   24,
		Slots: []models.ClockSlot{
			{
				ID:          "slot-afternoon",
				ClockHourID: "clock-afternoon",
				Position:    0,
				Offset:      0,
				Type:        models.SlotTypePlaylist,
				Payload:     map[string]any{"playlist_id": "p2"},
			},
		},
	}
	if err := db.Create(&morning).Error; err != nil {
		t.Fatalf("create morning clock: %v", err)
	}
	if err := db.Create(&afternoon).Error; err != nil {
		t.Fatalf("create afternoon clock: %v", err)
	}

	start := time.Date(2026, 2, 25, 10, 30, 0, 0, time.UTC)
	plans, err := planner.Compile(stationID, start, 4*time.Hour)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if len(plans) != 4 {
		t.Fatalf("plans len = %d, want 4", len(plans))
	}
	wantStarts := []time.Time{
		time.Date(2026, 2, 25, 11, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 25, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 25, 13, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 25, 14, 0, 0, 0, time.UTC),
	}
	wantSlots := []string{"slot-morning", "slot-afternoon", "slot-afternoon", "slot-afternoon"}
	for i, plan := range plans {
		if !plan.StartsAt.Equal(wantStarts[i]) {
			t.Fatalf("plan[%d].StartsAt = %v, want %v", i, plan.StartsAt, wantStarts[i])
		}
		if plan.SlotID != wantSlots[i] {
			t.Fatalf("plan[%d].SlotID = %q, want %q", i, plan.SlotID, wantSlots[i])
		}
	}
}

func TestCompileNarrowClockBeats24HourFallback(t *testing.T) {
	db := newPlannerTestDB(t)
	planner := NewPlanner(db, zerolog.Nop())

	stationID := "station-narrow"
	if err := db.Create(&models.Station{ID: stationID, Name: "Narrow", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	// Create 24-hour fallback clock FIRST (broader window)
	fallback := models.ClockHour{
		ID:        "clock-fallback",
		StationID: stationID,
		Name:      "All Day Fallback",
		StartHour: 0,
		EndHour:   24,
		Slots: []models.ClockSlot{
			{
				ID:          "slot-fallback",
				ClockHourID: "clock-fallback",
				Position:    0,
				Offset:      0,
				Type:        models.SlotTypePlaylist,
				Payload:     map[string]any{"playlist_id": "fallback"},
			},
		},
	}
	if err := db.Create(&fallback).Error; err != nil {
		t.Fatalf("create fallback clock: %v", err)
	}

	// Create narrow morning clock SECOND (should still win for hours 6-12)
	morning := models.ClockHour{
		ID:        "clock-morning-narrow",
		StationID: stationID,
		Name:      "Morning Show",
		StartHour: 6,
		EndHour:   12,
		Slots: []models.ClockSlot{
			{
				ID:          "slot-morning-narrow",
				ClockHourID: "clock-morning-narrow",
				Position:    0,
				Offset:      0,
				Type:        models.SlotTypePlaylist,
				Payload:     map[string]any{"playlist_id": "morning"},
			},
		},
	}
	if err := db.Create(&morning).Error; err != nil {
		t.Fatalf("create morning clock: %v", err)
	}

	// Compile from 5:30 to 13:30
	// Hour 5 plan (5:00) is before start (5:30), so filtered out.
	// Expected: morning for 6-11, fallback for 12-13 = 8 plans
	start := time.Date(2026, 2, 25, 5, 30, 0, 0, time.UTC)
	plans, err := planner.Compile(stationID, start, 8*time.Hour)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if len(plans) != 8 {
		t.Fatalf("plans len = %d, want 8", len(plans))
	}

	// Hours 6-11 → morning (narrower window wins over 24hr fallback)
	for i := 0; i < 6; i++ {
		if plans[i].SlotID != "slot-morning-narrow" {
			t.Errorf("plan[%d] slot = %q, want slot-morning-narrow", i, plans[i].SlotID)
		}
	}
	// Hours 12-13 → fallback (morning window ends at 12)
	for i := 6; i < 8; i++ {
		if plans[i].SlotID != "slot-fallback" {
			t.Errorf("plan[%d] slot = %q, want slot-fallback", i, plans[i].SlotID)
		}
	}
}

func TestCompileSupportsOvernightClockWindow(t *testing.T) {
	db := newPlannerTestDB(t)
	planner := NewPlanner(db, zerolog.Nop())

	stationID := "station-2"
	if err := db.Create(&models.Station{ID: stationID, Name: "Night", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	overnight := models.ClockHour{
		ID:        "clock-overnight",
		StationID: stationID,
		Name:      "Overnight",
		StartHour: 22,
		EndHour:   2,
		Slots: []models.ClockSlot{
			{
				ID:          "slot-overnight",
				ClockHourID: "clock-overnight",
				Position:    0,
				Offset:      0,
				Type:        models.SlotTypePlaylist,
				Payload:     map[string]any{"playlist_id": "p3"},
			},
		},
	}
	if err := db.Create(&overnight).Error; err != nil {
		t.Fatalf("create overnight clock: %v", err)
	}

	start := time.Date(2026, 2, 25, 21, 20, 0, 0, time.UTC)
	plans, err := planner.Compile(stationID, start, 6*time.Hour)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if len(plans) != 4 {
		t.Fatalf("plans len = %d, want 4", len(plans))
	}
	wantStarts := []time.Time{
		time.Date(2026, 2, 25, 22, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 25, 23, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 26, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 26, 1, 0, 0, 0, time.UTC),
	}
	for i, plan := range plans {
		if !plan.StartsAt.Equal(wantStarts[i]) {
			t.Fatalf("plan[%d].StartsAt = %v, want %v", i, plan.StartsAt, wantStarts[i])
		}
	}
}
