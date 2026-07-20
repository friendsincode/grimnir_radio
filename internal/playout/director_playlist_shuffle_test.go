/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func ptrBool(b bool) *bool { return &b }

func strSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func strSameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := map[string]int{}
	for _, v := range a {
		counts[v]++
	}
	for _, v := range b {
		counts[v]--
	}
	for _, c := range counts {
		if c != 0 {
			return false
		}
	}
	return true
}

func TestPlaylistShuffleEnabled(t *testing.T) {
	// nil override inherits the playlist's flag.
	if !playlistShuffleEnabled(models.Playlist{Shuffle: true}, models.ScheduleEntry{}) {
		t.Error("nil override should inherit playlist Shuffle=true")
	}
	if playlistShuffleEnabled(models.Playlist{Shuffle: false}, models.ScheduleEntry{}) {
		t.Error("nil override should inherit playlist Shuffle=false")
	}
	// A per-slot override wins either way.
	if playlistShuffleEnabled(models.Playlist{Shuffle: true}, models.ScheduleEntry{Shuffle: ptrBool(false)}) {
		t.Error("override false should force shuffle off despite playlist Shuffle=true")
	}
	if !playlistShuffleEnabled(models.Playlist{Shuffle: false}, models.ScheduleEntry{Shuffle: ptrBool(true)}) {
		t.Error("override true should force shuffle on despite playlist Shuffle=false")
	}
}

func TestMaybeShufflePlaylistItems(t *testing.T) {
	orig := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	items := append([]string(nil), orig...)
	entry := models.ScheduleEntry{
		ID: "e1", StationID: "s1", MountID: "m1",
		StartsAt: time.Unix(1000, 0).UTC(), EndsAt: time.Unix(2000, 0).UTC(),
	}

	// Shuffle off -> unchanged.
	if got := maybeShufflePlaylistItems(entry, models.Playlist{ID: "p1"}, items); !strSlicesEqual(got, orig) {
		t.Errorf("shuffle off should return items in position order, got %v", got)
	}

	pl := models.Playlist{ID: "p1", Shuffle: true}
	got := maybeShufflePlaylistItems(entry, pl, items)

	if !strSameSet(got, orig) {
		t.Errorf("shuffle must preserve the item set, got %v", got)
	}
	// 10! permutations make an accidental identity astronomically unlikely.
	if strSlicesEqual(got, orig) {
		t.Error("shuffle should reorder 10 items")
	}
	// The caller's slice must not be mutated.
	if !strSlicesEqual(items, orig) {
		t.Error("shuffle must not mutate the input slice")
	}
	// Deterministic: same entry+playlist -> same order (resume-safe).
	if got2 := maybeShufflePlaylistItems(entry, pl, items); !strSlicesEqual(got, got2) {
		t.Error("shuffle must be deterministic for the same entry+playlist")
	}
	// A different occurrence (different StartsAt) reshuffles differently.
	entry2 := entry
	entry2.StartsAt = time.Unix(50000, 0).UTC()
	if got3 := maybeShufflePlaylistItems(entry2, pl, items); strSlicesEqual(got, got3) {
		t.Error("a different occurrence should reshuffle to a different order")
	}
	// An entry override forces shuffle even when the playlist flag is off.
	forced := maybeShufflePlaylistItems(models.ScheduleEntry{
		ID: "e1", StationID: "s1", MountID: "m1",
		StartsAt: entry.StartsAt, EndsAt: entry.EndsAt, Shuffle: ptrBool(true),
	}, models.Playlist{ID: "p1", Shuffle: false}, items)
	if strSlicesEqual(forced, orig) {
		t.Error("entry override Shuffle=true should reorder even with playlist Shuffle=false")
	}

	// Fewer than 2 items is a no-op even with shuffle on.
	one := []string{"only"}
	if got := maybeShufflePlaylistItems(entry, pl, one); !strSlicesEqual(got, one) {
		t.Error("single-item playlist should be unchanged")
	}
}
