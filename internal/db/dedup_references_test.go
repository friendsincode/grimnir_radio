/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package db

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestDeduplicateMediaItems_RemapsAllReferences guards every FK the media-item
// dedup touches. The existing test only asserted play_histories + the dupe
// delete; the playlist_items and mount_playout_states remaps and the
// schedule_entries delete were unverified, so a wrong column/table there would
// silently orphan playlist items / playout state or drop schedule rows.
func TestDeduplicateMediaItems_RemapsAllReferences(t *testing.T) {
	db := migratedDB(t)
	st := dbtest.UUID("st")
	survivor := dbtest.UUID("old")
	dupe := dbtest.UUID("new")
	mount := dbtest.UUID("mount")

	must(t, db.Create(&models.Station{ID: st, OwnerID: dbtest.UUID("owner"), Name: "S"}).Error)
	db.Exec("DROP INDEX IF EXISTS idx_media_items_station_content_hash")
	must(t, db.Create(&models.MediaItem{ID: survivor, StationID: st, ContentHash: "h", CreatedAt: time.Now().Add(-time.Hour)}).Error)
	must(t, db.Create(&models.MediaItem{ID: dupe, StationID: st, ContentHash: "h", CreatedAt: time.Now()}).Error)

	// One reference of each kind, all pointing at the dupe.
	pl := dbtest.UUID("pl")
	must(t, db.Create(&models.Playlist{ID: pl, StationID: st, Name: "PL"}).Error)
	pli := dbtest.UUID("pli")
	must(t, db.Create(&models.PlaylistItem{ID: pli, PlaylistID: pl, MediaID: dupe}).Error)
	must(t, db.Create(&models.PlayHistory{ID: dbtest.UUID("ph"), StationID: st, MediaID: dupe}).Error)
	must(t, db.Create(&models.MountPlayoutState{StationID: st, MountID: mount, MediaID: dupe}).Error)
	se := dbtest.UUID("se")
	must(t, db.Create(&models.ScheduleEntry{ID: se, StationID: st, MountID: mount, SourceType: "media", SourceID: dupe, StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour)}).Error)

	if err := applyContentHashUniqueIndex(db); err != nil {
		t.Fatalf("applyContentHashUniqueIndex: %v", err)
	}

	// Dupe gone.
	var media int64
	db.Model(&models.MediaItem{}).Where("content_hash = ?", "h").Count(&media)
	if media != 1 {
		t.Fatalf("media items with hash h = %d, want 1", media)
	}
	// playlist_items remapped.
	var got models.PlaylistItem
	must(t, db.First(&got, "id = ?", pli).Error)
	if got.MediaID != survivor {
		t.Errorf("playlist_item.media_id = %q, want survivor", got.MediaID)
	}
	// play_histories remapped.
	var ph models.PlayHistory
	must(t, db.First(&ph, "id = ?", dbtest.UUID("ph")).Error)
	if ph.MediaID != survivor {
		t.Errorf("play_history.media_id = %q, want survivor", ph.MediaID)
	}
	// mount_playout_states remapped.
	var mps models.MountPlayoutState
	must(t, db.First(&mps, "mount_id = ?", mount).Error)
	if mps.MediaID != survivor {
		t.Errorf("mount_playout_state.media_id = %q, want survivor", mps.MediaID)
	}
	// schedule_entries for the dupe deleted.
	var se0 int64
	db.Model(&models.ScheduleEntry{}).Where("id = ?", se).Count(&se0)
	if se0 != 0 {
		t.Errorf("schedule_entry for dupe still present (%d), want deleted", se0)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}
