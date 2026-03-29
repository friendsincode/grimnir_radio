/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
)

// ── startClockEntry: playlist slot ───────────────────────────────────────

func TestStartClockEntry_PlaylistSlot_PlaylistNotFound_ReturnsError(t *testing.T) {
	d, _ := newMockDirector(t, &models.ClockHour{}, &models.ClockSlot{}, &models.Playlist{}, &models.PlaylistItem{})
	ctx := context.Background()

	stationID := uuid.NewString()
	clockID := uuid.NewString()

	clock := models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Playlist Clock",
	}
	if err := d.db.Create(&clock).Error; err != nil {
		t.Fatalf("seed clock: %v", err)
	}

	slot := models.ClockSlot{
		ID:          uuid.NewString(),
		ClockHourID: clockID,
		Position:    0,
		Type:        models.SlotTypePlaylist,
		Payload:     map[string]any{"playlist_id": uuid.NewString()}, // not in DB
	}
	if err := d.db.Create(&slot).Error; err != nil {
		t.Fatalf("seed slot: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    uuid.NewString(),
		SourceType: "clock_template",
		SourceID:   clockID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startClockEntry(ctx, entry)
	if err == nil {
		t.Error("expected error for clock playlist slot with missing playlist")
	}
}

func TestStartClockEntry_PlaylistSlot_WithMedia_SetsActiveState(t *testing.T) {
	d, _ := newMockDirector(t, &models.ClockHour{}, &models.ClockSlot{}, &models.Playlist{}, &models.PlaylistItem{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	clockID := uuid.NewString()
	playlistID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "clock-playlist-main",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Clock Playlist Track",
		Path:          "/tmp/clock-playlist.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	playlist := models.Playlist{
		ID:        playlistID,
		StationID: stationID,
		Name:      "Clock Playlist",
	}
	if err := d.db.Create(&playlist).Error; err != nil {
		t.Fatalf("seed playlist: %v", err)
	}

	item := models.PlaylistItem{
		ID:         uuid.NewString(),
		PlaylistID: playlistID,
		MediaID:    mediaID,
		Position:   0,
	}
	if err := d.db.Create(&item).Error; err != nil {
		t.Fatalf("seed playlist item: %v", err)
	}

	clock := models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Playlist Clock",
	}
	if err := d.db.Create(&clock).Error; err != nil {
		t.Fatalf("seed clock: %v", err)
	}

	slot := models.ClockSlot{
		ID:          uuid.NewString(),
		ClockHourID: clockID,
		Position:    0,
		Type:        models.SlotTypePlaylist,
		Payload:     map[string]any{"playlist_id": playlistID},
	}
	if err := d.db.Create(&slot).Error; err != nil {
		t.Fatalf("seed slot: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "clock_template",
		SourceID:   clockID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startClockEntry(ctx, entry)
	if err != nil {
		t.Errorf("startClockEntry with playlist slot returned error: %v", err)
	}

	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after clock playlist slot")
	}
	if state.MediaID != mediaID {
		t.Errorf("MediaID = %q, want %q", state.MediaID, mediaID)
	}
}

// ── startClockEntry: smart_block slot ────────────────────────────────────

func TestStartClockEntry_SmartBlockSlot_WithMedia_Succeeds(t *testing.T) {
	d, _ := newMockDirector(t, &models.ClockHour{}, &models.ClockSlot{}, &models.SmartBlock{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	clockID := uuid.NewString()
	blockID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "clock-block-main",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	// Smart block engine needs media but will fail rule evaluation → returns nil (no error)
	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Block Track",
		Path:          "/tmp/block.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	block := models.SmartBlock{
		ID:        blockID,
		StationID: stationID,
		Name:      "Test Block",
	}
	if err := d.db.Create(&block).Error; err != nil {
		t.Fatalf("seed smart block: %v", err)
	}

	clock := models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Smart Block Clock",
	}
	if err := d.db.Create(&clock).Error; err != nil {
		t.Fatalf("seed clock: %v", err)
	}

	slot := models.ClockSlot{
		ID:          uuid.NewString(),
		ClockHourID: clockID,
		Position:    0,
		Type:        models.SlotTypeSmartBlock,
		Payload:     map[string]any{"smart_block_id": blockID},
	}
	if err := d.db.Create(&slot).Error; err != nil {
		t.Fatalf("seed slot: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "clock_template",
		SourceID:   clockID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	// Smart block engine fails (no rules) → returns nil
	err := d.startClockEntry(ctx, entry)
	if err != nil {
		t.Errorf("startClockEntry with smart_block slot returned error: %v", err)
	}
}

// ── startClockEntry: stopset slot with playlist_id ────────────────────────

func TestStartClockEntry_StopsetSlot_WithPlaylistID_SetsActiveState(t *testing.T) {
	d, _ := newMockDirector(t, &models.ClockHour{}, &models.ClockSlot{}, &models.Playlist{}, &models.PlaylistItem{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	clockID := uuid.NewString()
	playlistID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "stopset-playlist",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Stopset Track",
		Path:          "/tmp/stopset.mp3",
		Duration:      30 * time.Second,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	playlist := models.Playlist{
		ID:        playlistID,
		StationID: stationID,
		Name:      "Stopset Playlist",
	}
	if err := d.db.Create(&playlist).Error; err != nil {
		t.Fatalf("seed playlist: %v", err)
	}

	pItem := models.PlaylistItem{
		ID:         uuid.NewString(),
		PlaylistID: playlistID,
		MediaID:    mediaID,
		Position:   0,
	}
	if err := d.db.Create(&pItem).Error; err != nil {
		t.Fatalf("seed playlist item: %v", err)
	}

	clock := models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Stopset Clock",
	}
	if err := d.db.Create(&clock).Error; err != nil {
		t.Fatalf("seed clock: %v", err)
	}

	slot := models.ClockSlot{
		ID:          uuid.NewString(),
		ClockHourID: clockID,
		Position:    0,
		Type:        models.SlotTypeStopset,
		Payload:     map[string]any{"playlist_id": playlistID},
	}
	if err := d.db.Create(&slot).Error; err != nil {
		t.Fatalf("seed slot: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "clock_template",
		SourceID:   clockID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startClockEntry(ctx, entry)
	if err != nil {
		t.Errorf("startClockEntry stopset with playlist returned error: %v", err)
	}

	d.mu.Lock()
	_, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after stopset with playlist")
	}
}

// ── startClockEntry: stopset slot with media_id ───────────────────────────

func TestStartClockEntry_StopsetSlot_WithMediaID_SetsActiveState(t *testing.T) {
	d, _ := newMockDirector(t, &models.ClockHour{}, &models.ClockSlot{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	clockID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "stopset-media",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Stopset Media Track",
		Path:          "/tmp/stopset-media.mp3",
		Duration:      30 * time.Second,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	clock := models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Stopset Media Clock",
	}
	if err := d.db.Create(&clock).Error; err != nil {
		t.Fatalf("seed clock: %v", err)
	}

	slot := models.ClockSlot{
		ID:          uuid.NewString(),
		ClockHourID: clockID,
		Position:    0,
		Type:        models.SlotTypeStopset,
		Payload:     map[string]any{"media_id": mediaID},
	}
	if err := d.db.Create(&slot).Error; err != nil {
		t.Fatalf("seed slot: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "clock_template",
		SourceID:   clockID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startClockEntry(ctx, entry)
	if err != nil {
		t.Errorf("startClockEntry stopset with media_id returned error: %v", err)
	}

	d.mu.Lock()
	_, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after stopset with media_id")
	}
}

// ── startClockEntry: unknown slot type ───────────────────────────────────

func TestStartClockEntry_UnknownSlotType_FallsBackToRandom(t *testing.T) {
	d, _ := newMockDirector(t, &models.ClockHour{}, &models.ClockSlot{})
	ctx := context.Background()

	stationID := uuid.NewString()
	clockID := uuid.NewString()
	mediaID := uuid.NewString()

	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Fallback Track",
		Path:          "/tmp/fallback.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	clock := models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Unknown Clock",
	}
	if err := d.db.Create(&clock).Error; err != nil {
		t.Fatalf("seed clock: %v", err)
	}

	slot := models.ClockSlot{
		ID:          uuid.NewString(),
		ClockHourID: clockID,
		Position:    0,
		Type:        "unknown_type", // not handled by switch
		Payload:     map[string]any{},
	}
	if err := d.db.Create(&slot).Error; err != nil {
		t.Fatalf("seed slot: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    uuid.NewString(),
		SourceType: "clock_template",
		SourceID:   clockID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	// Should not return error even with unknown slot type.
	err := d.startClockEntry(ctx, entry)
	if err != nil {
		t.Errorf("startClockEntry with unknown slot type returned error: %v", err)
	}
}

// ── handleEntry: clock_template source type ───────────────────────────────

func TestHandleEntry_ClockTemplate_ClockNotFound_ReturnsError(t *testing.T) {
	d, _ := newMockDirector(t, &models.ClockHour{}, &models.ClockSlot{})
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "clock_template",
		SourceID:   uuid.NewString(), // not in DB
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.handleEntry(ctx, entry)
	if err == nil {
		t.Error("expected error for missing clock in handleEntry")
	}
}
