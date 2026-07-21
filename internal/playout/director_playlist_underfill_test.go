/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func ptrStr(s string) *string { return &s }

// TestPlaylistUnderfillMode covers the resolution precedence: a per-slot schedule
// override wins over the playlist's own setting, an empty/absent override
// inherits, and anything that isn't "randomize" collapses to replay.
func TestPlaylistUnderfillMode(t *testing.T) {
	cases := []struct {
		name     string
		playlist string
		override *string
		want     string
	}{
		{"default empty -> replay", "", nil, models.UnderfillReplay},
		{"playlist randomize", models.UnderfillRandomize, nil, models.UnderfillRandomize},
		{"playlist replay", models.UnderfillReplay, nil, models.UnderfillReplay},
		{"override randomize beats playlist replay", models.UnderfillReplay, ptrStr(models.UnderfillRandomize), models.UnderfillRandomize},
		{"override replay beats playlist randomize", models.UnderfillRandomize, ptrStr(models.UnderfillReplay), models.UnderfillReplay},
		{"empty override inherits playlist", models.UnderfillRandomize, ptrStr(""), models.UnderfillRandomize},
		{"unknown value -> replay", "sideways", nil, models.UnderfillReplay},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pl := models.Playlist{UnderfillMode: c.playlist}
			entry := models.ScheduleEntry{UnderfillMode: c.override}
			if got := playlistUnderfillMode(pl, entry); got != c.want {
				t.Errorf("playlistUnderfillMode(%q, override=%v) = %q, want %q",
					c.playlist, c.override, got, c.want)
			}
		})
	}
}

// TestReshuffleForUnderfill verifies the wrap reshuffle keeps every item (a true
// permutation), never leads with the track that just played (no back-to-back
// repeat), and doesn't mutate the caller's slice.
func TestReshuffleForUnderfill(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e"}
	justPlayed := items[len(items)-1] // "e", the track that just ended before wrap

	original := make([]string, len(items))
	copy(original, items)

	for i := 0; i < 200; i++ {
		out := reshuffleForUnderfill(items, justPlayed)
		if !strSameSet(items, out) {
			t.Fatalf("reshuffle dropped/added items: got %v", out)
		}
		if out[0] == justPlayed {
			t.Fatalf("reshuffle led with the just-played track %q: %v", justPlayed, out)
		}
	}
	if !strSlicesEqual(items, original) {
		t.Fatalf("reshuffle mutated the caller's slice: %v", items)
	}
}

// TestReshuffleForUnderfill_Small guards the degenerate sizes the wrap path can
// hand it (the caller only reshuffles when TotalItems > 1, but the helper must
// still be safe for 0/1).
func TestReshuffleForUnderfill_Small(t *testing.T) {
	if got := reshuffleForUnderfill(nil, ""); len(got) != 0 {
		t.Errorf("nil input: got %v, want empty", got)
	}
	if got := reshuffleForUnderfill([]string{"only"}, "only"); len(got) != 1 || got[0] != "only" {
		t.Errorf("single input: got %v, want [only]", got)
	}
}
