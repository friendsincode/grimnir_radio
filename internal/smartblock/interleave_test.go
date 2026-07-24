/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package smartblock

import (
	"math/rand"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func music(id string, durMS int64) SequenceItem {
	return SequenceItem{MediaID: id, StartsAtMS: 0, EndsAtMS: durMS}
}

func countInterstitials(items []SequenceItem) int {
	n := 0
	for _, it := range items {
		if it.IsInterstitial {
			n++
		}
	}
	return n
}

func TestInterleaveInterstitials_EveryTwo(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	res := &GenerateResult{Items: []SequenceItem{
		music("m1", 60000), music("m2", 60000), music("m3", 60000), music("m4", 60000),
	}}
	ads := []models.MediaItem{{ID: "ad1", Duration: 30 * time.Second}}

	interleaveInterstitials(res, ads, 2, 1, rng)

	// After every 2 music tracks, one ad => 4 music + 2 ads.
	if len(res.Items) != 6 {
		t.Fatalf("item count = %d, want 6", len(res.Items))
	}
	if got := countInterstitials(res.Items); got != 2 {
		t.Fatalf("interstitials = %d, want 2", got)
	}
	// Timeline is contiguous and TotalMS is the sum.
	var cursor int64
	for i, it := range res.Items {
		if it.StartsAtMS != cursor {
			t.Fatalf("item %d starts at %d, want %d (gap in timeline)", i, it.StartsAtMS, cursor)
		}
		cursor = it.EndsAtMS
	}
	if res.TotalMS != cursor || res.TotalMS != 4*60000+2*30000 {
		t.Fatalf("TotalMS = %d, want %d", res.TotalMS, 4*60000+2*30000)
	}
}

func TestInterleaveInterstitials_Guards(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	base := []SequenceItem{music("m1", 1000), music("m2", 1000)}
	ads := []models.MediaItem{{ID: "ad1", Duration: time.Second}}

	// Each guard leaves the item list untouched.
	for _, tc := range []struct {
		name             string
		everyN, perBreak int
		ads              []models.MediaItem
	}{
		{"everyN<1", 0, 1, ads},
		{"perBreak<1", 2, 0, ads},
		{"no interstitials", 2, 1, nil},
	} {
		res := &GenerateResult{Items: append([]SequenceItem(nil), base...)}
		interleaveInterstitials(res, tc.ads, tc.everyN, tc.perBreak, rng)
		if len(res.Items) != 2 || countInterstitials(res.Items) != 0 {
			t.Fatalf("%s: expected no-op, got %d items / %d ads", tc.name, len(res.Items), countInterstitials(res.Items))
		}
	}
}

func TestInterleaveInterstitials_WrapsAdPool(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	res := &GenerateResult{Items: []SequenceItem{music("m1", 1000), music("m2", 1000)}}
	// One ad in the pool, but two ads per break => the pool index wraps.
	ads := []models.MediaItem{{ID: "ad1", Duration: time.Second}}

	interleaveInterstitials(res, ads, 1, 2, rng)

	// 2 music + 2 breaks * 2 ads = 6 items, 4 interstitials.
	if len(res.Items) != 6 || countInterstitials(res.Items) != 4 {
		t.Fatalf("got %d items / %d ads, want 6 / 4", len(res.Items), countInterstitials(res.Items))
	}
}
