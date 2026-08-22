/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package scheduler

import (
	"testing"
	"time"
)

// TestSchedulerSmartBlockSeed guards #85: this hashed seed exists specifically so
// consecutive-day seeds do NOT differ by a fixed 86400 (which correlates Go's
// PRNG for small pools). Assert it is deterministic, non-negative, decorrelated
// across days, and sensitive to each input.
func TestSchedulerSmartBlockSeed(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	s1 := schedulerSmartBlockSeed("blockA", "slot1", "stationX", base)

	// Deterministic.
	if s1 != schedulerSmartBlockSeed("blockA", "slot1", "stationX", base) {
		t.Fatal("seed is not deterministic for identical inputs")
	}
	// Non-negative (masked to positive int64).
	if s1 < 0 {
		t.Fatalf("seed must be non-negative, got %d", s1)
	}

	// Consecutive-day seeds must be far apart, not a fixed small delta.
	s2 := schedulerSmartBlockSeed("blockA", "slot1", "stationX", base.AddDate(0, 0, 1))
	diff := s1 - s2
	if diff < 0 {
		diff = -diff
	}
	if diff < 1_000_000 {
		t.Errorf("consecutive-day seeds too close (diff=%d); the PRNG would correlate", diff)
	}

	// Each identity input changes the seed.
	if schedulerSmartBlockSeed("blockB", "slot1", "stationX", base) == s1 {
		t.Error("a different block id should change the seed")
	}
	if schedulerSmartBlockSeed("blockA", "slot2", "stationX", base) == s1 {
		t.Error("a different slot id should change the seed")
	}
}
