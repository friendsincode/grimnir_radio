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
