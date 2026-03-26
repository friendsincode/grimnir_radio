/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// underwriting_coverage_test.go — additional tests targeting uncovered branches in
// handleDeleteSponsor, handleDeleteObligation, handleMarkAired, handleMarkMissed,
// plus secondary gaps in handleListSpots, handleScheduleSpot, handleFulfillmentReport,
// handleListSponsors, handleUpdateSponsor, handleUpdateObligation.

package api

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

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/underwriting"
)

// newUnderwritingCoverageTest creates a fresh DB + UnderwritingAPI for coverage tests.
func newUnderwritingCoverageTest(t *testing.T) (*UnderwritingAPI, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "uwcov.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&models.Sponsor{},
		&models.UnderwritingObligation{},
		&models.UnderwritingSpot{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc := underwriting.NewService(db, zerolog.Nop())
	api := &API{db: db, logger: zerolog.Nop()}
	return NewUnderwritingAPI(api, svc), db
}

// closeDB closes the underlying sql.DB, causing subsequent GORM queries to fail.
func closeUnderwritingDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.Close()
}

// --- handleDeleteSponsor ---

// TestUnderwritingCoverage_DeleteSponsor_DBError triggers the 500 error branch in
// handleDeleteSponsor by closing the DB before the call.
func TestUnderwritingCoverage_DeleteSponsor_DBError(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)
	closeUnderwritingDB(t, db)

	req := httptest.NewRequest("DELETE", "/", nil)
	req = withChiParam(req, "id", "some-sponsor-id")
	rr := httptest.NewRecorder()
	u.handleDeleteSponsor(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("delete sponsor db error: got %d, want 500", rr.Code)
	}
}

// --- handleDeleteObligation ---

// TestUnderwritingCoverage_DeleteObligation_DBError triggers the 500 error branch in
// handleDeleteObligation by closing the DB before the call.
func TestUnderwritingCoverage_DeleteObligation_DBError(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)
	closeUnderwritingDB(t, db)

	req := httptest.NewRequest("DELETE", "/", nil)
	req = withChiParam(req, "id", "some-obligation-id")
	rr := httptest.NewRecorder()
	u.handleDeleteObligation(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("delete obligation db error: got %d, want 500", rr.Code)
	}
}

// --- handleMarkAired ---

// TestUnderwritingCoverage_MarkAired_DBError triggers the 500 error branch in
// handleMarkAired by closing the DB before the call.
func TestUnderwritingCoverage_MarkAired_DBError(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)
	closeUnderwritingDB(t, db)

	req := httptest.NewRequest("POST", "/", nil)
	req = withChiParam(req, "id", "some-spot-id")
	rr := httptest.NewRecorder()
	u.handleMarkAired(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("mark aired db error: got %d, want 500", rr.Code)
	}
}

// --- handleMarkMissed ---

// TestUnderwritingCoverage_MarkMissed_DBError triggers the 500 error branch in
// handleMarkMissed by closing the DB before the call.
func TestUnderwritingCoverage_MarkMissed_DBError(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)
	closeUnderwritingDB(t, db)

	req := httptest.NewRequest("POST", "/", nil)
	req = withChiParam(req, "id", "some-spot-id")
	rr := httptest.NewRecorder()
	u.handleMarkMissed(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("mark missed db error: got %d, want 500", rr.Code)
	}
}

// --- handleListSpots status filter branch ---

// TestUnderwritingCoverage_ListSpots_WithStatusFilter covers the status query-param
// branch (line 265-266 in underwriting.go).
func TestUnderwritingCoverage_ListSpots_WithStatusFilter(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)

	// Seed one scheduled spot and one aired spot under the same obligation.
	db.Create(&models.Sponsor{ID: "sp-ls", Name: "Sp", StationID: "s1"}) //nolint:errcheck
	db.Create(&models.UnderwritingObligation{                            //nolint:errcheck
		ID: "obl-ls", SponsorID: "sp-ls", StationID: "s1", SpotsPerWeek: 2,
	})
	db.Create(&models.UnderwritingSpot{ //nolint:errcheck
		ID:           "spot-ls-1",
		ObligationID: "obl-ls",
		ScheduledAt:  time.Now(),
		Status:       models.SpotStatusScheduled,
	})
	now := time.Now()
	db.Create(&models.UnderwritingSpot{ //nolint:errcheck
		ID:           "spot-ls-2",
		ObligationID: "obl-ls",
		ScheduledAt:  now,
		Status:       models.SpotStatusAired,
		AiredAt:      &now,
	})

	// Filter by status=aired — exercises the `if status != ""` branch.
	req := httptest.NewRequest("GET", "/?obligation_id=obl-ls&status=aired", nil)
	rr := httptest.NewRecorder()
	u.handleListSpots(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list spots with status filter: got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	spots, _ := resp["spots"].([]any)
	if len(spots) != 1 {
		t.Fatalf("expected 1 aired spot, got %d", len(spots))
	}

	// Filter by status=missed (no matching spots) — still exercises the branch.
	req = httptest.NewRequest("GET", "/?obligation_id=obl-ls&status=missed", nil)
	rr = httptest.NewRecorder()
	u.handleListSpots(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list spots with status=missed: got %d, want 200", rr.Code)
	}
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	spots, _ = resp["spots"].([]any)
	if len(spots) != 0 {
		t.Fatalf("expected 0 missed spots, got %d", len(spots))
	}
}

