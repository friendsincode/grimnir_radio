/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package clock

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestRemainingInWindow_DST guards #85: remainingInWindow feeds webstream/fill
// span lengths, and it must use real elapsed time across a DST transition for a
// non-UTC station, not naive wall-clock hour arithmetic. This pins the DST-aware
// behavior so a future "simplification" to (endHour-hour) can't regress it.
func TestRemainingInWindow_DST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	ch := models.ClockHour{StartHour: 1, EndHour: 4}

	// Spring forward 2026-03-08: 02:00 EST jumps to 03:00 EDT. A 1am->4am window
	// spans 3 wall-clock hours but only 2 real hours.
	spring := time.Date(2026, 3, 8, 1, 0, 0, 0, ny)
	if got := remainingInWindow(ch, spring, ny); got != 2*time.Hour {
		t.Errorf("spring-forward remaining = %v, want 2h (real elapsed, not wall-clock 3h)", got)
	}

	// A normal (non-DST) day: 1am->4am is a full 3 real hours.
	normal := time.Date(2026, 6, 1, 1, 0, 0, 0, ny)
	if got := remainingInWindow(ch, normal, ny); got != 3*time.Hour {
		t.Errorf("normal-day remaining = %v, want 3h", got)
	}
}
