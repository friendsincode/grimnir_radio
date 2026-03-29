/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newTestDB opens an in-memory SQLite database and auto-migrates the tables
// needed for orphan scanner tests.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.OrphanMedia{}, &models.MediaItem{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

// newTestScanner creates an OrphanScanner pointed at a temp directory.
func newTestScanner(t *testing.T, db *gorm.DB) (*OrphanScanner, string) {
	t.Helper()
	mediaRoot := t.TempDir()
	logger := zerolog.Nop()
	return NewOrphanScanner(db, mediaRoot, logger), mediaRoot
}

// writeMediaFile creates a file with a media extension inside the given directory.
func writeMediaFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write file %q: %v", path, err)
	}
	return path
}

// ── NewOrphanScanner ──────────────────────────────────────────────────────────

func TestNewOrphanScanner_ReturnsNonNil(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	if scanner == nil {
		t.Fatal("NewOrphanScanner() returned nil")
	}
}

// ── isMediaFile ───────────────────────────────────────────────────────────────

func TestIsMediaFile(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		want     bool
	}{
		{"mp3", "song.mp3", true},
		{"MP3 uppercase", "song.MP3", true},
		{"flac", "song.flac", true},
		{"ogg", "track.ogg", true},
		{"m4a", "track.m4a", true},
		{"aac", "track.aac", true},
		{"wav", "track.wav", true},
		{"wma", "track.wma", true},
		{"opus", "track.opus", true},
		{"audio extension", "track.audio", true},
		{"txt not media", "readme.txt", false},
		{"jpg not media", "cover.jpg", false},
		{"no extension", "noextension", false},
		{"pdf not media", "doc.pdf", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isMediaFile(tc.filename)
			if got != tc.want {
				t.Errorf("isMediaFile(%q) = %v, want %v", tc.filename, got, tc.want)
			}
		})
	}
}

// ── computeFileHash ───────────────────────────────────────────────────────────

func TestComputeFileHash_Deterministic(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "test.mp3")
	content := []byte("fake audio content for hashing")
	if err := os.WriteFile(f, content, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	h1, err := computeFileHash(f)
	if err != nil {
		t.Fatalf("computeFileHash() error: %v", err)
	}
	h2, err := computeFileHash(f)
	if err != nil {
		t.Fatalf("computeFileHash() second call error: %v", err)
	}
	if h1 != h2 {
		t.Errorf("computeFileHash() not deterministic: %q != %q", h1, h2)
	}
	// SHA-256 hex is always 64 chars
	if len(h1) != 64 {
		t.Errorf("computeFileHash() hash length = %d, want 64", len(h1))
	}
}

func TestComputeFileHash_DifferentContentDifferentHash(t *testing.T) {
	tmp := t.TempDir()

	f1 := filepath.Join(tmp, "a.mp3")
	f2 := filepath.Join(tmp, "b.mp3")
	if err := os.WriteFile(f1, []byte("content A"), 0644); err != nil {
		t.Fatalf("write f1: %v", err)
	}
	if err := os.WriteFile(f2, []byte("content B"), 0644); err != nil {
		t.Fatalf("write f2: %v", err)
	}

	h1, _ := computeFileHash(f1)
	h2, _ := computeFileHash(f2)
	if h1 == h2 {
		t.Error("computeFileHash() produced same hash for different content")
	}
}

func TestComputeFileHash_NonExistentFile(t *testing.T) {
	_, err := computeFileHash("/nonexistent/path/file.mp3")
	if err == nil {
		t.Error("computeFileHash() should return error for nonexistent file")
	}
}

func TestComputeFileHash_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "empty.mp3")
	if err := os.WriteFile(f, []byte{}, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	hash, err := computeFileHash(f)
	if err != nil {
		t.Fatalf("computeFileHash() error on empty file: %v", err)
	}
	// SHA-256 of empty file is well-known constant
	const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if hash != emptySHA256 {
		t.Errorf("computeFileHash() of empty = %q, want %q", hash, emptySHA256)
	}
}

// ── ScanForOrphans ────────────────────────────────────────────────────────────

func TestScanForOrphans_EmptyDirectory(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	result, err := scanner.ScanForOrphans(ctx)
	if err != nil {
		t.Fatalf("ScanForOrphans() error: %v", err)
	}
	if result.TotalFiles != 0 {
		t.Errorf("TotalFiles = %d, want 0", result.TotalFiles)
	}
	if result.NewOrphans != 0 {
		t.Errorf("NewOrphans = %d, want 0", result.NewOrphans)
	}
}

