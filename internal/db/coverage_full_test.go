/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package db

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"gorm.io/gorm"
)

func TestBackfillScheduleSeriesID_AllBranches(t *testing.T) {
	db := migratedDB(t)
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "S"})
	base := time.Now().UTC()
	root := dbtest.UUID("root")
	// Non-overlapping windows so the overlap-guard trigger allows all three.
	db.Create(&models.ScheduleEntry{ID: root, MountID: dbtest.UUID("mnt"), SourceID: dbtest.UUID("src"), StationID: dbtest.UUID("st1"), StartsAt: base, EndsAt: base.Add(time.Hour), SourceType: "media", IsInstance: false})
	db.Create(&models.ScheduleEntry{ID: dbtest.UUID("ovr"), MountID: dbtest.UUID("mnt"), SourceID: dbtest.UUID("src"), StationID: dbtest.UUID("st1"), StartsAt: base.Add(2 * time.Hour), EndsAt: base.Add(3 * time.Hour), SourceType: "media", IsInstance: true, RecurrenceParentID: &root})
	db.Create(&models.ScheduleEntry{ID: dbtest.UUID("orph"), MountID: dbtest.UUID("mnt"), SourceID: dbtest.UUID("src"), StationID: dbtest.UUID("st1"), StartsAt: base.Add(4 * time.Hour), EndsAt: base.Add(5 * time.Hour), SourceType: "media", IsInstance: true})

	if err := backfillScheduleSeriesID(db); err != nil {
		t.Fatalf("backfillScheduleSeriesID: %v", err)
	}

	get := func(id string) string {
		var e models.ScheduleEntry
		db.First(&e, "id = ?", id)
		if e.SeriesID == nil {
			return ""
		}
		return *e.SeriesID
	}
	if get(root) != root {
		t.Errorf("root series_id = %q, want self", get(root))
	}
	if get(dbtest.UUID("ovr")) != root {
		t.Errorf("override series_id should inherit parent, got %q", get(dbtest.UUID("ovr")))
	}
	if get(dbtest.UUID("orph")) != dbtest.UUID("orph") {
		t.Errorf("orphan series_id should be self, got %q", get(dbtest.UUID("orph")))
	}
}

func TestMigrateWebstreamHealthMethod(t *testing.T) {
	db := migratedDB(t)
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "S"})
	db.Create(&models.Webstream{ID: dbtest.UUID("ws1"), StationID: dbtest.UUID("st1"), Name: "WS", HealthCheckMethod: "HEAD"})

	if err := migrateWebstreamHealthMethod(db); err != nil {
		t.Fatalf("migrateWebstreamHealthMethod: %v", err)
	}
	var ws models.Webstream
	db.First(&ws, "id = ?", dbtest.UUID("ws1"))
	if ws.HealthCheckMethod != "GET" {
		t.Fatalf("health method = %q, want GET", ws.HealthCheckMethod)
	}
}

func TestApplyContentHashUniqueIndex_DeduplicatesExisting(t *testing.T) {
	db := migratedDB(t)
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "S"})
	// Migrate already created the unique index; drop it so we can plant the
	// pre-existing duplicates that applyContentHashUniqueIndex must clean up.
	db.Exec("DROP INDEX IF EXISTS idx_media_items_station_content_hash")
	// Two rows with the same (station, content_hash) — a pre-existing duplicate.
	// Insert oldest first so it becomes the survivor.
	db.Create(&models.MediaItem{ID: dbtest.UUID("old"), StationID: dbtest.UUID("st1"), ContentHash: "hashA", CreatedAt: time.Now().Add(-time.Hour)})
	db.Create(&models.MediaItem{ID: dbtest.UUID("new"), StationID: dbtest.UUID("st1"), ContentHash: "hashA", CreatedAt: time.Now()})
	// A play-history referencing the dupe must be remapped to the survivor.
	db.Create(&models.PlayHistory{ID: dbtest.UUID("ph"), StationID: dbtest.UUID("st1"), MediaID: dbtest.UUID("new")})

	if err := applyContentHashUniqueIndex(db); err != nil {
		t.Fatalf("applyContentHashUniqueIndex: %v", err)
	}

	var count int64
	db.Model(&models.MediaItem{}).Where("content_hash = ?", "hashA").Count(&count)
	if count != 1 {
		t.Fatalf("duplicate not removed: %d media items with hashA", count)
	}
	var ph models.PlayHistory
	db.First(&ph, "id = ?", dbtest.UUID("ph"))
	if ph.MediaID != dbtest.UUID("old") {
		t.Fatalf("play-history not remapped to survivor: media_id=%q", ph.MediaID)
	}
}

