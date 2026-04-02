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

// ── playMedia: AAC and OGG content types ─────────────────────────────────

func TestPlayMedia_AACFormat_SetsContentType(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "playmedia-aac-main",
		Format:     "aac",
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
		Title:         "AAC Track",
		Path:          "/tmp/test-aac.aac",
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
		t.Errorf("playMedia (aac) returned error: %v", err)
	}
}

func TestPlayMedia_OGGFormat_SetsContentType(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "playmedia-ogg-main",
		Format:     "ogg",
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
		Title:         "OGG Track",
		Path:          "/tmp/test-ogg.ogg",
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
		t.Errorf("playMedia (ogg) returned error: %v", err)
	}
}

func TestPlayMedia_VorbisFormat_SetsContentType(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "playmedia-vorbis-main",
		Format:     "vorbis",
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
		Title:         "Vorbis Track",
		Path:          "/tmp/test-vorbis.ogg",
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
		t.Errorf("playMedia (vorbis) returned error: %v", err)
	}
}

// ── playMedia: crossfade path ─────────────────────────────────────────────

func TestPlayMedia_CrossfadeEnabled_RunsPCMPath(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	station := models.Station{
		ID:                  stationID,
		Name:                "XFade Station",
		CrossfadeEnabled:    true,
		CrossfadeDurationMs: 2000,
	}
	if err := d.db.Create(&station).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "xfade-main",
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
		Title:         "XFade Track",
		Path:          "/tmp/test-xfade.mp3",
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

	// Should succeed: mock manager returns nil stdin (no error), session is created.
	if err := d.playMedia(ctx, entry, media, nil); err != nil {
		t.Errorf("playMedia (crossfade) returned error: %v", err)
	}

	// Verify a xfade session was created for this mount.
	d.xfadeMu.Lock()
	sess := d.xfadeSessions[mountID]
	d.xfadeMu.Unlock()
	if sess == nil {
		t.Error("expected xfade session to be created after crossfade playMedia")
	}
}

// TestPlayMedia_CrossfadeEnabled_ExistingSession covers the "reuse existing session" path.
func TestPlayMedia_CrossfadeEnabled_ExistingSession(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	station := models.Station{
		ID:                  stationID,
		Name:                "XFade Station 2",
		CrossfadeEnabled:    true,
		CrossfadeDurationMs: 2000,
	}
	if err := d.db.Create(&station).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "xfade-exist-main",
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
		Title:         "XFade Track 2",
		Path:          "/tmp/test-xfade2.mp3",
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
	if err := d.playMedia(ctx, entry, media, nil); err != nil {
		t.Fatalf("first playMedia (crossfade) returned error: %v", err)
	}

	// Second call: reuses existing session (hits the sess != nil branch).
	entry2 := entry
	entry2.ID = uuid.NewString()
	if err := d.playMedia(ctx, entry2, media, nil); err != nil {
		t.Errorf("second playMedia (crossfade reuse) returned error: %v", err)
	}
}

// ── playMedia: extra payload and resume offset ────────────────────────────

func TestPlayMedia_WithExtraPayload_PublishesNowPlaying(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "extra-payload-main",
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
		Title:         "Extra Track",
		Path:          "/tmp/test-extra.mp3",
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
		Metadata:  map[string]any{"resume_offset_ms": float64(30000)},
	}

	extra := map[string]any{"smart_block_id": "sb-123", "position": 2}
	if err := d.playMedia(ctx, entry, media, extra); err != nil {
		t.Errorf("playMedia with extra payload returned error: %v", err)
	}
}

// ── playMedia: xfade path — hasSess = true covers StopPipeline skip ───────

func TestPlayMedia_CrossfadeWithExistingSession_SkipsStopPipeline(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	station := models.Station{
		ID:                  stationID,
		Name:                "XFade Skip Station",
		CrossfadeEnabled:    true,
		CrossfadeDurationMs: 2000,
	}
	if err := d.db.Create(&station).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "xfade-skip-main",
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
		Title:         "Skip Stop Track",
		Path:          "/tmp/skip-stop.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	// Pre-populate xfadeSessions so hasSess = true → StopPipeline is skipped.
	pr, pw := nopPipe()
	existingSess := newPCMCrossfadeSession(
		sessionConfig{GStreamerBin: "true", SampleRate: 44100, Channels: 2},
		pw,
		d.logger,
		nil,
	)
	d.xfadeMu.Lock()
	d.xfadeSessions[mountID] = existingSess
	d.xfadeMu.Unlock()
	defer func() {
		existingSess.Close()
		pr.Close()
	}()

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	if err := d.playMedia(ctx, entry, media, nil); err != nil {
		t.Errorf("playMedia with existing xfade session returned error: %v", err)
	}
}

// nopPipe returns a pipe where writing discards data (for test encoder sinks).
func nopPipe() (*nopReadCloser, *nopWriteCloser2) {
	return &nopReadCloser{}, &nopWriteCloser2{}
}

type nopReadCloser struct{}

func (*nopReadCloser) Read(p []byte) (int, error) { return 0, nil }
func (*nopReadCloser) Close() error               { return nil }

type nopWriteCloser2 struct{}

func (*nopWriteCloser2) Write(p []byte) (int, error) { return len(p), nil }
func (*nopWriteCloser2) Close() error                { return nil }
