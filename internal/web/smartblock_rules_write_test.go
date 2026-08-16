/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestWriteSmartBlockRules_PersistsMapField guards the gorm-serializer bug:
// Update("rules", map) skipped the serializer and silently failed, so smart
// block rule edits (fallback-ref cleanup, preview temp-save) never persisted.
func TestWriteSmartBlockRules_PersistsMapField(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.SmartBlock{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := db.Create(&models.SmartBlock{
		ID: "sb1", StationID: "st1",
		Rules: map[string]any{"fallback": "old"},
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	newRules := map[string]any{"filters": []any{"rock"}, "nested": map[string]any{"k": "v"}}
	if err := writeSmartBlockRules(db, "sb1", newRules); err != nil {
		t.Fatalf("writeSmartBlockRules: %v", err)
	}

	var got models.SmartBlock
	if err := db.First(&got, "id = ?", "sb1").Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Rules["fallback"] != nil {
		t.Fatalf("old rules not replaced: %v", got.Rules)
	}
	nested, _ := got.Rules["nested"].(map[string]any)
	if nested["k"] != "v" {
		t.Fatalf("nested rules not persisted: %v", got.Rules)
	}
}
