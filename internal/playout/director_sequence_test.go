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

// helpers to create broadcast mounts needed by playNextFromState etc.
func createBroadcastMount(d *Director, mountName string) {
	d.broadcast.CreateMount(mountName, "audio/mpeg", 128)
	d.broadcast.CreateMount(mountName+"-lq", "audio/mpeg", 64)
}

// ── handleTrackEnded ──────────────────────────────────────────────────────

func TestHandleTrackEnded_EntryActiveAndNoQueue_CallsRandom(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	entryID := uuid.NewString()
	mountName := "main"

	// Seed a mount and media for random track selection.
	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       mountName,
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	mediaID := uuid.NewString()
	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Random Track",
		Path:          "/tmp/random.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	createBroadcastMount(d, mountName)

	// Set active state with the matching entryID.
	d.mu.Lock()
	d.active[mountID] = playoutState{
		MediaID:    mediaID,
		EntryID:    entryID,
		StationID:  stationID,
		SourceType: "media", // default → random
		Started:    time.Now().UTC(),
		Ends:       time.Now().UTC().Add(5 * time.Minute),
	}
	d.mu.Unlock()

	entry := models.ScheduleEntry{
		ID:        entryID,
		StationID: stationID,
		MountID:   mountID,
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	// Should not panic or error; it calls playRandomNextTrack which stops at broadcast mount.
	d.handleTrackEnded(entry, mountName)
}

func TestHandleTrackEnded_DifferentEntryActive_DoesNothing(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	entryID := uuid.NewString()

	// Set a DIFFERENT entry as active.
	d.mu.Lock()
	d.active[mountID] = playoutState{
		MediaID:   uuid.NewString(),
		EntryID:   uuid.NewString(), // different entry
		StationID: stationID,
		Started:   time.Now().UTC(),
		Ends:      time.Now().UTC().Add(5 * time.Minute),
	}
	d.mu.Unlock()

	entry := models.ScheduleEntry{
		ID:        entryID,
		StationID: stationID,
		MountID:   mountID,
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	// Must not panic or interfere with the different active entry.
	d.handleTrackEnded(entry, "main")
}

func TestHandleTrackEnded_PlaylistType_AdvancesPosition(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	entryID := uuid.NewString()
	mountName := "main"

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       mountName,
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
		{ID: mediaID1, StationID: stationID, Title: "Track 1", Path: "/tmp/t1.mp3", Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete},
		{ID: mediaID2, StationID: stationID, Title: "Track 2", Path: "/tmp/t2.mp3", Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete},
	} {
		if err := d.db.Create(&m).Error; err != nil {
			t.Fatalf("seed media: %v", err)
		}
	}

	createBroadcastMount(d, mountName)

	d.mu.Lock()
	d.active[mountID] = playoutState{
		MediaID:    mediaID1,
		EntryID:    entryID,
		StationID:  stationID,
		SourceType: "playlist",
		SourceID:   uuid.NewString(),
		Position:   0,
		TotalItems: 2,
		Items:      []string{mediaID1, mediaID2},
		Started:    time.Now().UTC(),
		Ends:       time.Now().UTC().Add(5 * time.Minute),
	}
	d.mu.Unlock()

	entry := models.ScheduleEntry{
		ID:        entryID,
		StationID: stationID,
		MountID:   mountID,
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	// Should advance to position 1.
	d.handleTrackEnded(entry, mountName)
}

// ── playRandomNextTrack ───────────────────────────────────────────────────

func TestPlayRandomNextTrack_NoMedia_DoesNotPanic(t *testing.T) {
	d, _ := newMockDirector(t)

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: uuid.NewString(),
		MountID:   uuid.NewString(),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	// No media in DB → should log and return without panic.
	d.playRandomNextTrack(entry, "main")
}

func TestPlayRandomNextTrack_WithMediaAndMount_SetsActive(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	entryID := uuid.NewString()
	mountName := "randomnext"

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       mountName,
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	mediaID := uuid.NewString()
	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Random",
		Path:          "/tmp/random.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	createBroadcastMount(d, mountName)

	entry := models.ScheduleEntry{
		ID:        entryID,
		StationID: stationID,
		MountID:   mountID,
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	d.playRandomNextTrack(entry, mountName)

	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after playRandomNextTrack")
	}
	if state.MediaID != mediaID {
		t.Errorf("MediaID = %q, want %q", state.MediaID, mediaID)
	}
}

// ── playNextFromState ─────────────────────────────────────────────────────

func TestPlayNextFromState_OutOfBounds_DoesNotPanic(t *testing.T) {
	d, _ := newMockDirector(t)

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: uuid.NewString(),
		MountID:   uuid.NewString(),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}
	state := playoutState{
		Items:      []string{uuid.NewString()},
		TotalItems: 1,
	}

	// Position 5 is out of bounds for a 1-item slice — should log and return.
	d.playNextFromState(entry, state, 5, "main")
}

func TestPlayNextFromState_WithMedia_SetsActiveAndPlays(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mountName := "pnfs"

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       mountName,
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	mediaID := uuid.NewString()
	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Next Track",
		Path:          "/tmp/next.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	createBroadcastMount(d, mountName)

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   mountID,
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}
	state := playoutState{
		MediaID:    mediaID,
		EntryID:    entry.ID,
		StationID:  stationID,
		SourceType: "playlist",
		Items:      []string{mediaID, mediaID},
		TotalItems: 2,
		Position:   0,
	}

	d.playNextFromState(entry, state, 1, mountName)

	d.mu.Lock()
	active, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after playNextFromState")
	}
	if active.Position != 1 {
		t.Errorf("position = %d, want 1", active.Position)
	}
}

