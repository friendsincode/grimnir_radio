/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// JW case — same mount, a 1h media row at 16:00-17:00 already materialized,
// the 8h block (16:00-00:00) must fill 17:00-00:00, not skip the whole block.
func TestMaterializeSmartBlockEntry_FillsUncoveredRemainder(t *testing.T) {
	svc, db := newRunTestService(t)
	stationID, mountID := "st-jw", "mt-jw"
	if err := db.Create(&models.Station{ID: stationID, Name: "Test", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{
		ID: mountID, StationID: stationID, Name: "Main",
		URL: "https://example.invalid/main.mp3", Format: "mp3",
	}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	// seed a smart block so materializeSmartBlock has something to expand
	sbID := seedMinimalSmartBlock(t, db, stationID)

	base := time.Date(2026, 7, 20, 16, 0, 0, 0, time.UTC)
	// Leftover 1h media already covering 16:00-17:00 on the same mount.
	if err := db.Create(&models.ScheduleEntry{
		ID: "leftover-1h", StationID: stationID, MountID: mountID,
		SourceType: "media", SourceID: uuid.NewString(), IsInstance: true,
		StartsAt: base, EndsAt: base.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed leftover: %v", err)
	}

	entry := models.ScheduleEntry{
		ID: "block-8h", StationID: stationID, MountID: mountID,
		SourceType: "smart_block", SourceID: sbID,
		StartsAt: base, EndsAt: base.Add(8 * time.Hour),
	}
	// now is before the block window, so the whole remainder is fillable.
	now := base.Add(-time.Hour)
	if err := svc.materializeSmartBlockEntry(context.Background(), stationID, entry, mountID, now); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	// Media rows produced by the block must start at or after 17:00, never before.
	var media []models.ScheduleEntry
	db.Where("station_id = ? AND mount_id = ? AND source_type = 'media' AND id != ?",
		stationID, mountID, "leftover-1h").Order("starts_at ASC").Find(&media)
	if len(media) == 0 {
		t.Fatal("block produced no media in the uncovered 17:00-00:00 window (skipped whole block?)")
	}
	if media[0].StartsAt.Before(base.Add(time.Hour)) {
		t.Errorf("block materialized into covered 16:00-17:00 (starts %v); should start >= 17:00", media[0].StartsAt)
	}
}

// TestMaterialize_NewestParentWinsOverlap seeds two recurring smart-block parents on the
// same station+mount with overlapping windows and different CreatedAt. The NEWER parent
// should claim the contested span; the older parent only fills the non-overlapping remainder.
// Mechanism under test: Pass 2 orders recurring parents created_at DESC so the newest
// expands and materializes first, and its produced media counts as coverage (Task 1.2) that
// the older parent then subtracts.
func TestMaterialize_NewestParentWinsOverlap(t *testing.T) {
	svc, db := newRunTestService(t)
	svc.lookahead = 48 * time.Hour
	stationID, mountID := "st-overlap", "mt-overlap"
	if err := db.Create(&models.Station{ID: stationID, Name: "Test", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{
		ID: mountID, StationID: stationID, Name: "Main",
		URL: "https://example.invalid/main.mp3", Format: "mp3",
	}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	// Two distinct engine-backed smart blocks, each with its own analyzed media.
	olderBlockID := seedNamedSmartBlock(t, db, stationID, "sb-old")
	newerBlockID := seedNamedSmartBlock(t, db, stationID, "sb-new")

	// Anchor both recurring parents on a day inside the lookahead window so they both
	// expand into the same occurrence day. Use tomorrow at fixed hours (UTC station).
	now := time.Now().UTC().Truncate(time.Hour)
	base := now.Add(24 * time.Hour).Truncate(24 * time.Hour)

	// Older parent: 12:00-16:00, created earlier.
	olderStart := base.Add(12 * time.Hour)
	olderEnd := base.Add(16 * time.Hour)
	// Newer parent: 14:00-18:00, created later. Overlap span = 14:00-16:00.
	newerStart := base.Add(14 * time.Hour)
	newerEnd := base.Add(18 * time.Hour)
	overlapStart := newerStart
	overlapEnd := olderEnd

	older := models.ScheduleEntry{
		ID: "parent-old", StationID: stationID, MountID: mountID,
		SourceType: "smart_block", SourceID: olderBlockID,
		StartsAt: olderStart, EndsAt: olderEnd,
		RecurrenceType: models.RecurrenceDaily, IsInstance: false,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	newer := models.ScheduleEntry{
		ID: "parent-new", StationID: stationID, MountID: mountID,
		SourceType: "smart_block", SourceID: newerBlockID,
		StartsAt: newerStart, EndsAt: newerEnd,
		RecurrenceType: models.RecurrenceDaily, IsInstance: false,
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	// Create older first so its DB insert order does NOT favor it; the ordering must
	// come from created_at DESC, not insertion order.
	if err := db.Create(&older).Error; err != nil {
		t.Fatalf("create older parent: %v", err)
	}
	if err := db.Create(&newer).Error; err != nil {
		t.Fatalf("create newer parent: %v", err)
	}

	if err := svc.materializeDirectScheduleEntries(context.Background(), stationID, now); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	var media []models.ScheduleEntry
	db.Where("station_id = ? AND mount_id = ? AND source_type = 'media'", stationID, mountID).
		Order("starts_at ASC").Find(&media)
	if len(media) == 0 {
		t.Fatal("no media produced by either parent")
	}

	var newerProducedInOverlap, olderProducedInOverlap bool
	for _, m := range media {
		blockID, _ := m.Metadata["smart_block_id"].(string)
		// Does this media row fall inside the contested span?
		inOverlap := m.StartsAt.Before(overlapEnd) && m.EndsAt.After(overlapStart)
		if !inOverlap {
			continue
		}
		switch blockID {
		case newerBlockID:
			newerProducedInOverlap = true
		case olderBlockID:
			olderProducedInOverlap = true
		}
	}

	if olderProducedInOverlap {
		t.Errorf("older block %q produced media in the contested span %v-%v; newer block should win it",
			olderBlockID, overlapStart, overlapEnd)
	}
	if !newerProducedInOverlap {
		t.Errorf("newer block %q produced no media in the contested span %v-%v",
			newerBlockID, overlapStart, overlapEnd)
	}
}

func seedNamedSmartBlock(t *testing.T, db *gorm.DB, stationID, sbID string) string {
	t.Helper()
	plID := uuid.NewString()
	sb := models.SmartBlock{
		ID: sbID, StationID: stationID, Name: sbID,
		// loopToFill lets the block cover its whole multi-hour window from a small
		// media pool, so each parent can physically reach the contested span; the
		// contest is then decided by processing order, not by which pool runs dry.
		Rules: map[string]any{"targetMinutes": 480, "sourcePlaylists": []string{plID}, "loopToFill": true},
	}
	if err := db.Create(&sb).Error; err != nil {
		t.Fatalf("seed smart block %s: %v", sbID, err)
	}
	if err := db.Create(&models.Playlist{ID: plID, StationID: stationID, Name: sbID + " PL"}).Error; err != nil {
		t.Fatalf("seed playlist for %s: %v", sbID, err)
	}
	// Several analyzed media items so the engine can fill a multi-hour window.
	for i := 0; i < 40; i++ {
		mediaID := uuid.NewString()
		if err := db.Create(&models.MediaItem{
			ID: mediaID, StationID: stationID, Title: sbID + "-track", Path: "/tmp/" + mediaID + ".mp3",
			Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
		}).Error; err != nil {
			t.Fatalf("seed media item for %s: %v", sbID, err)
		}
		if err := db.Create(&models.PlaylistItem{
			ID: uuid.NewString(), PlaylistID: plID, MediaID: mediaID, Position: i,
		}).Error; err != nil {
			t.Fatalf("seed playlist item for %s: %v", sbID, err)
		}
	}
	return sb.ID
}

func seedMinimalSmartBlock(t *testing.T, db *gorm.DB, stationID string) string {
	t.Helper()
	plID := uuid.NewString()
	sb := models.SmartBlock{
		ID: "sb-fill", StationID: stationID, Name: "Fill",
		Rules: map[string]any{"targetMinutes": 480, "sourcePlaylists": []string{plID}},
	}
	if err := db.Create(&sb).Error; err != nil {
		t.Fatalf("seed smart block: %v", err)
	}
	// A source playlist the block's rules resolve to.
	if err := db.Create(&models.Playlist{ID: plID, StationID: stationID, Name: "Fill PL"}).Error; err != nil {
		t.Fatalf("seed playlist: %v", err)
	}
	// A single analyzed media item the block can place.
	mediaID := uuid.NewString()
	if err := db.Create(&models.MediaItem{
		ID: mediaID, StationID: stationID, Title: "Track", Path: "/tmp/x.mp3",
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}).Error; err != nil {
		t.Fatalf("seed media item: %v", err)
	}
	if err := db.Create(&models.PlaylistItem{
		ID: uuid.NewString(), PlaylistID: plID, MediaID: mediaID, Position: 0,
	}).Error; err != nil {
		t.Fatalf("seed playlist item: %v", err)
	}
	return sb.ID
}
