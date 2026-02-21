package migration

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestSourceImportExists_ScopedAndLegacySourceID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.SmartBlock{}, &models.Show{}, &models.Webstream{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := db.Create(&models.SmartBlock{
		ID:             "sb1",
		StationID:      "station-a",
		Name:           "SB",
		ImportSource:   string(SourceTypeAzuraCast),
		ImportSourceID: "7::123",
	}).Error; err != nil {
		t.Fatalf("seed smartblock: %v", err)
	}
	showStart := time.Now().UTC()
	if err := db.Create(&models.Show{
		ID:                     "show1",
		StationID:              "station-a",
		Name:                   "Show",
		DefaultDurationMinutes: 60,
		DTStart:                showStart,
		Timezone:               "UTC",
		ImportSource:           string(SourceTypeAzuraCast),
		ImportSourceID:         "123", // legacy unscoped
	}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	if err := db.Create(&models.Webstream{
		ID:             "ws1",
		StationID:      "station-a",
		Name:           "WS",
		URLs:           []string{"https://example.com/live"},
		ImportSource:   string(SourceTypeAzuraCast),
		ImportSourceID: "7::321",
	}).Error; err != nil {
		t.Fatalf("seed webstream: %v", err)
	}

	ctx := context.Background()

	exists, err := sourceImportExists(ctx, db, &models.SmartBlock{}, "station-a", string(SourceTypeAzuraCast), "7::123", "123")
	if err != nil {
		t.Fatalf("smartblock exists err: %v", err)
	}
	if !exists {
		t.Fatalf("expected smartblock source import to exist")
	}

	exists, err = sourceImportExists(ctx, db, &models.Show{}, "station-a", string(SourceTypeAzuraCast), "7::123", "123")
	if err != nil {
		t.Fatalf("show exists err: %v", err)
	}
	if !exists {
		t.Fatalf("expected legacy source ID to match for show")
	}

	exists, err = sourceImportExists(ctx, db, &models.Webstream{}, "station-a", string(SourceTypeAzuraCast), "7::999", "999")
	if err != nil {
		t.Fatalf("webstream exists err: %v", err)
	}
	if exists {
		t.Fatalf("expected no match for unknown source IDs")
	}
}
