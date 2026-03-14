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
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/underwriting"
)

func newUnderwritingAPITest(t *testing.T) (*UnderwritingAPI, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
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

func TestUnderwritingAPI_Sponsors(t *testing.T) {
	u, _ := newUnderwritingAPITest(t)

	// List sponsors when empty
	req := httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr := httptest.NewRecorder()
	u.handleListSponsors(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list empty sponsors: got %d, want 200", rr.Code)
	}
	var listResp map[string]any
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	sponsors, _ := listResp["sponsors"].([]any)
	if len(sponsors) != 0 {
		t.Fatalf("expected 0 sponsors, got %d", len(sponsors))
	}

	// Create a sponsor
	body, _ := json.Marshal(map[string]any{
		"name":       "Test Sponsor",
		"station_id": "s1",
		"active":     true,
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	u.handleCreateSponsor(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create sponsor: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var createResp map[string]any
	json.NewDecoder(rr.Body).Decode(&createResp) //nolint:errcheck
	sponsorMap, _ := createResp["sponsor"].(map[string]any)
	sponsorID, _ := sponsorMap["id"].(string)
	if sponsorID == "" {
		t.Fatal("expected sponsor id in response")
	}

	// Get the sponsor
	req = httptest.NewRequest("GET", "/"+sponsorID, nil)
	req = withAPIRouteID(req, sponsorID)
	rr = httptest.NewRecorder()
	u.handleGetSponsor(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get sponsor: got %d, want 200", rr.Code)
	}

	// Update the sponsor
	body, _ = json.Marshal(map[string]any{"name": "Updated Sponsor"})
	req = httptest.NewRequest("PUT", "/"+sponsorID, bytes.NewReader(body))
	req = withAPIRouteID(req, sponsorID)
	rr = httptest.NewRecorder()
	u.handleUpdateSponsor(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update sponsor: got %d, want 200", rr.Code)
	}

	// Delete the sponsor
	req = httptest.NewRequest("DELETE", "/"+sponsorID, nil)
	req = withAPIRouteID(req, sponsorID)
	rr = httptest.NewRecorder()
	u.handleDeleteSponsor(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete sponsor: got %d, want 200", rr.Code)
	}

	// Get after delete should return 404
	req = httptest.NewRequest("GET", "/"+sponsorID, nil)
	req = withAPIRouteID(req, sponsorID)
	rr = httptest.NewRecorder()
	u.handleGetSponsor(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get deleted sponsor: got %d, want 404", rr.Code)
	}
}

func TestUnderwritingAPI_Obligations(t *testing.T) {
	u, db := newUnderwritingAPITest(t)

	// Seed a sponsor
	sponsor := models.NewSponsor("s1", "Seed Sponsor")
	if err := db.Create(sponsor).Error; err != nil {
		t.Fatalf("seed sponsor: %v", err)
	}

	// List obligations (empty)
	req := httptest.NewRequest("GET", "/?station_id=s1&active=false", nil)
	rr := httptest.NewRecorder()
	u.handleListObligations(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list obligations: got %d, want 200", rr.Code)
	}

	// Create an obligation
	body, _ := json.Marshal(map[string]any{
		"sponsor_id":     sponsor.ID,
		"station_id":     "s1",
		"spots_per_week": 5,
		"start_date":     time.Now().Format(time.RFC3339),
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	u.handleCreateObligation(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create obligation: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var createResp map[string]any
	json.NewDecoder(rr.Body).Decode(&createResp) //nolint:errcheck
	oblMap, _ := createResp["obligation"].(map[string]any)
	oblID, _ := oblMap["id"].(string)
	if oblID == "" {
		t.Fatal("expected obligation id in response")
	}

	// Get the obligation
	req = httptest.NewRequest("GET", "/"+oblID, nil)
	req = withAPIRouteID(req, oblID)
	rr = httptest.NewRecorder()
	u.handleGetObligation(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get obligation: got %d, want 200", rr.Code)
	}

	// Update obligation
	body, _ = json.Marshal(map[string]any{"spots_per_week": float64(10)})
	req = httptest.NewRequest("PUT", "/"+oblID, bytes.NewReader(body))
	req = withAPIRouteID(req, oblID)
	rr = httptest.NewRecorder()
	u.handleUpdateObligation(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update obligation: got %d, want 200", rr.Code)
	}

	// Delete obligation
	req = httptest.NewRequest("DELETE", "/"+oblID, nil)
	req = withAPIRouteID(req, oblID)
	rr = httptest.NewRecorder()
	u.handleDeleteObligation(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete obligation: got %d, want 200", rr.Code)
	}
}

func TestUnderwritingAPI_Spots(t *testing.T) {
	u, db := newUnderwritingAPITest(t)

	// Seed sponsor + obligation
	sponsor := models.NewSponsor("s1", "Sponsor")
	db.Create(sponsor) //nolint:errcheck
	obl := models.NewUnderwritingObligation(sponsor.ID, "s1", 3)
	db.Create(obl) //nolint:errcheck

	// List spots (empty, obligation_id required)
	req := httptest.NewRequest("GET", "/?obligation_id="+obl.ID, nil)
	rr := httptest.NewRecorder()
	u.handleListSpots(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list spots: got %d, want 200", rr.Code)
	}

	// Schedule a spot
	scheduledAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	body, _ := json.Marshal(map[string]any{
		"obligation_id": obl.ID,
		"scheduled_at":  scheduledAt,
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	u.handleScheduleSpot(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("schedule spot: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var spotResp map[string]any
	json.NewDecoder(rr.Body).Decode(&spotResp) //nolint:errcheck
	spotMap, _ := spotResp["spot"].(map[string]any)
	spotID, _ := spotMap["id"].(string)
	if spotID == "" {
		t.Fatal("expected spot id in response")
	}

	// Mark spot as aired
	req = httptest.NewRequest("POST", "/"+spotID+"/aired", nil)
	req = withAPIRouteID(req, spotID)
	rr = httptest.NewRecorder()
	u.handleMarkAired(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("mark aired: got %d, want 200", rr.Code)
	}

	// Schedule another spot to mark as missed
	body, _ = json.Marshal(map[string]any{
		"obligation_id": obl.ID,
		"scheduled_at":  scheduledAt,
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	u.handleScheduleSpot(rr, req)
	json.NewDecoder(rr.Body).Decode(&spotResp) //nolint:errcheck
	spotMap, _ = spotResp["spot"].(map[string]any)
	spot2ID, _ := spotMap["id"].(string)

	req = httptest.NewRequest("POST", "/"+spot2ID+"/missed", nil)
	req = withAPIRouteID(req, spot2ID)
	rr = httptest.NewRecorder()
	u.handleMarkMissed(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("mark missed: got %d, want 200", rr.Code)
	}
}

func TestUnderwritingAPI_Errors(t *testing.T) {
	u, _ := newUnderwritingAPITest(t)

	t.Run("list sponsors missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		u.handleListSponsors(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("create sponsor missing fields", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "No Station"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		u.handleCreateSponsor(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("get non-existent sponsor", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/missing", nil)
		req = withAPIRouteID(req, "missing-id")
		rr := httptest.NewRecorder()
		u.handleGetSponsor(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("list spots missing obligation_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		u.handleListSpots(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("schedule spot missing obligation_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"scheduled_at": time.Now().Format(time.RFC3339)})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		u.handleScheduleSpot(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})
}

func TestUnderwritingAPI_ScheduleWeekly(t *testing.T) {
	u, _ := newUnderwritingAPITest(t)

	// Missing station_id → 400
	body, _ := json.Marshal(map[string]any{})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	u.handleScheduleWeekly(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}

	// Valid with no obligations → 200, 0 spots
	body, _ = json.Marshal(map[string]any{"station_id": "s1", "week_start": "2025-01-06"})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	u.handleScheduleWeekly(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("schedule weekly: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["spots_scheduled"]; !ok {
		t.Fatal("expected spots_scheduled key")
	}

	// Valid without week_start → uses time.Now()
	body, _ = json.Marshal(map[string]any{"station_id": "s1"})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	u.handleScheduleWeekly(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("schedule weekly default date: got %d, want 200", rr.Code)
	}
}

func TestUnderwritingAPI_FulfillmentReports(t *testing.T) {
	u, _ := newUnderwritingAPITest(t)

	// Missing station_id → 400
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	u.handleFulfillmentReports(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}

	// With station_id → 200 (empty reports)
	req = httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr = httptest.NewRecorder()
	u.handleFulfillmentReports(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("fulfillment reports: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["reports"]; !ok {
		t.Fatal("expected reports key")
	}

	// With custom date range
	req = httptest.NewRequest("GET", "/?station_id=s1&start=2025-01-01&end=2025-01-31", nil)
	rr = httptest.NewRecorder()
	u.handleFulfillmentReports(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("fulfillment reports with range: got %d, want 200", rr.Code)
	}
}

func TestUnderwritingAPI_FulfillmentReport(t *testing.T) {
	u, _ := newUnderwritingAPITest(t)

	// Non-existent obligation → 200 with empty report (service doesn't error on missing)
	req := httptest.NewRequest("GET", "/nonexistent", nil)
	req = withChiParam(req, "obligationID", "nonexistent-id")
	rr := httptest.NewRecorder()
	u.handleFulfillmentReport(rr, req)
	// The service returns a report struct even for non-existent obligations
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Fatalf("fulfillment report: got %d, want 200 or 500", rr.Code)
	}
}
