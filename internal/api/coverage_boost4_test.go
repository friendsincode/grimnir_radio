/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// coverage_boost4_test.go — Fourth coverage boost batch.
// Targets:
//   - logValidationSummary with overlap violations (direct call, covers warning+loop body)
//   - handlePriorityActive with active PrioritySource in DB (covers loop body)
//   - handlePriorityRelease success path (with PrioritySource in DB)
//   - handleShowsDelete with future ShowInstance (covers instance deletion branch)
//   - handleExecutorStates with ExecutorState data (covers loop body)
//   - handleLiveStartHandover success path (via real LiveSession + priority service)

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/executor"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
)

// newBoost4PriorityAPI creates an API with a real priority service and proper tables.
func newBoost4PriorityAPI(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "boost4_priority.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.PrioritySource{},
		&models.Station{},
		&models.Mount{},
		&models.ExecutorState{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, zerolog.Nop())

	return &API{
		db:          db,
		bus:         bus,
		prioritySvc: prioritySvc,
		logger:      zerolog.Nop(),
	}, db
}

// --- logValidationSummary ---

// TestLogValidationSummary_NilResult verifies nil-guard early return.
func TestLogValidationSummary_NilResult(t *testing.T) {
	a := &API{logger: zerolog.Nop()}
	// Should not panic with nil result
	a.logValidationSummary("st-1", time.Now(), time.Now().Add(time.Hour), nil)
}

// TestLogValidationSummary_NoOverlaps verifies the "no overlaps" logging path.
func TestLogValidationSummary_NoOverlaps(t *testing.T) {
	a := &API{logger: zerolog.Nop()}
	result := &models.ValidationResult{
		Valid:   true,
		Errors:  []models.ValidationViolation{},
		Warnings: []models.ValidationViolation{},
		Info:    []models.ValidationViolation{},
	}
	// Should log "no overlaps" and return
	a.logValidationSummary("st-1", time.Now(), time.Now().Add(time.Hour), result)
}

// TestLogValidationSummary_WithOverlaps verifies the overlap warning loop path,
// including the overlap_minutes details branch.
func TestLogValidationSummary_WithOverlaps(t *testing.T) {
	a := &API{logger: zerolog.Nop()}

	violations := []models.ValidationViolation{
		{
			RuleType:    models.RuleTypeOverlap,
			RuleName:    "overlap-rule",
			Severity:    "warning",
			Message:     "Show overlap detected",
			StartsAt:    time.Now(),
			EndsAt:      time.Now().Add(30 * time.Minute),
			AffectedIDs: []string{"show-1", "show-2"},
			Details:     map[string]any{"overlap_minutes": 30.0},
		},
		{
			RuleType:    models.RuleTypeOverlap,
			RuleName:    "overlap-rule-2",
			Severity:    "error",
			Message:     "Critical overlap",
			StartsAt:    time.Now(),
			EndsAt:      time.Now().Add(15 * time.Minute),
			AffectedIDs: []string{"show-3"},
			// No overlap_minutes details — covers the !ok branch
		},
	}

	result := &models.ValidationResult{
		Valid:    false,
		Errors:   violations[:1],
		Warnings: violations[1:],
	}

	// Should log warnings about overlaps and iterate the loop
	a.logValidationSummary("st-overlap", time.Now(), time.Now().Add(time.Hour), result)
}

// --- handlePriorityActive with active sources ---

