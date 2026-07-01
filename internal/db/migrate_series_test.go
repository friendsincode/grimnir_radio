/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package db

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// backfillScheduleSeriesID gives each recurring root its own series and makes
// overrides inherit their parent's series, without merging distinct shows.
func TestBackfillScheduleSeriesID(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&models.ScheduleEntry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	start := time.Date(2026, 1, 4, 10, 0, 0, 0, time.UTC)
	parentID := "root"
	rows := []models.ScheduleEntry{
		{ID: "root", StationID: "s1", MountID: "m1", StartsAt: start, EndsAt: start.Add(time.Hour), SourceType: "playlist", SourceID: "p1", RecurrenceType: "weekly"},
		{ID: "override", StationID: "s1", MountID: "m1", StartsAt: start, EndsAt: start.Add(time.Hour), SourceType: "playlist", SourceID: "p2", IsInstance: true, RecurrenceParentID: &parentID},
		{ID: "flat", StationID: "s1", MountID: "m1", StartsAt: start, EndsAt: start.Add(time.Hour), SourceType: "playlist", SourceID: "p3"},
	}
	for _, r := range rows {
		if err := database.Create(&r).Error; err != nil {
			t.Fatalf("seed %s: %v", r.ID, err)
		}
	}

	if err := backfillScheduleSeriesID(database); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	want := map[string]string{
		"root":     "root", // recurring root is its own series
		"override": "root", // override inherits the parent's series
		"flat":     "flat", // a one-off is its own series
	}
	for id, wantSeries := range want {
		var e models.ScheduleEntry
		if err := database.First(&e, "id = ?", id).Error; err != nil {
			t.Fatalf("reload %s: %v", id, err)
		}
		if e.SeriesID == nil || *e.SeriesID != wantSeries {
			t.Errorf("%s series id = %v, want %s", id, e.SeriesID, wantSeries)
		}
	}

	// Idempotent: a second run changes nothing.
	if err := backfillScheduleSeriesID(database); err != nil {
		t.Fatalf("backfill (2nd): %v", err)
	}
	var root models.ScheduleEntry
	database.First(&root, "id = ?", "root")
	if root.SeriesID == nil || *root.SeriesID != "root" {
		t.Errorf("root series id changed on second run: %v", root.SeriesID)
	}
}
