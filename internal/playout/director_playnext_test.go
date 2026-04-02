/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
)

// ── playNextFromState ─────────────────────────────────────────────────────

func TestPlayNextFromState_PositionOutOfBounds(t *testing.T) {
	d, _ := newMockDirector(t)

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: uuid.NewString(),
		MountID:   uuid.NewString(),
		StartsAt:  time.Now().UTC(),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}
	state := playoutState{
		Items:      []string{"item-1"},
		TotalItems: 1,
	}
	// nextPos = 2 >= len(state.Items) → early return, no panic
	d.playNextFromState(entry, state, 2, "main")
}

func TestPlayNextFromState_MediaNotFound(t *testing.T) {
	d, _ := newMockDirector(t)

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: uuid.NewString(),
		MountID:   uuid.NewString(),
		StartsAt:  time.Now().UTC(),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}
	state := playoutState{
		Items:      []string{uuid.NewString()}, // non-existent media ID
		TotalItems: 1,
	}
	// media not found → early return, no panic
	d.playNextFromState(entry, state, 0, "main")
}

func TestPlayNextFromState_MountNotFound(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mediaID := uuid.NewString()
	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Next Track No Mount",
		Path:          "/tmp/next-no-mount.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   uuid.NewString(), // not in DB
		StartsAt:  time.Now().UTC(),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}
	state := playoutState{
		Items:      []string{mediaID},
		TotalItems: 1,
	}
	// mount not found → early return
	d.playNextFromState(entry, state, 0, "main")
}

func TestPlayNextFromState_BroadcastMountNotFound(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "next-no-bcast-" + mountID[:8],
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
		Title:         "Next No Broadcast",
		Path:          "/tmp/next-no-bcast.mp3",
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
		StartsAt:  time.Now().UTC(),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}
	state := playoutState{Items: []string{mediaID}, TotalItems: 1}
	// No broadcast mount created → early return
	d.playNextFromState(entry, state, 0, mount.Name)
}

func TestPlayNextFromState_LQBroadcastMountNotFound(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "next-no-lq-" + mountID[:8],
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
		Title:         "Next No LQ",
		Path:          "/tmp/next-no-lq.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	// Create only HQ mount, no LQ.
	d.broadcast.CreateMount(mount.Name, "audio/mpeg", 128)

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC(),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}
	state := playoutState{Items: []string{mediaID}, TotalItems: 1}
	// LQ mount missing → early return
	d.playNextFromState(entry, state, 0, mount.Name)
}

func TestPlayNextFromState_NormalPath_BothMountsExist(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "next-normal-" + mountID[:8],
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
		Title:         "Next Normal Track",
		Artist:        "Artist",
		Album:         "Album",
		Path:          "/tmp/next-normal.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	// Pre-create both broadcast mounts.
	d.broadcast.CreateMount(mount.Name, "audio/mpeg", 128)
	d.broadcast.CreateMount(mount.Name+"-lq", "audio/mpeg", 64)

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}
	state := playoutState{
		SourceType: "playlist",
		SourceID:   "pl-123",
		Items:      []string{mediaID},
		TotalItems: 1,
		Position:   0,
	}
	// Should complete without panic.
	d.playNextFromState(entry, state, 0, mount.Name)

	// Verify active state was updated.
	d.mu.Lock()
	active, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Error("expected active state to be set after playNextFromState")
	}
	if ok && active.MediaID != mediaID {
		t.Errorf("active.MediaID = %q, want %q", active.MediaID, mediaID)
	}
}

func TestPlayNextFromState_CrossfadePath_BothMountsExist(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	station := models.Station{
		ID:                  stationID,
		Name:                "XFade Next Station",
		CrossfadeEnabled:    true,
		CrossfadeDurationMs: 2000,
	}
	if err := d.db.Create(&station).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "next-xfade-" + mountID[:8],
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
		Title:         "Next XFade Track",
		Path:          "/tmp/next-xfade.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	// Pre-create both broadcast mounts.
	d.broadcast.CreateMount(mount.Name, "audio/mpeg", 128)
	d.broadcast.CreateMount(mount.Name+"-lq", "audio/mpeg", 64)

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}
	state := playoutState{
		SourceType: "playlist",
		SourceID:   "pl-xfade",
		Items:      []string{mediaID},
		TotalItems: 1,
		Position:   0,
	}
	// Crossfade path — should complete without panic.
	d.playNextFromState(entry, state, 0, mount.Name)

	d.xfadeMu.Lock()
	sess := d.xfadeSessions[mountID]
	d.xfadeMu.Unlock()
	if sess == nil {
		t.Error("expected xfade session after crossfade playNextFromState")
	}
}
