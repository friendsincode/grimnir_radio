/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package smartblock

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/rs/zerolog"
)

// A title/label substring match can pull in a long file that merely contains
// the search word. On prod (2026-07) a ~60-minute "Before the First Cup ...
// Donate Your Body to Mad Science ..." episode matched a "donate" interstitial
// query and played mid-block. Both candidate fetchers cap pick duration so a
// full show can never land as a short insert. The defaults differ because the
// two pools do: station-local bumpers ran 21s–1:59, interstitials 2:45–4:47.

func idsOf(items []models.MediaItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func TestFetchInterstitialCandidates_DefaultCapExcludesLongFile(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-int"

	short := models.MediaItem{
		ID: "int-short", StationID: stationID, Title: "Donate to RLM spot",
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	long := models.MediaItem{
		ID: "int-long", StationID: stationID, Title: "Before the First Cup - Donate Your Body 5-5-16",
		Duration: 60 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&[]models.MediaItem{short, long}).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	eng := New(db, zerolog.Nop())
	cfg := InterstitialsConfig{
		Enabled: true,
		Sources: []InterstitialSource{{SourceType: "title", Query: "donate"}},
	}
	items, err := eng.fetchInterstitialCandidates(context.Background(), cfg, stationID)
	if err != nil {
		t.Fatalf("fetchInterstitialCandidates: %v", err)
	}
	if len(items) != 1 || items[0].ID != short.ID {
		t.Fatalf("default cap should keep only the short spot; got %d items %v", len(items), idsOf(items))
	}
}

func TestFetchInterstitialCandidates_ExplicitCapOverride(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-int2"

	short := models.MediaItem{
		ID: "int2-short", StationID: stationID, Title: "Donate short",
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	long := models.MediaItem{
		ID: "int2-long", StationID: stationID, Title: "Donate long sponsor read",
		Duration: 30 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&[]models.MediaItem{short, long}).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	eng := New(db, zerolog.Nop())
	// A station that genuinely runs long sponsor reads lifts the cap to 45min.
	cfg := InterstitialsConfig{
		Enabled:        true,
		MaxDurationSec: 45 * 60,
		Sources:        []InterstitialSource{{SourceType: "title", Query: "donate"}},
	}
	items, err := eng.fetchInterstitialCandidates(context.Background(), cfg, stationID)
	if err != nil {
		t.Fatalf("fetchInterstitialCandidates: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("explicit 45min cap should keep both; got %d items %v", len(items), idsOf(items))
	}
}

func TestFetchBumperCandidates_DefaultCapExcludesLongFile(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-bumpcap"

	short := models.MediaItem{
		ID: "bumpcap-short", StationID: stationID, Title: "Station bumper donate",
		Duration: 20 * time.Second, AnalysisState: models.AnalysisComplete,
	}
	long := models.MediaItem{
		ID: "bumpcap-long", StationID: stationID, Title: "Full episode bumper mislabel",
		Duration: 55 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&[]models.MediaItem{short, long}).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	eng := New(db, zerolog.Nop())
	cfg := BumperConfig{SourceType: "title", Query: "bumper"}
	items, err := eng.fetchBumperCandidates(context.Background(), cfg, stationID)
	if err != nil {
		t.Fatalf("fetchBumperCandidates: %v", err)
	}
	if len(items) != 1 || items[0].ID != short.ID {
		t.Fatalf("default cap should keep only the short bumper; got %d items %v", len(items), idsOf(items))
	}
}

// The two default caps differ: an 8-minute file is a plausible interstitial
// (under the 10min default) but never a bumper (over the 5min default). One
// file, matched by both queries, must pass the interstitial fetch and fail the
// bumper fetch.
func TestDefaultCaps_DifferByCategory(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-split"

	mid := models.MediaItem{
		ID: "split-8min", StationID: stationID, Title: "RLM bumper donate 8 minute segment",
		Duration: 8 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&mid).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	eng := New(db, zerolog.Nop())

	inter, err := eng.fetchInterstitialCandidates(context.Background(), InterstitialsConfig{
		Enabled: true,
		Sources: []InterstitialSource{{SourceType: "title", Query: "donate"}},
	}, stationID)
	if err != nil {
		t.Fatalf("fetchInterstitialCandidates: %v", err)
	}
	if len(inter) != 1 {
		t.Fatalf("8min file should pass the 10min interstitial cap; got %d", len(inter))
	}

	bump, err := eng.fetchBumperCandidates(context.Background(), BumperConfig{
		SourceType: "title", Query: "bumper",
	}, stationID)
	if err != nil {
		t.Fatalf("fetchBumperCandidates: %v", err)
	}
	if len(bump) != 0 {
		t.Fatalf("8min file should fail the 5min bumper cap; got %d %v", len(bump), idsOf(bump))
	}
}
