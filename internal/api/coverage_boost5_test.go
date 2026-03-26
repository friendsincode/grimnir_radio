/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// coverage_boost5_test.go covers DB-error branches in handleStationsList and
// handleStationsCreate that are only reachable when the database connection fails.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// newBoost5API creates an API backed by a SQLite database that has all station-
// related tables migrated.
func newBoost5API(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "boost5.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(&models.Station{}, &models.Mount{}, &models.StationUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{
		db:     db,
		bus:    events.NewBus(),
		logger: zerolog.Nop(),
	}, db
}

// TestHandleStationsList_DBError covers the db error branch in handleStationsList
// by closing the underlying SQL connection before calling the handler.
func TestHandleStationsList_DBError(t *testing.T) {
	a, db := newBoost5API(t)

	// Close the underlying connection pool so db.Find() returns an error.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.Close()

	req := httptest.NewRequest("GET", "/stations", nil)
	rr := httptest.NewRecorder()
	a.handleStationsList(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("closed db: got %d, want 500", rr.Code)
	}
}

// TestHandleStationsCreate_DBError covers the db-error-on-create branch in
// handleStationsCreate by closing the database before the insert.
func TestHandleStationsCreate_DBError(t *testing.T) {
	a, db := newBoost5API(t)

	// Close the connection pool before the create.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.Close()

	body, _ := json.Marshal(map[string]any{
		"name":     "Closed DB Station",
		"timezone": "UTC",
	})
	req := httptest.NewRequest("POST", "/stations", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleStationsCreate(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("closed db: got %d, want 500", rr.Code)
	}
}
