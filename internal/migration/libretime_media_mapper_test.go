/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package migration

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func iptr(v int) *int       { return &v }
func sptr(v string) *string { return &v }

// TestCreateMediaItemFromLTFile guards the LibreTime file mapper: LibreTime
// stores duration and cue points as "HH:MM:SS.mmm" strings and several numeric
// fields as pointers, so the string parsing and nil handling are where an import
// silently loses data.
func TestCreateMediaItemFromLTFile(t *testing.T) {
	imp := &LibreTimeImporter{}
	lt := LTFile{
		Name:        "fallback name",
		Title:       "Track Title",
		Artist:      "The Artist",
		Album:       "The Album",
		Genre:       "Ambient",
		Date:        "1998",
		Length:      "00:03:45.500",
		Filepath:    "/srv/airtime/imported/song.ogg",
		CueIn:       sptr("00:00:02.000"),
		CueOut:      sptr("00:03:40.000"),
		Bitrate:     iptr(320000),
		Samplerate:  iptr(44100),
		TrackNumber: iptr(7),
	}

	mi := imp.createMediaItemFromLTFile(lt, "station-1", "hash-xyz")

	if mi.Title != "Track Title" || mi.Artist != "The Artist" || mi.Genre != "Ambient" {
		t.Errorf("core metadata wrong: %+v", mi)
	}
	if mi.Year != "1998" {
		t.Errorf("Year = %q, want 1998 (from Date)", mi.Year)
	}
	if mi.OriginalFilename != "song.ogg" {
		t.Errorf("OriginalFilename = %q, want song.ogg", mi.OriginalFilename)
	}
	// "HH:MM:SS.mmm" -> the milliseconds are dropped; 3m45s = 225s.
	if want := 225 * time.Second; mi.Duration != want {
		t.Errorf("Duration = %v, want %v", mi.Duration, want)
	}
	if mi.TrackNumber != 7 || mi.Bitrate != 320000 || mi.Samplerate != 44100 {
		t.Errorf("pointer numeric fields wrong: track=%d bitrate=%d samplerate=%d", mi.TrackNumber, mi.Bitrate, mi.Samplerate)
	}
	// Cue strings parse to whole seconds.
	if mi.CuePoints.IntroEnd != 2 || mi.CuePoints.OutroIn != 220 {
		t.Errorf("cue points wrong: %+v", mi.CuePoints)
	}
}

// TestCreateMediaItemFromLTFile_TitleFallback guards the empty-title branch: a
// file with no title falls back to the basename of its Name, so it never imports
// as a blank-titled track.
func TestCreateMediaItemFromLTFile_TitleFallback(t *testing.T) {
	imp := &LibreTimeImporter{}
	lt := LTFile{Name: "/uploads/2003_-_untitled.mp3", Filepath: "/srv/x/untitled.mp3"}

	mi := imp.createMediaItemFromLTFile(lt, "s", "h")

	if mi.Title != "2003_-_untitled.mp3" {
		t.Errorf("Title fallback = %q, want basename of Name", mi.Title)
	}
	// No cue pointers: cue points stay zero.
	if mi.CuePoints != (models.CuePointSet{}) {
		t.Errorf("cue points should be zero without cue strings, got %+v", mi.CuePoints)
	}
}
