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

func f64(v float64) *float64 { return &v }

// TestCreateMediaItemFromAzMedia guards the AzuraCast media mapper: it turns an
// API media file into a Grimnir MediaItem, and the field-level translation
// (fractional-second length -> Duration, basename of the path, cue points from
// extra_metadata, replay gain from amplify) is exactly where a silent import
// corruption would hide.
func TestCreateMediaItemFromAzMedia(t *testing.T) {
	imp := &AzuraCastImporter{}
	az := AzuraCastAPIMediaFile{
		Title:        "Song",
		Artist:       "Artist",
		Album:        "Album",
		Genre:        "Jazz",
		ISRC:         "US-ABC-12-34567",
		Lyrics:       "la la",
		Length:       185.5, // seconds, fractional
		Path:         "/var/media/station1/track.mp3",
		CustomFields: map[string]string{"mood": "mellow"},
		ExtraMetadata: AzuraCastAPIExtraMetadata{
			Amplify: f64(-6.5),
			CueIn:   f64(1.25),
			CueOut:  f64(180.0),
			FadeIn:  f64(0.5),
			FadeOut: f64(2.0),
		},
	}

	mi := imp.createMediaItemFromAzMedia(az, "station-1", "hash-abc", []byte{0x1, 0x2}, "image/jpeg")

	if mi.StationID != "station-1" || mi.ContentHash != "hash-abc" {
		t.Fatalf("station/hash not carried: %+v", mi)
	}
	if mi.Title != "Song" || mi.Artist != "Artist" || mi.Album != "Album" || mi.Genre != "Jazz" {
		t.Errorf("core metadata wrong: %+v", mi)
	}
	if mi.ISRC != "US-ABC-12-34567" || mi.Lyrics != "la la" {
		t.Errorf("isrc/lyrics wrong: %q %q", mi.ISRC, mi.Lyrics)
	}
	// 185.5s must survive as a fractional duration, not a truncated 185s.
	if want := time.Duration(185.5 * float64(time.Second)); mi.Duration != want {
		t.Errorf("Duration = %v, want %v", mi.Duration, want)
	}
	if mi.OriginalFilename != "track.mp3" {
		t.Errorf("OriginalFilename = %q, want track.mp3 (basename of path)", mi.OriginalFilename)
	}
	if mi.ImportPath != "/var/media/station1/track.mp3" {
		t.Errorf("ImportPath = %q", mi.ImportPath)
	}
	if !mi.ShowInArchive || mi.AnalysisState != models.AnalysisComplete {
		t.Errorf("imported media must be archive-visible and analysis-complete: %+v", mi)
	}
	if mi.Artwork == nil || mi.ArtworkMime != "image/jpeg" {
		t.Errorf("artwork not carried: %v %q", mi.Artwork, mi.ArtworkMime)
	}
	if mi.CustomFields["mood"] != "mellow" {
		t.Errorf("custom fields not copied: %v", mi.CustomFields)
	}
	// Cue points map: cue_in -> IntroEnd, cue_out -> OutroIn, fades passthrough.
	if mi.CuePoints.IntroEnd != 1.25 || mi.CuePoints.OutroIn != 180.0 {
		t.Errorf("cue in/out wrong: %+v", mi.CuePoints)
	}
	if mi.CuePoints.FadeIn != 0.5 || mi.CuePoints.FadeOut != 2.0 {
		t.Errorf("fades wrong: %+v", mi.CuePoints)
	}
	if mi.ReplayGain != -6.5 {
		t.Errorf("ReplayGain = %v, want -6.5 (from amplify)", mi.ReplayGain)
	}
}

// TestCreateMediaItemFromAzMedia_NoExtraMetadata guards the empty-metadata path:
// with no amplify or cue values, cue points and replay gain stay zero rather
// than picking up garbage from nil pointers.
func TestCreateMediaItemFromAzMedia_NoExtraMetadata(t *testing.T) {
	imp := &AzuraCastImporter{}
	az := AzuraCastAPIMediaFile{Title: "Bare", Length: 60, Path: "x/y/bare.flac"}

	mi := imp.createMediaItemFromAzMedia(az, "s", "h", nil, "")

	if mi.CuePoints != (models.CuePointSet{}) {
		t.Errorf("cue points should be zero without extra metadata, got %+v", mi.CuePoints)
	}
	if mi.ReplayGain != 0 {
		t.Errorf("ReplayGain should be zero without amplify, got %v", mi.ReplayGain)
	}
	if mi.CustomFields != nil {
		t.Errorf("CustomFields should stay nil when none supplied, got %v", mi.CustomFields)
	}
	if mi.OriginalFilename != "bare.flac" {
		t.Errorf("OriginalFilename = %q, want bare.flac", mi.OriginalFilename)
	}
}
