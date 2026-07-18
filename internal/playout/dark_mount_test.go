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

// TestDarkMountGauge_FiresAfterGrace drives checkDeadAir with a station that has
// an entry that should be playing but whose mount has no running pipeline
// (Manager.GetPipeline returns nil). The dark gauge must stay 0 within the 45s
// grace window and flip to 1 once the mount has been dark past it. Time is
// injected via the `now` argument — no wall-clock sleep. A pipeline that
// reappears before the grace elapses must keep the gauge at 0.
func TestDarkMountGauge_FiresAfterGrace(t *testing.T) {
	d, mgr := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	// Isolate this gauge series so a stale value from another test can't leak in.
	telemetry.PlayoutMountDark.DeleteLabelValues(stationID, mountID)

	base := time.Now().UTC()

	// A media entry that should be playing: started well before the dead-air
	// grace so checkDeadAir considers it live, and ends far in the future.
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "media",
		SourceID:   uuid.NewString(),
		StartsAt:   base.Add(-1 * time.Minute),
		EndsAt:     base.Add(10 * time.Minute),
	}
	if err := d.db.Create(&entry).Error; err != nil {
		t.Fatalf("seed schedule entry: %v", err)
	}

	// Mark the mount active so checkDeadAir's own dead-air recovery treats the
	// station as playing and does NOT try to restart the entry. The dark-mount
	// tracking is driven purely by Manager.GetPipeline, which we control below.
	d.mu.Lock()
	d.active[mountID] = playoutState{
		MediaID:    entry.SourceID,
		EntryID:    entry.ID,
		StationID:  stationID,
		SourceType: "media",
		Started:    base,
		Ends:       base.Add(10 * time.Minute),
	}
	d.mu.Unlock()

	// No pipeline registered → GetPipeline returns nil → mount is dark.
	dark := func() float64 {
		return testutil.ToFloat64(telemetry.PlayoutMountDark.WithLabelValues(stationID, mountID))
	}

	// First observation: within grace, gauge must be 0.
	d.checkDeadAir(ctx, base)
	if got := dark(); got != 0 {
		t.Fatalf("dark gauge within grace = %v, want 0", got)
	}

	// A pipeline reappears well before the grace elapses → dark-since cleared,
	// gauge stays 0.
	mgr.pipelines[mountID] = newMockPipeline()
	d.checkDeadAir(ctx, base.Add(10*time.Second))
	if got := dark(); got != 0 {
		t.Fatalf("dark gauge after transient recovery = %v, want 0", got)
	}

	// Pipeline goes away again; a fresh dark-since window starts.
	delete(mgr.pipelines, mountID)
	d.checkDeadAir(ctx, base.Add(20*time.Second))
	if got := dark(); got != 0 {
		t.Fatalf("dark gauge after re-darkening (within grace) = %v, want 0", got)
	}

	// Past the 45s grace measured from the re-darkening: gauge flips to 1.
	darkNow := base.Add(20 * time.Second).Add(46 * time.Second)
	d.checkDeadAir(ctx, darkNow)
	if got := dark(); got != 1 {
		t.Fatalf("dark gauge past grace = %v, want 1", got)
	}

	// Deschedule the mount: remove the entry so no station should drive it, then
	// force a schedule refresh. The dark gauge series must be reset to 0 rather
	// than reading 1 forever for a mount that is no longer scheduled.
	if err := d.db.Where("id = ?", entry.ID).Delete(&models.ScheduleEntry{}).Error; err != nil {
		t.Fatalf("delete schedule entry: %v", err)
	}
	d.markScheduleDirty()
	// Drop the active mount too so the entry is fully gone from the director's view.
	d.mu.Lock()
	delete(d.active, mountID)
	d.mu.Unlock()

	d.checkDeadAir(ctx, darkNow.Add(1*time.Second))
	if got := dark(); got != 0 {
		t.Fatalf("dark gauge after deschedule = %v, want 0 (stale series)", got)
	}
}
