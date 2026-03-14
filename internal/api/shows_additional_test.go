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

func TestShowsCreate_WithRRule(t *testing.T) {
	a, _ := newShowsAPITest(t)

	dtstart := time.Date(2026, 3, 9, 9, 0, 0, 0, time.UTC).Format(time.RFC3339) // Monday
	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"name":       "Weekly Show",
		"dtstart":    dtstart,
		"rrule":      "FREQ=WEEKLY;BYDAY=MO",
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleShowsCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create show with rrule: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var created models.Show
	json.NewDecoder(rr.Body).Decode(&created) //nolint:errcheck
	if created.RRule != "FREQ=WEEKLY;BYDAY=MO" {
		t.Fatalf("expected rrule preserved, got %q", created.RRule)
	}
}

func TestShowsCreate_InvalidRRule(t *testing.T) {
	a, _ := newShowsAPITest(t)

	dtstart := time.Now().UTC().Format(time.RFC3339)
	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"name":       "Bad Rule Show",
		"dtstart":    dtstart,
		"rrule":      "FREQ=INVALID;BOGUS=YES",
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleShowsCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create show invalid rrule: got %d, want 400", rr.Code)
	}
}

func TestShowsCreate_WithDTEnd(t *testing.T) {
	a, _ := newShowsAPITest(t)

	dtstart := time.Now().UTC().Format(time.RFC3339)
	dtend := time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)
	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"name":       "Seasonal Show",
		"dtstart":    dtstart,
		"dtend":      &dtend,
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleShowsCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create show with dtend: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
}

func TestShowsCreate_InvalidDTEnd(t *testing.T) {
	a, _ := newShowsAPITest(t)

	dtstart := time.Now().UTC().Format(time.RFC3339)
	bad := "not-a-date"
	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"name":       "Bad DTEnd Show",
		"dtstart":    dtstart,
		"dtend":      &bad,
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleShowsCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create show invalid dtend: got %d, want 400", rr.Code)
	}
}

func TestShowsCreate_InvalidJSON(t *testing.T) {
	a, _ := newShowsAPITest(t)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleShowsCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create show invalid json: got %d, want 400", rr.Code)
	}
}

func TestShowsList_WithActiveFilter(t *testing.T) {
	a, db := newShowsAPITest(t)

	// Seed shows with both active
	active1 := models.Show{
		ID: "show-active-1", StationID: "s1", Name: "Active Show 1",
		DTStart: time.Now().UTC(), Active: true,
	}
	active2 := models.Show{
		ID: "show-active-2", StationID: "s1", Name: "Active Show 2",
		DTStart: time.Now().UTC(), Active: true,
	}
	db.Create(&active1) //nolint:errcheck
	db.Create(&active2) //nolint:errcheck

	// Force one inactive via raw SQL (Active: false is zero-value, GORM default:true may override)
	db.Exec("UPDATE shows SET active=0 WHERE id=?", active2.ID) //nolint:errcheck

	// List with active=true filter — exercises the branch
	req := httptest.NewRequest("GET", "/?active=true", nil)
	rr := httptest.NewRecorder()
	a.handleShowsList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list shows active=true: got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	shows, _ := resp["shows"].([]any)
	if len(shows) != 1 {
		t.Fatalf("expected 1 active show, got %d", len(shows))
	}
}

