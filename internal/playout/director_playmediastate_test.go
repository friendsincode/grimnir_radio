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

// helper to build a standard test environment for playMediaWithState tests.
func newPlayMediaStateEnv(t *testing.T) (d *Director, stationID, mountID string, media models.MediaItem, entry models.ScheduleEntry) {
	t.Helper()
	d, _ = newMockDirector(t)

	stationID = uuid.NewString()
	mountID = uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "pmstate-main-" + mountID[:8],
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	media = models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "State Track",
		Artist:        "Test Artist",
		Album:         "Test Album",
		Path:          "/tmp/state-track.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	entry = models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}
	return
}

func TestPlayMediaWithState_BasicPlaylist(t *testing.T) {
	d, stationID, _, media, entry := newPlayMediaStateEnv(t)
	ctx := context.Background()

	items := []string{media.ID, uuid.NewString(), uuid.NewString()}
	err := d.playMediaWithState(ctx, entry, media, "playlist", "pl-123", 0, items, nil)
	if err != nil {
		t.Errorf("playMediaWithState returned error: %v", err)
	}

	d.mu.Lock()
	state, ok := d.active[entry.MountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after playMediaWithState")
	}
	if state.SourceType != "playlist" {
		t.Errorf("SourceType = %q, want %q", state.SourceType, "playlist")
	}
	if state.TotalItems != len(items) {
		t.Errorf("TotalItems = %d, want %d", state.TotalItems, len(items))
	}
	_ = stationID
}

func TestPlayMediaWithState_SmartBlockSource(t *testing.T) {
	d, _, _, media, entry := newPlayMediaStateEnv(t)
	ctx := context.Background()

	items := []string{media.ID}
	err := d.playMediaWithState(ctx, entry, media, "smart_block", "sb-456", 0, items, nil)
	if err != nil {
		t.Errorf("playMediaWithState (smart_block) returned error: %v", err)
	}
}

func TestPlayMediaWithState_AACFormat(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "pmstate-aac-" + mountID[:8],
		Format:     "aac",
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
		Title:         "AAC State Track",
		Path:          "/tmp/state-aac.aac",
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

	err := d.playMediaWithState(ctx, entry, media, "playlist", "pl-aac", 0, []string{media.ID}, nil)
	if err != nil {
		t.Errorf("playMediaWithState (aac) returned error: %v", err)
	}
}

func TestPlayMediaWithState_OGGFormat(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "pmstate-ogg-" + mountID[:8],
		Format:     "ogg",
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
		Title:         "OGG State Track",
		Path:          "/tmp/state-ogg.ogg",
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

	err := d.playMediaWithState(ctx, entry, media, "playlist", "pl-ogg", 0, []string{media.ID}, nil)
	if err != nil {
		t.Errorf("playMediaWithState (ogg) returned error: %v", err)
	}
}

func TestPlayMediaWithState_WithExtraPayload(t *testing.T) {
	d, _, _, media, entry := newPlayMediaStateEnv(t)
	ctx := context.Background()

	extra := map[string]any{
		"smart_block_id": "sb-789",
		"custom_key":     "custom_val",
	}
	err := d.playMediaWithState(ctx, entry, media, "smart_block", "sb-789", 2, []string{media.ID, uuid.NewString()}, extra)
	if err != nil {
		t.Errorf("playMediaWithState with extra payload returned error: %v", err)
	}
}

func TestPlayMediaWithState_CrossfadeEnabled(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	station := models.Station{
		ID:                  stationID,
		Name:                "XFade State Station",
		CrossfadeEnabled:    true,
		CrossfadeDurationMs: 2000,
	}
	if err := d.db.Create(&station).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "xfade-state-main-" + mountID[:8],
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
		Title:         "XFade State Track",
		Path:          "/tmp/xfade-state.mp3",
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

	err := d.playMediaWithState(ctx, entry, media, "playlist", "pl-xfade", 0, []string{media.ID}, nil)
	if err != nil {
		t.Errorf("playMediaWithState (crossfade) returned error: %v", err)
	}

	d.xfadeMu.Lock()
	sess := d.xfadeSessions[mountID]
	d.xfadeMu.Unlock()
	if sess == nil {
		t.Error("expected xfade session after crossfade playMediaWithState")
	}
}

func TestPlayMediaWithState_CrossfadeEnabled_ExistingSession(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	station := models.Station{
		ID:                  stationID,
		Name:                "XFade Exist State Station",
		CrossfadeEnabled:    true,
		CrossfadeDurationMs: 2000,
	}
	if err := d.db.Create(&station).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "xfade-exist-state-" + mountID[:8],
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
		Title:         "XFade Exist State Track",
		Path:          "/tmp/xfade-exist-state.mp3",
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

	// First call: creates session.
	if err := d.playMediaWithState(ctx, entry, media, "playlist", "pl-1", 0, []string{media.ID}, nil); err != nil {
		t.Fatalf("first playMediaWithState returned error: %v", err)
	}

	// Second call: reuses existing session.
	entry2 := entry
	entry2.ID = uuid.NewString()
	if err := d.playMediaWithState(ctx, entry2, media, "playlist", "pl-1", 1, []string{media.ID, uuid.NewString()}, nil); err != nil {
		t.Errorf("second playMediaWithState returned error: %v", err)
	}
}

func TestPlayMediaWithState_WithoutMount_UsesDefaults(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString() // not in DB

	media := models.MediaItem{
		ID:            uuid.NewString(),
		StationID:     stationID,
		Title:         "Default Mount State Track",
		Path:          "/tmp/default-mount-state.mp3",
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

	err := d.playMediaWithState(ctx, entry, media, "playlist", "pl-noMount", 0, []string{media.ID}, nil)
	if err != nil {
		t.Errorf("playMediaWithState without mount returned error: %v", err)
	}
}
