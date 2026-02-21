package migration

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestVerifyImportDurations_WarnMode(t *testing.T) {
	db := openDurationVerifyDB(t)
	importer := &AzuraCastImporter{db: db, logger: zerolog.Nop()}
	jobID := "job-warn"
	seedDurationRows(t, db, jobID, 2, 1)

	result := &Result{
		Warnings: []string{},
		Skipped:  map[string]int{},
		Mappings: map[string]Mapping{},
	}
	if err := importer.verifyImportDurations(context.Background(), jobID, false, result); err != nil {
		t.Fatalf("verifyImportDurations warn mode returned error: %v", err)
	}
	if result.Skipped["media_duration_zero"] != 2 {
		t.Fatalf("expected media_duration_zero=2, got %d", result.Skipped["media_duration_zero"])
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("expected warning entry")
	}
}

func TestVerifyImportDurations_StrictMode(t *testing.T) {
	db := openDurationVerifyDB(t)
	importer := &AzuraCastImporter{db: db, logger: zerolog.Nop()}
	jobID := "job-strict"
	seedDurationRows(t, db, jobID, 1, 1)

	result := &Result{
		Warnings: []string{},
		Skipped:  map[string]int{},
		Mappings: map[string]Mapping{},
	}
	if err := importer.verifyImportDurations(context.Background(), jobID, true, result); err == nil {
		t.Fatalf("expected strict mode failure")
	}
}

func openDurationVerifyDB(t *testing.T) *gorm.DB {
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

func seedDurationRows(t *testing.T, db *gorm.DB, jobID string, zeroCount, nonZeroCount int) {
	t.Helper()
	for i := 0; i < zeroCount; i++ {
		if err := db.Create(&models.MediaItem{
			ID:          "zero-" + jobID + "-" + string(rune('a'+i)),
			StationID:   "station-a",
			Title:       "Zero Duration",
			ImportJobID: &jobID,
			Duration:    0,
		}).Error; err != nil {
			t.Fatalf("seed zero row: %v", err)
		}
	}
	for i := 0; i < nonZeroCount; i++ {
		if err := db.Create(&models.MediaItem{
			ID:          "ok-" + jobID + "-" + string(rune('a'+i)),
			StationID:   "station-a",
			Title:       "Normal Duration",
			ImportJobID: &jobID,
			Duration:    3 * time.Minute,
		}).Error; err != nil {
			t.Fatalf("seed non-zero row: %v", err)
		}
	}
}