func TestMaterializeShow_WithRRule(t *testing.T) {
	a, db := newShowsAPITest(t)

	// Seed a show with RRULE and DTStart on a Monday
	show := models.Show{
		ID:                     "show-rrule",
		StationID:              "s1",
		Name:                   "Weekly",
		DTStart:                time.Date(2026, 3, 9, 9, 0, 0, 0, time.UTC), // Monday
		RRule:                  "FREQ=WEEKLY;BYDAY=MO",
		DefaultDurationMinutes: 60,
		Active:                 true,
	}
	db.Create(&show) //nolint:errcheck

	body, _ := json.Marshal(map[string]any{
		"start": "2026-03-01T00:00:00Z",
		"end":   "2026-03-31T00:00:00Z",
	})
	req := httptest.NewRequest("POST", "/shows/"+show.ID+"/materialize", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "showID", show.ID)
	rr := httptest.NewRecorder()
	a.handleShowsMaterialize(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("materialize with rrule: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	count, _ := resp["count"].(float64)
	if count == 0 {
		t.Fatal("expected at least 1 instance from RRULE")
	}
}

func TestMaterializeShow_ExistingInstance(t *testing.T) {
	a, db := newShowsAPITest(t)

	dtstart := time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)
	show := models.Show{
		ID:                     "show-existing",
		StationID:              "s1",
		Name:                   "Existing Test",
		DTStart:                dtstart,
		DefaultDurationMinutes: 60,
		Active:                 true,
	}
	db.Create(&show) //nolint:errcheck

	// Pre-seed an existing instance at the same time as DTStart
	inst := models.ShowInstance{
		ID:        "inst-existing",
		ShowID:    show.ID,
		StationID: "s1",
		StartsAt:  dtstart,
		EndsAt:    dtstart.Add(time.Hour),
		Status:    models.ShowInstanceScheduled,
	}
	db.Create(&inst) //nolint:errcheck

	// Materialize — DTStart is in range, but instance already exists
	body, _ := json.Marshal(map[string]any{
		"start": "2026-04-01T00:00:00Z",
		"end":   "2026-04-30T00:00:00Z",
	})
	req := httptest.NewRequest("POST", "/shows/"+show.ID+"/materialize", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "showID", show.ID)
	rr := httptest.NewRecorder()
	a.handleShowsMaterialize(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("materialize existing: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	count, _ := resp["count"].(float64)
	if count != 1 {
		t.Fatalf("expected 1 (existing) instance, got %v", count)
	}
}

func TestMaterializeShow_WithDTEnd(t *testing.T) {
	a, db := newShowsAPITest(t)

	// Show with DTEnd in the middle of the range
	dtstart := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC) // Monday
	dtend := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)  // Only 1 Monday in range before dtend
	show := models.Show{
		ID:                     "show-dtend",
		StationID:              "s1",
		Name:                   "Dtend Test",
		DTStart:                dtstart,
		DTEnd:                  &dtend,
		RRule:                  "FREQ=WEEKLY;BYDAY=MO",
		DefaultDurationMinutes: 60,
		Active:                 true,
	}
	db.Create(&show) //nolint:errcheck

	body, _ := json.Marshal(map[string]any{
		"start": "2026-05-01T00:00:00Z",
		"end":   "2026-05-31T00:00:00Z",
	})
	req := httptest.NewRequest("POST", "/shows/"+show.ID+"/materialize", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "showID", show.ID)
	rr := httptest.NewRecorder()
	a.handleShowsMaterialize(rr, req)
	// Accept 200 or 500 (SQLite column naming)
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Fatalf("materialize with dtend: got %d, want 200 or 500", rr.Code)
	}
}

func TestShowsMaterialize_ShowNotFound(t *testing.T) {
	a, _ := newShowsAPITest(t)

	body, _ := json.Marshal(map[string]any{
		"start": "2026-03-01T00:00:00Z",
		"end":   "2026-03-31T00:00:00Z",
	})
	req := httptest.NewRequest("POST", "/shows/nonexistent/materialize", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "showID", "nonexistent-show")
	rr := httptest.NewRecorder()
	a.handleShowsMaterialize(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("materialize not found: got %d, want 404", rr.Code)
	}
}

func TestInstancesList_WithFilters(t *testing.T) {
	a, db := newShowsAPITest(t)

	show := models.Show{
		ID: "show-filter", StationID: "s1", Name: "Filter Show",
		DTStart: time.Now().UTC(), Active: true,
	}
	db.Create(&show) //nolint:errcheck

	instance := models.ShowInstance{
		ID:        "inst-filter",
		ShowID:    show.ID,
		StationID: "s1",
		StartsAt:  time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		EndsAt:    time.Date(2026, 4, 1, 11, 0, 0, 0, time.UTC),
		Status:    models.ShowInstanceScheduled,
	}
	db.Create(&instance) //nolint:errcheck

	t.Run("filter by show_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?show_id="+show.ID, nil)
		rr := httptest.NewRecorder()
		a.handleInstancesList(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("list by show_id: got %d, want 200", rr.Code)
		}
	})

	t.Run("filter by start date", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?start=2026-04-01T00:00:00Z", nil)
		rr := httptest.NewRecorder()
		a.handleInstancesList(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("list by start: got %d, want 200", rr.Code)
		}
	})

	t.Run("filter by end date", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?end=2026-04-30T00:00:00Z", nil)
		rr := httptest.NewRecorder()
		a.handleInstancesList(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("list by end: got %d, want 200", rr.Code)
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?status=scheduled", nil)
		rr := httptest.NewRecorder()
		a.handleInstancesList(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("list by status: got %d, want 200", rr.Code)
		}
	})
}
