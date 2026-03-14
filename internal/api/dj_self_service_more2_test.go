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

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func withDJUserClaims(req *http.Request, userID string, roles ...string) *http.Request {
	return req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: userID, Roles: roles}))
}

func TestDJSelfService_CreateAvailability_ErrorPaths(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)

	t.Run("unauthorized", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"start_time": "09:00", "end_time": "17:00"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
		rr := httptest.NewRecorder()
		a.handleCreateAvailability(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", rr.Code)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
		req = withDJUserClaims(req, "u1")
		rr := httptest.NewRecorder()
		a.handleCreateAvailability(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing start_time or end_time", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"start_time": "09:00"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u1")
		rr := httptest.NewRecorder()
		a.handleCreateAvailability(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing day_of_week and specific_date", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"start_time": "09:00", "end_time": "17:00"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u1")
		rr := httptest.NewRecorder()
		a.handleCreateAvailability(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("invalid specific_date", func(t *testing.T) {
		bad := "not-a-date"
		b, _ := json.Marshal(map[string]any{
			"start_time":    "09:00",
			"end_time":      "17:00",
			"specific_date": &bad,
		})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u1")
		rr := httptest.NewRecorder()
		a.handleCreateAvailability(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}

func TestDJSelfService_CreateAvailability_Success(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)

	dow := 1
	b, _ := json.Marshal(map[string]any{
		"start_time":  "09:00",
		"end_time":    "17:00",
		"day_of_week": &dow,
		"available":   true,
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
	req = withDJUserClaims(req, "u1")
	rr := httptest.NewRecorder()
	a.handleCreateAvailability(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create availability: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
}

func TestDJSelfService_CreateAvailability_WithSpecificDate(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)

	date := "2026-03-15"
	b, _ := json.Marshal(map[string]any{
		"start_time":    "10:00",
		"end_time":      "18:00",
		"specific_date": &date,
		"available":     true,
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
	req = withDJUserClaims(req, "u1")
	rr := httptest.NewRecorder()
	a.handleCreateAvailability(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create availability with specific_date: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
}

func TestDJSelfService_UpdateAvailability_ErrorPaths(t *testing.T) {
	a, db := newDJSelfServiceAPITest(t)

	// Create an availability owned by u1
	dow := 2
	createReq, _ := json.Marshal(map[string]any{
		"start_time":  "08:00",
		"end_time":    "16:00",
		"day_of_week": &dow,
		"available":   true,
	})
	cr := httptest.NewRequest("POST", "/", bytes.NewReader(createReq))
	cr = withDJUserClaims(cr, "u1")
	rr := httptest.NewRecorder()
	a.handleCreateAvailability(rr, cr)
	if rr.Code != http.StatusCreated {
		t.Fatalf("seed: got %d, want 201", rr.Code)
	}
	_ = db
	var avail models.DJAvailability
	json.NewDecoder(rr.Body).Decode(&avail) //nolint:errcheck

	t.Run("unauthorized", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"start_time": "10:00"})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(b))
		req = withChiParam(req, "id", avail.ID)
		rr := httptest.NewRecorder()
		a.handleUpdateAvailability(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", rr.Code)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"start_time": "10:00"})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u1")
		rr := httptest.NewRecorder()
		a.handleUpdateAvailability(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("not found", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"start_time": "10:00"})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u1")
		req = withChiParam(req, "id", "nonexistent")
		rr := httptest.NewRecorder()
		a.handleUpdateAvailability(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})

	t.Run("forbidden — wrong user", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"start_time": "10:00"})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u-other")
		req = withChiParam(req, "id", avail.ID)
		rr := httptest.NewRecorder()
		a.handleUpdateAvailability(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("got %d, want 403", rr.Code)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/", bytes.NewReader([]byte("{")))
		req = withDJUserClaims(req, "u1")
		req = withChiParam(req, "id", avail.ID)
		rr := httptest.NewRecorder()
		a.handleUpdateAvailability(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("success", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"start_time": "09:30", "end_time": "17:30"})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u1")
		req = withChiParam(req, "id", avail.ID)
		rr := httptest.NewRecorder()
		a.handleUpdateAvailability(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestDJSelfService_DeleteAvailability_ErrorPaths(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)

	// Create one first
	dow := 3
	createReq, _ := json.Marshal(map[string]any{
		"start_time":  "07:00",
		"end_time":    "15:00",
		"day_of_week": &dow,
		"available":   true,
	})
	cr := httptest.NewRequest("POST", "/", bytes.NewReader(createReq))
	cr = withDJUserClaims(cr, "u1")
	rr := httptest.NewRecorder()
	a.handleCreateAvailability(rr, cr)
	if rr.Code != http.StatusCreated {
		t.Fatalf("seed: got %d, want 201", rr.Code)
	}
	var avail models.DJAvailability
	json.NewDecoder(rr.Body).Decode(&avail) //nolint:errcheck

	t.Run("unauthorized", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/", nil)
		req = withChiParam(req, "id", avail.ID)
		rr := httptest.NewRecorder()
		a.handleDeleteAvailability(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", rr.Code)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/", nil)
		req = withDJUserClaims(req, "u1")
		rr := httptest.NewRecorder()
		a.handleDeleteAvailability(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/", nil)
		req = withDJUserClaims(req, "u1")
		req = withChiParam(req, "id", "nonexistent")
		rr := httptest.NewRecorder()
		a.handleDeleteAvailability(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})

	t.Run("forbidden — wrong user", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/", nil)
		req = withDJUserClaims(req, "u-other")
		req = withChiParam(req, "id", avail.ID)
		rr := httptest.NewRecorder()
		a.handleDeleteAvailability(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("got %d, want 403", rr.Code)
		}
	})
}

func TestDJSelfService_ListScheduleRequests_ErrorPaths(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)

	t.Run("unauthorized", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?station_id=s1", nil)
		rr := httptest.NewRecorder()
		a.handleListScheduleRequests(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", rr.Code)
		}
	})

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req = withDJUserClaims(req, "u1")
		rr := httptest.NewRecorder()
		a.handleListScheduleRequests(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("manager sees all requests", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?station_id=s1", nil)
		req = withDJUserClaims(req, "u-mgr", string(models.RoleManager))
		rr := httptest.NewRecorder()
		a.handleListScheduleRequests(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
	})

	t.Run("dj sees own requests with status filter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?station_id=s1&status=pending", nil)
		req = withDJUserClaims(req, "u-dj", string(models.RoleDJ))
		rr := httptest.NewRecorder()
		a.handleListScheduleRequests(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
	})
}

func TestDJSelfService_CreateScheduleRequest_ErrorPaths(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)

	t.Run("unauthorized", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"station_id": "s1", "request_type": "time_off"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
		rr := httptest.NewRecorder()
		a.handleCreateScheduleRequest(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", rr.Code)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
		req = withDJUserClaims(req, "u1")
		rr := httptest.NewRecorder()
		a.handleCreateScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing station_id or request_type", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"station_id": "s1"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u1")
		rr := httptest.NewRecorder()
		a.handleCreateScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}
