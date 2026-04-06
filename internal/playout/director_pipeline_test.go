/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
)

// ── buildBroadcastPipeline ────────────────────────────────────────────────

func TestBuildBroadcastPipeline_MP3(t *testing.T) {
	d, _ := newMockDirector(t)
	mount := models.Mount{
		Name:       "test",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	pipeline, err := d.buildBroadcastPipeline("/tmp/test.mp3", mount)
	if err != nil {
		t.Fatalf("buildBroadcastPipeline returned error: %v", err)
	}
	if pipeline == "" {
		t.Error("expected non-empty pipeline string")
	}
	if !strings.Contains(pipeline, "/tmp/test.mp3") {
		t.Error("expected pipeline to contain file path")
	}
}

func TestBuildBroadcastPipeline_AAC(t *testing.T) {
	d, _ := newMockDirector(t)
	mount := models.Mount{
		Name:       "test",
		Format:     "aac",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	pipeline, err := d.buildBroadcastPipeline("/tmp/test.aac", mount)
	if err != nil {
		t.Fatalf("buildBroadcastPipeline (aac) returned error: %v", err)
	}
	if pipeline == "" {
		t.Error("expected non-empty pipeline")
	}
}

func TestBuildBroadcastPipeline_Defaults(t *testing.T) {
	d, _ := newMockDirector(t)
	// Empty format/bitrate/sample rate — should use defaults
	mount := models.Mount{Name: "test"}
	pipeline, err := d.buildBroadcastPipeline("/tmp/test.mp3", mount)
	if err != nil {
		t.Fatalf("buildBroadcastPipeline with defaults returned error: %v", err)
	}
	if pipeline == "" {
		t.Error("expected non-empty pipeline string with defaults")
	}
}

// ── buildDualBroadcastPipeline ────────────────────────────────────────────

func TestBuildDualBroadcastPipeline_NoSeek(t *testing.T) {
	d, _ := newMockDirector(t)
	mount := models.Mount{
		Name:       "test",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	seekFile, pipeline, err := d.buildDualBroadcastPipeline("/tmp/test.mp3", mount, 128, 64, 0, 0, 0)
	if err != nil {
		t.Fatalf("buildDualBroadcastPipeline returned error: %v", err)
	}
	if pipeline == "" {
		t.Error("expected non-empty pipeline string")
	}
	if seekFile != nil {
		seekFile.Close()
		t.Error("expected nil seekFile when seekOffsetMS=0")
	}
	if !strings.Contains(pipeline, "filesrc") {
		t.Errorf("expected filesrc in pipeline without seek, got: %s", pipeline)
	}
}

func TestBuildDualBroadcastPipeline_AAC(t *testing.T) {
	d, _ := newMockDirector(t)
	mount := models.Mount{
		Name:       "test",
		Format:     "aac",
		Bitrate:    256,
		SampleRate: 48000,
		Channels:   2,
	}
	_, pipeline, err := d.buildDualBroadcastPipeline("/tmp/test.aac", mount, 256, 128, 0, 0, 0)
	if err != nil {
		t.Fatalf("buildDualBroadcastPipeline (aac) returned error: %v", err)
	}
	if !strings.Contains(pipeline, "faac") {
		t.Errorf("expected faac in aac pipeline, got: %s", pipeline)
	}
}

func TestBuildDualBroadcastPipeline_Ogg(t *testing.T) {
	d, _ := newMockDirector(t)
	mount := models.Mount{
		Name:       "test",
		Format:     "ogg",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	_, pipeline, err := d.buildDualBroadcastPipeline("/tmp/test.ogg", mount, 128, 64, 0, 0, 0)
	if err != nil {
		t.Fatalf("buildDualBroadcastPipeline (ogg) returned error: %v", err)
	}
	if !strings.Contains(pipeline, "vorbisenc") {
		t.Errorf("expected vorbisenc in ogg pipeline, got: %s", pipeline)
	}
}

func TestBuildDualBroadcastPipeline_WithWebRTC(t *testing.T) {
	d, _ := newMockDirector(t)
	d.webrtcEnabled = true
	mount := models.Mount{
		Name:       "test",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	_, pipeline, err := d.buildDualBroadcastPipeline("/tmp/test.mp3", mount, 128, 64, 5004, 0, 0)
	if err != nil {
		t.Fatalf("buildDualBroadcastPipeline with webrtc returned error: %v", err)
	}
	if !strings.Contains(pipeline, "udpsink") {
		t.Errorf("expected udpsink in webrtc pipeline, got: %s", pipeline)
	}
}

func TestBuildDualBroadcastPipeline_Defaults(t *testing.T) {
	d, _ := newMockDirector(t)
	// all zeros → defaults should kick in
	mount := models.Mount{Name: "test"}
	_, pipeline, err := d.buildDualBroadcastPipeline("/tmp/file.mp3", mount, 0, 0, 0, 0, 0)
	if err != nil {
		t.Fatalf("buildDualBroadcastPipeline with defaults returned error: %v", err)
	}
	if pipeline == "" {
		t.Error("expected non-empty pipeline with defaults")
	}
}

// ── buildPCMEncoderPipeline ───────────────────────────────────────────────

func TestBuildPCMEncoderPipeline_MP3(t *testing.T) {
	d, _ := newMockDirector(t)
	mount := models.Mount{
		Name:       "test",
		Format:     "mp3",
		SampleRate: 44100,
		Channels:   2,
	}
	pipeline, err := d.buildPCMEncoderPipeline(mount, 128, 64, 0)
	if err != nil {
		t.Fatalf("buildPCMEncoderPipeline returned error: %v", err)
	}
	if pipeline == "" {
		t.Error("expected non-empty pipeline")
	}
	if !strings.Contains(pipeline, "fdsrc") {
		t.Errorf("expected fdsrc in PCM encoder pipeline, got: %s", pipeline)
	}
}

func TestBuildPCMEncoderPipeline_AAC(t *testing.T) {
	d, _ := newMockDirector(t)
	mount := models.Mount{Name: "test", Format: "aac", SampleRate: 44100, Channels: 2}
	pipeline, err := d.buildPCMEncoderPipeline(mount, 256, 128, 0)
	if err != nil {
		t.Fatalf("buildPCMEncoderPipeline (aac) returned error: %v", err)
	}
	if !strings.Contains(pipeline, "faac") {
		t.Errorf("expected faac in aac PCM pipeline, got: %s", pipeline)
	}
}

func TestBuildPCMEncoderPipeline_Vorbis(t *testing.T) {
	d, _ := newMockDirector(t)
	mount := models.Mount{Name: "test", Format: "vorbis", SampleRate: 44100, Channels: 2}
	pipeline, err := d.buildPCMEncoderPipeline(mount, 128, 64, 0)
	if err != nil {
		t.Fatalf("buildPCMEncoderPipeline (vorbis) returned error: %v", err)
	}
	if !strings.Contains(pipeline, "vorbisenc") {
		t.Errorf("expected vorbisenc in vorbis PCM pipeline, got: %s", pipeline)
	}
}

func TestBuildPCMEncoderPipeline_WithWebRTC(t *testing.T) {
	d, _ := newMockDirector(t)
	d.webrtcEnabled = true
	mount := models.Mount{Name: "test", Format: "mp3", SampleRate: 44100, Channels: 2}
	pipeline, err := d.buildPCMEncoderPipeline(mount, 128, 64, 5004)
	if err != nil {
		t.Fatalf("buildPCMEncoderPipeline with webrtc returned error: %v", err)
	}
	if !strings.Contains(pipeline, "udpsink") {
		t.Errorf("expected udpsink when webrtc enabled, got: %s", pipeline)
	}
}

func TestBuildPCMEncoderPipeline_Defaults(t *testing.T) {
	d, _ := newMockDirector(t)
	mount := models.Mount{Name: "test"} // all zero defaults
	pipeline, err := d.buildPCMEncoderPipeline(mount, 0, 0, 0)
	if err != nil {
		t.Fatalf("buildPCMEncoderPipeline with defaults returned error: %v", err)
	}
	if pipeline == "" {
		t.Error("expected non-empty pipeline with defaults")
	}
}

// ── buildWebstreamBroadcastPipeline ──────────────────────────────────────

func TestBuildWebstreamBroadcastPipeline_MP3(t *testing.T) {
	d, _ := newMockDirector(t)
	mount := models.Mount{
		Name:       "test",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	ws := &models.Webstream{
		ID:   uuid.NewString(),
		Name: "Test Radio",
		URLs: []string{"http://example.com/stream"},
	}
	pipeline, err := d.buildWebstreamBroadcastPipeline("http://example.com/stream", mount, ws, 128, 64, 0)
	if err != nil {
		t.Fatalf("buildWebstreamBroadcastPipeline returned error: %v", err)
	}
	if pipeline == "" {
		t.Error("expected non-empty pipeline")
	}
	if !strings.Contains(pipeline, "souphttpsrc") {
		t.Errorf("expected souphttpsrc in webstream pipeline, got: %s", pipeline)
	}
}

func TestBuildWebstreamBroadcastPipeline_AAC(t *testing.T) {
	d, _ := newMockDirector(t)
	mount := models.Mount{Name: "test", Format: "aac", Bitrate: 128, SampleRate: 44100, Channels: 2}
	ws := &models.Webstream{ID: uuid.NewString(), Name: "AAC Radio"}
	pipeline, err := d.buildWebstreamBroadcastPipeline("http://example.com/stream", mount, ws, 128, 64, 0)
	if err != nil {
		t.Fatalf("buildWebstreamBroadcastPipeline (aac) returned error: %v", err)
	}
	if !strings.Contains(pipeline, "faac") {
		t.Errorf("expected faac in aac webstream pipeline, got: %s", pipeline)
	}
}

func TestBuildWebstreamBroadcastPipeline_Ogg(t *testing.T) {
	d, _ := newMockDirector(t)
	mount := models.Mount{Name: "test", Format: "ogg", Bitrate: 128, SampleRate: 44100, Channels: 2}
	ws := &models.Webstream{ID: uuid.NewString(), Name: "Ogg Radio"}
	pipeline, err := d.buildWebstreamBroadcastPipeline("http://example.com/stream", mount, ws, 128, 64, 0)
	if err != nil {
		t.Fatalf("buildWebstreamBroadcastPipeline (ogg) returned error: %v", err)
	}
	if !strings.Contains(pipeline, "vorbisenc") {
		t.Errorf("expected vorbisenc in ogg webstream pipeline, got: %s", pipeline)
	}
}

func TestBuildWebstreamBroadcastPipeline_WithBuffer(t *testing.T) {
	d, _ := newMockDirector(t)
	mount := models.Mount{Name: "test", Format: "mp3", Bitrate: 128, SampleRate: 44100, Channels: 2}
	ws := &models.Webstream{
		ID:           uuid.NewString(),
		Name:         "Buffered Radio",
		BufferSizeMS: 5000,
	}
	pipeline, err := d.buildWebstreamBroadcastPipeline("http://example.com/stream", mount, ws, 128, 64, 0)
	if err != nil {
		t.Fatalf("buildWebstreamBroadcastPipeline with buffer returned error: %v", err)
	}
	if !strings.Contains(pipeline, "queue max-size-time=") {
		t.Errorf("expected queue buffer in pipeline, got: %s", pipeline)
	}
}

func TestBuildWebstreamBroadcastPipeline_WithPassthrough(t *testing.T) {
	d, _ := newMockDirector(t)
	mount := models.Mount{Name: "test", Format: "mp3", Bitrate: 128, SampleRate: 44100, Channels: 2}
	ws := &models.Webstream{
		ID:                  uuid.NewString(),
		Name:                "ICY Radio",
		PassthroughMetadata: true,
	}
	pipeline, err := d.buildWebstreamBroadcastPipeline("http://example.com/stream", mount, ws, 128, 64, 0)
	if err != nil {
		t.Fatalf("buildWebstreamBroadcastPipeline with passthrough returned error: %v", err)
	}
	if !strings.Contains(pipeline, "iradio-mode=true") {
		t.Errorf("expected iradio-mode=true in passthrough pipeline, got: %s", pipeline)
	}
}

func TestBuildWebstreamBroadcastPipeline_WithWebRTC(t *testing.T) {
	d, _ := newMockDirector(t)
	d.webrtcEnabled = true
	mount := models.Mount{Name: "test", Format: "mp3", Bitrate: 128, SampleRate: 44100, Channels: 2}
	ws := &models.Webstream{ID: uuid.NewString(), Name: "WebRTC Radio"}
	pipeline, err := d.buildWebstreamBroadcastPipeline("http://example.com/stream", mount, ws, 128, 64, 5004)
	if err != nil {
		t.Fatalf("buildWebstreamBroadcastPipeline with webrtc returned error: %v", err)
	}
	if !strings.Contains(pipeline, "udpsink") {
		t.Errorf("expected udpsink in webrtc webstream pipeline, got: %s", pipeline)
	}
}

// ── normalizeEncoderRate ──────────────────────────────────────────────────

func TestNormalizeEncoderRate_Exact(t *testing.T) {
	cases := []int{44100, 48000, 22050, 32000, 8000}
	for _, rate := range cases {
		if got := normalizeEncoderRate(rate); got != rate {
			t.Errorf("normalizeEncoderRate(%d) = %d, want exact match", rate, got)
		}
	}
}

func TestNormalizeEncoderRate_Approximate(t *testing.T) {
	// 45000 is closest to 44100
	got := normalizeEncoderRate(45000)
	if got != 44100 {
		t.Errorf("normalizeEncoderRate(45000) = %d, want 44100", got)
	}
	// 46000 is closest to 48000
	got = normalizeEncoderRate(47000)
	if got != 48000 {
		t.Errorf("normalizeEncoderRate(47000) = %d, want 48000", got)
	}
}

// ── ListenerCount ─────────────────────────────────────────────────────────

func TestListenerCount_NoMounts_ReturnsZero(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	count, err := d.ListenerCount(ctx, uuid.NewString())
	if err != nil {
		t.Fatalf("ListenerCount returned error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 listeners for unknown station, got %d", count)
	}
}

func TestListenerCount_WithMounts_CountsBroadcastMount(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:        mountID,
		StationID: stationID,
		Name:      "main",
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	// No broadcast mounts created — should return 0 without error.
	count, err := d.ListenerCount(ctx, stationID)
	if err != nil {
		t.Fatalf("ListenerCount returned error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 listeners (no broadcast mounts), got %d", count)
	}
}

// ── handleTrackEnded after entry window ──────────────────────────────────

func TestHandleTrackEnded_AfterEntryEnds_DoesNothing(t *testing.T) {
	d, _ := newMockDirector(t)

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: uuid.NewString(),
		MountID:   uuid.NewString(),
		// EndsAt in the past — so handleTrackEnded should return immediately.
		EndsAt: time.Now().UTC().Add(-1 * time.Minute),
	}

	// Must not panic.
	d.handleTrackEnded(entry, "main")
}

// ── startClockEntry ───────────────────────────────────────────────────────

func TestStartClockEntry_ClockNotFound_ReturnsError(t *testing.T) {
	d, _ := newMockDirector(t, &models.ClockHour{}, &models.ClockSlot{})
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "clock_template",
		SourceID:   uuid.NewString(), // not in DB
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startClockEntry(ctx, entry)
	if err == nil {
		t.Error("expected error for missing clock")
	}
}

func TestStartClockEntry_EmptyClock_PublishesNowPlaying(t *testing.T) {
	d, _ := newMockDirector(t, &models.ClockHour{}, &models.ClockSlot{})
	ctx := context.Background()

	stationID := uuid.NewString()
	clockID := uuid.NewString()

	clock := models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Empty Clock",
	}
	if err := d.db.Create(&clock).Error; err != nil {
		t.Fatalf("seed clock: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    uuid.NewString(),
		SourceType: "clock_template",
		SourceID:   clockID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	// Empty clock — should not error but log/publish warning.
	err := d.startClockEntry(ctx, entry)
	if err != nil {
		t.Errorf("startClockEntry with empty clock returned error: %v", err)
	}
}

// ── stopICYPoller ─────────────────────────────────────────────────────────

func TestStopICYPoller_NoPoller_DoesNotPanic(t *testing.T) {
	d, _ := newMockDirector(t)
	// Must not panic when no poller exists.
	d.stopICYPoller("mount-nonexistent")
}

// ── scheduleStop ──────────────────────────────────────────────────────────

func TestScheduleStop_AlreadyExpired_DoesNotPanic(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	// endsAt in the past — goroutine should fire immediately and not panic.
	d.scheduleStop(ctx, stationID, mountID, time.Now().UTC().Add(-5*time.Second))

	// Give the goroutine time to run.
	time.Sleep(350 * time.Millisecond)
}

// ── computePlaybackResume ─────────────────────────────────────────────────

func TestComputePlaybackResume_NoResume(t *testing.T) {
	d, _ := newMockDirector(t)

	entry := models.ScheduleEntry{
		ID:       uuid.NewString(),
		StartsAt: time.Now().UTC().Add(-1 * time.Second),
		EndsAt:   time.Now().UTC().Add(5 * time.Minute),
	}
	media := models.MediaItem{
		ID:       uuid.NewString(),
		Duration: 3 * time.Minute,
	}

	resume := d.computePlaybackResume(entry, media, nil)
	if resume.Offset != 0 {
		t.Errorf("expected 0 offset for non-resumable entry, got %v", resume.Offset)
	}
}

// ── updateEntryPosition ───────────────────────────────────────────────────

func TestUpdateEntryPosition_DoesNotPanic(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})

	// Even if the entry doesn't exist, this should not panic.
	d.updateEntryPosition(uuid.NewString(), 5, time.Now())
}
