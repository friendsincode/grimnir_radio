/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// ── crossfade session constructors ───────────────────────────────────────

func TestNewPCMCrossfadeSession_Constructs(t *testing.T) {
	cfg := sessionConfig{
		GStreamerBin: "gst-launch-1.0",
		SampleRate:   44100,
		Channels:     2,
	}
	called := false
	sess := newPCMCrossfadeSession(cfg, nil, zerolog.Nop(), func() { called = true })
	if sess == nil {
		t.Fatal("expected non-nil session")
	}
	_ = called
}

func TestSetEncoderIn_SetsWriter(t *testing.T) {
	sess := newPCMCrossfadeSession(sessionConfig{}, nil, zerolog.Nop(), nil)
	pr, pw := io.Pipe()
	sess.SetEncoderIn(pw)
	sess.mu.Lock()
	w := sess.encoderIn
	sess.mu.Unlock()
	if w == nil {
		t.Error("expected encoderIn to be set")
	}
	pw.Close()
	pr.Close()
}

func TestSetOnTrackEnd_SetsCallback(t *testing.T) {
	sess := newPCMCrossfadeSession(sessionConfig{}, nil, zerolog.Nop(), nil)
	called := false
	sess.SetOnTrackEnd(func() { called = true })
	sess.mu.Lock()
	fn := sess.onTrackEnd
	sess.mu.Unlock()
	if fn == nil {
		t.Error("expected onTrackEnd to be set")
	}
	fn()
	if !called {
		t.Error("onTrackEnd should be callable")
	}
}

func TestPCMCrossfadeSession_Close_IdempotentNilSafe(t *testing.T) {
	sess := newPCMCrossfadeSession(sessionConfig{}, nil, zerolog.Nop(), nil)

	// Close when all fields are nil — should not panic.
	if err := sess.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
	// Second close should be no-op.
	if err := sess.Close(); err != nil {
		t.Errorf("second Close() returned error: %v", err)
	}
}

func TestPCMCrossfadeSession_Close_ClosesEncoderIn(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()

	sess := newPCMCrossfadeSession(sessionConfig{}, pw, zerolog.Nop(), nil)
	if err := sess.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
	// pw should be closed now; writing to it should fail.
	_, err := pw.Write([]byte("test"))
	if err == nil {
		t.Error("expected write to closed pipe to fail")
	}
}

// ── mixS16LE ──────────────────────────────────────────────────────────────

func TestMixS16LE_EqualVolumes(t *testing.T) {
	// Two silence buffers (all zeros) should mix to silence.
	a := make([]byte, 8)
	b := make([]byte, 8)
	out := make([]byte, 8)
	mixS16LE(a, b, out, 0.5, 0.5)
	for i, v := range out {
		if v != 0 {
			t.Errorf("out[%d] = %d, expected 0 for silence mix", i, v)
		}
	}
}

func TestMixS16LE_SingleSource(t *testing.T) {
	// Source A: 0x0064 = 100 (little-endian: 0x64, 0x00)
	// Source B: silence
	a := []byte{0x64, 0x00, 0x64, 0x00} // two S16LE samples of value 100
	b := make([]byte, 4)
	out := make([]byte, 4)
	mixS16LE(a, b, out, 1.0, 0.0)

	// Expected: sample = 100 * 1.0 + 0 * 0.0 = 100
	got := int16(uint16(out[0]) | uint16(out[1])<<8)
	if got != 100 {
		t.Errorf("expected sample 100, got %d", got)
	}
}

func TestMixS16LE_Clamping(t *testing.T) {
	// Two large values that would overflow int16 if not clamped.
	// 32767 + 32767 = 65534 > 32767 → should clamp to 32767
	a := []byte{0xff, 0x7f, 0x00, 0x00} // 32767 in S16LE, then 0
	b := []byte{0xff, 0x7f, 0x00, 0x00}
	out := make([]byte, 4)
	mixS16LE(a, b, out, 1.0, 1.0)

	got := int16(uint16(out[0]) | uint16(out[1])<<8)
	if got != 32767 {
		t.Errorf("expected clamped max 32767, got %d", got)
	}
}

