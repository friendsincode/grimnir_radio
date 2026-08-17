/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package db

import (
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"gorm.io/gorm"
)

func migratedDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := dbtest.Open(t)
	if err := Migrate(database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return database
}

func TestRepairOriginalFilenames(t *testing.T) {
	database := migratedDB(t)
	// A station is required — media_items.station_id has a FK to stations.
	database.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "S"})

	// import_path wins; a "{uuid}.audio" storage path is skipped; an already-named
	// row is left alone.
	database.Create(&models.MediaItem{ID: dbtest.UUID("m1"), StationID: dbtest.UUID("st1"), ImportPath: "/lib/Great Song.mp3"})
	database.Create(&models.MediaItem{ID: dbtest.UUID("m2"), StationID: dbtest.UUID("st1"), Path: "/store/" + dbtest.UUID("x") + ".audio"})
	database.Create(&models.MediaItem{ID: dbtest.UUID("m3"), StationID: dbtest.UUID("st1"), Path: "/upload/track.flac"})
	database.Create(&models.MediaItem{ID: dbtest.UUID("m4"), StationID: dbtest.UUID("st1"), OriginalFilename: "already.mp3", ImportPath: "/lib/other.mp3"})

	updated, err := RepairOriginalFilenames(database)
	if err != nil {
		t.Fatalf("RepairOriginalFilenames: %v", err)
	}
	if updated != 2 { // m1 (import_path) and m3 (path, not .audio); m2 skipped, m4 already set
		t.Fatalf("updated = %d, want 2", updated)
	}

	var m1 models.MediaItem
	database.First(&m1, "id = ?", dbtest.UUID("m1"))
	if m1.OriginalFilename != "Great Song.mp3" {
		t.Fatalf("m1 original_filename = %q, want %q", m1.OriginalFilename, "Great Song.mp3")
	}
	var m2 models.MediaItem
	database.First(&m2, "id = ?", dbtest.UUID("m2"))
	if m2.OriginalFilename != "" {
		t.Fatalf("m2 (.audio path) should be skipped, got %q", m2.OriginalFilename)
	}
}

func TestNormalizeLegacyPlatformRoles(t *testing.T) {
	database := migratedDB(t)
	database.Create(&models.User{ID: dbtest.UUID("u-admin"), Email: "a@t.local", PlatformRole: models.PlatformRole("admin")})
	database.Create(&models.User{ID: dbtest.UUID("u-mod"), Email: "m@t.local", PlatformRole: models.PlatformRole("moderator")})

	if err := normalizeLegacyPlatformRoles(database); err != nil {
		t.Fatalf("normalizeLegacyPlatformRoles: %v", err)
	}

	var admin, mod models.User
	database.First(&admin, "id = ?", dbtest.UUID("u-admin"))
	database.First(&mod, "id = ?", dbtest.UUID("u-mod"))
	if admin.PlatformRole != models.PlatformRoleAdmin {
		t.Fatalf("admin role = %q, want %q", admin.PlatformRole, models.PlatformRoleAdmin)
	}
	if mod.PlatformRole != models.PlatformRoleMod {
		t.Fatalf("moderator role = %q, want %q", mod.PlatformRole, models.PlatformRoleMod)
	}
}
