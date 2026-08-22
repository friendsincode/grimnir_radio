/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package smartblock

import "testing"

// TestApplyFallbackLimit guards #85: when a fallback smart block resolves with
// more items than the fallback's Limit, the sequence is truncated and TotalMS is
// recomputed from the last surviving item — not left at the untruncated total.
// A zero or negative limit means "no cap".
func TestApplyFallbackLimit(t *testing.T) {
	base := GenerateResult{
		Items: []SequenceItem{
			{MediaID: "a", EndsAtMS: 1000},
			{MediaID: "b", EndsAtMS: 2000},
			{MediaID: "c", EndsAtMS: 3000},
		},
		TotalMS: 3000,
	}

	t.Run("truncates and recomputes TotalMS", func(t *testing.T) {
		got := applyFallbackLimit(base, 2)
		if len(got.Items) != 2 {
			t.Fatalf("Items = %d, want 2", len(got.Items))
		}
		if got.TotalMS != 2000 {
			t.Errorf("TotalMS = %d, want 2000 (last surviving item's EndsAtMS)", got.TotalMS)
		}
	})

	t.Run("no cap when limit is zero", func(t *testing.T) {
		got := applyFallbackLimit(base, 0)
		if len(got.Items) != 3 || got.TotalMS != 3000 {
			t.Errorf("zero limit must not truncate, got %d items TotalMS=%d", len(got.Items), got.TotalMS)
		}
	})

	t.Run("limit at or above count is a no-op", func(t *testing.T) {
		got := applyFallbackLimit(base, 3)
		if len(got.Items) != 3 || got.TotalMS != 3000 {
			t.Errorf("limit==count must not truncate, got %d items TotalMS=%d", len(got.Items), got.TotalMS)
		}
	})
}
