/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func wsGateTestDB(t *testing.T) *gorm.DB {
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

func setWebsocketEnabled(t *testing.T, db *gorm.DB, on bool) {
	t.Helper()
	if err := db.Model(&models.SystemSettings{}).Where("id = ?", 1).
		Update("websocket_enabled", on).Error; err != nil {
		t.Fatalf("set websocket_enabled=%v: %v", on, err)
	}
}

func TestWebsocketGate_ServesWhenEnabled(t *testing.T) {
	db := wsGateTestDB(t)
	setWebsocketEnabled(t, db, true)

	served := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	websocketGate(db, next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/events", nil))

	if !served {
		t.Error("next handler was not called when websockets enabled")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestWebsocketGate_ForbiddenWhenDisabled(t *testing.T) {
	db := wsGateTestDB(t)
	setWebsocketEnabled(t, db, false)

	served := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
	})

	rec := httptest.NewRecorder()
	websocketGate(db, next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/events", nil))

	if served {
		t.Error("next handler was called when websockets disabled; upgrade must be refused")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}
