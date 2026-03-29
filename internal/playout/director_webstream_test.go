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

// ── startWebstreamEntry ───────────────────────────────────────────────────

func TestStartWebstreamEntry_NoWebstreamID_ReturnsError(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "webstream",
		SourceID:   "", // no ID
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startWebstreamEntry(ctx, entry)
	if err == nil {
		t.Error("expected error when webstream_id is empty")
	}
}

func TestStartWebstreamEntry_WebstreamNotFound_ReturnsError(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "webstream",
		SourceID:   uuid.NewString(), // not in DB
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startWebstreamEntry(ctx, entry)
	if err == nil {
		t.Error("expected error for missing webstream")
	}
}

func TestStartWebstreamEntry_NoURL_ReturnsError(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	wsID := uuid.NewString()

	webstream := models.Webstream{
		ID:        wsID,
		StationID: stationID,
		Name:      "No URL Radio",
		URLs:      nil, // no URLs configured
	}
	if err := d.db.Create(&webstream).Error; err != nil {
		t.Fatalf("seed webstream: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    uuid.NewString(),
		SourceType: "webstream",
		SourceID:   wsID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startWebstreamEntry(ctx, entry)
	if err == nil {
		t.Error("expected error for webstream with no URL")
	}
}

func TestStartWebstreamEntry_WithURL_SetsActiveState(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	wsID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "webstream-main",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	webstream := models.Webstream{
		ID:        wsID,
		StationID: stationID,
		Name:      "Test Radio",
		URLs:      []string{"http://stream.example.com/listen"},
	}
	if err := d.db.Create(&webstream).Error; err != nil {
		t.Fatalf("seed webstream: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "webstream",
		SourceID:   wsID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startWebstreamEntry(ctx, entry)
	if err != nil {
		t.Errorf("startWebstreamEntry returned error: %v", err)
	}

	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after startWebstreamEntry")
	}
	if state.SourceType != "webstream" {
		t.Errorf("SourceType = %q, want \"webstream\"", state.SourceType)
	}
}

// ── handleEntry webstream ─────────────────────────────────────────────────

func TestHandleEntry_WebstreamSourceType_CallsStart(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	// Webstream not in DB → should return error from startWebstreamEntry
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "webstream",
		SourceID:   uuid.NewString(),
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.handleEntry(ctx, entry)
	if err == nil {
		t.Error("expected error for missing webstream in handleEntry")
	}
}

// ── startWebstreamByID via clock (covers startWebstreamByID) ─────────────

func TestStartClockEntry_WebstreamSlot_WebstreamNotFound(t *testing.T) {
	d, _ := newMockDirector(t, &models.ClockHour{}, &models.ClockSlot{})
	ctx := context.Background()

	stationID := uuid.NewString()
	clockID := uuid.NewString()

	clock := models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Webstream Clock",
	}
	if err := d.db.Create(&clock).Error; err != nil {
		t.Fatalf("seed clock: %v", err)
	}

	slot := models.ClockSlot{
		ID:          uuid.NewString(),
		ClockHourID: clockID,
		Position:    0,
		Type:        models.SlotTypeWebstream,
		Payload:     map[string]any{"webstream_id": uuid.NewString()}, // not in DB
	}
	if err := d.db.Create(&slot).Error; err != nil {
		t.Fatalf("seed slot: %v", err)
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

	// startWebstreamByID → startWebstreamEntry → webstream not found → error
	err := d.startClockEntry(ctx, entry)
	if err == nil {
		t.Error("expected error for clock webstream slot with missing webstream")
	}
}

// ── stopset slot in clock ─────────────────────────────────────────────────

func TestStartClockEntry_StopsetSlot_FallsBackToRandom(t *testing.T) {
	d, _ := newMockDirector(t, &models.ClockHour{}, &models.ClockSlot{})
	ctx := context.Background()

	stationID := uuid.NewString()
	clockID := uuid.NewString()

	clock := models.ClockHour{
		ID:        clockID,
		StationID: stationID,
		Name:      "Stopset Clock",
	}
	if err := d.db.Create(&clock).Error; err != nil {
		t.Fatalf("seed clock: %v", err)
	}

	// Stopset slot with no payload → falls back to random media
	slot := models.ClockSlot{
		ID:          uuid.NewString(),
		ClockHourID: clockID,
		Position:    0,
		Type:        models.SlotTypeStopset,
		Payload:     map[string]any{}, // no playlist_id or media_id
	}
	if err := d.db.Create(&slot).Error; err != nil {
		t.Fatalf("seed slot: %v", err)
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

	// No media in DB → fallback also fails → should publishNowPlaying with error message
	err := d.startClockEntry(ctx, entry)
	if err != nil {
		t.Errorf("startClockEntry stopset fallback should not error, got: %v", err)
	}
}
