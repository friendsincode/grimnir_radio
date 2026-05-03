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

// ── getScheduleSnapshot: instance-suppresses-parent ──────────────────────

// TestGetScheduleSnapshot_InstanceSuppressesRecurringParent verifies that when
// pre-materialized media instances are active for a mount, the recurring
// smart_block parent for that mount is excluded from the snapshot.  Without
// this filter the director would start a live-generated sequence that a
// subsequent tick immediately overrides with the first instance, causing a
// brief wrong-track flash.
func TestGetScheduleSnapshot_InstanceSuppressesRecurringParent(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()
	blockID := uuid.NewString()

	// Seed a media item so the instance entry is valid.
	if err := d.db.Create(&models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Jingle",
		Path:          "/tmp/jingle.mp3",
		Duration:      45 * time.Second,
		AnalysisState: models.AnalysisComplete,
	}).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	now := time.Now().UTC()

	// Recurring smart_block parent (first occurrence in the past, recurring daily).
	parent := models.ScheduleEntry{
		ID:             uuid.NewString(),
		StationID:      stationID,
		MountID:        mountID,
		SourceType:     "smart_block",
		SourceID:       blockID,
		StartsAt:       now.Add(-7 * 24 * time.Hour).Truncate(time.Minute), // first occurrence a week ago
		EndsAt:         now.Add(-7 * 24 * time.Hour).Truncate(time.Minute).Add(2 * time.Hour),
		RecurrenceType: models.RecurrenceDaily,
		IsInstance:     false,
	}
	if err := d.db.Create(&parent).Error; err != nil {
		t.Fatalf("seed recurring parent: %v", err)
	}

	// Pre-materialized media instance currently active on the same mount.
	instance := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "media",
		SourceID:   mediaID,
		StartsAt:   now.Add(-10 * time.Second), // started 10s ago
		EndsAt:     now.Add(35 * time.Second),  // ends in 35s
		IsInstance: true,
	}
	if err := d.db.Create(&instance).Error; err != nil {
		t.Fatalf("seed instance: %v", err)
	}

	d.scheduleCache.dirty = true
	entries, err := d.getScheduleSnapshot(ctx, now)
	if err != nil {
		t.Fatalf("getScheduleSnapshot: %v", err)
	}

	for _, e := range entries {
		if e.ID == parent.ID {
			t.Errorf("recurring smart_block parent should be suppressed when instances are active, but it appeared in snapshot")
		}
	}

	found := false
	for _, e := range entries {
		if e.ID == instance.ID {
			found = true
		}
	}
	if !found {
		t.Error("pre-materialized instance should still appear in snapshot")
	}
}

// TestGetScheduleSnapshot_ParentKeptWhenNoInstances verifies that the recurring
// smart_block parent is NOT suppressed when no pre-materialized instances are
// active for the mount (fall-through to live generation).
func TestGetScheduleSnapshot_ParentKeptWhenNoInstances(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	blockID := uuid.NewString()

	now := time.Now().UTC()

	parent := models.ScheduleEntry{
		ID:             uuid.NewString(),
		StationID:      stationID,
		MountID:        mountID,
		SourceType:     "smart_block",
		SourceID:       blockID,
		StartsAt:       now.Add(-7 * 24 * time.Hour).Truncate(time.Minute),
		EndsAt:         now.Add(-7 * 24 * time.Hour).Truncate(time.Minute).Add(2 * time.Hour),
		RecurrenceType: models.RecurrenceDaily,
		IsInstance:     false,
	}
	if err := d.db.Create(&parent).Error; err != nil {
		t.Fatalf("seed recurring parent: %v", err)
	}

	d.scheduleCache.dirty = true
	entries, err := d.getScheduleSnapshot(ctx, now)
	if err != nil {
		t.Fatalf("getScheduleSnapshot: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.ID == parent.ID {
			found = true
		}
	}
	if !found {
		t.Error("recurring parent should be kept in snapshot when no instances exist")
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

func TestTick_HardBoundaryPreemption_ClearsOldActiveBeforeStartingNew(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{}, &models.Playlist{}, &models.PlaylistItem{}, &models.SmartBlock{})

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "hb-preempt-" + mountID[:8],
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	media1 := models.MediaItem{
		ID:            uuid.NewString(),
		StationID:     stationID,
		Path:          "/tmp/m1.mp3",
		Duration:      5 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	media2 := models.MediaItem{
		ID:            uuid.NewString(),
		StationID:     stationID,
		Path:          "/tmp/m2.mp3",
		Duration:      5 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media1).Error; err != nil {
		t.Fatalf("seed media1: %v", err)
	}
	if err := d.db.Create(&media2).Error; err != nil {
		t.Fatalf("seed media2: %v", err)
	}

	now := time.Now().UTC()
	entry1 := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "media",
		SourceID:   media1.ID,
		StartsAt:   now.Add(-10 * time.Minute),
		EndsAt:     now.Add(-1 * time.Second), // PAST its end
	}
	entry2 := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "media",
		SourceID:   media2.ID,
		StartsAt:   now.Add(-1 * time.Second),
		EndsAt:     now.Add(5 * time.Minute),
	}
	if err := d.db.Create(&entry1).Error; err != nil {
		t.Fatalf("seed entry1: %v", err)
	}
	if err := d.db.Create(&entry2).Error; err != nil {
		t.Fatalf("seed entry2: %v", err)
	}

	// Prime d.active with entry1 still listed as active, but its Ends is in the past.
	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    entry1.ID,
		StationID:  stationID,
		MediaID:    media1.ID,
		Ends:       entry1.EndsAt,
		SourceType: "media",
	}
	d.mu.Unlock()

	d.broadcast.CreateMount(mount.Name, "audio/mpeg", 128)
	d.broadcast.CreateMount(mount.Name+"-lq", "audio/mpeg", 64)

	if err := d.tick(context.Background()); err != nil {
		t.Errorf("tick returned error: %v", err)
	}

	d.mu.Lock()
	activeState, ok := d.active[mountID]
	d.mu.Unlock()

	if !ok {
		t.Fatal("expected active state on mount after tick with preempting entry")
	}
	if activeState.EntryID != entry2.ID {
		t.Errorf("EntryID = %q, want %q (new entry should own mount after preemption)", activeState.EntryID, entry2.ID)
	}
}
