/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package smartblock

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// tailFloorEngine builds a station whose only candidates are long tracks, so
// nothing fits the residue at the end of a block.
func tailFloorEngine(t *testing.T, trackLen time.Duration, count int) *Engine {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.MediaItem{}, &models.Playlist{}, &models.PlaylistItem{}, &models.SmartBlock{}, &models.PlayHistory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := db.Create(&models.Playlist{ID: "pl-tail", StationID: "st-tail", Name: "Long Form"}).Error; err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("m-tail-%d", i)
		if err := db.Create(&models.MediaItem{
			ID: id, StationID: "st-tail", Title: fmt.Sprintf("Episode %d", i),
			Duration: trackLen, AnalysisState: models.AnalysisComplete,
		}).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
		if err := db.Create(&models.PlaylistItem{
			ID: fmt.Sprintf("pi-tail-%d", i), PlaylistID: "pl-tail", MediaID: id, Position: i,
		}).Error; err != nil {
			t.Fatalf("create playlist item: %v", err)
		}
	}
	if err := db.Create(&models.SmartBlock{
		ID: "sb-tail", StationID: "st-tail", Name: "Long Form Block",
		Rules: map[string]any{
			"targetMinutes":    1440,
			"durationAccuracy": 2,
			"sourcePlaylists":  []string{"pl-tail"},
		},
	}).Error; err != nil {
		t.Fatalf("create smart block: %v", err)
	}

	return New(db, zerolog.Nop())
}

// A track that overruns the block is clamped to the boundary so the executor
// hard-cuts it, which is fine for a song losing its tail. It is not fine when
// only seconds remain and every candidate is an hour long: prod on 2026-08-12
// aired 81 such fragments under a minute each, including a 35,537-second
// reading clamped into a 0-second slot. MinTailSlotMS sets the floor.
func TestSelectSequence_DoesNotClampBelowTailFloor(t *testing.T) {
	const track = 59*time.Minute + 30*time.Second
	target := (60 * time.Minute).Milliseconds()

	eng := tailFloorEngine(t, track, 4)

	res, err := eng.Generate(context.Background(), GenerateRequest{
		SmartBlockID:  "sb-tail",
		Seed:          1,
		Duration:      target,
		StationID:     "st-tail",
		MinTailSlotMS: (60 * time.Second).Milliseconds(),
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(res.Items) == 0 {
		t.Fatal("expected the first episode to be scheduled")
	}
	for _, it := range res.Items {
		if slot := it.EndsAtMS - it.StartsAtMS; slot < (60 * time.Second).Milliseconds() {
			t.Errorf("item %s got a %dms slot, under the 60000ms floor", it.MediaID, slot)
		}
		if it.EndsAtMS > target {
			t.Errorf("item %s ends past the block", it.MediaID)
		}
	}
}

// Zero keeps the original always-clamp behaviour, so the floor is opt-in and
// an existing deployment that wants the old trade can set it back.
func TestSelectSequence_ZeroFloorStillClamps(t *testing.T) {
	const track = 59*time.Minute + 30*time.Second
	target := (60 * time.Minute).Milliseconds()

	eng := tailFloorEngine(t, track, 4)

	res, err := eng.Generate(context.Background(), GenerateRequest{
		SmartBlockID:  "sb-tail",
		Seed:          1,
		Duration:      target,
		StationID:     "st-tail",
		MinTailSlotMS: 0,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	var sawFragment bool
	for _, it := range res.Items {
		if slot := it.EndsAtMS - it.StartsAtMS; slot < (60 * time.Second).Milliseconds() {
			sawFragment = true
		}
	}
	if !sawFragment {
		t.Fatal("with the floor disabled the overrunning track should still be clamped to a sub-minute slot; " +
			"if this fails the scenario no longer reproduces the bug and the floor test above proves nothing")
	}
}
