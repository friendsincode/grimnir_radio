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

// The retry loops must observe a failover the health checker wrote to the DB.
// The checker mutates its own copy of the row; the relay's captured struct
// goes stale, and before the reload fix it re-dialed the dead primary for the
// whole slot (issue #247).
func TestReloadWebstream_PicksUpFailover(t *testing.T) {
	d, _ := newMockDirector(t)

	stale := &models.Webstream{
		ID:              uuid.NewString(),
		StationID:       uuid.NewString(),
		Name:            "relay",
		URLs:            []string{"http://primary.example/stream", "http://backup.example/stream"},
		FailoverEnabled: true,
		Active:          true,
	}
	if err := d.db.Create(stale).Error; err != nil {
		t.Fatalf("seed webstream: %v", err)
	}
	if got := stale.GetCurrentURL(); got != "http://primary.example/stream" {
		t.Fatalf("precondition: current url = %q", got)
	}

	// Simulate the health checker: its own copy fails over and is saved.
	var checker models.Webstream
	if err := d.db.First(&checker, "id = ?", stale.ID).Error; err != nil {
		t.Fatalf("load checker copy: %v", err)
	}
	if !checker.FailoverToNext() {
		t.Fatal("FailoverToNext returned false")
	}
	if err := d.db.Save(&checker).Error; err != nil {
		t.Fatalf("save failover: %v", err)
	}

	// The stale struct still points at the dead primary.
	if got := stale.GetCurrentURL(); got != "http://primary.example/stream" {
		t.Fatalf("stale struct should still see primary, got %q", got)
	}

	fresh := d.reloadWebstream(context.Background(), stale)
	if got := fresh.GetCurrentURL(); got != "http://backup.example/stream" {
		t.Errorf("reload did not pick up failover: current url = %q, want backup", got)
	}
}

// A failed reload must not abort the retry loop: the stale struct comes back.
func TestReloadWebstream_FallsBackToStale(t *testing.T) {
	d, _ := newMockDirector(t)

	stale := &models.Webstream{
		ID:   uuid.NewString(),
		Name: "gone",
		URLs: []string{"http://only.example/stream"},
	}
	// Row intentionally not created: reload hits ErrWebstreamNotFound.
	got := d.reloadWebstream(context.Background(), stale)
	if got != stale {
		t.Error("reload of a missing row should return the stale struct")
	}

	// Nil service (some Director constructions pass nil): same fallback.
	d.webstreamSvc = nil
	if got := d.reloadWebstream(context.Background(), stale); got != stale {
		t.Error("reload with nil webstreamSvc should return the stale struct")
	}
}

// Exit timestamps outside the window are dropped; the survivor count is what
// the flap detector compares against webstreamFlapThreshold.
func TestPruneFlapWindow(t *testing.T) {
	now := time.Now()
	window := 2 * time.Minute

	exits := []time.Time{
		now.Add(-3 * time.Minute),  // outside — dropped
		now.Add(-119 * time.Second) /* inside */, now.Add(-30 * time.Second), now,
	}
	kept := pruneFlapWindow(exits, now, window)
	if len(kept) != 3 {
		t.Fatalf("kept %d exits, want 3", len(kept))
	}
	for _, ts := range kept {
		if now.Sub(ts) > window {
			t.Errorf("kept a timestamp outside the window: %v", ts)
		}
	}

	// A stream that exits 4 times in the window crosses the shipped threshold.
	burst := []time.Time{now.Add(-90 * time.Second), now.Add(-60 * time.Second), now.Add(-30 * time.Second), now}
	burst = pruneFlapWindow(burst, now, webstreamFlapWindow)
	if len(burst) < webstreamFlapThreshold {
		t.Errorf("burst of 4 exits in window = %d survivors, want >= threshold %d", len(burst), webstreamFlapThreshold)
	}

	// Sparse exits (one per 45s over a long stream) never cross it.
	sparse := pruneFlapWindow([]time.Time{now.Add(-135 * time.Second), now.Add(-90 * time.Second), now.Add(-45 * time.Second), now}, now, webstreamFlapWindow)
	if len(sparse) >= webstreamFlapThreshold {
		t.Errorf("sparse exits crossed the flap threshold: %d survivors", len(sparse))
	}
}
