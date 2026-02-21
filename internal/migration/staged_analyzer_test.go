package migration

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestDetectDuplicates_HashAndMetadataFallback(t *testing.T) {
	db := openStagedAnalyzerTestDB(t)
	if err := db.Create(&models.MediaItem{
		ID:          "mhash",
		StationID:   "s1",
		Title:       "Hash Song",
		Artist:      "Artist A",
		Album:       "Album A",
		ContentHash: "abc123",
	}).Error; err != nil {
		t.Fatalf("seed hash media: %v", err)
	}
	if err := db.Create(&models.MediaItem{
		ID:        "mmeta",
		StationID: "s1",
		Title:     "  Song  Name  ",
		Artist:    "The Artist",
		Album:     "The Album",
	}).Error; err != nil {
		t.Fatalf("seed metadata media: %v", err)
	}

	analyzer := NewStagedAnalyzer(db, zerolog.Nop())
	staged := []models.StagedMediaItem{
		{SourceID: "src-hash", Title: "Hash Song", Artist: "Artist A", Album: "Album A", ContentHash: "abc123"},
		{SourceID: "src-meta", Title: "song name", Artist: "the artist", Album: "the album"},
	}

	got := analyzer.DetectDuplicates(context.Background(), staged, "s1")

	if !got[0].IsDuplicate || got[0].DuplicateOfID != "mhash" {
		t.Fatalf("expected hash duplicate match to mhash, got duplicate=%v id=%s", got[0].IsDuplicate, got[0].DuplicateOfID)
	}
	if !got[1].IsDuplicate || got[1].DuplicateOfID != "mmeta" {
		t.Fatalf("expected metadata fallback duplicate match to mmeta, got duplicate=%v id=%s", got[1].IsDuplicate, got[1].DuplicateOfID)
	}
}

func TestDetectDuplicates_MetadataFallback_StationScoped(t *testing.T) {
	db := openStagedAnalyzerTestDB(t)
	if err := db.Create(&models.MediaItem{
		ID:        "m-other-station",
		StationID: "s2",
		Title:     "Song Name",
		Artist:    "Artist",
		Album:     "Album",
	}).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	analyzer := NewStagedAnalyzer(db, zerolog.Nop())
	staged := []models.StagedMediaItem{
		{SourceID: "src-meta", Title: "song name", Artist: "artist", Album: "album"},
	}

	got := analyzer.DetectDuplicates(context.Background(), staged, "s1")
	if got[0].IsDuplicate {
		t.Fatalf("expected no duplicate for different station when stationID is scoped")
	}

	got = analyzer.DetectDuplicates(context.Background(), staged, "")
	if !got[0].IsDuplicate || got[0].DuplicateOfID != "m-other-station" {
		t.Fatalf("expected cross-station duplicate when stationID is empty")
	}
}

func openStagedAnalyzerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.MediaItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}
