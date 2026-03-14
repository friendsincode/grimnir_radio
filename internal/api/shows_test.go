/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// withChiParam injects an arbitrary chi route param into the request context.
func withChiParam(req *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// withAdminClaims attaches platform admin claims to the request context.
func withAdminClaims(req *http.Request) *http.Request {
	return req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u-admin",
		Roles:  []string{string(models.PlatformRoleAdmin)},
	}))
}

func newShowsAPITest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Show{}, &models.ShowInstance{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{db: db, logger: zerolog.Nop()}, db
}

func TestShowsAPI_Shows(t *testing.T) {
	a, _ := newShowsAPITest(t)

	// List shows (no station filter — no auth check)
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleShowsList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list shows: got %d, want 200", rr.Code)
	}
	var listResp map[string]any
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	shows, _ := listResp["shows"].([]any)
	if len(shows) != 0 {
		t.Fatalf("expected 0 shows, got %d", len(shows))
	}

	// Create a show
	dtstart := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"name":       "Morning Drive",
		"dtstart":    dtstart,
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr = httptest.NewRecorder()
	a.handleShowsCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create show: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var created models.Show
	json.NewDecoder(rr.Body).Decode(&created) //nolint:errcheck
	if created.ID == "" {
		t.Fatal("expected show id in response")
	}

	// Get the show
	req = httptest.NewRequest("GET", "/"+created.ID, nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "showID", created.ID)
	rr = httptest.NewRecorder()
	a.handleShowsGet(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get show: got %d, want 200", rr.Code)
	}

	// Update the show
	newName := "Afternoon Drive"
	body, _ = json.Marshal(map[string]any{"name": &newName})
	req = httptest.NewRequest("PUT", "/"+created.ID, bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "showID", created.ID)
	rr = httptest.NewRecorder()
	a.handleShowsUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update show: got %d, want 200", rr.Code)
	}

	// List with station filter (admin bypass)
	req = httptest.NewRequest("GET", "/?station_id=s1", nil)
	req = withAdminClaims(req)
	rr = httptest.NewRecorder()
	a.handleShowsList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list shows with station: got %d, want 200", rr.Code)
	}
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	shows, _ = listResp["shows"].([]any)
	if len(shows) != 1 {
		t.Fatalf("expected 1 show, got %d", len(shows))
	}

	// Delete the show
	req = httptest.NewRequest("DELETE", "/"+created.ID, nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "showID", created.ID)
	rr = httptest.NewRecorder()
	a.handleShowsDelete(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete show: got %d, want 200", rr.Code)
	}

	// Get after delete → 404
	req = httptest.NewRequest("GET", "/"+created.ID, nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "showID", created.ID)
	rr = httptest.NewRecorder()
	a.handleShowsGet(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get deleted show: got %d, want 404", rr.Code)
	}
}

func TestShowsAPI_Instances(t *testing.T) {
	a, db := newShowsAPITest(t)

	// Seed a show and an instance
	dtstart := time.Now().Add(2 * time.Hour).UTC()
	show := models.Show{
		ID:                     "show-1",
		StationID:              "s1",
		Name:                   "Jazz Hour",
		DTStart:                dtstart,
		DefaultDurationMinutes: 60,
		Timezone:               "UTC",
		Active:                 true,
	}
	db.Create(&show) //nolint:errcheck

	instance := models.ShowInstance{
		ID:        "inst-1",
		ShowID:    show.ID,
		StationID: show.StationID,
		StartsAt:  dtstart,
		EndsAt:    dtstart.Add(time.Hour),
		Status:    models.ShowInstanceScheduled,
	}
	db.Create(&instance) //nolint:errcheck

	// List instances (default date range is next 7 days from now; our instance is ~2h ahead)
	startStr := time.Now().UTC().Format(time.RFC3339)
	endStr := time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	req := httptest.NewRequest("GET", "/?station_id=s1&start="+startStr+"&end="+endStr, nil)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	a.handleInstancesList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list instances: got %d, want 200", rr.Code)
	}
	var listResp map[string]any
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	instances, _ := listResp["instances"].([]any)
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}

	// Get instance
	req = httptest.NewRequest("GET", "/inst-1", nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "instanceID", "inst-1")
	rr = httptest.NewRecorder()
	a.handleInstancesGet(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get instance: got %d, want 200", rr.Code)
	}

	// Update instance (change status)
	status := models.ShowInstanceCompleted
	body, _ := json.Marshal(map[string]any{"status": status})
	req = httptest.NewRequest("PUT", "/inst-1", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "instanceID", "inst-1")
	rr = httptest.NewRecorder()
	a.handleInstancesUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update instance: got %d, want 200", rr.Code)
	}

	// Cancel (delete) instance
	req = httptest.NewRequest("DELETE", "/inst-1", nil)
	req = withAdminClaims(req)
	req = withChiParam(req, "instanceID", "inst-1")
	rr = httptest.NewRecorder()
	a.handleInstancesDelete(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("cancel instance: got %d, want 200", rr.Code)
	}

	// Verify instance is cancelled
	var got models.ShowInstance
	db.First(&got, "id = ?", "inst-1")
	if got.Status != models.ShowInstanceCancelled {
		t.Fatalf("expected cancelled status, got %s", got.Status)
	}
}

func TestShowsAPI_Materialize(t *testing.T) {
	a, db := newShowsAPITest(t)

	// Create a weekly recurring show
	dtstart := time.Date(2026, 3, 9, 19, 0, 0, 0, time.UTC) // Monday
	show := models.Show{
		ID:                     "show-rrule",
		StationID:              "s1",
		Name:                   "Weekly Show",
		RRule:                  "FREQ=WEEKLY;BYDAY=MO",
		DTStart:                dtstart,
		DefaultDurationMinutes: 60,
		Timezone:               "UTC",
		Active:                 true,
	}
	db.Create(&show) //nolint:errcheck

	// Materialize for 2 weeks
	start := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)
	body, _ := json.Marshal(map[string]any{
		"start": start.Format(time.RFC3339),
		"end":   end.Format(time.RFC3339),
	})
	req := httptest.NewRequest("POST", "/show-rrule/materialize", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "showID", "show-rrule")
	rr := httptest.NewRecorder()
	a.handleShowsMaterialize(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("materialize: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	count, _ := resp["count"].(float64)
	if count < 1 {
		t.Fatalf("expected at least 1 instance materialized, got %v", count)
	}
}

func TestShowsAPI_Errors(t *testing.T) {
	a, _ := newShowsAPITest(t)

	t.Run("create show missing station_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "Test"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withAdminClaims(req)
		rr := httptest.NewRecorder()
		a.handleShowsCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("create show invalid dtstart", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"station_id": "s1",
			"name":       "Test",
			"dtstart":    "not-a-date",
		})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withAdminClaims(req)
		rr := httptest.NewRecorder()
		a.handleShowsCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("get non-existent show", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/missing", nil)
		req = withAdminClaims(req)
		req = withChiParam(req, "showID", "nonexistent")
		rr := httptest.NewRecorder()
		a.handleShowsGet(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("shows create requires auth", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"station_id": "s1",
			"name":       "Test",
			"dtstart":    time.Now().UTC().Format(time.RFC3339),
		})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		// No claims
		rr := httptest.NewRecorder()
		a.handleShowsCreate(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rr.Code)
		}
	})
}