func TestScanForOrphans_DetectsNewOrphans(t *testing.T) {
	db := newTestDB(t)
	scanner, mediaRoot := newTestScanner(t, db)
	ctx := context.Background()

	writeMediaFile(t, mediaRoot, "track1.mp3", []byte("fake mp3 data"))
	writeMediaFile(t, mediaRoot, "track2.flac", []byte("fake flac data"))

	result, err := scanner.ScanForOrphans(ctx)
	if err != nil {
		t.Fatalf("ScanForOrphans() error: %v", err)
	}
	if result.TotalFiles != 2 {
		t.Errorf("TotalFiles = %d, want 2", result.TotalFiles)
	}
	if result.NewOrphans != 2 {
		t.Errorf("NewOrphans = %d, want 2", result.NewOrphans)
	}
}

func TestScanForOrphans_SkipsNonMediaFiles(t *testing.T) {
	db := newTestDB(t)
	scanner, mediaRoot := newTestScanner(t, db)
	ctx := context.Background()

	writeMediaFile(t, mediaRoot, "cover.jpg", []byte("jpeg data"))
	writeMediaFile(t, mediaRoot, "notes.txt", []byte("notes"))
	writeMediaFile(t, mediaRoot, "track.mp3", []byte("audio"))

	result, err := scanner.ScanForOrphans(ctx)
	if err != nil {
		t.Fatalf("ScanForOrphans() error: %v", err)
	}
	if result.TotalFiles != 1 {
		t.Errorf("TotalFiles = %d, want 1 (only mp3)", result.TotalFiles)
	}
}

func TestScanForOrphans_SkipsAlreadyKnownOrphans(t *testing.T) {
	db := newTestDB(t)
	scanner, mediaRoot := newTestScanner(t, db)
	ctx := context.Background()

	writeMediaFile(t, mediaRoot, "track.mp3", []byte("audio content"))

	// First scan: detects as new orphan.
	r1, err := scanner.ScanForOrphans(ctx)
	if err != nil {
		t.Fatalf("first scan error: %v", err)
	}
	if r1.NewOrphans != 1 {
		t.Fatalf("first scan NewOrphans = %d, want 1", r1.NewOrphans)
	}

	// Second scan: should be AlreadyKnown.
	r2, err := scanner.ScanForOrphans(ctx)
	if err != nil {
		t.Fatalf("second scan error: %v", err)
	}
	if r2.NewOrphans != 0 {
		t.Errorf("second scan NewOrphans = %d, want 0", r2.NewOrphans)
	}
	if r2.AlreadyKnown != 1 {
		t.Errorf("second scan AlreadyKnown = %d, want 1", r2.AlreadyKnown)
	}
}

func TestScanForOrphans_SkipsKnownMediaItems(t *testing.T) {
	db := newTestDB(t)
	scanner, mediaRoot := newTestScanner(t, db)
	ctx := context.Background()

	relPath := "track.mp3"
	writeMediaFile(t, mediaRoot, relPath, []byte("known audio"))

	// Insert a MediaItem with the same path so the scanner considers it known.
	item := &models.MediaItem{
		ID:            uuid.New().String(),
		StationID:     uuid.New().String(),
		Path:          relPath,
		Title:         "Known Track",
		AnalysisState: models.AnalysisPending,
	}
	if err := db.Create(item).Error; err != nil {
		t.Fatalf("create media item: %v", err)
	}

	result, err := scanner.ScanForOrphans(ctx)
	if err != nil {
		t.Fatalf("ScanForOrphans() error: %v", err)
	}
	if result.NewOrphans != 0 {
		t.Errorf("NewOrphans = %d, want 0 (file is a known MediaItem)", result.NewOrphans)
	}
}

func TestScanForOrphans_WithSubdirectories(t *testing.T) {
	db := newTestDB(t)
	scanner, mediaRoot := newTestScanner(t, db)
	ctx := context.Background()

	subdir := filepath.Join(mediaRoot, "station1", "ab", "cd")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "track.ogg"), []byte("ogg"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := scanner.ScanForOrphans(ctx)
	if err != nil {
		t.Fatalf("ScanForOrphans() error: %v", err)
	}
	if result.TotalFiles != 1 {
		t.Errorf("TotalFiles = %d, want 1", result.TotalFiles)
	}
}

// ── GetOrphans ────────────────────────────────────────────────────────────────

