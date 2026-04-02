/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
)

// ── buildBroadcastPipeline ────────────────────────────────────────────────

func TestBuildBroadcastPipeline_UnknownFormat_ReturnsError(t *testing.T) {
	d, _ := newMockDirector(t)
	mount := models.Mount{Format: "unknown-xyz", Bitrate: 128, SampleRate: 44100, Channels: 2}
	_, err := d.buildBroadcastPipeline("/tmp/test.mp3", mount)
	if err == nil {
		t.Error("expected error for unknown format")
	}
}

// ── buildDualBroadcastPipeline ────────────────────────────────────────────

func TestBuildDualBroadcastPipeline_AllFormats(t *testing.T) {
	d, _ := newMockDirector(t)

	for _, format := range []string{"aac", "ogg", "vorbis", "mp3", ""} {
		mount := models.Mount{Name: "test", Format: format, Bitrate: 128, SampleRate: 44100, Channels: 2}
		_, pipeline, err := d.buildDualBroadcastPipeline("/tmp/test.mp3", mount, 128, 64, 0, 0, 0)
		if err != nil {
			t.Errorf("format %q: unexpected error: %v", format, err)
		}
		if pipeline == "" {
			t.Errorf("format %q: empty pipeline", format)
		}
	}
}

func TestBuildDualBroadcastPipeline_WebRTCBranch(t *testing.T) {
	d, _ := newMockDirector(t)
	d.webrtcEnabled = true

	mount := models.Mount{Name: "webrtc-test", Format: "mp3", Bitrate: 128, SampleRate: 44100, Channels: 2}
	_, pipeline, err := d.buildDualBroadcastPipeline("/tmp/test.mp3", mount, 128, 64, 5004, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(pipeline, "udpsink") {
		t.Error("expected WebRTC udpsink in pipeline when webrtcEnabled=true and port>0")
	}
}

func TestBuildDualBroadcastPipeline_SeekPath_RealFile(t *testing.T) {
	d, _ := newMockDirector(t)

	// Create a real file so os.Stat and os.Open succeed.
	f, err := os.CreateTemp("", "grimnir-seek-test-*.mp3")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	// Write 1MB of data so byteOffset is meaningful.
	data := make([]byte, 1024*1024)
	if _, err := f.Write(data); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())

	mount := models.Mount{Name: "seek-test", Format: "mp3", Bitrate: 128, SampleRate: 44100, Channels: 2}
	seekFile, pipeline, err := d.buildDualBroadcastPipeline(
		f.Name(), mount, 128, 64, 0,
		30000,  // seekOffsetMS = 30s
		180000, // fileDurationMS = 3 min
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seekFile == nil {
		t.Error("expected non-nil seekFile when seek succeeded")
	} else {
		seekFile.Close()
	}
	if !strings.Contains(pipeline, "fdsrc") {
		t.Error("expected fdsrc in pipeline when seeking")
	}
}

func TestBuildDualBroadcastPipeline_SeekPath_ZeroOffsets(t *testing.T) {
	d, _ := newMockDirector(t)

	mount := models.Mount{Name: "no-seek-test", Format: "mp3", Bitrate: 128, SampleRate: 44100, Channels: 2}
	seekFile, pipeline, err := d.buildDualBroadcastPipeline(
		"/tmp/test.mp3", mount, 128, 64, 0,
		0, // no seek
		0,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seekFile != nil {
		t.Error("expected nil seekFile when seekOffsetMS=0")
		seekFile.Close()
	}
	if !strings.Contains(pipeline, "filesrc") {
		t.Error("expected filesrc in pipeline when not seeking")
	}
}

// ── applyTrackOverrides: map[string]string typed override ─────────────────

func TestApplyTrackOverrides_MapStringString(t *testing.T) {
	d, _ := newMockDirector(t)

	mediaID1 := uuid.NewString()
	mediaID2 := uuid.NewString()
	entry := models.ScheduleEntry{
		StationID: uuid.NewString(),
		Metadata: map[string]any{
			"track_overrides": map[string]string{
				"0": mediaID2,
			},
		},
	}
	// map[string]string type case: overrides index 0 with mediaID2.
	// Both items have the same stationID → but mediaID2 not in DB → keep original.
	result := d.applyTrackOverrides(context.Background(), entry, []string{mediaID1})
	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
	}
}

func TestApplyTrackOverrides_UnknownType_ReturnsItems(t *testing.T) {
	d, _ := newMockDirector(t)

	mediaID := uuid.NewString()
	entry := models.ScheduleEntry{
		StationID: uuid.NewString(),
		Metadata: map[string]any{
			"track_overrides": 12345, // unknown type → return original
		},
	}
	result := d.applyTrackOverrides(context.Background(), entry, []string{mediaID})
	if len(result) != 1 || result[0] != mediaID {
		t.Errorf("expected [%s], got %v", mediaID, result)
	}
}

func TestApplyTrackOverrides_RemoveItem(t *testing.T) {
	d, _ := newMockDirector(t)

	mediaID := uuid.NewString()
	entry := models.ScheduleEntry{
		StationID: uuid.NewString(),
		Metadata: map[string]any{
			"track_overrides": map[string]any{
				"0": "__remove__",
			},
		},
	}
	result := d.applyTrackOverrides(context.Background(), entry, []string{mediaID})
	if len(result) != 0 {
		t.Errorf("expected empty result after __remove__, got %v", result)
	}
}

// ── playMediaByID: overrides remove the only item ─────────────────────────

func TestPlayMediaByID_ItemRemovedByOverrides(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	if err := d.db.Create(&models.Mount{
		ID:        mountID,
		StationID: stationID,
		Name:      "pmbyid-rm",
		Format:    "mp3", Bitrate: 128, SampleRate: 44100, Channels: 2,
	}).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
		Metadata: map[string]any{
			"track_overrides": map[string]any{
				"0": "__remove__", // remove the only item
			},
		},
	}

	// Should return nil (early exit with warning log) not an error.
	if err := d.playMediaByID(context.Background(), entry, mediaID, "clk-123", "Clock Name"); err != nil {
		t.Errorf("playMediaByID returned error: %v", err)
	}
}

// ── startPlaylistByID: empty playlist ID ─────────────────────────────────

func TestStartPlaylistByID_PlaylistNotFound(t *testing.T) {
	d, _ := newMockDirector(t)

	entry := models.ScheduleEntry{
		ID:        uuid.NewString(),
		StationID: uuid.NewString(),
		MountID:   uuid.NewString(),
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	// Non-existent playlist → should return error.
	err := d.startPlaylistByID(context.Background(), entry, uuid.NewString(), "clk-456", "Clock")
	if err == nil {
		t.Error("expected error for non-existent playlist")
	}
}
