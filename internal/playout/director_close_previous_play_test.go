/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// seedOpenPlay writes a history row shaped the way recordPlayHistory writes
// one: started now-ish, ending a full file length later, because the end time
// is projected when the track starts and nothing revisits it.
func seedOpenPlay(t *testing.T, d *Director, stationID, mountID string, playedFor, fileLen time.Duration) models.PlayHistory {
	t.Helper()
	now := time.Now()
	h := models.PlayHistory{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   mountID,
		MediaID:   uuid.NewString(),
		Title:     "Two Hour Episode",
		StartedAt: now.Add(-playedFor),
		EndedAt:   now.Add(-playedFor).Add(fileLen),
	}
	if err := d.db.Create(&h).Error; err != nil {
		t.Fatalf("seed play history: %v", err)
	}
	return h
}

// A mount with no MountPlayoutState row must still get its previous play
// closed. Gating the close on that row is what let a show cut off after twelve
// seconds report as a complete hour: on prod only 3 of 39 mounts had a state
// row, and 1 of 3,212 plays in 24h was marked interrupted.
func TestClosePreviousPlay_ClosesWithoutMountPlayoutState(t *testing.T) {
	d := newCoverageDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	h := seedOpenPlay(t, d, stationID, mountID, 12*time.Second, 3587*time.Second)

	// No MountPlayoutState row exists for this mount, matching 36 of 39 on prod.
	var stateCount int64
	d.db.Model(&models.MountPlayoutState{}).Where("mount_id = ?", mountID).Count(&stateCount)
	if stateCount != 0 {
		t.Fatalf("precondition: expected no state row, found %d", stateCount)
	}

	d.closePreviousPlay(ctx, stationID, mountID)

	var got models.PlayHistory
	if err := d.db.First(&got, "id = ?", h.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	aired := got.EndedAt.Sub(got.StartedAt)
	if aired > time.Minute {
		t.Fatalf("play recorded as airing %v; the track was cut after 12s, so the end time was never corrected", aired)
	}
	if got.Metadata == nil || got.Metadata["was_interrupted"] != true {
		t.Errorf("expected was_interrupted on a play cut this early, metadata=%v", got.Metadata)
	}
}

// When a state row does exist its position is still recorded, so the resume
// offset feature keeps working.
func TestClosePreviousPlay_UsesStatePositionWhenPresent(t *testing.T) {
	d := newCoverageDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	h := seedOpenPlay(t, d, stationID, mountID, 30*time.Second, 3587*time.Second)

	if err := d.db.Create(&models.MountPlayoutState{
		StationID:       stationID,
		MountID:         mountID,
		TrackPositionMS: 27_000,
	}).Error; err != nil {
		t.Fatalf("seed state: %v", err)
	}

	d.closePreviousPlay(ctx, stationID, mountID)

	var got models.PlayHistory
	if err := d.db.First(&got, "id = ?", h.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Metadata == nil {
		t.Fatal("expected metadata on the closed row")
	}
	// GORM round-trips JSON numbers as float64.
	switch v := got.Metadata["cut_offset_ms"].(type) {
	case float64:
		if int64(v) != 27_000 {
			t.Errorf("cut_offset_ms = %v, want 27000", v)
		}
	case int64:
		if v != 27_000 {
			t.Errorf("cut_offset_ms = %v, want 27000", v)
		}
	default:
		t.Errorf("cut_offset_ms missing or unexpected type: %#v", got.Metadata["cut_offset_ms"])
	}
}

// A track that runs essentially to its end is left alone, so normal full plays
// keep their recorded end time and do not all become "interrupted".
func TestClosePreviousPlay_LeavesCompletedPlayAlone(t *testing.T) {
	d := newCoverageDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	// Started 3580s ago on a 3587s file: only 7s remain, inside the 30s grace.
	h := seedOpenPlay(t, d, stationID, mountID, 3580*time.Second, 3587*time.Second)
	original := h.EndedAt

	d.closePreviousPlay(ctx, stationID, mountID)

	var got models.PlayHistory
	if err := d.db.First(&got, "id = ?", h.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Metadata != nil {
		if _, marked := got.Metadata["was_interrupted"]; marked {
			t.Error("a play that reached its end must not be marked interrupted")
		}
	}
	if !got.EndedAt.Equal(original) {
		t.Errorf("ended_at = %v, want unchanged %v", got.EndedAt, original)
	}
}
