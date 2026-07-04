/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package libretime

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

// LibreTime stores absolute paths in cc_files.filepath; the importer must
// persist RELATIVE paths (station_id/<file>) or playback double-joins
// MediaRoot onto an absolute path (issue #253). This is the importer prod
// (rlmapp02) actually came from, so the path discipline here is
// load-bearing history, not hypothetical.

func newLTHarness(t *testing.T) (*Importer, *gorm.DB, *sql.DB, string) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Station{}, &models.MediaItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ltDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open lt db: %v", err)
	}
	t.Cleanup(func() { _ = ltDB.Close() })

	mediaRoot := t.TempDir()
	imp := NewImporter(gdb, zerolog.Nop(), migration.MigrationOptions{
		MediaRoot:       mediaRoot,
		MediaCopyMethod: "copy",
	})
	return imp, gdb, ltDB, mediaRoot
}

func seedCCFiles(t *testing.T, ltDB *sql.DB, rows [][]any) {
	t.Helper()
	if _, err := ltDB.Exec(`CREATE TABLE cc_files (
		id INTEGER, name TEXT, filepath TEXT, track_title TEXT, artist_name TEXT,
		album_title TEXT, genre TEXT, mood TEXT, year TEXT, bpm TEXT,
		replay_gain TEXT, length TEXT, cuein TEXT, cueout TEXT,
		label TEXT, language TEXT, isrc TEXT, ftype TEXT, hidden BOOLEAN
	)`); err != nil {
		t.Fatalf("create cc_files: %v", err)
	}
	for _, r := range rows {
		if _, err := ltDB.Exec(
			`INSERT INTO cc_files VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, r...,
		); err != nil {
			t.Fatalf("insert cc_files row: %v", err)
		}
	}
}

func TestLibretimeImportMedia_StoresRelativePaths(t *testing.T) {
	imp, gdb, ltDB, mediaRoot := newLTHarness(t)

	// LibreTime keeps absolute paths on its own disk layout.
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "imported_srv", "show.mp3")
	if err := os.MkdirAll(filepath.Dir(srcFile), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(srcFile, []byte("audio"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	seedCCFiles(t, ltDB, [][]any{
		{1, "show.mp3", srcFile, "The Show", "Host", "S1", "talk", "", "2020", "0",
			"0", "01:00:00", "00:00:10", "00:59:00", "", "en", "", "audioclip", false},
		// hidden & non-audioclip rows are filtered by the query itself.
		{2, "hidden.mp3", srcFile, "Hidden", "", "", "", "", "", "", "", "00:01:00", "", "", "", "", "", "audioclip", true},
		{3, "art.jpg", srcFile, "Art", "", "", "", "", "", "", "", "", "", "", "", "", "", "image", false},
	})

	stationID := "22222222-2222-2222-2222-222222222222"
	if err := imp.importMedia(context.Background(), ltDB, stationID); err != nil {
		t.Fatalf("importMedia: %v", err)
	}

	var count int64
	gdb.Model(&models.MediaItem{}).Count(&count)
	if count != 1 {
		t.Fatalf("imported %d rows, want 1 (hidden & non-audio filtered)", count)
	}

	var media models.MediaItem
	if err := gdb.First(&media, "title = ?", "The Show").Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if filepath.IsAbs(media.Path) {
		t.Errorf("stored path is absolute: %q — this is the double-join bug", media.Path)
	}
	if want := filepath.Join(stationID, "show.mp3"); media.Path != want {
		t.Errorf("stored path = %q, want %q", media.Path, want)
	}
	if _, err := os.Stat(filepath.Join(mediaRoot, media.Path)); err != nil {
		t.Errorf("file not copied under MediaRoot: %v", err)
	}
}

func TestLibretimeImportMedia_MissingSourceKeepsMetadataOnly(t *testing.T) {
	imp, gdb, ltDB, _ := newLTHarness(t)

	seedCCFiles(t, ltDB, [][]any{
		{1, "gone.mp3", "/srv/airtime/does/not/exist.mp3", "Gone", "", "", "", "", "", "",
			"", "00:30:00", "", "", "", "", "", "audioclip", false},
	})

	if err := imp.importMedia(context.Background(), ltDB, "st-1"); err != nil {
		t.Fatalf("importMedia: %v", err)
	}
	var media models.MediaItem
	if err := gdb.First(&media, "title = ?", "Gone").Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if media.Path != "" {
		t.Errorf("path = %q, want empty when the source file is missing", media.Path)
	}
	if filepath.IsAbs(media.Path) {
		t.Errorf("absolute path leaked for missing file: %q", media.Path)
	}
}