func TestGetOrphans_Empty(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	orphans, total, err := scanner.GetOrphans(ctx, 1, 25)
	if err != nil {
		t.Fatalf("GetOrphans() error: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if len(orphans) != 0 {
		t.Errorf("len(orphans) = %d, want 0", len(orphans))
	}
}

func TestGetOrphans_Pagination(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	// Insert 5 orphan records directly.
	for i := 0; i < 5; i++ {
		o := &models.OrphanMedia{
			ID:          uuid.New().String(),
			FilePath:    filepath.Join("station", uuid.New().String()+".mp3"),
			ContentHash: uuid.New().String(),
			FileSize:    100,
			DetectedAt:  time.Now(),
		}
		if err := db.Create(o).Error; err != nil {
			t.Fatalf("create orphan: %v", err)
		}
	}

	// Page 1, size 3.
	page1, total, err := scanner.GetOrphans(ctx, 1, 3)
	if err != nil {
		t.Fatalf("GetOrphans() page1 error: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(page1) != 3 {
		t.Errorf("page1 len = %d, want 3", len(page1))
	}

	// Page 2, size 3.
	page2, _, err := scanner.GetOrphans(ctx, 2, 3)
	if err != nil {
		t.Fatalf("GetOrphans() page2 error: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("page2 len = %d, want 2", len(page2))
	}
}

func TestGetOrphans_AllRows_ZeroPageSize(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		o := &models.OrphanMedia{
			ID:          uuid.New().String(),
			FilePath:    filepath.Join("s", uuid.New().String()+".mp3"),
			ContentHash: uuid.New().String(),
			FileSize:    50,
			DetectedAt:  time.Now(),
		}
		if err := db.Create(o).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	// pageSize=0 means return all.
	rows, total, err := scanner.GetOrphans(ctx, 1, 0)
	if err != nil {
		t.Fatalf("GetOrphans(pageSize=0) error: %v", err)
	}
	if total != 4 {
		t.Errorf("total = %d, want 4", total)
	}
	if len(rows) != 4 {
		t.Errorf("len(rows) = %d, want 4", len(rows))
	}
}

func TestGetOrphans_InvalidPageDefaults(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	// page=0 and pageSize=-1 should use defaults without panicking.
	_, _, err := scanner.GetOrphans(ctx, 0, -1)
	if err != nil {
		t.Fatalf("GetOrphans(page=0, pageSize=-1) error: %v", err)
	}
}

// ── GetAllOrphanIDs ───────────────────────────────────────────────────────────

func TestGetAllOrphanIDs(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	ids := []string{uuid.New().String(), uuid.New().String()}
	for _, id := range ids {
		o := &models.OrphanMedia{
			ID:          id,
			FilePath:    id + ".mp3",
			ContentHash: id,
			FileSize:    1,
			DetectedAt:  time.Now(),
		}
		if err := db.Create(o).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	got, err := scanner.GetAllOrphanIDs(ctx)
	if err != nil {
		t.Fatalf("GetAllOrphanIDs() error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

// ── GetAllOrphans ─────────────────────────────────────────────────────────────

func TestGetAllOrphans(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	o := &models.OrphanMedia{
		ID:          uuid.New().String(),
		FilePath:    "s/track.mp3",
		ContentHash: "abc",
		FileSize:    10,
		DetectedAt:  time.Now(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	all, err := scanner.GetAllOrphans(ctx)
	if err != nil {
		t.Fatalf("GetAllOrphans() error: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("len = %d, want 1", len(all))
	}
}

// ── GetOrphanByHash ───────────────────────────────────────────────────────────

func TestGetOrphanByHash_Found(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	o := &models.OrphanMedia{
		ID:          uuid.New().String(),
		FilePath:    "s/hash.mp3",
		ContentHash: "deadbeef",
		FileSize:    1,
		DetectedAt:  time.Now(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := scanner.GetOrphanByHash(ctx, "deadbeef")
	if err != nil {
		t.Fatalf("GetOrphanByHash() error: %v", err)
	}
	if got == nil {
		t.Fatal("GetOrphanByHash() returned nil, want record")
	}
	if got.ContentHash != "deadbeef" {
		t.Errorf("ContentHash = %q, want %q", got.ContentHash, "deadbeef")
	}
}

func TestGetOrphanByHash_NotFound(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	got, err := scanner.GetOrphanByHash(ctx, "nonexistenthash")
	if err != nil {
		t.Fatalf("GetOrphanByHash() error: %v", err)
	}
	if got != nil {
		t.Errorf("GetOrphanByHash() = %+v, want nil", got)
	}
}

// ── GetOrphanByID ─────────────────────────────────────────────────────────────

func TestGetOrphanByID_Found(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	id := uuid.New().String()
	o := &models.OrphanMedia{
		ID:          id,
		FilePath:    "s/byid.mp3",
		ContentHash: "hash123",
		FileSize:    1,
		DetectedAt:  time.Now(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := scanner.GetOrphanByID(ctx, id)
	if err != nil {
		t.Fatalf("GetOrphanByID() error: %v", err)
	}
	if got == nil {
		t.Fatal("GetOrphanByID() returned nil")
	}
	if got.ID != id {
		t.Errorf("ID = %q, want %q", got.ID, id)
	}
}

func TestGetOrphanByID_NotFound(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	got, err := scanner.GetOrphanByID(ctx, uuid.New().String())
	if err != nil {
		t.Fatalf("GetOrphanByID() error: %v", err)
	}
	if got != nil {
		t.Errorf("GetOrphanByID() = %+v, want nil", got)
	}
}

// ── GetOrphanStats ────────────────────────────────────────────────────────────

func TestGetOrphanStats_Empty(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	count, size, err := scanner.GetOrphanStats(ctx)
	if err != nil {
		t.Fatalf("GetOrphanStats() error: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if size != 0 {
		t.Errorf("size = %d, want 0", size)
	}
}

func TestGetOrphanStats_WithData(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	for i, sz := range []int64{100, 200, 300} {
		o := &models.OrphanMedia{
			ID:          uuid.New().String(),
			FilePath:    filepath.Join("s", uuid.New().String()) + ".mp3",
			ContentHash: uuid.New().String(),
			FileSize:    sz,
			DetectedAt:  time.Now(),
		}
		_ = i
		if err := db.Create(o).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	count, size, err := scanner.GetOrphanStats(ctx)
	if err != nil {
		t.Fatalf("GetOrphanStats() error: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	if size != 600 {
		t.Errorf("size = %d, want 600", size)
	}
}

// ── BuildOrphanHashMap ────────────────────────────────────────────────────────

func TestBuildOrphanHashMap(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	hashes := []string{"hash-aaa", "hash-bbb", "hash-ccc"}
	for _, h := range hashes {
		o := &models.OrphanMedia{
			ID:          uuid.New().String(),
			FilePath:    h + ".mp3",
			ContentHash: h,
			FileSize:    1,
			DetectedAt:  time.Now(),
		}
		if err := db.Create(o).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	m, err := scanner.BuildOrphanHashMap(ctx)
	if err != nil {
		t.Fatalf("BuildOrphanHashMap() error: %v", err)
	}
	if len(m) != 3 {
		t.Errorf("map len = %d, want 3", len(m))
	}
	for _, h := range hashes {
		if _, ok := m[h]; !ok {
			t.Errorf("hash %q not found in map", h)
		}
	}
}

func TestBuildOrphanHashMap_EmptyHashSkipped(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	// Create an orphan with an empty hash — it should be excluded from the map.
	o := &models.OrphanMedia{
		ID:          uuid.New().String(),
		FilePath:    "no-hash.mp3",
		ContentHash: "", // empty
		FileSize:    1,
		DetectedAt:  time.Now(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	m, err := scanner.BuildOrphanHashMap(ctx)
	if err != nil {
		t.Fatalf("BuildOrphanHashMap() error: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("map len = %d, want 0 (empty hash must not be inserted)", len(m))
	}
}

// ── DeleteOrphan ──────────────────────────────────────────────────────────────

func TestDeleteOrphan_RemovesRecord(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	id := uuid.New().String()
	o := &models.OrphanMedia{
		ID:          id,
		FilePath:    "s/todelete.mp3",
		ContentHash: "delhash",
		FileSize:    1,
		DetectedAt:  time.Now(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := scanner.DeleteOrphan(ctx, id, false); err != nil {
		t.Fatalf("DeleteOrphan() error: %v", err)
	}

	got, _ := scanner.GetOrphanByID(ctx, id)
	if got != nil {
		t.Error("orphan record should be deleted")
	}
}

func TestDeleteOrphan_DeleteFile(t *testing.T) {
	db := newTestDB(t)
	scanner, mediaRoot := newTestScanner(t, db)
	ctx := context.Background()

	// Create an actual file.
	fname := "station/track-del.mp3"
	fpath := filepath.Join(mediaRoot, fname)
	if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fpath, []byte("audio"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	id := uuid.New().String()
	o := &models.OrphanMedia{
		ID:          id,
		FilePath:    fname,
		ContentHash: "filetest",
		FileSize:    5,
		DetectedAt:  time.Now(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := scanner.DeleteOrphan(ctx, id, true); err != nil {
		t.Fatalf("DeleteOrphan(deleteFile=true) error: %v", err)
	}

	if _, err := os.Stat(fpath); !os.IsNotExist(err) {
		t.Error("file should be deleted from disk")
	}
}

func TestDeleteOrphan_NotFound(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	err := scanner.DeleteOrphan(ctx, uuid.New().String(), false)
	if err == nil {
		t.Error("DeleteOrphan() should return error for non-existent orphan")
	}
}

// ── AdoptOrphan ───────────────────────────────────────────────────────────────

func TestAdoptOrphan_CreatesMediaItemDeletesOrphan(t *testing.T) {
	db := newTestDB(t)
	// MediaItem.BeforeCreate uses gorm hooks; we need the Station table
	// to not fail the FK. SQLite doesn't enforce FK by default so just
	// make sure the tables exist.
	if err := db.AutoMigrate(&models.Station{}); err != nil {
		t.Fatalf("automigrate station: %v", err)
	}

	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	id := uuid.New().String()
	stationID := uuid.New().String()
	o := &models.OrphanMedia{
		ID:          id,
		FilePath:    "adopt/track.mp3",
		ContentHash: "adopthash",
		Title:       "Adopt Me",
		Artist:      "Test Artist",
		FileSize:    100,
		DetectedAt:  time.Now(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create orphan: %v", err)
	}

	item, err := scanner.AdoptOrphan(ctx, id, stationID)
	if err != nil {
		t.Fatalf("AdoptOrphan() error: %v", err)
	}
	if item == nil {
		t.Fatal("AdoptOrphan() returned nil MediaItem")
	}
	if item.StationID != stationID {
		t.Errorf("StationID = %q, want %q", item.StationID, stationID)
	}
	if item.Title != "Adopt Me" {
		t.Errorf("Title = %q, want %q", item.Title, "Adopt Me")
	}

	// Orphan record should be gone.
	got, _ := scanner.GetOrphanByID(ctx, id)
	if got != nil {
		t.Error("orphan should be deleted after adoption")
	}
}

func TestAdoptOrphan_FallsBackToFilenameTitle(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Station{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	id := uuid.New().String()
	o := &models.OrphanMedia{
		ID:          id,
		FilePath:    "station/ab/cd/my-song.mp3",
		ContentHash: "fallbackhash",
		Title:       "", // No title → should use filename
		FileSize:    1,
		DetectedAt:  time.Now(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	item, err := scanner.AdoptOrphan(ctx, id, uuid.New().String())
	if err != nil {
		t.Fatalf("AdoptOrphan() error: %v", err)
	}
	if item.Title != "my-song" {
		t.Errorf("Title = %q, want %q", item.Title, "my-song")
	}
}

func TestAdoptOrphan_NotFound(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	_, err := scanner.AdoptOrphan(ctx, uuid.New().String(), "station-x")
	if err == nil {
		t.Error("AdoptOrphan() should return error for non-existent orphan")
	}
}

// ── AdoptOrphanForImport ──────────────────────────────────────────────────────

func TestAdoptOrphanForImport(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Station{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	o := &models.OrphanMedia{
		ID:          uuid.New().String(),
		FilePath:    "import/track.mp3",
		ContentHash: "importhash",
		Title:       "Import Track",
		FileSize:    200,
		DetectedAt:  time.Now(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	stationID := uuid.New().String()
	jobID := uuid.New().String()
	sourceID := "lt-123"

	item, err := scanner.AdoptOrphanForImport(ctx, o, stationID, jobID, sourceID)
	if err != nil {
		t.Fatalf("AdoptOrphanForImport() error: %v", err)
	}
	if item == nil {
		t.Fatal("AdoptOrphanForImport() returned nil")
	}
	if item.ImportSource != "libretime" {
		t.Errorf("ImportSource = %q, want %q", item.ImportSource, "libretime")
	}
	if item.ImportSourceID != sourceID {
		t.Errorf("ImportSourceID = %q, want %q", item.ImportSourceID, sourceID)
	}
	if *item.ImportJobID != jobID {
		t.Errorf("ImportJobID = %q, want %q", *item.ImportJobID, jobID)
	}

	// Orphan should be gone.
	got, _ := scanner.GetOrphanByID(ctx, o.ID)
	if got != nil {
		t.Error("orphan should be deleted after import adoption")
	}
}

func TestAdoptOrphanForImport_FallsBackToFilenameTitle(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Station{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	o := &models.OrphanMedia{
		ID:          uuid.New().String(),
		FilePath:    "st/cool-song.flac",
		ContentHash: "flachash",
		Title:       "", // will fall back
		FileSize:    1,
		DetectedAt:  time.Now(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	item, err := scanner.AdoptOrphanForImport(ctx, o, uuid.New().String(), uuid.New().String(), "src-1")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if item.Title != "cool-song" {
		t.Errorf("Title = %q, want %q", item.Title, "cool-song")
	}
}

// ── BulkAdoptOrphans ──────────────────────────────────────────────────────────

func TestBulkAdoptOrphans(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Station{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()
	stationID := uuid.New().String()

	ids := make([]string, 3)
	for i := range ids {
		id := uuid.New().String()
		ids[i] = id
		o := &models.OrphanMedia{
			ID:          id,
			FilePath:    id + ".mp3",
			ContentHash: id,
			FileSize:    1,
			DetectedAt:  time.Now(),
		}
		if err := db.Create(o).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	adopted, err := scanner.BulkAdoptOrphans(ctx, ids, stationID)
	if err != nil {
		t.Fatalf("BulkAdoptOrphans() error: %v", err)
	}
	if adopted != 3 {
		t.Errorf("adopted = %d, want 3", adopted)
	}
}

func TestBulkAdoptOrphans_SkipsMissing(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Station{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	// Mix of valid and non-existent IDs.
	validID := uuid.New().String()
	o := &models.OrphanMedia{
		ID:          validID,
		FilePath:    "valid.mp3",
		ContentHash: "vhash",
		FileSize:    1,
		DetectedAt:  time.Now(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	adopted, err := scanner.BulkAdoptOrphans(ctx, []string{validID, uuid.New().String()}, uuid.New().String())
	if err != nil {
		t.Fatalf("BulkAdoptOrphans() error: %v", err)
	}
	if adopted != 1 {
		t.Errorf("adopted = %d, want 1", adopted)
	}
}

// ── BulkDeleteOrphans ─────────────────────────────────────────────────────────

func TestBulkDeleteOrphans(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	ids := make([]string, 3)
	for i := range ids {
		id := uuid.New().String()
		ids[i] = id
		o := &models.OrphanMedia{
			ID:          id,
			FilePath:    id + ".mp3",
			ContentHash: id,
			FileSize:    1,
			DetectedAt:  time.Now(),
		}
		if err := db.Create(o).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	deleted, err := scanner.BulkDeleteOrphans(ctx, ids, false)
	if err != nil {
		t.Fatalf("BulkDeleteOrphans() error: %v", err)
	}
	if deleted != 3 {
		t.Errorf("deleted = %d, want 3", deleted)
	}

	count, _, _ := scanner.GetOrphanStats(ctx)
	if count != 0 {
		t.Errorf("count after bulk delete = %d, want 0", count)
	}
}

func TestBulkDeleteOrphans_SkipsMissing(t *testing.T) {
	db := newTestDB(t)
	scanner, _ := newTestScanner(t, db)
	ctx := context.Background()

	validID := uuid.New().String()
	o := &models.OrphanMedia{
		ID:          validID,
		FilePath:    "bulk-del.mp3",
		ContentHash: "bdhash",
		FileSize:    1,
		DetectedAt:  time.Now(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	deleted, err := scanner.BulkDeleteOrphans(ctx, []string{validID, uuid.New().String()}, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
}

// ── probeFileMetadata ─────────────────────────────────────────────────────────

func TestProbeFileMetadata_FfprobeNotInstalled(t *testing.T) {
	// If ffprobe is not installed the call should return an error gracefully.
	// The scanner creates an orphan record even when probing fails (uses filename).
	tmp := t.TempDir()
	f := filepath.Join(tmp, "audio.mp3")
	if err := os.WriteFile(f, []byte("not real audio"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := probeFileMetadata(context.Background(), f)
	// Either success (if ffprobe is installed) or error — both are valid.
	// This test just verifies the function doesn't panic.
	_ = err
}