// --- handleScheduleSpot with instanceID ---

// TestUnderwritingCoverage_ScheduleSpot_WithInstanceID covers the non-empty
// instanceID branch (line 304-306 in underwriting.go).
func TestUnderwritingCoverage_ScheduleSpot_WithInstanceID(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)

	db.Create(&models.Sponsor{ID: "sp-si", Name: "Sp", StationID: "s1"}) //nolint:errcheck
	db.Create(&models.UnderwritingObligation{                            //nolint:errcheck
		ID: "obl-si", SponsorID: "sp-si", StationID: "s1", SpotsPerWeek: 1,
	})

	scheduledAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	body, _ := json.Marshal(map[string]any{
		"obligation_id": "obl-si",
		"scheduled_at":  scheduledAt,
		"instance_id":   "inst-abc-123",
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	u.handleScheduleSpot(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("schedule spot with instance_id: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	spotMap, _ := resp["spot"].(map[string]any)
	if spotMap["instance_id"] == nil {
		t.Fatal("expected instance_id to be set in spot response")
	}
}

// TestUnderwritingCoverage_ScheduleSpot_InvalidDate covers the bad scheduled_at
// time parse branch.
func TestUnderwritingCoverage_ScheduleSpot_InvalidDate(t *testing.T) {
	u, _ := newUnderwritingCoverageTest(t)

	body, _ := json.Marshal(map[string]any{
		"obligation_id": "obl-x",
		"scheduled_at":  "not-a-timestamp",
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	u.handleScheduleSpot(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("schedule spot invalid date: got %d, want 400", rr.Code)
	}
}

// TestUnderwritingCoverage_ScheduleSpot_DBError triggers the 500 path in
// handleScheduleSpot when the DB is closed (service.ScheduleSpot fails).
func TestUnderwritingCoverage_ScheduleSpot_DBError(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)
	closeUnderwritingDB(t, db)

	body, _ := json.Marshal(map[string]any{
		"obligation_id": "obl-x",
		"scheduled_at":  time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	u.handleScheduleSpot(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("schedule spot db error: got %d, want 500", rr.Code)
	}
}

// --- handleListSponsors error path ---

// TestUnderwritingCoverage_ListSponsors_DBError triggers the 500 path in
// handleListSponsors by closing the DB.
func TestUnderwritingCoverage_ListSponsors_DBError(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)
	closeUnderwritingDB(t, db)

	req := httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr := httptest.NewRecorder()
	u.handleListSponsors(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("list sponsors db error: got %d, want 500", rr.Code)
	}
}

// --- handleCreateSponsor error path ---

// TestUnderwritingCoverage_CreateSponsor_DBError triggers the 500 path in
// handleCreateSponsor by closing the DB before the create call.
func TestUnderwritingCoverage_CreateSponsor_DBError(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)
	closeUnderwritingDB(t, db)

	body, _ := json.Marshal(map[string]any{
		"name":       "Test Sponsor",
		"station_id": "s1",
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	u.handleCreateSponsor(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("create sponsor db error: got %d, want 500", rr.Code)
	}
}

// --- handleUpdateSponsor error path ---

// TestUnderwritingCoverage_UpdateSponsor_DBError triggers the 500 path in
// handleUpdateSponsor by closing the DB.
func TestUnderwritingCoverage_UpdateSponsor_DBError(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)
	closeUnderwritingDB(t, db)

	body := []byte(`{"name":"Updated"}`)
	req := httptest.NewRequest("PUT", "/", bytes.NewReader(body))
	req = withChiParam(req, "id", "sp-doesnt-exist")
	rr := httptest.NewRecorder()
	u.handleUpdateSponsor(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("update sponsor db error: got %d, want 500", rr.Code)
	}
}

// --- handleCreateObligation error path ---

// TestUnderwritingCoverage_CreateObligation_DBError triggers the 500 path in
// handleCreateObligation by closing the DB.
func TestUnderwritingCoverage_CreateObligation_DBError(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)
	closeUnderwritingDB(t, db)

	body, _ := json.Marshal(map[string]any{
		"sponsor_id":     "sp-x",
		"station_id":     "s1",
		"spots_per_week": 3,
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	u.handleCreateObligation(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("create obligation db error: got %d, want 500", rr.Code)
	}
}

// --- handleUpdateObligation error path ---

// TestUnderwritingCoverage_UpdateObligation_DBError triggers the 500 path in
// handleUpdateObligation by closing the DB.
func TestUnderwritingCoverage_UpdateObligation_DBError(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)
	closeUnderwritingDB(t, db)

	body := []byte(`{"spots_per_week":5}`)
	req := httptest.NewRequest("PUT", "/", bytes.NewReader(body))
	req = withChiParam(req, "id", "obl-doesnt-exist")
	rr := httptest.NewRecorder()
	u.handleUpdateObligation(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("update obligation db error: got %d, want 500", rr.Code)
	}
}

// --- handleListObligations error path ---

// TestUnderwritingCoverage_ListObligations_DBError triggers the 500 path in
// handleListObligations by closing the DB.
func TestUnderwritingCoverage_ListObligations_DBError(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)
	closeUnderwritingDB(t, db)

	req := httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr := httptest.NewRecorder()
	u.handleListObligations(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("list obligations db error: got %d, want 500", rr.Code)
	}
}

// --- handleListSpots error path ---

// TestUnderwritingCoverage_ListSpots_DBError triggers the 500 path in
// handleListSpots by closing the DB.
func TestUnderwritingCoverage_ListSpots_DBError(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)
	closeUnderwritingDB(t, db)

	req := httptest.NewRequest("GET", "/?obligation_id=obl-x", nil)
	rr := httptest.NewRecorder()
	u.handleListSpots(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("list spots db error: got %d, want 500", rr.Code)
	}
}

// --- handleFulfillmentReport date range branches ---

// TestUnderwritingCoverage_FulfillmentReport_DateParams covers the start/end query
// param parsing branches in handleFulfillmentReport.
func TestUnderwritingCoverage_FulfillmentReport_DateParams(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)

	db.Create(&models.Sponsor{ID: "sp-fr", Name: "SpFR", StationID: "s1"}) //nolint:errcheck
	db.Create(&models.UnderwritingObligation{                              //nolint:errcheck
		ID: "obl-fr", SponsorID: "sp-fr", StationID: "s1", SpotsPerWeek: 2,
	})

	// Both start and end provided — exercises both branches.
	req := httptest.NewRequest("GET", "/obl-fr?start=2025-01-01&end=2025-03-31", nil)
	req = withChiParam(req, "obligationID", "obl-fr")
	rr := httptest.NewRecorder()
	u.handleFulfillmentReport(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("fulfillment report with date params: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["report"]; !ok {
		t.Fatal("expected report key in response")
	}
}

// TestUnderwritingCoverage_FulfillmentReport_DBError triggers the 500 path in
// handleFulfillmentReport by closing the DB.
func TestUnderwritingCoverage_FulfillmentReport_DBError(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)
	closeUnderwritingDB(t, db)

	req := httptest.NewRequest("GET", "/obl-x", nil)
	req = withChiParam(req, "obligationID", "obl-x")
	rr := httptest.NewRecorder()
	u.handleFulfillmentReport(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("fulfillment report db error: got %d, want 500", rr.Code)
	}
}

// --- handleScheduleWeekly error path ---

// TestUnderwritingCoverage_ScheduleWeekly_DBError triggers the 500 path in
// handleScheduleWeekly by closing the DB.
func TestUnderwritingCoverage_ScheduleWeekly_DBError(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)
	closeUnderwritingDB(t, db)

	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"week_start": "2025-01-06",
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	u.handleScheduleWeekly(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("schedule weekly db error: got %d, want 500", rr.Code)
	}
}

// --- handleFulfillmentReports error path ---

// TestUnderwritingCoverage_FulfillmentReports_DBError triggers the 500 path in
// handleFulfillmentReports by closing the DB.
func TestUnderwritingCoverage_FulfillmentReports_DBError(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)
	closeUnderwritingDB(t, db)

	req := httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr := httptest.NewRecorder()
	u.handleFulfillmentReports(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("fulfillment reports db error: got %d, want 500", rr.Code)
	}
}

// --- handleListSponsors active=false branch ---

// TestUnderwritingCoverage_ListSponsors_ActiveFalse covers the activeOnly=false
// branch (when active query param is "false").
func TestUnderwritingCoverage_ListSponsors_ActiveFalse(t *testing.T) {
	u, db := newUnderwritingCoverageTest(t)

	// Create an inactive sponsor.
	db.Create(&models.Sponsor{ //nolint:errcheck
		ID: "sp-inactive", Name: "Inactive Sponsor", StationID: "s2", Active: false,
	})

	req := httptest.NewRequest("GET", "/?station_id=s2&active=false", nil)
	rr := httptest.NewRecorder()
	u.handleListSponsors(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list sponsors active=false: got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	sponsors, _ := resp["sponsors"].([]any)
	if len(sponsors) != 1 {
		t.Fatalf("expected 1 sponsor (including inactive), got %d", len(sponsors))
	}
}
