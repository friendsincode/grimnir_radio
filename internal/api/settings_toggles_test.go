/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestAnalysisEnabledSetting(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settings.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.SystemSettings{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a := &API{db: db, logger: zerolog.Nop()}

	// Fresh settings default to Media Analysis on.
	if !a.analysisEnabled() {
		t.Error("expected analysis enabled by default")
	}

	// Turning the toggle off is respected.
	s, err := models.GetSystemSettings(db)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	s.AnalysisEnabled = false
	if err := db.Save(s).Error; err != nil {
		t.Fatalf("save: %v", err)
	}
	if a.analysisEnabled() {
		t.Error("expected analysis disabled after AnalysisEnabled=false")
	}

	// And back on.
	s.AnalysisEnabled = true
	if err := db.Save(s).Error; err != nil {
		t.Fatalf("save: %v", err)
	}
	if !a.analysisEnabled() {
		t.Error("expected analysis enabled after AnalysisEnabled=true")
	}
}
