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

// ── tick with schedule entries ─────────────────────────────────────────────

func TestTick_WithActiveMediaEntry_CallsHandleEntry(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Tick Track",
		Path:          "/tmp/tick.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	now := time.Now().UTC()
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "media",
		SourceID:   mediaID,
		StartsAt:   now.Add(-1 * time.Second),
		EndsAt:     now.Add(5 * time.Minute),
	}
	if err := d.db.Create(&entry).Error; err != nil {
		t.Fatalf("seed schedule entry: %v", err)
	}

	d.scheduleCache.dirty = true

	if err := d.tick(ctx); err != nil {
		t.Errorf("tick with media entry returned error: %v", err)
	}

	// After tick, the entry should be in active state.
	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after tick with media entry")
	}
	if state.MediaID != mediaID {
		t.Errorf("MediaID = %q, want %q", state.MediaID, mediaID)
	}
}

func TestTick_AlreadyPlayedEntry_SkipsIt(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Already Played",
		Path:          "/tmp/played.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	now := time.Now().UTC()
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "media",
		SourceID:   mediaID,
		StartsAt:   now.Add(-1 * time.Second),
		EndsAt:     now.Add(5 * time.Minute),
	}
	if err := d.db.Create(&entry).Error; err != nil {
		t.Fatalf("seed schedule entry: %v", err)
	}

	d.scheduleCache.dirty = true

	// First tick: plays the entry.
	if err := d.tick(ctx); err != nil {
		t.Errorf("first tick returned error: %v", err)
	}

	// Mark the entry as not active so we can detect if second tick re-plays it.
	d.mu.Lock()
	delete(d.active, mountID)
	d.mu.Unlock()

	// Second tick: entry already played, should skip.
	if err := d.tick(ctx); err != nil {
		t.Errorf("second tick returned error: %v", err)
	}

	d.mu.Lock()
	_, active := d.active[mountID]
	d.mu.Unlock()
	if active {
		t.Error("expected entry to be skipped on second tick (already played)")
	}
}

// ── scheduleStop ─────────────────────────────────────────────────────────

func TestScheduleStop_PastEndTime_StopsImmediately(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	mountID := uuid.NewString()
	stationID := uuid.NewString()
	entryID := uuid.NewString()

	// Set up an active state.
	endsAt := time.Now().UTC().Add(-1 * time.Second) // already ended
	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:   entryID,
		StationID: stationID,
		Ends:      endsAt,
	}
	d.mu.Unlock()

	d.scheduleStop(ctx, stationID, mountID, endsAt)

	// Wait for the goroutine to fire (delay is max 200ms after stopAt).
	// Since stopAt is in the past, delay=0, so 300ms should be enough.
	time.Sleep(400 * time.Millisecond)

	d.mu.Lock()
	_, stillActive := d.active[mountID]
	d.mu.Unlock()
	if stillActive {
		t.Error("expected active state to be cleared by scheduleStop")
	}
}

func TestScheduleStop_ActiveEntryChanged_DoesNotStop(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	mountID := uuid.NewString()
	stationID := uuid.NewString()

	// Set state with a future end time (well past the original endsAt).
	futureEnd := time.Now().UTC().Add(10 * time.Minute)
	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:   uuid.NewString(),
		StationID: stationID,
		Ends:      futureEnd,
	}
	d.mu.Unlock()

	// Schedule stop for a past time — but active state has a far-future Ends.
	// The goroutine should detect state.Ends.After(expected+500ms) and bail.
	pastEnd := time.Now().UTC().Add(-2 * time.Second)
	d.scheduleStop(ctx, stationID, mountID, pastEnd)

	time.Sleep(400 * time.Millisecond)

	d.mu.Lock()
	_, stillActive := d.active[mountID]
	d.mu.Unlock()
	if !stillActive {
		t.Error("expected active state to remain when entry has been superseded")
	}
}

// ── playNextFromState ─────────────────────────────────────────────────────

func TestPlayNextFromState_WithMountAndMedia_SetsActiveStateAtPosition(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID1 := uuid.NewString()
	mediaID2 := uuid.NewString()
	entryID := uuid.NewString()

	for _, id := range []string{mediaID1, mediaID2} {
		m := models.MediaItem{
			ID:            id,
			StationID:     stationID,
			Title:         "Track " + id[:8],
			Path:          "/tmp/" + id + ".mp3",
			Duration:      3 * time.Minute,
			AnalysisState: models.AnalysisComplete,
		}
		if err := d.db.Create(&m).Error; err != nil {
			t.Fatalf("seed media: %v", err)
		}
	}

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "nextfromstate",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	d.broadcast.CreateMount("nextfromstate", "audio/mpeg", 128)
	d.broadcast.CreateMount("nextfromstate-lq", "audio/mpeg", 64)

	entry := models.ScheduleEntry{
		ID:        entryID,
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	state := playoutState{
		EntryID:    entryID,
		StationID:  stationID,
		SourceType: "playlist",
		SourceID:   uuid.NewString(),
		Position:   0,
		TotalItems: 2,
		Items:      []string{mediaID1, mediaID2},
		Ends:       entry.EndsAt,
	}

	d.playNextFromState(entry, state, 1, "nextfromstate")

	d.mu.Lock()
	active, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after playNextFromState")
	}
	if active.MediaID != mediaID2 {
		t.Errorf("MediaID = %q, want %q", active.MediaID, mediaID2)
	}
	if active.Position != 1 {
		t.Errorf("Position = %d, want 1", active.Position)
	}
}
