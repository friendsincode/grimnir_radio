/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func gateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.SystemSettings{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := models.GetSystemSettings(db); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	return db
}

func setMetricsEnabled(t *testing.T, db *gorm.DB, on bool) {
	t.Helper()
	if err := db.Model(&models.SystemSettings{}).Where("id = ?", 1).
		Update("metrics_enabled", on).Error; err != nil {
		t.Fatalf("set metrics_enabled=%v: %v", on, err)
	}
}

func TestMetricsGate_ServesWhenEnabled(t *testing.T) {
	db := gateTestDB(t)
	setMetricsEnabled(t, db, true)

	served := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("metrics"))
	})

	rec := httptest.NewRecorder()
	metricsGate(db, next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if !served {
		t.Error("next handler was not called when metrics enabled")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestMetricsGate_NotFoundWhenDisabled(t *testing.T) {
	db := gateTestDB(t)
	setMetricsEnabled(t, db, false)

	served := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
	})

	rec := httptest.NewRecorder()
	metricsGate(db, next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if served {
		t.Error("next handler was called when metrics disabled; endpoint must look unmounted")
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
