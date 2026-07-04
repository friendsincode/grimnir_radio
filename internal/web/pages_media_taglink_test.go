/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// newFKEnforcedDB opens sqlite with foreign-key enforcement ON and a single
// connection, so FK constraints behave like Postgres. The default test DBs
// leave enforcement off — the same permissiveness that let the v1.40.7 fix
// pass every test while still 500ing in prod (#223/#228/#242).
func newFKEnforcedDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1) // the PRAGMA is per-connection
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		t.Fatalf("enable fk enforcement: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Station{},
		&models.MediaItem{},
		&models.Tag{},
		&models.MediaTagLink{},
		&models.PlaylistItem{},
		&models.ScheduleEntry{},
		&models.MountPlayoutState{},
		&models.PlayHistory{},
		&models.UnderwritingObligation{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

// A tagged media item could never be deleted: media_tag_links holds a real FK
// to media_items that adminDeleteMediaReferences didn't clear, so the final
// delete aborted, the transaction rolled back, and the item stayed in the
// library (issue #248 / GitLab #1). This test runs with FK enforcement ON and
// first proves the rig would catch the bug, then proves the cleanup fixes it.
func TestAdminDeleteMediaReferences_ClearsTagLinks(t *testing.T) {
	db := newFKEnforcedDB(t)

	if err := db.Create(&models.Station{ID: "22222222-2222-2222-2222-222222222222", Name: "S"}).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	media := models.MediaItem{
		ID:        "11111111-1111-1111-1111-111111111111",
		StationID: "22222222-2222-2222-2222-222222222222",
		Title:     "Tagged Item",
		Path:      "st/ab/cd/x.audio",
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}
	tag := models.Tag{ID: "33333333-3333-3333-3333-333333333333", Name: "show"}
	if err := db.Create(&tag).Error; err != nil {
		t.Fatalf("seed tag: %v", err)
	}
	if err := db.Create(&models.MediaTagLink{MediaItemID: media.ID, TagID: tag.ID}).Error; err != nil {
		t.Fatalf("seed tag link: %v", err)
	}

	// Rig validation: with the link still present, deleting the media row
	// must violate the FK. If this succeeds, enforcement is off and the rest
	// of the test proves nothing — fail loudly instead of passing silently.
	if err := db.Delete(&models.MediaItem{}, "id = ?", media.ID).Error; err == nil {
		t.Fatal("FK rig broken: media delete succeeded with tag links present; this test cannot catch the regression")
	}

	// The fixed path: clear references, then delete, inside one transaction —
	// the same shape MediaDelete/MediaBulk/adminDeleteMediaByIDsTx use.
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := adminDeleteMediaReferences(tx, []string{media.ID}); err != nil {
			return err
		}
		return tx.Delete(&models.MediaItem{}, "id = ?", media.ID).Error
	})
	if err != nil {
		t.Fatalf("delete of tagged media still fails: %v", err)
	}

	var mediaCount, linkCount, tagCount int64
	db.Model(&models.MediaItem{}).Where("id = ?", media.ID).Count(&mediaCount)
	db.Model(&models.MediaTagLink{}).Where("media_item_id = ?", media.ID).Count(&linkCount)
	db.Model(&models.Tag{}).Count(&tagCount)
	if mediaCount != 0 {
		t.Errorf("media row survived the delete")
	}
	if linkCount != 0 {
		t.Errorf("tag links survived the delete")
	}
	if tagCount != 1 {
		t.Errorf("the tag itself must survive (only the link is cleared), got %d", tagCount)
	}
}
