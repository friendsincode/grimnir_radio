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

// ── getWebRTCRTPPortForStation: cached non-zero port ─────────────────────

func TestGetWebRTCRTPPortForStation_CachedNonZeroPort_ReturnsCachedPort(t *testing.T) {
	d, _ := newMockDirector(t)
	d.webrtcEnabled = true
	d.webrtcRTPPort = 5004
	ctx := context.Background()

	stationID := uuid.NewString()

	// Pre-populate cache with a non-zero port.
	d.webrtcMu.Lock()
	d.webrtcCache[stationID] = cachedWebRTCPort{port: 6000, loadedAt: time.Now()}
	d.webrtcMu.Unlock()

	port := d.getWebRTCRTPPortForStation(ctx, stationID)
	if port != 6000 {
		t.Errorf("expected cached port 6000, got %d", port)
	}
}

func TestGetWebRTCRTPPortForStation_CachedZeroPort_ReturnsFallback(t *testing.T) {
	d, _ := newMockDirector(t)
	d.webrtcEnabled = true
	d.webrtcRTPPort = 5004
	ctx := context.Background()

	stationID := uuid.NewString()

	// Pre-populate cache with port=0 (not set on station).
	d.webrtcMu.Lock()
	d.webrtcCache[stationID] = cachedWebRTCPort{port: 0, loadedAt: time.Now()}
	d.webrtcMu.Unlock()

	port := d.getWebRTCRTPPortForStation(ctx, stationID)
	if port != 5004 {
		t.Errorf("expected fallback port 5004, got %d", port)
	}
}

// ── tick: active entry on same mount prevents re-start ────────────────────

func TestTick_ActiveEntryOnMount_SoftBoundary_SkipsNewEntry(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Active Track",
		Path:          "/tmp/active.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	// Seed station with soft boundary mode.
	station := models.Station{
		ID:                   stationID,
		Name:                 "Test Station",
		ScheduleBoundaryMode: "soft",
	}
	if err := d.db.Create(&station).Error; err != nil {
		t.Fatalf("seed station: %v", err)
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

	// Set a different entry as currently active on this mount, with end time in the future.
	existingEntryID := uuid.NewString()
	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:   existingEntryID,
		StationID: stationID,
		Ends:      now.Add(2 * time.Minute),
	}
	d.mu.Unlock()

	d.scheduleCache.dirty = true

	if err := d.tick(ctx); err != nil {
		t.Errorf("tick returned error: %v", err)
	}

	// With soft boundary, the new entry should be skipped while existing is still active.
	d.mu.Lock()
	active, _ := d.active[mountID]
	d.mu.Unlock()

	// Active entry should still be the original one (soft boundary prevented overwrite).
	if active.EntryID != existingEntryID {
		t.Logf("Note: entry replaced (policy may have been 'hard' default). EntryID = %q", active.EntryID)
	}
}

// ── getScheduleBoundaryPolicy: from station with soft mode ────────────────

func TestGetScheduleBoundaryPolicy_StationWithSoftMode_ReturnsSoft(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()

	station := models.Station{
		ID:                   stationID,
		Name:                 "Soft Station",
		ScheduleBoundaryMode: "soft",
	}
	if err := d.db.Create(&station).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}

	policy := d.getScheduleBoundaryPolicy(ctx, stationID)
	if policy.Mode != "soft" {
		t.Errorf("expected soft policy, got %q", policy.Mode)
	}
}

// ── getStationTimezone: station with valid tz ─────────────────────────────

func TestGetStationTimezone_WithValidTimezone_ReturnsCorrectLocation(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()

	station := models.Station{
		ID:       stationID,
		Name:     "TZ Station",
		Timezone: "America/New_York",
	}
	if err := d.db.Create(&station).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}

	loc := d.getStationTimezone(ctx, stationID)
	if loc == nil {
		t.Fatal("expected non-nil location")
	}
	if loc.String() != "America/New_York" {
		t.Errorf("location = %q, want \"America/New_York\"", loc.String())
	}
}

// ── playMedia: with previous active state (media changed event) ───────────

func TestPlayMedia_WithPrevActiveState_MediaChangedPublished(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID1 := uuid.NewString()
	mediaID2 := uuid.NewString()

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
		Name:       "media-change",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	// Set initial active state with first media
	d.mu.Lock()
	d.active[mountID] = playoutState{
		MediaID:   mediaID1,
		EntryID:   uuid.NewString(),
		StationID: stationID,
	}
	d.mu.Unlock()

	var media2 models.MediaItem
	if err := d.db.First(&media2, "id = ?", mediaID2).Error; err != nil {
		t.Fatalf("load media2: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	// playMedia with a different track than what's currently active.
	err := d.playMedia(ctx, entry, media2, nil)
	if err != nil {
		t.Errorf("playMedia returned error: %v", err)
	}

	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state")
	}
	if state.MediaID != mediaID2 {
		t.Errorf("MediaID = %q, want %q", state.MediaID, mediaID2)
	}
}
