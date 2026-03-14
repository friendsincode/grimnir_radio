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
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestUnderwritingAPI_UpdateSponsor_InvalidJSON(t *testing.T) {
	u, _ := newUnderwritingAPITest(t)

	req := httptest.NewRequest("PUT", "/", bytes.NewReader([]byte("{")))
	req = withAPIRouteID(req, "some-id")
	rr := httptest.NewRecorder()
	u.handleUpdateSponsor(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("update sponsor invalid json: got %d, want 400", rr.Code)
	}
}

func TestUnderwritingAPI_UpdateObligation_InvalidJSON(t *testing.T) {
	u, _ := newUnderwritingAPITest(t)

	req := httptest.NewRequest("PUT", "/", bytes.NewReader([]byte("{")))
	req = withAPIRouteID(req, "some-id")
	rr := httptest.NewRecorder()
	u.handleUpdateObligation(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("update obligation invalid json: got %d, want 400", rr.Code)
	}
}

func TestUnderwritingAPI_CreateObligation_InvalidJSON(t *testing.T) {
	u, _ := newUnderwritingAPITest(t)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	rr := httptest.NewRecorder()
	u.handleCreateObligation(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create obligation invalid json: got %d, want 400", rr.Code)
	}
}

func TestUnderwritingAPI_CreateObligation_MissingFields(t *testing.T) {
	u, _ := newUnderwritingAPITest(t)

	t.Run("missing sponsor_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"station_id": "s1", "spots_per_week": 3})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		u.handleCreateObligation(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing spots_per_week", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"sponsor_id": "sp1", "station_id": "s1"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		u.handleCreateObligation(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}

func TestUnderwritingAPI_GetObligation_NotFound(t *testing.T) {
	u, _ := newUnderwritingAPITest(t)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	req = withAPIRouteID(req, "nonexistent-id")
	rr := httptest.NewRecorder()
	u.handleGetObligation(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get obligation not found: got %d, want 404", rr.Code)
	}
}

func TestUnderwritingAPI_ScheduleSpot_InvalidJSON(t *testing.T) {
	u, _ := newUnderwritingAPITest(t)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	rr := httptest.NewRecorder()
	u.handleScheduleSpot(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("schedule spot invalid json: got %d, want 400", rr.Code)
	}
}

func TestUnderwritingAPI_ListObligations_WithSponsorID(t *testing.T) {
	u, db := newUnderwritingAPITest(t)

	sponsor := models.NewSponsor("s1", "Test Sponsor")
	db.Create(sponsor) //nolint:errcheck

	obl := models.NewUnderwritingObligation(sponsor.ID, "s1", 5)
	db.Create(obl) //nolint:errcheck

	// Filter by sponsor_id (no station_id = all obligations for that sponsor)
	req := httptest.NewRequest("GET", "/?sponsor_id="+sponsor.ID, nil)
	rr := httptest.NewRecorder()
	u.handleListObligations(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list by sponsor_id: got %d, want 200", rr.Code)
	}
}

func TestUnderwritingAPI_ScheduleWeekly_BadDate(t *testing.T) {
	u, _ := newUnderwritingAPITest(t)

	// Invalid week_start is ignored, service uses time.Now()
	body, _ := json.Marshal(map[string]any{"station_id": "s1", "week_start": "not-a-date"})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	u.handleScheduleWeekly(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("schedule weekly bad date: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
}

func TestUnderwritingAPI_ScheduleWeekly_InvalidJSON(t *testing.T) {
	u, _ := newUnderwritingAPITest(t)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	rr := httptest.NewRecorder()
	u.handleScheduleWeekly(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("schedule weekly invalid json: got %d, want 400", rr.Code)
	}
}

func TestUnderwritingAPI_FulfillmentReports_BadDates(t *testing.T) {
	u, _ := newUnderwritingAPITest(t)

	// Invalid date range params are ignored, defaults used
	req := httptest.NewRequest("GET", "/?station_id=s1&start=bad&end=bad", nil)
	rr := httptest.NewRecorder()
	u.handleFulfillmentReports(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("fulfillment reports bad dates: got %d, want 200", rr.Code)
	}
}

func TestUnderwritingAPI_CreateSponsor_InvalidJSON(t *testing.T) {
	u, _ := newUnderwritingAPITest(t)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	rr := httptest.NewRecorder()
	u.handleCreateSponsor(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create sponsor invalid json: got %d, want 400", rr.Code)
	}
}

func TestUnderwritingAPI_FulfillmentReport_WithObligation(t *testing.T) {
	u, db := newUnderwritingAPITest(t)

	sponsor := models.NewSponsor("s1", "Sponsor X")
	db.Create(sponsor) //nolint:errcheck

	obl := models.NewUnderwritingObligation(sponsor.ID, "s1", 2)
	db.Create(obl) //nolint:errcheck

	// Schedule a spot within the default report period
	spot := models.NewUnderwritingSpot(obl.ID, time.Now().Add(-24*time.Hour))
	db.Create(spot) //nolint:errcheck

	req := httptest.NewRequest("GET", "/"+obl.ID, nil)
	req = withChiParam(req, "obligationID", obl.ID)
	rr := httptest.NewRecorder()
	u.handleFulfillmentReport(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("fulfillment report with obligation: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
}
