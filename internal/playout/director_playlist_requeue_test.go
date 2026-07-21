/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"sort"
	"testing"
)

// TestStationsPlayingPlaylist verifies the scan behind RequeuePlaylist: it picks
// up direct playlist and clock-playlist sources for the target playlist, dedupes
// by station, and ignores other source types and other playlists.
func TestStationsPlayingPlaylist(t *testing.T) {
	d := &Director{active: map[string]playoutState{
		"m1": {StationID: "sA", SourceType: "playlist", SourceID: "pl-1"},
		"m2": {StationID: "sB", SourceType: "clock_playlist", SourceID: "pl-1"},
		"m3": {StationID: "sA", SourceType: "playlist", SourceID: "pl-1"},    // same station -> deduped
		"m4": {StationID: "sC", SourceType: "smart_block", SourceID: "pl-1"}, // wrong source type
		"m5": {StationID: "sD", SourceType: "playlist", SourceID: "pl-2"},    // different playlist
	}}

	got := d.stationsPlayingPlaylist("pl-1")
	sort.Strings(got)
	want := []string{"sA", "sB"}
	if len(got) != len(want) {
		t.Fatalf("stations = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stations = %v, want %v", got, want)
		}
	}
}

// TestStationsPlayingPlaylist_None returns nil when nothing is on air for the
// playlist, which drives RequeuePlaylist's 0-stations (not-on-air) result.
func TestStationsPlayingPlaylist_None(t *testing.T) {
	d := &Director{active: map[string]playoutState{
		"m1": {StationID: "sA", SourceType: "playlist", SourceID: "pl-other"},
	}}
	if got := d.stationsPlayingPlaylist("pl-1"); len(got) != 0 {
		t.Fatalf("stations = %v, want none", got)
	}
	// Also safe on an empty active map.
	d2 := &Director{active: map[string]playoutState{}}
	if got := d2.stationsPlayingPlaylist("pl-1"); len(got) != 0 {
		t.Fatalf("empty active: stations = %v, want none", got)
	}
}
