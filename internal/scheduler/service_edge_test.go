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
)

func TestDuplicatePreventionSameSlotNotScheduledTwice(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := "station-dup"
	mountID := "mount-dup"
	createTestMount(t, db, stationID, mountID)

	startsAt := time.Now().UTC().Add(time.Minute).Truncate(time.Second)

	// Pre-create an entry
	if err := db.Create(&models.ScheduleEntry{
		ID:         "existing-entry",
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   startsAt,
		EndsAt:     startsAt.Add(3 * time.Minute),
		SourceType: "media",
		SourceID:   "media-1",
	}).Error; err != nil {
		t.Fatalf("create existing entry: %v", err)
	}

	plan := clock.SlotPlan{
		SlotID:   "slot-dup",
		StartsAt: startsAt,
		EndsAt:   startsAt.Add(3 * time.Minute),
		SlotType: string(models.SlotTypeHardItem),
		Payload:  map[string]any{"mount_id": mountID, "media_id": "media-2"},
	}

	already, err := svc.slotAlreadyScheduled(ctx, stationID, plan)
	if err != nil {
		t.Fatalf("slotAlreadyScheduled: %v", err)
	}
	if !already {
		t.Fatal("expected slotAlreadyScheduled to return true for same StartsAt+MountID")
	}
}

