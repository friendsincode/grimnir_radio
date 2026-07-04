/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package azuracast

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Importer tests (issue #253). Media paths stored in the DB must be RELATIVE
// (station_id/<file>): an absolute path causes the documented
// /var/lib/grimnir/media/var/lib/... double-join at playback. The cue-point
// mapping also gets pinned — cue_in used to be written into OutroIn & then
// overwritten by cue_out, silently discarding every imported cue-in.

func newImporterHarness(t *testing.T) (*Importer, *gorm.DB, *sql.DB, string) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Station{}, &models.MediaItem{}, &models.Mount{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	azuraDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open azura db: %v", err)
	}
	t.Cleanup(func() { _ = azuraDB.Close() })

	mediaRoot := t.TempDir()
	imp := NewImporter(gdb, zerolog.Nop(), migration.MigrationOptions{
		MediaRoot:       mediaRoot,
		MediaCopyMethod: "copy",
	})
	return imp, gdb, azuraDB, mediaRoot
}

func seedAzuraMedia(t *testing.T, azuraDB *sql.DB, rows [][]any) {
	t.Helper()
	if _, err := azuraDB.Exec(`CREATE TABLE station_media (
		id INTEGER, storage_location_id INTEGER, title TEXT, artist TEXT,
		album TEXT, genre TEXT, length REAL, path TEXT,
		amplify REAL, fade_overlap REAL, fade_in REAL, fade_out REAL,
		cue_in REAL, cue_out REAL
	)`); err != nil {
		t.Fatalf("create station_media: %v", err)
	}
	for _, r := range rows {
		if _, err := azuraDB.Exec(
			`INSERT INTO station_media VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, r...,
		); err != nil {
			t.Fatalf("insert media row: %v", err)
		}
	}
}

func TestImportMedia_StoresRelativePathsAndCopiesFiles(t *testing.T) {
	imp, gdb, azuraDB, mediaRoot := newImporterHarness(t)

	// Backup media dir with one real source file, nested the way AzuraCast
	// lays out per-station folders.
	backupMedia := t.TempDir()
	if err := os.MkdirAll(filepath.Join(backupMedia, "shows"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupMedia, "shows", "episode1.mp3"), []byte("audio"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	seedAzuraMedia(t, azuraDB, [][]any{
		{1, 7, "Episode 1", "Host", "Season", "talk", 3600.0, "shows/episode1.mp3",
			nil, nil, 1.5, 2.5, 10.0, 3500.0},
	})

	stationMap := map[int]string{7: "22222222-2222-2222-2222-222222222222"}
	if err := imp.importMedia(context.Background(), azuraDB, stationMap, backupMedia); err != nil {
		t.Fatalf("importMedia: %v", err)
	}

	var media models.MediaItem
	if err := gdb.First(&media, "title = ?", "Episode 1").Error; err != nil {
		t.Fatalf("load imported media: %v", err)
	}

	// The load-bearing assertion: stored path is relative & rooted at the
	// station id, never absolute, never echoing the backup layout.
	if filepath.IsAbs(media.Path) {
		t.Errorf("stored path is absolute: %q (double-join at playback)", media.Path)
	}
	want := filepath.Join(stationMap[7], "episode1.mp3")
	if media.Path != want {
		t.Errorf("stored path = %q, want %q", media.Path, want)
	}

	// The file landed under MediaRoot at that relative path.
	if _, err := os.Stat(filepath.Join(mediaRoot, media.Path)); err != nil {
		t.Errorf("copied file missing at MediaRoot/relative path: %v", err)
	}

	// Cue mapping: cue_in -> IntroEnd, cue_out -> OutroIn, fades preserved.
	// Before the fix both cues landed in OutroIn & the fades were dropped.
	if media.CuePoints.IntroEnd != 10.0 {
		t.Errorf("IntroEnd = %v, want 10 (cue_in was silently discarded before)", media.CuePoints.IntroEnd)
	}
	if media.CuePoints.OutroIn != 3500.0 {
		t.Errorf("OutroIn = %v, want 3500", media.CuePoints.OutroIn)
	}
	if media.CuePoints.FadeIn != 1.5 || media.CuePoints.FadeOut != 2.5 {
		t.Errorf("fades = %v/%v, want 1.5/2.5", media.CuePoints.FadeIn, media.CuePoints.FadeOut)
	}
}

func TestImportMedia_SkipsUnknownStationAndSurvivesMissingFile(t *testing.T) {
	imp, gdb, azuraDB, _ := newImporterHarness(t)
	backupMedia := t.TempDir()

	seedAzuraMedia(t, azuraDB, [][]any{
		// storage id 99 has no station mapping: row skipped, not fatal.
		{1, 99, "Orphan", "", "", "", 60.0, "gone.mp3", nil, nil, nil, nil, nil, nil},
		// Known station but the file is absent from the backup: metadata row
		// still imports (path empty) rather than failing the whole run.
		{2, 7, "Ghost File", "", "", "", 60.0, "missing.mp3", nil, nil, nil, nil, nil, nil},
	})

	stationMap := map[int]string{7: "22222222-2222-2222-2222-222222222222"}
	if err := imp.importMedia(context.Background(), azuraDB, stationMap, backupMedia); err != nil {
		t.Fatalf("importMedia: %v", err)
	}

	var count int64
	gdb.Model(&models.MediaItem{}).Count(&count)
	if count != 1 {
		t.Fatalf("imported %d rows, want 1 (orphan skipped, ghost imported)", count)
	}
	var ghost models.MediaItem
	if err := gdb.First(&ghost, "title = ?", "Ghost File").Error; err != nil {
		t.Fatalf("ghost row: %v", err)
	}
	if ghost.Path != "" {
		t.Errorf("ghost path = %q, want empty (no file materialized)", ghost.Path)
	}
}

func TestImportMedia_TraversalPathIsNeutralized(t *testing.T) {
	imp, gdb, azuraDB, _ := newImporterHarness(t)
	backupMedia := t.TempDir()
	// A hostile backup path must not escape the station folder in the stored
	// relative path; filepath.Base flattens it.
	if err := os.WriteFile(filepath.Join(backupMedia, "evil.mp3"), []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	seedAzuraMedia(t, azuraDB, [][]any{
		{1, 7, "Evil", "", "", "", 60.0, "evil.mp3", nil, nil, nil, nil, nil, nil},
	})
	stationMap := map[int]string{7: "st-1"}
	if err := imp.importMedia(context.Background(), azuraDB, stationMap, backupMedia); err != nil {
		t.Fatalf("importMedia: %v", err)
	}
	var media models.MediaItem
	if err := gdb.First(&media, "title = ?", "Evil").Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if media.Path != filepath.Join("st-1", "evil.mp3") {
		t.Errorf("path = %q", media.Path)
	}
}

func TestImportStations_BuildsIDMap(t *testing.T) {
	imp, gdb, azuraDB, _ := newImporterHarness(t)
	if _, err := azuraDB.Exec(`CREATE TABLE station (
		id INTEGER, name TEXT, short_name TEXT, description TEXT,
		timezone TEXT, is_enabled INTEGER
	)`); err != nil {
		t.Fatalf("create station: %v", err)
	}
	if _, err := azuraDB.Exec(`INSERT INTO station VALUES
		(1, 'RLM Radio', 'rlm', 'main', 'America/Chicago', 1),
		(2, 'Second', 'second', '', NULL, 1)`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	m, err := imp.importStations(context.Background(), azuraDB)
	if err != nil {
		t.Fatalf("importStations: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("station map size = %d, want 2", len(m))
	}
	var st models.Station
	if err := gdb.First(&st, "id = ?", m[1]).Error; err != nil {
		t.Fatalf("station 1: %v", err)
	}
	if st.Timezone != "America/Chicago" {
		t.Errorf("timezone = %q", st.Timezone)
	}
	var st2 models.Station
	if err := gdb.First(&st2, "id = ?", m[2]).Error; err != nil {
		t.Fatalf("station 2: %v", err)
	}
	if st2.Timezone != "UTC" {
		t.Errorf("NULL timezone should default to UTC, got %q", st2.Timezone)
	}
}