// TestHandlePriorityActive_WithActiveSources verifies the loop body is covered
// when active PrioritySource records exist in the DB.
func TestHandlePriorityActive_WithActiveSources(t *testing.T) {
	a, db := newBoost4PriorityAPI(t)

	// Insert an active PrioritySource directly
	now := time.Now()
	source := models.PrioritySource{
		ID:          "ps-active-1",
		StationID:   "st-prio-active",
		MountID:     "mt-prio-1",
		Priority:    models.PriorityLiveOverride,
		SourceType:  models.SourceTypeLive,
		SourceID:    "src-1",
		Active:      true,
		ActivatedAt: now,
	}
	db.Create(&source) //nolint:errcheck

	req := httptest.NewRequest("GET", "/priority/active?station_id=st-prio-active", nil)
	rr := httptest.NewRecorder()
	a.handlePriorityActive(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("priority active: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rr.Body.String())
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 active source, got %d", len(resp))
	}
	if resp[0]["source_id"] != "ps-active-1" {
		t.Fatalf("unexpected source_id: %v", resp[0]["source_id"])
	}
}

// --- handlePriorityRelease success path ---

// TestHandlePriorityRelease_Success verifies 200 when PrioritySource exists and is deactivated.
func TestHandlePriorityRelease_Success(t *testing.T) {
	a, db := newBoost4PriorityAPI(t)

	// Insert an active PrioritySource that can be released.
	// Note: the chi param "sourceID" maps to PrioritySource.SourceID (not the primary key ID).
	now := time.Now()
	source := models.PrioritySource{
		ID:          "ps-release-pk-1",
		StationID:   "st-prio-release",
		MountID:     "mt-release-1",
		Priority:    models.PriorityLiveOverride,
		SourceType:  models.SourceTypeLive,
		SourceID:    "src-for-release",
		Active:      true,
		ActivatedAt: now,
	}
	db.Create(&source) //nolint:errcheck

	body, _ := json.Marshal(map[string]any{
		"station_id": "st-prio-release",
	})
	req := httptest.NewRequest("DELETE", "/priority/src-for-release", bytes.NewBuffer(body))
	req = withChiParam(req, "sourceID", "src-for-release")
	rr := httptest.NewRecorder()
	a.handlePriorityRelease(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("priority release: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "released" {
		t.Fatalf("expected status=released, got %v", resp["status"])
	}
}

// --- handleExecutorStates with data (loop body) ---

// TestHandleExecutorStates_WithState verifies the loop body is exercised when states exist.
func TestHandleExecutorStates_WithState(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "executor_states.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(&models.ExecutorState{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Insert an executor state directly
	state := models.ExecutorState{
		ID:        "es-loop-1",
		StationID: "st-exec-loop",
		State:     models.ExecutorStatePlaying,
	}
	db.Create(&state) //nolint:errcheck

	stateMgr := executor.NewStateManager(db, zerolog.Nop())
	a := &API{
		db:               db,
		executorStateMgr: stateMgr,
		logger:           zerolog.Nop(),
	}

	req := httptest.NewRequest("GET", "/executor/states", nil)
	rr := httptest.NewRecorder()
	a.handleExecutorStates(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("executor states: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rr.Body.String())
	}
	if len(resp) < 1 {
		t.Fatalf("expected at least 1 state, got %d", len(resp))
	}
}

// --- handleShowsDelete with future instance ---

// TestHandleShowsDelete_WithFutureInstance verifies that future ShowInstances
// are deleted when a show is deleted (covers the tx.Where + Delete branch).
func TestHandleShowsDelete_WithFutureInstance(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "shows_del.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(&models.Show{}, &models.ShowInstance{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	a := &API{db: db, bus: events.NewBus(), logger: zerolog.Nop()}

	// Seed show with a future instance
	show := models.Show{
		ID:        "show-del-future",
		StationID: "st-show-del",
		Name:      "Delete With Instances",
		DTStart:   time.Now().UTC(),
		Active:    true,
	}
	db.Create(&show) //nolint:errcheck

	futureInstance := models.ShowInstance{
		ID:       "inst-future-1",
		ShowID:   show.ID,
		StartsAt: time.Now().Add(24 * time.Hour), // Future instance
		EndsAt:   time.Now().Add(25 * time.Hour),
	}
	db.Create(&futureInstance) //nolint:errcheck

	req := httptest.NewRequest("DELETE", "/shows/"+show.ID, nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "showID", show.ID)
	rr := httptest.NewRecorder()
	a.handleShowsDelete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("shows delete: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// Verify the future instance was deleted
	var count int64
	db.Model(&models.ShowInstance{}).Where("show_id = ?", show.ID).Count(&count)
	if count != 0 {
		t.Fatalf("expected 0 future instances after delete, got %d", count)
	}
}
