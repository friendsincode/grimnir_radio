/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package server

import (
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestMetricsEnabled guards #86: /metrics exposes internal Prometheus data and is
// gated by a system setting, read once at startup. The gate is default-on — a
// fresh install and even a settings read error must leave metrics enabled — and
// only an explicit MetricsEnabled=false turns it off. Getting this wrong either
// leaks metrics when an operator disabled them or blackholes monitoring on a
// transient DB hiccup.
func TestMetricsEnabled(t *testing.T) {
	newDB := func(t *testing.T) *gorm.DB {
		t.Helper()
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatalf("open db: %v", err)
		}
		if err := db.AutoMigrate(&models.SystemSettings{}); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		return db
	}

	t.Run("default-on for a fresh install", func(t *testing.T) {
		if !metricsEnabled(newDB(t), zerolog.Nop()) {
			t.Error("a fresh install should enable /metrics by default")
		}
	})

	t.Run("explicit disable turns it off", func(t *testing.T) {
		db := newDB(t)
		db.Create(&models.SystemSettings{ID: 1, MetricsEnabled: false})
		// MetricsEnabled has a gorm default:true, so a false zero-value Create is
		// dropped; force the column to false explicitly.
		db.Model(&models.SystemSettings{}).Where("id = ?", 1).Update("metrics_enabled", false)
		if metricsEnabled(db, zerolog.Nop()) {
			t.Error("MetricsEnabled=false must disable /metrics")
		}
	})

	t.Run("read error stays default-on", func(t *testing.T) {
		// A DB with no settings table: GetSystemSettings errors, and the gate must
		// still enable metrics rather than blackhole monitoring.
		badDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatalf("open db: %v", err)
		}
		if !metricsEnabled(badDB, zerolog.Nop()) {
			t.Error("a settings read error should leave /metrics enabled")
		}
	})
}
