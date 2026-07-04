/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Cross-station isolation (issues #243/#245): clock-slot payloads and playlist
// items store bare ids, and the playout loaders used to fetch them with no
// ownership check — a copied clock template or mis-mapped import could put any
// station's audio on this mount. Foreign refs must now behave like deleted
// refs, except media another public station explicitly archive-shares.

func scopeTestStations(t *testing.T, d *Director) (mine, other models.Station) {
	t.Helper()
	mine = models.Station{ID: uuid.NewString(), Name: "mine", Active: true}
	other = models.Station{ID: uuid.NewString(), Name: "other", Active: true, Public: true, Approved: true}
	for _, s := range []*models.Station{&mine, &other} {
		if err := d.db.Create(s).Error; err != nil {
			t.Fatalf("seed station: %v", err)
		}
	}
	return mine, other
}

func TestLoadPlayableMedia_StationScoping(t *testing.T) {
	d, _ := newMockDirector(t)
	mine, other := scopeTestStations(t, d)

	own := models.MediaItem{ID: uuid.NewString(), StationID: mine.ID, Title: "own", Duration: 1000}
	foreignShared := models.MediaItem{ID: uuid.NewString(), StationID: other.ID, Title: "shared", Duration: 1000, ShowInArchive: true}
	foreignPrivate := models.MediaItem{ID: uuid.NewString(), StationID: other.ID, Title: "private", Duration: 1000}
	for _, m := range []*models.MediaItem{&own, &foreignShared, &foreignPrivate} {
		if err := d.db.Create(m).Error; err != nil {
			t.Fatalf("seed media: %v", err)
		}
	}
	// ShowInArchive carries `gorm:"default:true"`, so Create silently writes
	// TRUE over an explicit false zero-value — the very default that makes
	// nearly every track archive-eligible (#19 cluster A). Force it off.
	if err := d.db.Model(&foreignPrivate).Update("show_in_archive", false).Error; err != nil {
		t.Fatalf("unset show_in_archive: %v", err)
	}

	if _, err := d.loadPlayableMedia(context.Background(), mine.ID, own.ID); err != nil {
		t.Errorf("own media must load: %v", err)
	}
	if _, err := d.loadPlayableMedia(context.Background(), mine.ID, foreignShared.ID); err != nil {
		t.Errorf("public-archive-shared media must load (the archive feature): %v", err)
	}
	if _, err := d.loadPlayableMedia(context.Background(), mine.ID, foreignPrivate.ID); err == nil {
		t.Error("foreign non-shared media loaded — cross-station bleed")
	}
}

func TestLoadPlayableMedia_ArchiveRequiresPublicApprovedStation(t *testing.T) {
	d, _ := newMockDirector(t)
	mine, _ := scopeTestStations(t, d)

	// Archive-flagged track on a PRIVATE station: not shareable.
	private := models.Station{ID: uuid.NewString(), Name: "priv", Active: true, Public: false, Approved: true}
	if err := d.db.Create(&private).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	m := models.MediaItem{ID: uuid.NewString(), StationID: private.ID, Title: "x", Duration: 1000, ShowInArchive: true}
	if err := d.db.Create(&m).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	if _, err := d.loadPlayableMedia(context.Background(), mine.ID, m.ID); err == nil {
		t.Error("archive flag on a private station must not grant cross-station access")
	}
}

func TestStartPlaylistByID_RejectsForeignPlaylist(t *testing.T) {
	d, _ := newMockDirector(t, &models.Playlist{}, &models.PlaylistItem{})
	mine, other := scopeTestStations(t, d)

	foreign := models.Playlist{ID: uuid.NewString(), StationID: other.ID, Name: "theirs"}
	if err := d.db.Create(&foreign).Error; err != nil {
		t.Fatalf("seed playlist: %v", err)
	}

	entry := models.ScheduleEntry{
		ID: uuid.NewString(), StationID: mine.ID, MountID: uuid.NewString(),
		StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour), SourceType: "clock_template",
	}
	if err := d.startPlaylistByID(context.Background(), entry, foreign.ID, "clock1", "Clock"); err == nil {
		t.Error("foreign playlist started without error — must fail like a missing reference")
	}
}

func TestStartSmartBlockByID_RejectsForeignBlock(t *testing.T) {
	d, _ := newMockDirector(t, &models.SmartBlock{})
	mine, other := scopeTestStations(t, d)

	foreign := models.SmartBlock{ID: uuid.NewString(), StationID: other.ID, Name: "theirs"}
	if err := d.db.Create(&foreign).Error; err != nil {
		t.Fatalf("seed smart block: %v", err)
	}

	entry := models.ScheduleEntry{
		ID: uuid.NewString(), StationID: mine.ID, MountID: uuid.NewString(),
		StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour), SourceType: "clock_template",
	}
	if err := d.startSmartBlockByID(context.Background(), entry, foreign.ID, "clock1", "Clock"); err == nil {
		t.Error("foreign smart block started without error — must fail like a missing reference")
	}
}
