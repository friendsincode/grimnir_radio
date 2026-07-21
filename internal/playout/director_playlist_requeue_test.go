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

// TestMountsPlayingPlaylist backs the crossfade re-queue: it returns one target
// per active mount sourced from the playlist (mounts are distinct even within a
// station), carrying the mount name and entry id, and skips other source types
// and other playlists.
func TestMountsPlayingPlaylist(t *testing.T) {
	d := &Director{active: map[string]playoutState{
		"m1": {StationID: "sA", MountName: "main-a", EntryID: "e1", SourceType: "playlist", SourceID: "pl-1"},
		"m2": {StationID: "sA", MountName: "main-a-lq", EntryID: "e1", SourceType: "clock_playlist", SourceID: "pl-1"},
		"m3": {StationID: "sC", MountName: "sb", EntryID: "e3", SourceType: "smart_block", SourceID: "pl-1"}, // wrong type
		"m4": {StationID: "sD", MountName: "other", EntryID: "e4", SourceType: "playlist", SourceID: "pl-2"},  // other playlist
	}}

	got := d.mountsPlayingPlaylist("pl-1")
	if len(got) != 2 {
		t.Fatalf("mountsPlayingPlaylist = %d targets, want 2: %+v", len(got), got)
	}
	byMount := map[string]requeueTarget{}
	for _, tg := range got {
		byMount[tg.mountID] = tg
	}
	if _, ok := byMount["m1"]; !ok {
		t.Error("expected mount m1 among targets")
	}
	if _, ok := byMount["m2"]; !ok {
		t.Error("expected clock-playlist mount m2 among targets")
	}
	if _, ok := byMount["m3"]; ok {
		t.Error("smart_block mount m3 must not be a target")
	}
	if byMount["m1"].mountName != "main-a" || byMount["m1"].entryID != "e1" {
		t.Errorf("m1 target carries wrong metadata: %+v", byMount["m1"])
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
