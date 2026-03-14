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

func TestSyndicationAPI_UpdateNetwork_InvalidJSON(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	req := httptest.NewRequest("PUT", "/", bytes.NewReader([]byte("{")))
	req = withAPIRouteID(req, "some-id")
	rr := httptest.NewRecorder()
	s.handleUpdateNetwork(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("update network invalid json: got %d, want 400", rr.Code)
	}
}

func TestSyndicationAPI_UpdateNetworkShow_InvalidJSON(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	req := httptest.NewRequest("PUT", "/", bytes.NewReader([]byte("{")))
	req = withAPIRouteID(req, "some-id")
	rr := httptest.NewRecorder()
	s.handleUpdateNetworkShow(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("update network show invalid json: got %d, want 400", rr.Code)
	}
}

func TestSyndicationAPI_CreateNetworkShow_InvalidJSON(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	rr := httptest.NewRecorder()
	s.handleCreateNetworkShow(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create network show invalid json: got %d, want 400", rr.Code)
	}
}

func TestSyndicationAPI_CreateNetworkShow_MissingName(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	body, _ := json.Marshal(map[string]any{"duration": 60})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleCreateNetworkShow(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create network show missing name: got %d, want 400", rr.Code)
	}
}

func TestSyndicationAPI_GetNetworkShow_NotFound(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	req = withAPIRouteID(req, "nonexistent-id")
	rr := httptest.NewRecorder()
	s.handleGetNetworkShow(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get network show not found: got %d, want 404", rr.Code)
	}
}

func TestSyndicationAPI_CreateSubscription_InvalidJSON(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	rr := httptest.NewRecorder()
	s.handleCreateSubscription(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create subscription invalid json: got %d, want 400", rr.Code)
	}
}

func TestSyndicationAPI_CreateSubscription_MissingFields(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"network_show_id": "nsh1", "local_time": "10:00"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		s.handleCreateSubscription(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing network_show_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"station_id": "st1", "local_time": "10:00"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		s.handleCreateSubscription(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}

func TestSyndicationAPI_DeleteSubscription_NotFound(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	req := httptest.NewRequest("DELETE", "/nonexistent", nil)
	req = withAPIRouteID(req, "nonexistent-sub-id")
	rr := httptest.NewRecorder()
	s.handleDeleteSubscription(rr, req)
	// Service might return error → 500, or succeed gracefully → 200
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Fatalf("delete subscription not found: got %d", rr.Code)
	}
}

func TestSyndicationAPI_UpdateNetwork_AllFields(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	// Create a network first
	body, _ := json.Marshal(map[string]any{"name": "To Update"})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withSyndicationAdminCtx(req)
	rr := httptest.NewRecorder()
	s.handleCreateNetwork(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("seed network: got %d, want 201", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	netMap, _ := resp["network"].(map[string]any)
	netID, _ := netMap["id"].(string)

	// Update with all fields including description + active
	desc := "Updated desc"
	active := false
	body, _ = json.Marshal(map[string]any{
		"name":        "Updated Network",
		"description": &desc,
		"active":      &active,
	})
	req = httptest.NewRequest("PUT", "/"+netID, bytes.NewReader(body))
	req = withAPIRouteID(req, netID)
	rr = httptest.NewRecorder()
	s.handleUpdateNetwork(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update network all fields: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
}

func TestSyndicationAPI_ListNetworkShows_WithNetworkID(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	// Create a network show
	body, _ := json.Marshal(map[string]any{"name": "Filtered Show", "duration": 30})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleCreateNetworkShow(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("seed network show: got %d, want 201", rr.Code)
	}

	// List with network_id filter (empty = all)
	req = httptest.NewRequest("GET", "/?network_id=nonexistent", nil)
	rr = httptest.NewRecorder()
	s.handleListNetworkShows(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list by network_id: got %d, want 200", rr.Code)
	}
}

func TestSyndicationAPI_Materialize_InvalidJSON(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	rr := httptest.NewRecorder()
	s.handleMaterialize(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("materialize invalid json: got %d, want 400", rr.Code)
	}
}

func TestSyndicationAPI_IsPlatformAdmin(t *testing.T) {
	// Admin claims → true
	adminClaims := &auth.Claims{
		UserID: "u1",
		Roles:  []string{string(models.PlatformRoleAdmin)},
	}
	if !isPlatformAdmin(adminClaims) {
		t.Fatal("expected isPlatformAdmin = true for admin role")
	}

	// No admin role → false
	djClaims := &auth.Claims{
		UserID: "u2",
		Roles:  []string{string(models.RoleDJ)},
	}
	if isPlatformAdmin(djClaims) {
		t.Fatal("expected isPlatformAdmin = false for DJ role")
	}
}
