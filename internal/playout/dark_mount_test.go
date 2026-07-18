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
	"github.com/friendsincode/grimnir_radio/internal/telemetry"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestPlayoutActivePipelinesGauge drives the director through a real start
// (playMedia → EnsurePipelineWithDualOutput) and stop (StopStation →
// StopPipeline) and asserts the per-mount gauge tracks the pipeline lifecycle.
func TestPlayoutActivePipelinesGauge(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	// Isolate this gauge series so a stale value from another test can't leak in.
	telemetry.PlayoutActivePipelines.DeleteLabelValues(stationID, mountID)

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "dark-mount-gauge-main",
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
		Title:         "Gauge Track",
		Path:          "/tmp/test-gauge.mp3",
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

	if err := d.playMedia(ctx, entry, media, nil); err != nil {
		t.Fatalf("playMedia returned error: %v", err)
	}

	if got := testutil.ToFloat64(telemetry.PlayoutActivePipelines.WithLabelValues(stationID, mountID)); got != 1 {
		t.Fatalf("active-pipelines gauge after start = %v, want 1", got)
	}

	// Active state is required for StopStation to count and stop the mount.
	d.mu.Lock()
	d.active[mountID] = playoutState{
		MediaID:   mediaID,
		EntryID:   entry.ID,
		StationID: stationID,
		Started:   time.Now().UTC(),
		Ends:      time.Now().UTC().Add(5 * time.Minute),
	}
	d.mu.Unlock()

	if _, err := d.StopStation(ctx, stationID); err != nil {
		t.Fatalf("StopStation returned error: %v", err)
	}

	if got := testutil.ToFloat64(telemetry.PlayoutActivePipelines.WithLabelValues(stationID, mountID)); got != 0 {
		t.Fatalf("active-pipelines gauge after stop = %v, want 0", got)
	}
}
