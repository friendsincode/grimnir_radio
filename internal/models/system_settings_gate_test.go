/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newGateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&SystemSettings{}); err != nil {
		t.Fatalf("migrate SystemSettings: %v", err)
	}
	return db
}

// The three feature gates must reflect the stored toggle values.
func TestFeatureGates_ReflectStoredValues(t *testing.T) {
	db := newGateTestDB(t)

	// Create the singleton (defaults all true), then force everything OFF.
	// A map-based Updates includes zero values and runs as an UPDATE, so it
	// avoids the gorm `default:true` behaviour that applies to INSERTs.
	if _, err := GetSystemSettings(db); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	setToggles := func(on bool) {
		t.Helper()
		if err := db.Model(&SystemSettings{}).Where("id = ?", 1).Updates(map[string]any{
			"metrics_enabled":   on,
			"websocket_enabled": on,
			"analysis_enabled":  on,
		}).Error; err != nil {
			t.Fatalf("set toggles to %v: %v", on, err)
		}
	}

	setToggles(false)

	if IsMetricsEnabled(db) {
		t.Error("IsMetricsEnabled = true, want false")
	}
	if IsWebsocketEnabled(db) {
		t.Error("IsWebsocketEnabled = true, want false")
	}
	if IsAnalysisEnabled(db) {
		t.Error("IsAnalysisEnabled = true, want false")
	}

	setToggles(true)

	if !IsMetricsEnabled(db) {
		t.Error("IsMetricsEnabled = false, want true")
	}
	if !IsWebsocketEnabled(db) {
		t.Error("IsWebsocketEnabled = false, want true")
	}
	if !IsAnalysisEnabled(db) {
		t.Error("IsAnalysisEnabled = false, want true")
	}
}

// When settings can't be read (e.g. the table is missing / DB error), the gates
// must fail open so a transient error never silently disables these features.
func TestFeatureGates_FailOpenOnReadError(t *testing.T) {
	// Fresh in-memory DB with NO migration, so GetSystemSettings errors.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if !IsMetricsEnabled(db) {
		t.Error("IsMetricsEnabled should fail open to true on read error")
	}
	if !IsWebsocketEnabled(db) {
		t.Error("IsWebsocketEnabled should fail open to true on read error")
	}
	if !IsAnalysisEnabled(db) {
		t.Error("IsAnalysisEnabled should fail open to true on read error")
	}
}
