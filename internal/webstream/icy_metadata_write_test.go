/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import (
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestWriteHistoryMetadata_PersistsMapField guards the gorm-serializer bug: the
// old Updates(map{"metadata": map}) form failed at database/sql and persisted
// nothing (dropping artist/title with it). The struct-field write must round-trip.
func TestWriteHistoryMetadata_PersistsMapField(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.PlayHistory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// A webstream-style row: empty media_id, seeded metadata map.
	hist := models.PlayHistory{
		ID: "h1", StationID: "st1",
		Metadata: map[string]any{"existing": "value"},
	}
	if err := db.Create(&hist).Error; err != nil {
		t.Fatalf("seed history: %v", err)
	}

	p := &ICYPoller{db: db, logger: zerolog.Nop(), stationID: "st1"}
	if err := p.writeHistoryMetadata(&hist, "Now Title", "Now Artist"); err != nil {
		t.Fatalf("writeHistoryMetadata: %v", err)
	}

	var got models.PlayHistory
	if err := db.First(&got, "id = ?", "h1").Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Artist != "Now Artist" || got.Title != "Now Title" {
		t.Fatalf("artist/title not persisted: artist=%q title=%q", got.Artist, got.Title)
	}
	if got.Metadata["stream_title"] != "Now Title" || got.Metadata["stream_artist"] != "Now Artist" {
		t.Fatalf("stream metadata not persisted: %v", got.Metadata)
	}
	if got.Metadata["icy_metadata"] != true {
		t.Fatalf("icy_metadata flag not set: %v", got.Metadata)
	}
	if got.Metadata["existing"] != "value" {
		t.Fatalf("existing metadata lost: %v", got.Metadata)
	}
}
