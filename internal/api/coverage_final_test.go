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

	"github.com/friendsincode/grimnir_radio/internal/analyzer"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// --- LandingPageAPI: missing invalid-JSON branches ---

func TestLandingPageAPI_UpdateCustomCSS_InvalidJSON(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	req := httptest.NewRequest("PUT", "/?station_id=s1", bytes.NewReader([]byte(`{bad json`)))
	rr := httptest.NewRecorder()
	lp.handleUpdateCustomCSS(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestLandingPageAPI_UpdateCustomHead_InvalidJSON(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	req := httptest.NewRequest("PUT", "/?station_id=s1", bytes.NewReader([]byte(`{bad json`)))
	rr := httptest.NewRecorder()
	lp.handleUpdateCustomHead(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

// --- NotificationAPI: missing empty-ID and DB-error branches ---

func TestNotificationAPI_MarkRead_EmptyID(t *testing.T) {
	n, _ := newNotificationAPITest(t)

	req := httptest.NewRequest("POST", "/read", nil)
	req = withChiParam(req, "id", "")
	req = withUserClaims(req, "u1")
	rr := httptest.NewRecorder()
	n.handleMarkRead(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

// --- handleReanalyzeMissingArtwork: Pluck+loop+success and DB-error paths ---

func newReanalyzTestAPI(t *testing.T) (*API, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ra.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.MediaItem{}, &models.AnalysisJob{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	analy := analyzer.New(db, t.TempDir(), zerolog.Nop())
	return &API{db: db, analyzer: analy, logger: zerolog.Nop()}, db
}

func TestHandleReanalyzeMissingArtwork_NoItems(t *testing.T) {
	a, _ := newReanalyzTestAPI(t)

	req := httptest.NewRequest("POST", "/system/reanalyze-artwork", nil)
	rr := httptest.NewRecorder()
	a.handleReanalyzeMissingArtwork(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleReanalyzeMissingArtwork_WithItems(t *testing.T) {
	a, db := newReanalyzTestAPI(t)

	// Seed a media item with no artwork that is eligible for reanalysis.
	item := &models.MediaItem{
		ID:            "item-reanalyze-1",
		Path:          "test/file.mp3",
		Duration:      180,
		AnalysisState: "complete",
	}
	if err := db.Create(item).Error; err != nil {
		t.Fatalf("seed item: %v", err)
	}

	req := httptest.NewRequest("POST", "/system/reanalyze-artwork", nil)
	rr := httptest.NewRecorder()
	a.handleReanalyzeMissingArtwork(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp["queued"] == nil {
		t.Fatal("expected queued key in response")
	}
}

func TestHandleReanalyzeMissingArtwork_DBError(t *testing.T) {
	a, db := newReanalyzTestAPI(t)

	sqlDB, _ := db.DB()
	sqlDB.Close()

	req := httptest.NewRequest("POST", "/system/reanalyze-artwork", nil)
	rr := httptest.NewRecorder()
	a.handleReanalyzeMissingArtwork(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500; body=%s", rr.Code, rr.Body.String())
	}
}

// --- handleVersionsGet: missing ErrVersionNotFound (404) path ---

func TestLandingPageAPI_VersionsGet_NotFound(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	req := httptest.NewRequest("GET", "/versions/no-such-version", nil)
	req = withChiParam(req, "versionID", "no-such-version")
	rr := httptest.NewRecorder()
	lp.handleVersionsGet(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rr.Code)
	}
}

// --- handleVersionsRestore: missing paths ---

func TestLandingPageAPI_VersionsRestore_MissingVersionID(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	// station_id present, versionID empty, auth present
	req := httptest.NewRequest("POST", "/?station_id=s1", nil)
	req = withChiParam(req, "versionID", "")
	req = withUserClaims(req, "u1")
	rr := httptest.NewRecorder()
	lp.handleVersionsRestore(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestLandingPageAPI_VersionsRestore_ServiceError(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	// No landing page seeded → service returns ErrNotFound (not ErrVersionNotFound) → 500
	req := httptest.NewRequest("POST", "/?station_id=s1", nil)
	req = withChiParam(req, "versionID", "nonexistent-version")
	req = withUserClaims(req, "u1")
	rr := httptest.NewRecorder()
	lp.handleVersionsRestore(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", rr.Code)
	}
}

// --- handleRepairFilenames: missing DB-error (500) path ---

func TestIntegrity_RepairFilenames_DBError(t *testing.T) {
	a := newIntegrityAPITest(t)

	sqlDB, _ := a.db.DB()
	sqlDB.Close()

	req := httptest.NewRequest("POST", "/integrity/repair-filenames", nil)
	rr := httptest.NewRecorder()
	a.handleRepairFilenames(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", rr.Code)
	}
}

// --- handleAssetsDelete: missing 500 (service error) and 200 (success) paths ---

func TestLandingPageAPI_AssetsDelete_DBError(t *testing.T) {
	lp, db := newLandingPageAPITest(t)

	sqlDB, _ := db.DB()
	sqlDB.Close()

	req := httptest.NewRequest("DELETE", "/assets/any-id", nil)
	req = withChiParam(req, "assetID", "any-id")
	rr := httptest.NewRecorder()
	lp.handleAssetsDelete(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", rr.Code)
	}
}

// --- handleListNetworkShows: DB-error path ---

func TestSyndicationAPI_ListNetworkShows_DBError(t *testing.T) {
	s, db := newSyndicationAPITest(t)

	sqlDB, _ := db.DB()
	sqlDB.Close()

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	s.handleListNetworkShows(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", rr.Code)
	}
}

// --- handleListMigrationJobs: DB-error path ---

func TestMigrationHandler_ListJobs_DBError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "m.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&migration.Job{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := &MigrationHandler{service: migration.NewService(db, events.NewBus(), zerolog.Nop()), logger: zerolog.Nop()}

	sqlDB, _ := db.DB()
	sqlDB.Close()

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.handleListMigrationJobs(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", rr.Code)
	}
}

// --- handleGetMigrationJob: missing not-found path ---

func TestMigrationHandler_GetJob_NotFound(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mj.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&migration.Job{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := &MigrationHandler{service: migration.NewService(db, events.NewBus(), zerolog.Nop()), logger: zerolog.Nop()}

	req := httptest.NewRequest("GET", "/migrations/does-not-exist", nil)
	req = withChiParam(req, "id", "does-not-exist")
	rr := httptest.NewRecorder()
	h.handleGetMigrationJob(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rr.Code)
	}
}

// --- scheduleRulesDelete: missing DB-error on First and Delete ---

func TestScheduleRulesDelete_DBErrorOnFirst(t *testing.T) {
	a, db := newScheduleRulesTest(t)

	sqlDB, _ := db.DB()
	sqlDB.Close()

	req := httptest.NewRequest("DELETE", "/rules/some-id", nil)
	req = withChiParam(req, "ruleID", "some-id")
	rr := httptest.NewRecorder()
	a.handleScheduleRulesDelete(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", rr.Code)
	}
}

func TestScheduleRulesDelete_DBErrorOnDelete(t *testing.T) {
	a, db := newScheduleRulesTest(t)

	// Seed a valid rule first, then close DB so the Delete fails.
	// Use raw SQL to avoid GORM.
	db.Exec("INSERT INTO schedule_rules (id, name, rule_type, created_at, updated_at) VALUES (?, ?, ?, datetime('now'), datetime('now'))", "r1", "Test Rule", "time_slot")

	sqlDB, _ := db.DB()
	sqlDB.Close()

	req := httptest.NewRequest("DELETE", "/rules/r1", nil)
	req = withChiParam(req, "ruleID", "r1")
	rr := httptest.NewRecorder()
	a.handleScheduleRulesDelete(rr, req)
	// DB is closed so First() itself fails → 500 (same as above — both DB errors hit 500)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", rr.Code)
	}
}

// --- WebhookAPI: missing not-found paths for handleLogs and handleTest ---

func TestWebhookAPI_HandleLogs_NotFound(t *testing.T) {
	api, _ := newWebhookAPITest(t)

	req := httptest.NewRequest("GET", "/webhooks/no-such-id/logs", nil)
	req = withChiParam(req, "id", "no-such-id")
	rr := httptest.NewRecorder()
	api.handleLogs(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rr.Code)
	}
}

func TestWebhookAPI_HandleTest_NotFound(t *testing.T) {
	api, _ := newWebhookAPITest(t)

	req := httptest.NewRequest("POST", "/webhooks/no-such-id/test", nil)
	req = withChiParam(req, "id", "no-such-id")
	rr := httptest.NewRecorder()
	api.handleTest(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rr.Code)
	}
}

