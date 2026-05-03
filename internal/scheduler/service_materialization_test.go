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
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newMaterializationTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&models.Mount{}, &models.ScheduleEntry{}); err != nil {
		t.Fatalf("failed to migrate schema: %v", err)
	}

	svc := &Service{
		db:     db,
		logger: zerolog.Nop(),
	}
	return svc, db
}

func createTestMount(t *testing.T, db *gorm.DB, stationID, mountID string) {
	t.Helper()
	m := models.Mount{
		ID:        mountID,
		StationID: stationID,
		Name:      "Main",
		URL:       "https://example.invalid/main.mp3",
		Format:    "mp3",
	}
	if err := db.Create(&m).Error; err != nil {
		t.Fatalf("failed to create mount: %v", err)
	}
}

func TestCreateHardItemEntry(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := "station-hard"
	mountID := "mount-hard"
	mediaID := "media-hard"
	createTestMount(t, db, stationID, mountID)

	plan := clock.SlotPlan{
		SlotID:   "slot-hard",
		StartsAt: time.Now().UTC().Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(3 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypeHardItem),
		Payload: map[string]any{
			"mount_id":  mountID,
			"media_id":  mediaID,
			"slot_name": "Hard Item Slot",
		},
	}

	if err := svc.createHardItemEntry(ctx, stationID, plan); err != nil {
		t.Fatalf("createHardItemEntry returned error: %v", err)
	}

	var entry models.ScheduleEntry
	if err := db.First(&entry).Error; err != nil {
		t.Fatalf("failed to load schedule entry: %v", err)
	}
	if entry.SourceType != "media" {
		t.Fatalf("source_type = %q, want %q", entry.SourceType, "media")
	}
	if entry.SourceID != mediaID {
		t.Fatalf("source_id = %q, want %q", entry.SourceID, mediaID)
	}
	if got, _ := entry.Metadata["slot_type"].(string); got != string(models.SlotTypeHardItem) {
		t.Fatalf("metadata.slot_type = %q, want %q", got, string(models.SlotTypeHardItem))
	}
}

func TestCreateStopsetEntryPrefersPlaylistSource(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := "station-stopset"
	mountID := "mount-stopset"
	playlistID := "playlist-stopset"
	mediaID := "media-stopset"
	createTestMount(t, db, stationID, mountID)

	plan := clock.SlotPlan{
		SlotID:   "slot-stopset",
		StartsAt: time.Now().UTC().Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(2 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypeStopset),
		Payload: map[string]any{
			"mount_id":    mountID,
			"playlist_id": playlistID,
			"media_id":    mediaID,
		},
	}

	if err := svc.createStopsetEntry(ctx, stationID, plan); err != nil {
		t.Fatalf("createStopsetEntry returned error: %v", err)
	}

	var entry models.ScheduleEntry
	if err := db.First(&entry).Error; err != nil {
		t.Fatalf("failed to load schedule entry: %v", err)
	}
	if entry.SourceType != "playlist" {
		t.Fatalf("source_type = %q, want %q", entry.SourceType, "playlist")
	}
	if entry.SourceID != playlistID {
		t.Fatalf("source_id = %q, want %q", entry.SourceID, playlistID)
	}
	if got, _ := entry.Metadata["slot_type"].(string); got != string(models.SlotTypeStopset) {
		t.Fatalf("metadata.slot_type = %q, want %q", got, string(models.SlotTypeStopset))
	}
}

