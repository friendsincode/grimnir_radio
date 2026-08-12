/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/clock"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
)

// trimFloorDB builds an in-memory station with one smart block sourced from a
// playlist of the given track lengths, in order. Distinct tracks are required
// to get more than one generated item: the engine will not repeat a single
// track to fill a block, so a one-track playlist always yields one item and
// never reaches the boundary logic under test.
func trimFloorDB(t *testing.T, trackLens []time.Duration, targetMinutes int) (*gorm.DB, *Service) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Mount{}, &models.ScheduleEntry{}, &models.MediaItem{},
		&models.Playlist{}, &models.PlaylistItem{}, &models.SmartBlock{}, &models.PlayHistory{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	createTestMount(t, db, "station-floor", "mount-floor")

	if err := db.Create(&models.Playlist{ID: "pl-floor", StationID: "station-floor", Name: "PL"}).Error; err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	for i, d := range trackLens {
		mediaID := fmt.Sprintf("media-floor-%d", i)
		if err := db.Create(&models.MediaItem{
			ID: mediaID, StationID: "station-floor", Title: fmt.Sprintf("Track %d", i),
			Duration: d, AnalysisState: models.AnalysisComplete,
		}).Error; err != nil {
			t.Fatalf("create media %d: %v", i, err)
		}
		if err := db.Create(&models.PlaylistItem{
			ID: fmt.Sprintf("pi-floor-%d", i), PlaylistID: "pl-floor", MediaID: mediaID, Position: i,
		}).Error; err != nil {
			t.Fatalf("create playlist item %d: %v", i, err)
		}
	}
	if err := db.Create(&models.SmartBlock{
		ID: "sb-floor", StationID: "station-floor", Name: "Block",
		Rules: map[string]any{
			"targetMinutes":    targetMinutes,
			"durationAccuracy": 2,
			"sourcePlaylists":  []string{"pl-floor"},
		},
	}).Error; err != nil {
		t.Fatalf("create smart block: %v", err)
	}

	svc := &Service{
		db:             db,
		engine:         smartblock.New(db, zerolog.Nop()),
		logger:         zerolog.Nop(),
		minTrimmedSlot: 60 * time.Second,
	}
	return db, svc
}

func floorPlan(slotStart time.Time, slotLen time.Duration) clock.SlotPlan {
	return clock.SlotPlan{
		SlotID:   "slot-floor",
		StartsAt: slotStart,
		EndsAt:   slotStart.Add(slotLen),
		Duration: slotLen,
		SlotType: string(models.SlotTypeSmartBlock),
		Payload:  map[string]any{"mount_id": "mount-floor", "smart_block_id": "sb-floor"},
	}
}

// An item that lands with only seconds left before the block boundary must not
// be scheduled at all. Before the floor, a two-hour show could be given a
// one-second slot: prod carried 121 such entries in a 36-hour window, 81 of
// them airing under a minute, which is what an operator hears as the stream
// playing a moment of a show and jumping to a promo.
func TestMaterializeSmartBlock_SkipsFragmentAtBoundary(t *testing.T) {
	// Mirrors the prod configuration that produced the fragments: the smart
	// blocks target 1440 minutes (a full day) while the slot they materialize
	// into is one hour, so the engine lays out far more content than the block
	// holds and the tail lands across the boundary. Three distinct 59m30s
	// tracks put the second item 30s before the boundary, under the floor.
	track := 59*time.Minute + 30*time.Second
	db, svc := trimFloorDB(t, []time.Duration{track, track, track}, 1440)

	slotStart := time.Date(2026, 3, 17, 5, 0, 0, 0, time.UTC)
	plan := floorPlan(slotStart, 60*time.Minute)

	if err := svc.materializeSmartBlock(context.Background(), "station-floor", plan); err != nil {
		t.Fatalf("materializeSmartBlock: %v", err)
	}

	var entries []models.ScheduleEntry
	if err := db.Find(&entries).Error; err != nil {
		t.Fatalf("load entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected the first item to be scheduled")
	}
	for _, e := range entries {
		slot := e.EndsAt.Sub(e.StartsAt)
		// A trimmed entry is one cut short at the boundary. Those must clear the floor.
		if e.EndsAt.Equal(plan.EndsAt) && slot < svc.minTrimmedSlot {
			t.Errorf("entry %s was trimmed to a %v slot, under the %v floor", e.ID, slot, svc.minTrimmedSlot)
		}
		if e.EndsAt.After(plan.EndsAt) {
			t.Errorf("entry %s ends %v past the block boundary", e.ID, e.EndsAt.Sub(plan.EndsAt))
		}
	}
}

// The floor gates only the trim branch. An item shorter than the floor that
// fits entirely inside the block is normal programming (a 20-second bumper)
// and must still be scheduled.
func TestMaterializeSmartBlock_ShortItemThatFitsIsStillScheduled(t *testing.T) {
	db, svc := trimFloorDB(t, []time.Duration{30 * time.Second}, 1)

	slotStart := time.Date(2026, 3, 17, 5, 0, 0, 0, time.UTC)
	plan := floorPlan(slotStart, 60*time.Minute)

	if err := svc.materializeSmartBlock(context.Background(), "station-floor", plan); err != nil {
		t.Fatalf("materializeSmartBlock: %v", err)
	}

	var entries []models.ScheduleEntry
	if err := db.Find(&entries).Error; err != nil {
		t.Fatalf("load entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("a 30s item that fits inside a 60m block must still be scheduled, got none")
	}
	for _, e := range entries {
		if e.EndsAt.After(plan.EndsAt) {
			t.Errorf("entry %s ends past the boundary", e.ID)
		}
	}
}

func TestResolveMinTrimmedSlot(t *testing.T) {
	clear := func(t *testing.T) {
		t.Helper()
		for _, k := range MinTrimmedSlotEnvKeys {
			t.Setenv(k, "")
		}
	}

	t.Run("default when unset", func(t *testing.T) {
		clear(t)
		if got := resolveMinTrimmedSlot(); got != defaultMinTrimmedSlot {
			t.Fatalf("got %v, want %v", got, defaultMinTrimmedSlot)
		}
	})

	for _, key := range MinTrimmedSlotEnvKeys {
		t.Run("override via "+key, func(t *testing.T) {
			clear(t)
			t.Setenv(key, "300")
			if got := resolveMinTrimmedSlot(); got != 300*time.Second {
				t.Fatalf("got %v, want 5m", got)
			}
		})
	}

	t.Run("zero disables the floor", func(t *testing.T) {
		clear(t)
		t.Setenv(MinTrimmedSlotEnvKeys[0], "0")
		if got := resolveMinTrimmedSlot(); got != 0 {
			t.Fatalf("got %v, want 0 (disabled)", got)
		}
	})

	t.Run("unparseable falls back to default", func(t *testing.T) {
		clear(t)
		t.Setenv(MinTrimmedSlotEnvKeys[0], "a minute or so")
		if got := resolveMinTrimmedSlot(); got != defaultMinTrimmedSlot {
			t.Fatalf("got %v, want %v", got, defaultMinTrimmedSlot)
		}
	})

	t.Run("first key wins", func(t *testing.T) {
		clear(t)
		t.Setenv(MinTrimmedSlotEnvKeys[0], "120")
		t.Setenv(MinTrimmedSlotEnvKeys[1], "999")
		if got := resolveMinTrimmedSlot(); got != 120*time.Second {
			t.Fatalf("got %v, want 2m", got)
		}
	})
}
