/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// executor_errors_test.go – additional coverage for the error paths in
// handleExecutorStates and handleExecutorHealth.
//
// Both handlers share the same uncovered branch: when ListStates returns an
// error the handler must write HTTP 500.  We trigger this by closing the
// underlying sql.DB so that every subsequent GORM query fails.

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/executor"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// newExecutorAPIWithBrokenDB creates an API whose underlying DB connection pool
// is closed immediately after migration, so any further SQL query returns an error.
func newExecutorAPIWithBrokenDB(t *testing.T) *API {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "broken.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.ExecutorState{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Close the connection pool so all subsequent queries fail with an error.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql.DB: %v", err)
	}

	mgr := executor.NewStateManager(db, zerolog.Nop())
	return &API{db: db, executorStateMgr: mgr, logger: zerolog.Nop()}
}

// TestExecutorAPI_StatesError verifies that handleExecutorStates returns 500
// when ListStates fails (e.g., closed DB).
func TestExecutorAPI_StatesError(t *testing.T) {
	a := newExecutorAPIWithBrokenDB(t)

	req := httptest.NewRequest(http.MethodGet, "/executor/states", nil)
	rr := httptest.NewRecorder()
	a.handleExecutorStates(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("handleExecutorStates with broken DB: got %d, want 500; body=%s",
			rr.Code, rr.Body.String())
	}
}

// TestExecutorAPI_HealthError verifies that handleExecutorHealth returns 500
// when ListStates fails (e.g., closed DB).
func TestExecutorAPI_HealthError(t *testing.T) {
	a := newExecutorAPIWithBrokenDB(t)

	req := httptest.NewRequest(http.MethodGet, "/executor/health", nil)
	rr := httptest.NewRecorder()
	a.handleExecutorHealth(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("handleExecutorHealth with broken DB: got %d, want 500; body=%s",
			rr.Code, rr.Body.String())
	}
}
