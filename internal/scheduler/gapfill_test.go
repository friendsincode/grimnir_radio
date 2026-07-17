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
