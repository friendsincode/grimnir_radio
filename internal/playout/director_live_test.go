/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
)

// ── InjectLiveSource ──────────────────────────────────────────────────────

func TestInjectLiveSource_MountNotFound_ReturnsError(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	_, _, err := d.InjectLiveSource(ctx, uuid.NewString(), uuid.NewString())
	if err == nil {
		t.Error("expected error for unknown mount")
	}
}

func TestInjectLiveSource_WithMount_ReturnsWriter(t *testing.T) {
	d, mgr := newMockDirector(t)
	ctx := context.Background()

	// Give the mock manager a stdin writer to return.
	pr, pw := io.Pipe()
	defer pr.Close()
	mgr.stdinWriter = pw

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "live-main",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	stdin, release, err := d.InjectLiveSource(ctx, stationID, mountID)
	if err != nil {
		t.Fatalf("InjectLiveSource returned error: %v", err)
	}
	if stdin == nil {
		t.Error("expected non-nil stdin writer")
	}
	if release == nil {
		t.Error("expected non-nil release function")
	}

	// Active state should be set.
	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after InjectLiveSource")
	}
	if state.SourceType != "live" {
		t.Errorf("SourceType = %q, want \"live\"", state.SourceType)
	}

	// Release should clear active state.
	release()
	d.mu.Lock()
	_, stillActive := d.active[mountID]
	d.mu.Unlock()
	if stillActive {
		t.Error("expected active state to be cleared after release()")
	}
}

// ── ListenerCount ─────────────────────────────────────────────────────────

func TestListenerCount_NilBroadcast_ReturnsZero(t *testing.T) {
	d, _ := newMockDirector(t)
	d.broadcast = nil

	n, err := d.ListenerCount(context.Background(), uuid.NewString())
	if err != nil {
		t.Errorf("ListenerCount returned error: %v", err)
	}
	if n != 0 {
		t.Errorf("ListenerCount = %d, want 0", n)
	}
}

func TestListenerCount_WithMount_ReturnsZeroListeners(t *testing.T) {
	d, _ := newMockDirector(t)
	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:        mountID,
		StationID: stationID,
		Name:      "count-main",
		Format:    "mp3",
		Bitrate:   128,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	// No broadcast mount created → 0 listeners.
	n, err := d.ListenerCount(context.Background(), stationID)
	if err != nil {
		t.Errorf("ListenerCount returned error: %v", err)
	}
	if n != 0 {
		t.Errorf("ListenerCount = %d, want 0", n)
	}
}

// ── startSmartBlockByID ───────────────────────────────────────────────────

func TestStartSmartBlockByID_BlockNotFound_ReturnsError(t *testing.T) {
	d, _ := newMockDirector(t, &models.SmartBlock{})
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: uuid.NewString(),
		MountID:   uuid.NewString(),
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startSmartBlockByID(ctx, entry, uuid.NewString(), uuid.NewString(), "Test Clock")
	if err == nil {
		t.Error("expected error for missing smart block")
	}
}

func TestStartSmartBlockByID_BlockFound_NoMedia_ReturnsNil(t *testing.T) {
	d, _ := newMockDirector(t, &models.SmartBlock{})
	ctx := context.Background()

	stationID := uuid.NewString()
	blockID := uuid.NewString()

	block := models.SmartBlock{
		ID:        blockID,
		StationID: stationID,
		Name:      "Empty Block",
	}
	if err := d.db.Create(&block).Error; err != nil {
		t.Fatalf("seed smart block: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   uuid.NewString(),
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	// No media → generation fails or empty → returns nil (logs warning)
	err := d.startSmartBlockByID(ctx, entry, blockID, uuid.NewString(), "Test Clock")
	if err != nil {
		t.Errorf("startSmartBlockByID with empty block returned error: %v", err)
	}
}

// ── playRandomNextTrack ───────────────────────────────────────────────────

func TestPlayRandomNextTrack_WithMountAndMedia_SetsActiveState(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "random-main",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	// Create broadcast mounts so playRandomNextTrack doesn't bail on nil mount check.
	d.broadcast.CreateMount("random-main", "audio/mpeg", 128)
	d.broadcast.CreateMount("random-main-lq", "audio/mpeg", 64)

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

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	d.playRandomNextTrack(entry, "random-main")

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

// ── handleTrackEnded with playlist advance ────────────────────────────────

func TestHandleTrackEnded_PlaylistSourceType_AdvancesPosition(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID1 := uuid.NewString()
	mediaID2 := uuid.NewString()

	// Seed two media items so the playlist has items to advance to.
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

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	// Set up active state with playlist source type.
	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    entry.ID,
		StationID:  stationID,
		SourceType: "playlist",
		SourceID:   uuid.NewString(),
		Position:   0,
		TotalItems: 2,
		Items:      []string{mediaID1, mediaID2},
		Ends:       entry.EndsAt,
	}
	d.mu.Unlock()

	// Create broadcast mounts for playNextFromState.
	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "playlist-advance",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}
	d.broadcast.CreateMount("playlist-advance", "audio/mpeg", 128)
	d.broadcast.CreateMount("playlist-advance-lq", "audio/mpeg", 64)

	// handleTrackEnded with still-active entry should advance to position 1.
	d.handleTrackEnded(entry, "playlist-advance")

	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state to still be set")
	}
	if state.MediaID != mediaID2 {
		t.Errorf("expected to advance to mediaID2, got MediaID = %q", state.MediaID)
	}
}

// ── StopStation (not active) ──────────────────────────────────────────────

func TestStopStation_EmptyStation_NoError(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	_, err := d.StopStation(ctx, uuid.NewString())
	if err != nil {
		t.Errorf("StopStation on empty station returned error: %v", err)
	}
}

// ── InjectLiveSource with xfade session already open ─────────────────────

func TestInjectLiveSource_ExistingXfadeSession_ClosesIt(t *testing.T) {
	d, mgr := newMockDirector(t)
	ctx := context.Background()

	pr, pw := io.Pipe()
	defer pr.Close()
	mgr.stdinWriter = pw

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "xfade-live",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	// Pre-populate an xfade session that should be closed.
	sessPr, sessPw := io.Pipe()
	defer sessPr.Close()
	sess := newPCMCrossfadeSession(sessionConfig{}, sessPw, d.logger, nil)
	d.xfadeMu.Lock()
	d.xfadeSessions[mountID] = sess
	d.xfadeMu.Unlock()

	_, release, err := d.InjectLiveSource(ctx, stationID, mountID)
	if err != nil {
		t.Fatalf("InjectLiveSource returned error: %v", err)
	}

	// After inject, the xfade session should have been removed.
	d.xfadeMu.Lock()
	_, hasSession := d.xfadeSessions[mountID]
	d.xfadeMu.Unlock()
	if hasSession {
		t.Error("expected xfade session to be removed after InjectLiveSource")
	}

	release()
}
