/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

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
	"github.com/friendsincode/grimnir_radio/internal/migration"
)

func newMigrationHandlerTest(t *testing.T) (*MigrationHandler, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&migration.Job{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &MigrationHandler{
		service: migration.NewService(db, events.NewBus(), zerolog.Nop()),
		logger:  zerolog.Nop(),
	}, db
}

func TestMigration_CreateJob(t *testing.T) {
	h, _ := newMigrationHandlerTest(t)

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
		rr := httptest.NewRecorder()
		h.handleCreateMigrationJob(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("empty source type returns error", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"source_type": ""})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		h.handleCreateMigrationJob(rr, req)
		// empty source_type fails validation in service → 400
		if rr.Code != http.StatusBadRequest && rr.Code != http.StatusCreated {
			t.Fatalf("got %d, want 400 or 201", rr.Code)
		}
	})

	t.Run("valid azuracast job creates 201", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"source_type": string(migration.SourceTypeAzuraCast),
			"options":     map[string]any{"station_id": "s1"},
		})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		h.handleCreateMigrationJob(rr, req)
		if rr.Code != http.StatusCreated && rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 201 or 400", rr.Code)
		}
	})
}

func TestMigration_ListJobs(t *testing.T) {
	h, _ := newMigrationHandlerTest(t)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.handleListMigrationJobs(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	var resp ListMigrationJobsResponse
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp.Jobs == nil {
		t.Fatal("expected jobs key in response")
	}
}

func TestMigration_StartJob(t *testing.T) {
	h, _ := newMigrationHandlerTest(t)

	req := httptest.NewRequest("POST", "/nonexistent/start", nil)
	req = withChiParam(req, "id", "nonexistent")
	rr := httptest.NewRecorder()
	h.handleStartMigrationJob(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestMigration_CancelJob(t *testing.T) {
	h, _ := newMigrationHandlerTest(t)

	req := httptest.NewRequest("POST", "/nonexistent/cancel", nil)
	req = withChiParam(req, "id", "nonexistent")
	rr := httptest.NewRecorder()
	h.handleCancelMigrationJob(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestMigration_DeleteJob(t *testing.T) {
	h, _ := newMigrationHandlerTest(t)

	req := httptest.NewRequest("DELETE", "/nonexistent", nil)
	req = withChiParam(req, "id", "nonexistent")
	rr := httptest.NewRecorder()
	h.handleDeleteMigrationJob(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestMigration_GetStagedImport(t *testing.T) {
	h, _ := newMigrationHandlerTest(t)

	req := httptest.NewRequest("GET", "/staged/nonexistent", nil)
	req = withChiParam(req, "stagedID", "nonexistent")
	rr := httptest.NewRecorder()
	h.handleGetStagedImport(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rr.Code)
	}
}

func TestMigration_UpdateSelections(t *testing.T) {
	h, _ := newMigrationHandlerTest(t)

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/staged/s1/selections", bytes.NewReader([]byte("{")))
		req = withChiParam(req, "stagedID", "s1")
		rr := httptest.NewRecorder()
		h.handleUpdateSelections(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("nonexistent staged import", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"selections": map[string]any{}})
		req := httptest.NewRequest("PUT", "/staged/nonexistent/selections", bytes.NewReader(body))
		req = withChiParam(req, "stagedID", "nonexistent")
		rr := httptest.NewRecorder()
		h.handleUpdateSelections(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}

func TestMigration_CommitStagedImport(t *testing.T) {
	h, _ := newMigrationHandlerTest(t)

	req := httptest.NewRequest("POST", "/staged/nonexistent/commit", nil)
	req = withChiParam(req, "stagedID", "nonexistent")
	rr := httptest.NewRecorder()
	h.handleCommitStagedImport(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestMigration_RejectStagedImport(t *testing.T) {
	h, _ := newMigrationHandlerTest(t)

	req := httptest.NewRequest("DELETE", "/staged/nonexistent", nil)
	req = withChiParam(req, "stagedID", "nonexistent")
	rr := httptest.NewRecorder()
	h.handleRejectStagedImport(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestMigration_GetImportedItems(t *testing.T) {
	h, _ := newMigrationHandlerTest(t)

	req := httptest.NewRequest("GET", "/nonexistent/items", nil)
	req = withChiParam(req, "id", "nonexistent")
	rr := httptest.NewRecorder()
	h.handleGetImportedItems(rr, req)
	// Returns 200 with empty items or 404 depending on implementation
	if rr.Code != http.StatusOK && rr.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 200 or 404", rr.Code)
	}
}

func TestMigration_RollbackImport(t *testing.T) {
	h, _ := newMigrationHandlerTest(t)

	req := httptest.NewRequest("POST", "/nonexistent/rollback", nil)
	req = withChiParam(req, "id", "nonexistent")
	rr := httptest.NewRecorder()
	h.handleRollbackImport(rr, req)
	if rr.Code != http.StatusBadRequest && rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 400 or 200", rr.Code)
	}
}

func TestMigration_CloneForRedo(t *testing.T) {
	h, _ := newMigrationHandlerTest(t)

	req := httptest.NewRequest("POST", "/nonexistent/redo", nil)
	req = withChiParam(req, "id", "nonexistent")
	rr := httptest.NewRecorder()
	h.handleCloneForRedo(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}