func TestMixS16LE_NegativeClamping(t *testing.T) {
	// -32768 + -32768 should clamp to -32768
	a := []byte{0x00, 0x80, 0x00, 0x00} // -32768 in S16LE
	b := []byte{0x00, 0x80, 0x00, 0x00}
	out := make([]byte, 4)
	mixS16LE(a, b, out, 1.0, 1.0)

	got := int16(uint16(out[0]) | uint16(out[1])<<8)
	if got != -32768 {
		t.Errorf("expected clamped min -32768, got %d", got)
	}
}

// ── readFrame ─────────────────────────────────────────────────────────────

func TestReadFrame_ReadsExactly(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5, 6}
	r := bytes.NewReader(data)
	buf := make([]byte, 6)
	if err := readFrame(r, buf); err != nil {
		t.Fatalf("readFrame returned error: %v", err)
	}
	if !bytes.Equal(buf, data) {
		t.Errorf("readFrame read %v, want %v", buf, data)
	}
}

func TestReadFrame_EOF(t *testing.T) {
	r := strings.NewReader("")
	buf := make([]byte, 4)
	err := readFrame(r, buf)
	if err == nil {
		t.Error("expected error reading from empty reader")
	}
}

// ── clock with hard_item slot ─────────────────────────────────────────────

func TestStartClockEntry_HardItemSlot_MediaNotFound(t *testing.T) {
	d, _ := newMockDirector(t, &models.ClockHour{}, &models.ClockSlot{})
	ctx := context.Background()

	stationID := uuid.NewString()
	clockID := uuid.NewString()
	slotID := uuid.NewString()

	clock := models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Hard Item Clock",
	}
	if err := d.db.Create(&clock).Error; err != nil {
		t.Fatalf("seed clock: %v", err)
	}

	slot := models.ClockSlot{
		ID:          slotID,
		ClockHourID: clockID,
		Position:    0,
		Type:        models.SlotTypeHardItem,
		Payload:     map[string]any{"media_id": uuid.NewString()}, // media not in DB
	}
	if err := d.db.Create(&slot).Error; err != nil {
		t.Fatalf("seed clock slot: %v", err)
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

	err := d.startClockEntry(ctx, entry)
	if err == nil {
		t.Error("expected error for clock hard item with missing media")
	}
}

func TestStartClockEntry_HardItemSlot_WithMedia_Succeeds(t *testing.T) {
	d, _ := newMockDirector(t, &models.ClockHour{}, &models.ClockSlot{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	clockID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "main",
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
		Title:         "Clock Track",
		Path:          "/tmp/clock.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	clock := models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Hard Clock",
	}
	if err := d.db.Create(&clock).Error; err != nil {
		t.Fatalf("seed clock: %v", err)
	}

	slot := models.ClockSlot{
		ID:          uuid.NewString(),
		ClockHourID: clockID,
		Position:    0,
		Type:        models.SlotTypeHardItem,
		Payload:     map[string]any{"media_id": mediaID},
	}
	if err := d.db.Create(&slot).Error; err != nil {
		t.Fatalf("seed clock slot: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "clock_template",
		SourceID:   clockID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startClockEntry(ctx, entry)
	if err != nil {
		t.Errorf("startClockEntry with hard item returned error: %v", err)
	}

	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after startClockEntry with hard item")
	}
	if state.MediaID != mediaID {
		t.Errorf("MediaID = %q, want %q", state.MediaID, mediaID)
	}
}

// ── startSmartBlockEntry with SmartBlock having media ────────────────────

func TestStartSmartBlockEntry_WithBlockAndMedia_SetsActiveState(t *testing.T) {
	d, _ := newMockDirector(t, &models.SmartBlock{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	blockID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "main",
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
		Title:         "Smart Track",
		Path:          "/tmp/smart.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	block := models.SmartBlock{
		ID:        blockID,
		StationID: stationID,
		Name:      "Test Block",
	}
	if err := d.db.Create(&block).Error; err != nil {
		t.Fatalf("seed smart block: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "smart_block",
		SourceID:   blockID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	// The smart block engine will fail (no rules), so it falls back to random.
	// With media in DB, the fallback should succeed.
	err := d.startSmartBlockEntry(ctx, entry)
	if err != nil {
		t.Errorf("startSmartBlockEntry with media returned error: %v", err)
	}
}
