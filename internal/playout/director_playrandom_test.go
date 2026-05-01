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

// ── playRandomNextTrack ───────────────────────────────────────────────────

func TestPlayRandomNextTrack_NoMedia(t *testing.T) {
	d, _ := newMockDirector(t)

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: uuid.NewString(), // no media in DB for this station
		MountID:   uuid.NewString(),
		StartsAt:  time.Now().UTC(),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}
	// No media → early return, no panic
	d.playRandomNextTrack(entry, "main")
}

func TestPlayRandomNextTrack_MountNotFound(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	media := models.MediaItem{
		ID:            uuid.NewString(),
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
		MountID:   uuid.NewString(), // not in DB
		StartsAt:  time.Now().UTC(),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}
	// mount not found → early return
	d.playRandomNextTrack(entry, "main")
}

func TestPlayRandomNextTrack_AutoCreatesMissingMounts(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "random-no-bcast-" + mountID[:8],
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	media := models.MediaItem{
		ID:            uuid.NewString(),
		StationID:     stationID,
		Title:         "Random No Bcast",
		Path:          "/tmp/random-no-bcast.mp3",
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
	// No broadcast mounts pre-created — they should be auto-created and pipeline started.
	d.playRandomNextTrack(entry, mount.Name)

	if d.broadcast.GetMount(mount.Name) == nil {
		t.Error("expected HQ broadcast mount to be auto-created")
	}
	if d.broadcast.GetMount(mount.Name+"-lq") == nil {
		t.Error("expected LQ broadcast mount to be auto-created")
	}
	d.mu.Lock()
	_, active := d.active[mountID]
	d.mu.Unlock()
	if !active {
		t.Error("expected active playout state after auto-creating mounts")
	}
}

func TestPlayRandomNextTrack_NormalPath(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "random-normal-" + mountID[:8],
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	media := models.MediaItem{
		ID:            uuid.NewString(),
		StationID:     stationID,
		Title:         "Random Normal Track",
		Artist:        "Random Artist",
		Path:          "/tmp/random-normal.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	// Both broadcast mounts.
	d.broadcast.CreateMount(mount.Name, "audio/mpeg", 128)
	d.broadcast.CreateMount(mount.Name+"-lq", "audio/mpeg", 64)

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}
	// Should complete without panic.
	d.playRandomNextTrack(entry, mount.Name)

	d.mu.Lock()
	active, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Error("expected active state after playRandomNextTrack")
	}
	if ok && active.MediaID != media.ID {
		t.Errorf("active.MediaID = %q, want %q", active.MediaID, media.ID)
	}
}