func TestBackfillOriginalFilenames_FromData(t *testing.T) {
	db := migratedDB(t)
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "S"})
	db.Create(&models.MediaItem{ID: dbtest.UUID("m1"), StationID: dbtest.UUID("st1"), ImportPath: "/lib/Song.mp3"})
	db.Create(&models.MediaItem{ID: dbtest.UUID("m2"), StationID: dbtest.UUID("st1"), Path: "/store/track.flac"})

	if err := backfillOriginalFilenames(db); err != nil {
		t.Fatalf("backfillOriginalFilenames: %v", err)
	}
	var m1 models.MediaItem
	db.First(&m1, "id = ?", dbtest.UUID("m1"))
	if m1.OriginalFilename != "Song.mp3" {
		t.Fatalf("m1 filename = %q, want Song.mp3", m1.OriginalFilename)
	}
}

// TestMigrationsErrorOnClosedDB covers every migration helper's error-return
// path: against a closed connection the underlying Exec/query fails.
func TestMigrationsErrorOnClosedDB(t *testing.T) {
	db := dbtest.Open(t) // no migrate; we just need a live handle to close
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.Close()

	fns := map[string]func(*gorm.DB) error{
		"Migrate":                           Migrate,
		"backfillScheduleSeriesID":          backfillScheduleSeriesID,
		"migrateWebstreamHealthMethod":      migrateWebstreamHealthMethod,
		"applyContentHashUniqueIndex":       applyContentHashUniqueIndex,
		"applyPostgresScheduleOverlapGuard": applyPostgresScheduleOverlapGuard,
		"backfillOriginalFilenames":         backfillOriginalFilenames,
		"normalizeLegacyPlatformRoles":      normalizeLegacyPlatformRoles,
	}
	for name, fn := range fns {
		if err := fn(db); err == nil {
			t.Errorf("%s on a closed DB should return an error", name)
		}
	}
	if _, err := RepairOriginalFilenames(db); err == nil {
		t.Error("RepairOriginalFilenames on a closed DB should return an error")
	}
	// db.DB() still succeeds on a closed pool, so UpdateConnectionMetrics just
	// runs; exercise it here for completeness.
	UpdateConnectionMetrics(db)
}

func TestConnect_Variants(t *testing.T) {
	// Happy path against the test Postgres.
	dsn := "host=localhost port=15432 user=postgres password=postgres dbname=postgres sslmode=disable"
	good, err := Connect(&config.Config{DBBackend: config.DatabasePostgres, DBDSN: dsn})
	if err != nil {
		t.Fatalf("Connect(postgres): %v", err)
	}
	if err := Close(good); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Unknown backend -> error.
	if _, err := Connect(&config.Config{DBBackend: "bogus", DBDSN: dsn}); err == nil {
		t.Error("Connect with unknown backend should error")
	}

	// Unreachable Postgres -> gorm.Open (ping on init) fails.
	if _, err := Connect(&config.Config{DBBackend: config.DatabasePostgres, DBDSN: "host=127.0.0.1 port=1 user=x dbname=x sslmode=disable connect_timeout=1"}); err == nil {
		t.Error("Connect to an unreachable Postgres should error")
	}
}

func TestCallbacks_QueryTriggersAfter(t *testing.T) {
	db := migratedDB(t)
	// A query error runs the after-callback's error branch.
	var x models.Station
	_ = db.First(&x, "id = ?", dbtest.UUID("does-not-exist")).Error // record-not-found path
	// A successful query runs the success branch.
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "S"})
	db.First(&x, "id = ?", dbtest.UUID("st1"))
}