func TestCreatePlaylistEntryMissingPlaylistID(t *testing.T) {
	svc, db := newMaterializationTestService(t)

	stationID := "station-plmissing"
	mountID := "mount-plmissing"
	createTestMount(t, db, stationID, mountID)

	plan := clock.SlotPlan{
		SlotID:   "slot-plmissing",
		StartsAt: time.Now().UTC().Add(time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(4 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypePlaylist),
		Payload:  map[string]any{"mount_id": mountID},
	}

	if svc.validatePlanPayload(stationID, plan) {
		t.Fatal("expected validatePlanPayload to return false for missing playlist_id")
	}

	var count int64
	db.Model(&models.ScheduleEntry{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected 0 entries, got %d", count)
	}
}

func TestCreateHardItemEntryMissingMediaID(t *testing.T) {
	svc, _ := newMaterializationTestService(t)

	plan := clock.SlotPlan{
		SlotID:   "slot-himissing",
		StartsAt: time.Now().UTC().Add(time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(4 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypeHardItem),
		Payload:  map[string]any{"mount_id": "some-mount"},
	}

	if svc.validatePlanPayload("station-x", plan) {
		t.Fatal("expected validatePlanPayload to return false for missing media_id")
	}
}

func TestCreateWebstreamEntryMissingWebstreamID(t *testing.T) {
	svc, _ := newMaterializationTestService(t)

	plan := clock.SlotPlan{
		SlotID:   "slot-wsmissing",
		StartsAt: time.Now().UTC().Add(time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(4 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypeWebstream),
		Payload:  map[string]any{"mount_id": "some-mount"},
	}

	if svc.validatePlanPayload("station-x", plan) {
		t.Fatal("expected validatePlanPayload to return false for missing webstream_id")
	}
}

func TestCreateStopsetEntryMediaFallback(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := "station-stopset-media"
	mountID := "mount-stopset-media"
	mediaID := "media-stopset"
	createTestMount(t, db, stationID, mountID)

	plan := clock.SlotPlan{
		SlotID:   "slot-stopset-media",
		StartsAt: time.Now().UTC().Add(time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(3 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypeStopset),
		Payload:  map[string]any{"mount_id": mountID, "media_id": mediaID},
	}

	if err := svc.createStopsetEntry(ctx, stationID, plan); err != nil {
		t.Fatalf("createStopsetEntry: %v", err)
	}

	var entry models.ScheduleEntry
	if err := db.First(&entry).Error; err != nil {
		t.Fatalf("load entry: %v", err)
	}
	if entry.SourceType != "media" {
		t.Fatalf("source_type = %q, want %q", entry.SourceType, "media")
	}
	if entry.SourceID != mediaID {
		t.Fatalf("source_id = %q, want %q", entry.SourceID, mediaID)
	}
}

func TestCreateStopsetEntryNoSource(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := "station-stopset-none"
	mountID := "mount-stopset-none"
	createTestMount(t, db, stationID, mountID)

	plan := clock.SlotPlan{
		SlotID:   "slot-stopset-none",
		StartsAt: time.Now().UTC().Add(time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(3 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypeStopset),
		Payload:  map[string]any{"mount_id": mountID},
	}

	if err := svc.createStopsetEntry(ctx, stationID, plan); err != nil {
		t.Fatalf("createStopsetEntry: %v", err)
	}

	var entry models.ScheduleEntry
	if err := db.First(&entry).Error; err != nil {
		t.Fatalf("load entry: %v", err)
	}
	if entry.SourceType != "stopset" {
		t.Fatalf("source_type = %q, want %q", entry.SourceType, "stopset")
	}
	if entry.SourceID != "" {
		t.Fatalf("source_id = %q, want empty", entry.SourceID)
	}
}

func TestDefaultMountIDResolution(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := "station-defmount"
	mountID := "mount-default"
	createTestMount(t, db, stationID, mountID)

	got := svc.getDefaultMountID(ctx, stationID)
	if got != mountID {
		t.Fatalf("getDefaultMountID = %q, want %q", got, mountID)
	}
}

func TestNoMountAvailableSkipsEntry(t *testing.T) {
	svc, _ := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := "station-nomount"

	plan := clock.SlotPlan{
		SlotID:   "slot-nomount",
		StartsAt: time.Now().UTC().Add(time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(3 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypeHardItem),
		Payload:  map[string]any{"media_id": "media-1"},
	}

	// createHardItemEntry should silently skip (return nil) when no mount available
	err := svc.createHardItemEntry(ctx, stationID, plan)
	if err != nil {
		t.Fatalf("expected nil error for missing mount, got: %v", err)
	}
}

func TestPickRandomTrackSuccess(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	// Need MediaItem table
	if err := db.AutoMigrate(&models.MediaItem{}); err != nil {
		t.Fatalf("migrate media: %v", err)
	}

	stationID := "station-pick"
	mountID := "mount-pick"
	createTestMount(t, db, stationID, mountID)

	if err := db.Create(&models.MediaItem{
		ID: "media-analyzed", StationID: stationID,
		Title: "Test", Duration: 3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	plan := clock.SlotPlan{
		SlotID:   "slot-pick",
		StartsAt: time.Now().UTC().Add(time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(4 * time.Minute).Truncate(time.Second),
	}

	if err := svc.pickRandomTrack(ctx, stationID, mountID, plan); err != nil {
		t.Fatalf("pickRandomTrack: %v", err)
	}

	var entry models.ScheduleEntry
	if err := db.First(&entry).Error; err != nil {
		t.Fatalf("load entry: %v", err)
	}
	if entry.SourceID != "media-analyzed" {
		t.Fatalf("source_id = %q, want %q", entry.SourceID, "media-analyzed")
	}
	if v, ok := entry.Metadata["emergency_fallback"].(bool); !ok || !v {
		t.Fatal("expected metadata.emergency_fallback = true")
	}
}

func TestPickRandomTrackNoMedia(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	if err := db.AutoMigrate(&models.MediaItem{}); err != nil {
		t.Fatalf("migrate media: %v", err)
	}

	plan := clock.SlotPlan{
		SlotID:   "slot-nopick",
		StartsAt: time.Now().UTC().Add(time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(4 * time.Minute).Truncate(time.Second),
	}

	err := svc.pickRandomTrack(ctx, "station-empty", "mount-empty", plan)
	if err == nil {
		t.Fatal("expected error when no media available")
	}
}

func TestPickRandomTrackIgnoresFailedAnalysis(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	if err := db.AutoMigrate(&models.MediaItem{}); err != nil {
		t.Fatalf("migrate media: %v", err)
	}

	stationID := "station-ignore-failed"
	mountID := "mount-ignore-failed"
	createTestMount(t, db, stationID, mountID)

	// Failed media - should be excluded
	if err := db.Create(&models.MediaItem{
		ID: "media-failed", StationID: stationID,
		Title: "Failed", Duration: 3 * time.Minute,
		AnalysisState: models.AnalysisFailed,
	}).Error; err != nil {
		t.Fatalf("create failed media: %v", err)
	}

	// Complete media - should be picked
	if err := db.Create(&models.MediaItem{
		ID: "media-ok", StationID: stationID,
		Title: "OK", Duration: 3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}).Error; err != nil {
		t.Fatalf("create ok media: %v", err)
	}

	plan := clock.SlotPlan{
		SlotID:   "slot-ignore",
		StartsAt: time.Now().UTC().Add(time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(4 * time.Minute).Truncate(time.Second),
	}

	if err := svc.pickRandomTrack(ctx, stationID, mountID, plan); err != nil {
		t.Fatalf("pickRandomTrack: %v", err)
	}

	var entry models.ScheduleEntry
	if err := db.First(&entry).Error; err != nil {
		t.Fatalf("load entry: %v", err)
	}
	if entry.SourceID != "media-ok" {
		t.Fatalf("source_id = %q, want %q (failed media should be excluded)", entry.SourceID, "media-ok")
	}
}

func TestValidatePlanPayloadTable(t *testing.T) {
	svc, _ := newMaterializationTestService(t)

	tests := []struct {
		name     string
		slotType string
		payload  map[string]any
		valid    bool
	}{
		{"hard_item with media_id", string(models.SlotTypeHardItem), map[string]any{"media_id": "m1"}, true},
		{"hard_item missing media_id", string(models.SlotTypeHardItem), map[string]any{}, false},
		{"playlist with playlist_id", string(models.SlotTypePlaylist), map[string]any{"playlist_id": "p1"}, true},
		{"playlist missing playlist_id", string(models.SlotTypePlaylist), map[string]any{}, false},
		{"webstream with webstream_id", string(models.SlotTypeWebstream), map[string]any{"webstream_id": "w1"}, true},
		{"webstream missing webstream_id", string(models.SlotTypeWebstream), map[string]any{}, false},
		{"smart_block always valid", string(models.SlotTypeSmartBlock), map[string]any{}, true},
		{"stopset always valid", string(models.SlotTypeStopset), map[string]any{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := clock.SlotPlan{
				SlotID:   "slot-validate",
				SlotType: tt.slotType,
				Payload:  tt.payload,
			}
			got := svc.validatePlanPayload("station-test", plan)
			if got != tt.valid {
				t.Errorf("validatePlanPayload() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestWebstreamEntryPreservesTimeSpan(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := "station-ws-span"
	mountID := "mount-ws-span"
	createTestMount(t, db, stationID, mountID)

	startsAt := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	endsAt := time.Date(2026, 3, 1, 16, 0, 0, 0, time.UTC)

	plan := clock.SlotPlan{
		SlotID:   "slot-ws-span",
		StartsAt: startsAt,
		EndsAt:   endsAt,
		SlotType: string(models.SlotTypeWebstream),
		Payload:  map[string]any{"mount_id": mountID, "webstream_id": "ws1"},
	}

	if err := svc.createWebstreamEntry(ctx, stationID, plan); err != nil {
		t.Fatalf("createWebstreamEntry: %v", err)
	}

	var entry models.ScheduleEntry
	if err := db.First(&entry).Error; err != nil {
		t.Fatalf("load entry: %v", err)
	}
	if !entry.StartsAt.Equal(startsAt) {
		t.Errorf("StartsAt = %v, want %v", entry.StartsAt, startsAt)
	}
	if !entry.EndsAt.Equal(endsAt) {
		t.Errorf("EndsAt = %v, want %v", entry.EndsAt, endsAt)
	}
}

func TestPlaylistEntryPreservesPayloadMetadata(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := "station-pl-meta"
	mountID := "mount-pl-meta"
	createTestMount(t, db, stationID, mountID)

	plan := clock.SlotPlan{
		SlotID:   "slot-pl-meta",
		StartsAt: time.Now().UTC().Add(time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(4 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypePlaylist),
		Payload: map[string]any{
			"mount_id":    mountID,
			"playlist_id": "p1",
			"custom_key":  "custom_value",
		},
	}

	if err := svc.createPlaylistEntry(ctx, stationID, plan); err != nil {
		t.Fatalf("createPlaylistEntry: %v", err)
	}

	var entry models.ScheduleEntry
	if err := db.First(&entry).Error; err != nil {
		t.Fatalf("load entry: %v", err)
	}
	if v, _ := entry.Metadata["custom_key"].(string); v != "custom_value" {
		t.Errorf("metadata.custom_key = %q, want %q", v, "custom_value")
	}
	if v, _ := entry.Metadata["playlist_id"].(string); v != "p1" {
		t.Errorf("metadata.playlist_id = %q, want %q", v, "p1")
	}
}
