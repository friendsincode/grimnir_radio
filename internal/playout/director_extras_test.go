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

// ── resumeEntryIfPossible ─────────────────────────────────────────────────

func TestResumeEntryIfPossible_UnsupportedSourceType_ReturnsFalse(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "media", // not in the resume set
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	if d.resumeEntryIfPossible(ctx, entry) {
		t.Error("expected false for unsupported source type")
	}
}

func TestResumeEntryIfPossible_ActiveStateDifferentEntry_ReturnsFalse(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	mountID := uuid.NewString()
	stationID := uuid.NewString()
	mediaID := uuid.NewString()

	// Set active state for a different entry
	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    uuid.NewString(), // different entry ID
		StationID:  stationID,
		MediaID:    mediaID,
		SourceType: "playlist",
		SourceID:   uuid.NewString(),
		Position:   0,
		TotalItems: 2,
		Items:      []string{mediaID, uuid.NewString()},
		Ends:       time.Now().UTC().Add(5 * time.Minute),
	}
	d.mu.Unlock()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(), // different entry ID
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "playlist",
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	if d.resumeEntryIfPossible(ctx, entry) {
		t.Error("expected false when active state has different entry ID")
	}
}

func TestResumeEntryIfPossible_ValidState_WithMedia_ReturnsTrue(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	entryID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "resume-main",
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
		Title:         "Resume Track",
		Path:          "/tmp/resume.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    entryID,
		StationID:  stationID,
		MediaID:    mediaID,
		SourceType: "playlist",
		SourceID:   uuid.NewString(),
		Position:   0,
		TotalItems: 1,
		Items:      []string{mediaID},
		Ends:       time.Now().UTC().Add(5 * time.Minute),
	}
	d.mu.Unlock()

	entry := models.ScheduleEntry{
		ID:         entryID,
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "playlist",
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	if !d.resumeEntryIfPossible(ctx, entry) {
		t.Error("expected true when valid resumable state exists")
	}
}

// ── handleEntry: live and unknown source types ────────────────────────────

func TestHandleEntry_LiveSourceType_NoError(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "live",
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.handleEntry(ctx, entry)
	if err != nil {
		t.Errorf("handleEntry with live source type returned error: %v", err)
	}
}

// ── playMedia: with and without mount ────────────────────────────────────

func TestPlayMedia_WithMount_SetsActiveState(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "play-media-main",
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
		Title:         "Play Media Track",
		Path:          "/tmp/play.mp3",
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

	err := d.playMedia(ctx, entry, media, nil)
	if err != nil {
		t.Errorf("playMedia returned error: %v", err)
	}

	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after playMedia")
	}
	if state.MediaID != mediaID {
		t.Errorf("MediaID = %q, want %q", state.MediaID, mediaID)
	}
}

func TestPlayMedia_WithoutMount_UsesDefaults(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "No Mount Track",
		Path:          "/tmp/nomount.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   mountID, // not in DB → uses defaults
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.playMedia(ctx, entry, media, nil)
	if err != nil {
		t.Errorf("playMedia without mount returned error: %v", err)
	}

	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state even when mount missing")
	}
	if state.MediaID != mediaID {
		t.Errorf("MediaID = %q, want %q", state.MediaID, mediaID)
	}
}

// ── handleTrackEnded: smart_block advance ────────────────────────────────

func TestHandleTrackEnded_SmartBlockSourceType_AdvancesPosition(t *testing.T) {
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
			Title:         "SB Track " + id[:8],
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
		Name:       "sb-advance",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}
	d.broadcast.CreateMount("sb-advance", "audio/mpeg", 128)
	d.broadcast.CreateMount("sb-advance-lq", "audio/mpeg", 64)

	entry := models.ScheduleEntry{
		ID:        entryID,
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    entryID,
		StationID:  stationID,
		SourceType: "smart_block",
		SourceID:   uuid.NewString(),
		Position:   0,
		TotalItems: 2,
		Items:      []string{mediaID1, mediaID2},
		Ends:       entry.EndsAt,
	}
	d.mu.Unlock()

	d.handleTrackEnded(entry, "sb-advance")

	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after smart_block advance")
	}
	if state.MediaID != mediaID2 {
		t.Errorf("expected to advance to mediaID2, got MediaID = %q", state.MediaID)
	}
}

func TestHandleTrackEnded_SmartBlockExhausted_ClearsActive(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()
	entryID := uuid.NewString()

	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Last Track",
		Path:          "/tmp/last.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:        entryID,
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    entryID,
		StationID:  stationID,
		SourceType: "smart_block",
		SourceID:   uuid.NewString(),
		Position:   0,
		TotalItems: 1,
		Items:      []string{mediaID},
		Ends:       entry.EndsAt,
	}
	d.mu.Unlock()

	d.handleTrackEnded(entry, "sb-main")

	d.mu.Lock()
	_, stillActive := d.active[mountID]
	d.mu.Unlock()
	if stillActive {
		t.Error("expected active state to be cleared after smart_block exhaustion")
	}
}
