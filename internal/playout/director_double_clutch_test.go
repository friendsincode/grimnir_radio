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

// TestTick_NoDoubleClutchOnEqualCreatedAtOverlap reproduces the on-air
// "double-clutch": two overlapping media instances on one mount whose created_at
// the overlap-winner check can't separate (they're equal) both reached
// handleEntry in a single tick, building the broadcast pipeline twice back-to-back
// (observed on mount rlmradioxyz / RLMradio.xyz-M). The tick must launch the mount
// exactly once per pass.
func TestTick_NoDoubleClutchOnEqualCreatedAtOverlap(t *testing.T) {
	now := time.Now().UTC()
	d, mgr := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "double-clutch-" + mountID[:8],
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	mediaA := uuid.NewString()
	mediaB := uuid.NewString()
	for _, m := range []models.MediaItem{
		{ID: mediaA, StationID: stationID, Title: "Track A", Path: "/tmp/a.mp3", Duration: 4 * time.Minute, AnalysisState: models.AnalysisComplete},
		{ID: mediaB, StationID: stationID, Title: "Track B", Path: "/tmp/b.mp3", Duration: 4 * time.Minute, AnalysisState: models.AnalysisComplete},
	} {
		if err := d.db.Create(&m).Error; err != nil {
			t.Fatalf("seed media: %v", err)
		}
	}

	// Two instances on the SAME mount, both covering now, with the SAME created_at
	// so the overlap-winner check (skips only a STRICTLY newer instance) can't
	// drop either. Before the fix, both launch -> two pipeline builds.
	created := now.Add(-1 * time.Hour)
	for _, e := range []models.ScheduleEntry{
		{ID: uuid.NewString(), StationID: stationID, MountID: mountID, SourceType: "media", SourceID: mediaA, IsInstance: true, StartsAt: now.Add(-2 * time.Minute), EndsAt: now.Add(5 * time.Minute), CreatedAt: created},
		{ID: uuid.NewString(), StationID: stationID, MountID: mountID, SourceType: "media", SourceID: mediaB, IsInstance: true, StartsAt: now.Add(-2 * time.Minute), EndsAt: now.Add(5 * time.Minute), CreatedAt: created},
	} {
		if err := d.db.Create(&e).Error; err != nil {
			t.Fatalf("seed entry: %v", err)
		}
	}

	d.markScheduleDirty()
	if err := d.tick(ctx); err != nil {
		t.Fatalf("tick returned error: %v", err)
	}

	if got := mgr.ensureCalls[mountID]; got != 1 {
		t.Errorf("pipeline builds for mount = %d, want exactly 1 (double-clutch)", got)
	}
	d.mu.Lock()
	_, active := d.active[mountID]
	d.mu.Unlock()
	if !active {
		t.Error("expected the mount to be active after tick")
	}
}
