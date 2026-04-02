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

// ── clearPersistedMountState ──────────────────────────────────────────────

func TestClearPersistedMountState_EmptyID(t *testing.T) {
	d, _ := newMockDirector(t)
	// Empty mountID should be a no-op (early return).
	d.clearPersistedMountState(context.Background(), "")
}

func TestClearPersistedMountState_WithID(t *testing.T) {
	d, _ := newMockDirector(t)
	// Should not error even if no record exists.
	d.clearPersistedMountState(context.Background(), uuid.NewString())
}

// ── handleTrackEnded: entry expired ──────────────────────────────────────

func TestHandleTrackEnded_EntryExpired(t *testing.T) {
	d, _ := newMockDirector(t)

	entry := models.ScheduleEntry{
		ID:       uuid.NewString(),
		MountID:  uuid.NewString(),
		StartsAt: time.Now().UTC().Add(-10 * time.Minute),
		EndsAt:   time.Now().UTC().Add(-1 * time.Second), // already ended
	}
	// Should return immediately (after entry expired, don't start next track).
	d.handleTrackEnded(entry, "main")
}

// ── handleTrackEnded: different entry now active ──────────────────────────

func TestHandleTrackEnded_DifferentEntryActive(t *testing.T) {
	d, _ := newMockDirector(t)

	mountID := uuid.NewString()
	entry := models.ScheduleEntry{
		ID:       uuid.NewString(),
		MountID:  mountID,
		StartsAt: time.Now().UTC().Add(-1 * time.Second),
		EndsAt:   time.Now().UTC().Add(10 * time.Minute),
	}

	// Set a different entry as active.
	d.mu.Lock()
	d.active[mountID] = playoutState{EntryID: uuid.NewString()}
	d.mu.Unlock()

	// Should detect mismatch and return.
	d.handleTrackEnded(entry, "main")
}

// ── handleTrackEnded: smart_block exhausted ───────────────────────────────

func TestHandleTrackEnded_SmartBlockExhausted(t *testing.T) {
	d, _ := newMockDirector(t)

	mountID := uuid.NewString()
	entryID := uuid.NewString()
	entry := models.ScheduleEntry{
		ID:       entryID,
		MountID:  mountID,
		StartsAt: time.Now().UTC().Add(-1 * time.Second),
		EndsAt:   time.Now().UTC().Add(10 * time.Minute),
	}

	// Set active state: smart_block, position at last item.
	playKey := playbackKey(entryID, entry.StartsAt)
	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    entryID,
		SourceType: "smart_block",
		SourceID:   "sb-123",
		Position:   1,
		TotalItems: 1, // exhausted (nextPos = 2 >= 1)
		Items:      []string{uuid.NewString()},
	}
	d.played[playKey] = time.Now()
	d.mu.Unlock()

	// Smart block exhausted: should clear active + played state.
	d.handleTrackEnded(entry, "main")

	d.mu.Lock()
	_, activeExists := d.active[mountID]
	_, playedExists := d.played[playKey]
	d.mu.Unlock()
	if activeExists {
		t.Error("expected active state to be cleared after smart_block exhaustion")
	}
	if playedExists {
		t.Error("expected played state to be cleared after smart_block exhaustion")
	}
}

// ── handleTrackEnded: clock_playlist wraps around ────────────────────────

func TestHandleTrackEnded_ClockPlaylistWraps(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	entryID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "hte-clkpl-" + mountID[:8],
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	mediaID1 := uuid.NewString()
	mediaID2 := uuid.NewString()
	for _, m := range []models.MediaItem{
		{ID: mediaID1, StationID: stationID, Title: "T1", Path: "/tmp/t1.mp3", Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete},
		{ID: mediaID2, StationID: stationID, Title: "T2", Path: "/tmp/t2.mp3", Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete},
	} {
		if err := d.db.Create(&m).Error; err != nil {
			t.Fatalf("seed media: %v", err)
		}
	}

	// Pre-create broadcast mounts so playNextFromState doesn't early-return.
	d.broadcast.CreateMount(mount.Name, "audio/mpeg", 128)
	d.broadcast.CreateMount(mount.Name+"-lq", "audio/mpeg", 64)

	entry := models.ScheduleEntry{
		ID:        entryID,
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(10 * time.Minute),
	}

	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    entryID,
		SourceType: "clock_playlist",
		SourceID:   "clk-123",
		Position:   0,
		TotalItems: 2,
		Items:      []string{mediaID1, mediaID2},
	}
	d.mu.Unlock()

	// Should wrap to position 1 and call playNextFromState.
	d.handleTrackEnded(entry, mount.Name)
}

// ── handleTrackEnded: clock source wraps around ──────────────────────────

func TestHandleTrackEnded_ClockSourceWraps(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	entryID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "hte-clk-" + mountID[:8],
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	mediaID := uuid.NewString()
	if err := d.db.Create(&models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Clock Track",
		Path:          "/tmp/clock.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	d.broadcast.CreateMount(mount.Name, "audio/mpeg", 128)
	d.broadcast.CreateMount(mount.Name+"-lq", "audio/mpeg", 64)

	entry := models.ScheduleEntry{
		ID:        entryID,
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(10 * time.Minute),
	}

	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    entryID,
		SourceType: "clock",
		SourceID:   "clk-456",
		Position:   0,
		TotalItems: 1,
		Items:      []string{mediaID},
	}
	d.mu.Unlock()

	d.handleTrackEnded(entry, mount.Name)
}

// ── handleTrackEnded: clock source zero total items ──────────────────────

func TestHandleTrackEnded_ClockZeroTotal(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	entryID := uuid.NewString()

	mount := models.Mount{
		ID: mountID, StationID: stationID,
		Name:   "hte-clk0-" + mountID[:8],
		Format: "mp3", Bitrate: 128, SampleRate: 44100, Channels: 2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:        entryID,
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(10 * time.Minute),
	}

	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    entryID,
		SourceType: "clock",
		TotalItems: 0, // zero → break, fall through to random
	}
	d.mu.Unlock()

	// Falls through to playRandomNextTrack, which will find no media → early return.
	d.handleTrackEnded(entry, mount.Name)
}

// ── handleTrackEnded: playlist wraps, no active items ─────────────────────

func TestHandleTrackEnded_PlaylistOutOfItems(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	entryID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "hte-pl-empty-" + mountID[:8],
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:        entryID,
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(10 * time.Minute),
	}

	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    entryID,
		SourceType: "playlist",
		Position:   0,
		TotalItems: 2,
		Items:      []string{}, // empty items, nextPos = 1 which is not < len(items)=0
	}
	d.mu.Unlock()

	// nextPos = 1, len(Items) = 0 → falls to random.
	d.handleTrackEnded(entry, mount.Name)
}