func TestMaterializeSmartBlock_TracksClippedToSlotBoundary(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Mount{},
		&models.ScheduleEntry{},
		&models.MediaItem{},
		&models.Playlist{},
		&models.PlaylistItem{},
		&models.SmartBlock{},
		&models.PlayHistory{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	const (
		stationID = "station-clip"
		mountID   = "mount-clip"
		blockID   = "sb-clip"
		mediaID   = "media-clip-long"
	)

	createTestMount(t, db, stationID, mountID)

	// One 90-minute track — longer than the 60-minute slot.
	track := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Long Track",
		Duration:      90 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&track).Error; err != nil {
		t.Fatalf("create media item: %v", err)
	}

	pl := models.Playlist{ID: "pl-clip", StationID: stationID, Name: "Clip Test Playlist"}
	if err := db.Create(&pl).Error; err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	pi := models.PlaylistItem{ID: "pi-clip", PlaylistID: pl.ID, MediaID: mediaID, Position: 0}
	if err := db.Create(&pi).Error; err != nil {
		t.Fatalf("create playlist item: %v", err)
	}

	sb := models.SmartBlock{
		ID:        blockID,
		StationID: stationID,
		Name:      "Clip Test Block",
		Rules: map[string]any{
			"targetMinutes":    90,
			"durationAccuracy": 2,
			"sourcePlaylists":  []string{pl.ID},
		},
	}
	if err := db.Create(&sb).Error; err != nil {
		t.Fatalf("create smart block: %v", err)
	}

	eng := smartblock.New(db, zerolog.Nop())
	svc := &Service{
		db:     db,
		engine: eng,
		logger: zerolog.Nop(),
	}

	slotStart := time.Date(2026, 3, 17, 5, 0, 0, 0, time.UTC)
	slotEnd := slotStart.Add(60 * time.Minute)
	plan := clock.SlotPlan{
		SlotID:   "slot-clip",
		StartsAt: slotStart,
		EndsAt:   slotEnd,
		Duration: 60 * time.Minute,
		SlotType: string(models.SlotTypeSmartBlock),
		Payload: map[string]any{
			"mount_id":       mountID,
			"smart_block_id": blockID,
		},
	}

	if err := svc.materializeSmartBlock(context.Background(), stationID, plan); err != nil {
		t.Fatalf("materializeSmartBlock returned error: %v", err)
	}

	var entries []models.ScheduleEntry
	if err := db.Find(&entries).Error; err != nil {
		t.Fatalf("load entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one schedule entry")
	}
	for _, e := range entries {
		if e.EndsAt.After(slotEnd) {
			t.Errorf("entry %s ends_at %v exceeds slot boundary %v", e.ID, e.EndsAt, slotEnd)
		}
	}
}

func TestMaterializeDirectSmartBlock_SkipsWhenPlaylistOccupiesMount(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	svc.lookahead = 48 * time.Hour
	stationID := "station-sb-playlist-skip"
	mountID := "mount-sb-playlist-skip"
	now := time.Now().UTC().Truncate(time.Minute)
	ctx := context.Background()

	createTestMount(t, db, stationID, mountID)

	// Playlist already occupies this mount+window
	playlist := models.ScheduleEntry{
		ID:         "playlist-entry-skip",
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "playlist",
		SourceID:   "pl-skip-01",
		StartsAt:   now.Add(-8 * time.Minute),
		EndsAt:     now.Add(2 * time.Hour),
	}
	if err := db.Create(&playlist).Error; err != nil {
		t.Fatalf("create playlist entry: %v", err)
	}

	// Recurring smart_block parent on same mount
	sbParent := models.ScheduleEntry{
		ID:             "sb-parent-skip",
		StationID:      stationID,
		MountID:        mountID,
		SourceType:     "smart_block",
		SourceID:       "sb-source-skip",
		StartsAt:       now.Add(-time.Hour),
		EndsAt:         now.Add(time.Hour),
		RecurrenceType: models.RecurrenceWeekly,
		IsInstance:     false,
	}
	if err := db.Create(&sbParent).Error; err != nil {
		t.Fatalf("create smart_block parent entry: %v", err)
	}

	if err := svc.materializeDirectSmartBlockEntries(ctx, stationID, now); err != nil {
		t.Fatalf("materializeDirectSmartBlockEntries returned error: %v", err)
	}

	var count int64
	db.Model(&models.ScheduleEntry{}).
		Where("station_id = ? AND source_type = 'media'", stationID).
		Count(&count)
	if count != 0 {
		t.Errorf("expected 0 media entries (smart_block should not materialize when playlist occupies same mount), got %d", count)
	}
}

func TestCreateWebstreamEntry(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := "station-web"
	mountID := "mount-web"
	webstreamID := "webstream-abc"
	createTestMount(t, db, stationID, mountID)

	plan := clock.SlotPlan{
		SlotID:   "slot-webstream",
		StartsAt: time.Now().UTC().Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypeWebstream),
		Payload: map[string]any{
			"mount_id":     mountID,
			"webstream_id": webstreamID,
		},
	}

	if err := svc.createWebstreamEntry(ctx, stationID, plan); err != nil {
		t.Fatalf("createWebstreamEntry returned error: %v", err)
	}

	var entry models.ScheduleEntry
	if err := db.First(&entry).Error; err != nil {
		t.Fatalf("failed to load schedule entry: %v", err)
	}
	if entry.SourceType != "webstream" {
		t.Fatalf("source_type = %q, want %q", entry.SourceType, "webstream")
	}
	if entry.SourceID != webstreamID {
		t.Fatalf("source_id = %q, want %q", entry.SourceID, webstreamID)
	}
	if got, _ := entry.Metadata["slot_type"].(string); got != string(models.SlotTypeWebstream) {
		t.Fatalf("metadata.slot_type = %q, want %q", got, string(models.SlotTypeWebstream))
	}
}
