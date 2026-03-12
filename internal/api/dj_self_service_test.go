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
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newDJSelfServiceAPITest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.DJAvailability{},
		&models.ScheduleRequest{},
		&models.ScheduleLock{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{db: db, logger: zerolog.Nop()}, db
}

// withRequestID injects a requestID chi route param into the request context.
func withRequestID(req *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("requestID", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func djClaims(userID string, roles ...string) *auth.Claims {
	return &auth.Claims{UserID: userID, Roles: roles}
}

func TestDJSelfService_Availability(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)

	userClaims := djClaims("u-dj1")

	// Create availability
	dow := 1 // Monday
	body, _ := json.Marshal(map[string]any{
		"day_of_week": dow,
		"start_time":  "19:00",
		"end_time":    "22:00",
		"available":   true,
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = req.WithContext(auth.WithClaims(req.Context(), userClaims))
	rr := httptest.NewRecorder()
	a.handleCreateAvailability(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create availability: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var created models.DJAvailability
	json.NewDecoder(rr.Body).Decode(&created) //nolint:errcheck
	if created.ID == "" {
		t.Fatal("expected availability id in response")
	}

	// Get my availability
	req = httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), userClaims))
	rr = httptest.NewRecorder()
	a.handleGetMyAvailability(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get availability: got %d, want 200", rr.Code)
	}
	var listResp map[string]any
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	avail, _ := listResp["availability"].([]any)
	if len(avail) != 1 {
		t.Fatalf("expected 1 availability entry, got %d", len(avail))
	}

	// Update availability
	body, _ = json.Marshal(map[string]any{
		"day_of_week": dow,
		"start_time":  "20:00",
		"end_time":    "23:00",
		"available":   true,
	})
	req = httptest.NewRequest("PUT", "/"+created.ID, bytes.NewReader(body))
	req = req.WithContext(auth.WithClaims(req.Context(), userClaims))
	req = withAPIRouteID(req, created.ID)
	rr = httptest.NewRecorder()
	a.handleUpdateAvailability(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update availability: got %d, want 200", rr.Code)
	}

	// Delete availability
	req = httptest.NewRequest("DELETE", "/"+created.ID, nil)
	req = req.WithContext(auth.WithClaims(req.Context(), userClaims))
	req = withAPIRouteID(req, created.ID)
	rr = httptest.NewRecorder()
	a.handleDeleteAvailability(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete availability: got %d, want 200", rr.Code)
	}
}

func TestDJSelfService_ScheduleRequests(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)

	djClm := djClaims("u-dj2")
	adminClm := djClaims("u-admin", string(models.RoleAdmin))

	// List requests (requires station_id)
	req := httptest.NewRequest("GET", "/?station_id=s1", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), djClm))
	rr := httptest.NewRecorder()
	a.handleListScheduleRequests(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list schedule requests: got %d, want 200", rr.Code)
	}
	var listResp map[string]any
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	requests, _ := listResp["requests"].([]any)
	if len(requests) != 0 {
		t.Fatalf("expected 0 requests, got %d", len(requests))
	}

	// Create a schedule request
	body, _ := json.Marshal(map[string]any{
		"station_id":   "s1",
		"request_type": "time_off",
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = req.WithContext(auth.WithClaims(req.Context(), djClm))
	rr = httptest.NewRecorder()
	a.handleCreateScheduleRequest(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create schedule request: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var created models.ScheduleRequest
	json.NewDecoder(rr.Body).Decode(&created) //nolint:errcheck
	if created.ID == "" {
		t.Fatal("expected request id in response")
	}

	// Get the request as DJ (own request)
	req = httptest.NewRequest("GET", "/"+created.ID, nil)
	req = req.WithContext(auth.WithClaims(req.Context(), djClm))
	req = withRequestID(req, created.ID)
	rr = httptest.NewRecorder()
	a.handleGetScheduleRequest(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get schedule request: got %d, want 200", rr.Code)
	}

	// Cancel (requester cancels own)
	req = httptest.NewRequest("DELETE", "/"+created.ID, nil)
	req = req.WithContext(auth.WithClaims(req.Context(), djClm))
	req = withRequestID(req, created.ID)
	rr = httptest.NewRecorder()
	a.handleCancelScheduleRequest(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("cancel schedule request: got %d, want 200", rr.Code)
	}

	// Create another request to approve
	body, _ = json.Marshal(map[string]any{
		"station_id":   "s1",
		"request_type": "new_show",
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = req.WithContext(auth.WithClaims(req.Context(), djClm))
	rr = httptest.NewRecorder()
	a.handleCreateScheduleRequest(rr, req)
	var req2 models.ScheduleRequest
	json.NewDecoder(rr.Body).Decode(&req2) //nolint:errcheck

	req = httptest.NewRequest("PUT", "/"+req2.ID+"/approve", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), adminClm))
	req = withRequestID(req, req2.ID)
	rr = httptest.NewRecorder()
	a.handleApproveScheduleRequest(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("approve schedule request: got %d, want 200", rr.Code)
	}

	// Create another request to reject
	body, _ = json.Marshal(map[string]any{
		"station_id":   "s1",
		"request_type": "swap",
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = req.WithContext(auth.WithClaims(req.Context(), djClm))
	rr = httptest.NewRecorder()
	a.handleCreateScheduleRequest(rr, req)
	var req3 models.ScheduleRequest
	json.NewDecoder(rr.Body).Decode(&req3) //nolint:errcheck

	req = httptest.NewRequest("PUT", "/"+req3.ID+"/reject", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), adminClm))
	req = withRequestID(req, req3.ID)
	rr = httptest.NewRecorder()
	a.handleRejectScheduleRequest(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("reject schedule request: got %d, want 200", rr.Code)
	}
}

func TestDJSelfService_ScheduleLock(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)

	// Get lock when none exists — returns 200 with defaults
	req := httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr := httptest.NewRecorder()
	a.handleGetScheduleLock(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get default lock: got %d, want 200", rr.Code)
	}
	var lockResp models.ScheduleLock
	json.NewDecoder(rr.Body).Decode(&lockResp) //nolint:errcheck
	if lockResp.LockBeforeDays != 7 {
		t.Fatalf("expected default LockBeforeDays=7, got %d", lockResp.LockBeforeDays)
	}

	// Upsert (create) a lock
	body, _ := json.Marshal(map[string]any{
		"station_id":       "s1",
		"lock_before_days": 14,
		"min_bypass_role":  "admin",
	})
	req = httptest.NewRequest("PUT", "/", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	a.handleUpdateScheduleLock(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("upsert lock: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var created models.ScheduleLock
	json.NewDecoder(rr.Body).Decode(&created) //nolint:errcheck
	if created.LockBeforeDays != 14 {
		t.Fatalf("expected LockBeforeDays=14, got %d", created.LockBeforeDays)
	}

	// Get the created lock
	req = httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr = httptest.NewRecorder()
	a.handleGetScheduleLock(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get lock: got %d, want 200", rr.Code)
	}
	var got models.ScheduleLock
	json.NewDecoder(rr.Body).Decode(&got) //nolint:errcheck
	if got.LockBeforeDays != 14 {
		t.Fatalf("expected persisted LockBeforeDays=14, got %d", got.LockBeforeDays)
	}
}

func TestDJSelfService_Errors(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)

	t.Run("create availability without auth", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"day_of_week": 1, "start_time": "10:00", "end_time": "11:00"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		// No claims
		rr := httptest.NewRecorder()
		a.handleCreateAvailability(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("create availability missing times", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"day_of_week": 1})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = req.WithContext(auth.WithClaims(req.Context(), djClaims("u1")))
		rr := httptest.NewRecorder()
		a.handleCreateAvailability(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("list schedule requests requires station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), djClaims("u1")))
		rr := httptest.NewRecorder()
		a.handleListScheduleRequests(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("get schedule lock requires station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		a.handleGetScheduleLock(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})
}
