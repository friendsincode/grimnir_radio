/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestTopArtistsNormalization checks the aggregation used by the analytics pages:
// case- and surrounding-whitespace-variant spellings collapse into one artist,
// while an internal-spacing difference stays distinct (trim/case-fold only).
func TestTopArtistsNormalization(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.PlayHistory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	station := uuid.NewString()
	now := time.Now()
	seed := []string{
		"Sound Minds", "Sound Minds", // exact
		"sound minds",   // case
		" Sound Minds ", // surrounding whitespace
		"soundminds",    // internal spacing differs -> stays separate
		"", "", "",      // unknown
		"Other Artist",
	}
	for _, a := range seed {
		if err := db.Create(&models.PlayHistory{
			ID: uuid.NewString(), StationID: station, MountID: uuid.NewString(),
			Artist: a, Title: "T", StartedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	type artistCount struct {
		Artist string
		Count  int64
	}
	var got []artistCount
	if err := db.Model(&models.PlayHistory{}).
		Select("MIN(TRIM(artist)) as artist, COUNT(*) as count").
		Where("station_id = ? AND started_at >= ?", station, now.AddDate(0, 0, -7)).
		Group("LOWER(TRIM(artist))").
		Order("count DESC").
		Scan(&got).Error; err != nil {
		t.Fatalf("query: %v", err)
	}

	counts := map[string]int64{}
	for _, g := range got {
		counts[g.Artist] = g.Count
	}
	// The four "Sound Minds" case/whitespace variants merge into one group of 4.
	if counts["Sound Minds"] != 4 {
		t.Errorf("Sound Minds variants: got %d, want 4 (groups=%v)", counts["Sound Minds"], counts)
	}
	// Trim/case-fold does not remove internal spacing, so "soundminds" stays its own.
	if counts["soundminds"] != 1 {
		t.Errorf("soundminds: got %d, want 1 (separate group)", counts["soundminds"])
	}
	// Blank artists collapse to a single group (labeled Unknown in the template).
	if counts[""] != 3 {
		t.Errorf("blank/unknown: got %d, want 3", counts[""])
	}
	if counts["Other Artist"] != 1 {
		t.Errorf("Other Artist: got %d, want 1", counts["Other Artist"])
	}
}
