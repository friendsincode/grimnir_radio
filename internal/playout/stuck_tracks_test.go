/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestCheckStuckTracks guards the watchdog (#85): a track whose pipeline never
// emitted EOS past its expected TrackEndsAt must be force-stopped so the mount
// can advance, while a track still within its window is left alone. The function
// ran every tick with zero tests, so a hung track could play forever unnoticed.
func TestCheckStuckTracks(t *testing.T) {
	d, mgr := newMockDirector(t)
	now := time.Now().UTC()

	stuck := uuid.NewString()
	healthy := uuid.NewString()

	d.mu.Lock()
	d.active[stuck] = playoutState{
		SourceType: "media", StationID: uuid.NewString(), EntryID: uuid.NewString(),
		MediaID: uuid.NewString(), MountName: "stuck",
		TrackEndsAt: now.Add(-time.Minute), Ends: now.Add(time.Hour),
	}
	d.active[healthy] = playoutState{
		SourceType: "media", StationID: uuid.NewString(), MountName: "healthy",
		TrackEndsAt: now.Add(time.Minute),
	}
	d.mu.Unlock()

	d.checkStuckTracks(now)

	if mgr.stopCalls < 1 {
		t.Errorf("watchdog did not stop the overdue mount (stopCalls=%d)", mgr.stopCalls)
	}

	// The stuck mount's TrackEndsAt is zeroed so the watchdog does not re-fire.
	d.mu.Lock()
	st := d.active[stuck]
	h := d.active[healthy]
	d.mu.Unlock()
	if !st.TrackEndsAt.IsZero() {
		t.Errorf("stuck mount TrackEndsAt not cleared: %v", st.TrackEndsAt)
	}
	if h.TrackEndsAt.IsZero() {
		t.Errorf("healthy mount (still within its window) was wrongly flagged stuck")
	}
}

// TestCheckStuckTracks_SkipsHealthyAndWebstream proves the watchdog leaves a mount
// alone when nothing is overdue, including webstream sources (which have no track
// end time and must never be watchdog-restarted).
func TestCheckStuckTracks_SkipsHealthyAndWebstream(t *testing.T) {
	d, mgr := newMockDirector(t)
	now := time.Now().UTC()

	d.mu.Lock()
	d.active[uuid.NewString()] = playoutState{SourceType: "media", MountName: "ok", TrackEndsAt: now.Add(time.Minute)}
	// A webstream with a past end time must still be skipped.
	d.active[uuid.NewString()] = playoutState{SourceType: "webstream", MountName: "ws", TrackEndsAt: now.Add(-time.Minute)}
	d.mu.Unlock()

	d.checkStuckTracks(now)

	if mgr.stopCalls != 0 {
		t.Errorf("watchdog stopped a mount when nothing playable was overdue (stopCalls=%d)", mgr.stopCalls)
	}
}
