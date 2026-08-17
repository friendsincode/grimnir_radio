/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models_test

import (
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestPlayHistory_EmptyMediaMount_PersistsAsNull guards the live/webstream bug:
// a media-less PlayHistory (live DJ, webstream) has empty MediaID/MountID, which
// Postgres rejects as an empty string for a uuid column (SQLSTATE 22P02). The
// nulluuid serializer must store empty as NULL and read NULL back as empty.
func TestPlayHistory_EmptyMediaMount_PersistsAsNull(t *testing.T) {
	db := dbtest.Open(t, &models.PlayHistory{})

	live := models.PlayHistory{
		ID:        dbtest.UUID("h-live"),
		StationID: dbtest.UUID("st1"),
		Artist:    "DJ Test",
		Title:     "Live DJ",
		// MediaID and MountID intentionally empty (no media, no mount).
	}
	if err := db.Create(&live).Error; err != nil {
		t.Fatalf("create media-less PlayHistory: %v", err)
	}

	var got models.PlayHistory
	if err := db.First(&got, "id = ?", dbtest.UUID("h-live")).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.MediaID != "" || got.MountID != "" {
		t.Fatalf("empty uuids should read back as \"\": MediaID=%q MountID=%q", got.MediaID, got.MountID)
	}

	// A populated MediaID/MountID must still round-trip unchanged.
	full := models.PlayHistory{
		ID:        dbtest.UUID("h-media"),
		StationID: dbtest.UUID("st1"),
		MountID:   dbtest.UUID("m1"),
		MediaID:   dbtest.UUID("md1"),
		Title:     "A Song",
	}
	if err := db.Create(&full).Error; err != nil {
		t.Fatalf("create media PlayHistory: %v", err)
	}
	var got2 models.PlayHistory
	db.First(&got2, "id = ?", dbtest.UUID("h-media"))
	if got2.MediaID != dbtest.UUID("md1") || got2.MountID != dbtest.UUID("m1") {
		t.Fatalf("populated uuids not round-tripped: MediaID=%q MountID=%q", got2.MediaID, got2.MountID)
	}
}