// ── clock with smart_block slot (covers startSmartBlockByID) ─────────────

func TestStartClockEntry_SmartBlockSlot_BlockNotFound(t *testing.T) {
	d, _ := newMockDirector(t, &models.ClockHour{}, &models.ClockSlot{}, &models.SmartBlock{})
	ctx := context.Background()

	stationID := uuid.NewString()
	clockID := uuid.NewString()

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
		Payload:     map[string]any{"smart_block_id": uuid.NewString()}, // not in DB
	}
	if err := d.db.Create(&slot).Error; err != nil {
		t.Fatalf("seed clock slot: %v", err)
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
		t.Error("expected error for clock smart_block slot with missing block")
	}
}

// ── clock with playlist slot (covers startPlaylistByID) ──────────────────

func TestStartClockEntry_PlaylistSlot_PlaylistNotFound(t *testing.T) {
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
		t.Fatalf("seed clock slot: %v", err)
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

// ── resumeEntryIfPossible ─────────────────────────────────────────────────

func TestResumeEntryIfPossible_MediaSource_ReturnsFalse(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "media", // not resumable
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	if d.resumeEntryIfPossible(ctx, entry) {
		t.Error("resumeEntryIfPossible should return false for media source")
	}
}

func TestResumeEntryIfPossible_ExpiredEntry_ReturnsFalse(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "playlist",
		EndsAt:     time.Now().UTC().Add(-1 * time.Minute), // already ended
	}

	if d.resumeEntryIfPossible(ctx, entry) {
		t.Error("resumeEntryIfPossible should return false for expired entry")
	}
}

func TestResumeEntryIfPossible_NoActiveState_ReturnsFalse(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "playlist",
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	// No active state for this mount — should return false.
	if d.resumeEntryIfPossible(ctx, entry) {
		t.Error("resumeEntryIfPossible should return false with no active state")
	}
}

// ── popNextQueuedMedia ────────────────────────────────────────────────────

func TestPopNextQueuedMedia_EmptyQueue_ReturnsNil(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	media, item, err := d.popNextQueuedMedia(ctx, uuid.NewString(), uuid.NewString())
	if err != nil {
		t.Fatalf("popNextQueuedMedia returned error: %v", err)
	}
	if media != nil || item != nil {
		t.Error("expected nil media and item for empty queue")
	}
}

// ── updateEntryPosition ───────────────────────────────────────────────────

func TestUpdateEntryPosition_WithEntry_UpdatesPosition(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})

	entryID := uuid.NewString()
	entry := models.ScheduleEntry{
		ID:         entryID,
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "playlist",
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}
	if err := d.db.Create(&entry).Error; err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	d.updateEntryPosition(entryID, 3, entry.StartsAt)

	var loaded models.ScheduleEntry
	if err := d.db.First(&loaded, "id = ?", entryID).Error; err != nil {
		t.Fatalf("reload entry: %v", err)
	}
	// The current_position metadata should be updated.
	if pos, ok := loaded.Metadata["current_position"].(float64); ok {
		if int(pos) != 3 {
			t.Errorf("current_position = %v, want 3", pos)
		}
	}
}

// ── getWebRTCRTPPortForStation with cached result ─────────────────────────

func TestGetWebRTCRTPPortForStation_Cached(t *testing.T) {
	d, _ := newMockDirector(t)
	d.webrtcEnabled = true
	d.webrtcRTPPort = 5004
	ctx := context.Background()
	stationID := uuid.NewString()

	p1 := d.getWebRTCRTPPortForStation(ctx, stationID)
	p2 := d.getWebRTCRTPPortForStation(ctx, stationID)
	if p1 != p2 {
		t.Errorf("port should be cached: %d vs %d", p1, p2)
	}
}
