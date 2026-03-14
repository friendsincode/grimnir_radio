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

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/syndication"
)

func newSyndicationAPITest(t *testing.T) (*SyndicationAPI, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Network{},
		&models.NetworkShow{},
		&models.NetworkSubscription{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	svc := syndication.NewService(db, zerolog.Nop())
	api := &API{db: db, logger: zerolog.Nop()}
	return NewSyndicationAPI(api, svc), db
}

func withSyndicationAdminCtx(req *http.Request) *http.Request {
	return req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u1",
		Roles:  []string{string(models.PlatformRoleAdmin)},
	}))
}

func TestSyndicationAPI_Networks(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	// List networks (empty)
	req := httptest.NewRequest("GET", "/", nil)
	req = withSyndicationAdminCtx(req)
	rr := httptest.NewRecorder()
	s.handleListNetworks(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list networks: got %d, want 200", rr.Code)
	}
	var listResp map[string]any
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	networks, _ := listResp["networks"].([]any)
	if len(networks) != 0 {
		t.Fatalf("expected 0 networks, got %d", len(networks))
	}

	// Create a network
	body, _ := json.Marshal(map[string]any{
		"name":        "Test Network",
		"description": "A test network",
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withSyndicationAdminCtx(req)
	rr = httptest.NewRecorder()
	s.handleCreateNetwork(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create network: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var createResp map[string]any
	json.NewDecoder(rr.Body).Decode(&createResp) //nolint:errcheck
	netMap, _ := createResp["network"].(map[string]any)
	netID, _ := netMap["id"].(string)
	if netID == "" {
		t.Fatal("expected network id in response")
	}

	// Get the network
	req = httptest.NewRequest("GET", "/"+netID, nil)
	req = withAPIRouteID(req, netID)
	rr = httptest.NewRecorder()
	s.handleGetNetwork(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get network: got %d, want 200", rr.Code)
	}

	// Update the network
	newName := "Updated Network"
	body, _ = json.Marshal(map[string]any{"name": &newName})
	req = httptest.NewRequest("PUT", "/"+netID, bytes.NewReader(body))
	req = withAPIRouteID(req, netID)
	rr = httptest.NewRecorder()
	s.handleUpdateNetwork(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update network: got %d, want 200", rr.Code)
	}

	// Delete the network
	req = httptest.NewRequest("DELETE", "/"+netID, nil)
	req = withAPIRouteID(req, netID)
	rr = httptest.NewRecorder()
	s.handleDeleteNetwork(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete network: got %d, want 200", rr.Code)
	}

	// Get after delete should return 404
	req = httptest.NewRequest("GET", "/"+netID, nil)
	req = withAPIRouteID(req, netID)
	rr = httptest.NewRecorder()
	s.handleGetNetwork(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get deleted network: got %d, want 404", rr.Code)
	}
}

func TestSyndicationAPI_NetworkShows(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	// List shows (empty)
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	s.handleListNetworkShows(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list network shows: got %d, want 200", rr.Code)
	}

	// Create a network show
	body, _ := json.Marshal(map[string]any{
		"name":     "Test Show",
		"duration": 60,
		"active":   true,
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	s.handleCreateNetworkShow(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create network show: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var createResp map[string]any
	json.NewDecoder(rr.Body).Decode(&createResp) //nolint:errcheck
	showMap, _ := createResp["network_show"].(map[string]any)
	showID, _ := showMap["id"].(string)
	if showID == "" {
		t.Fatal("expected show id in response")
	}

	// Get the show
	req = httptest.NewRequest("GET", "/"+showID, nil)
	req = withAPIRouteID(req, showID)
	rr = httptest.NewRecorder()
	s.handleGetNetworkShow(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get network show: got %d, want 200", rr.Code)
	}

	// Update the show
	body, _ = json.Marshal(map[string]any{"name": "Updated Show"})
	req = httptest.NewRequest("PUT", "/"+showID, bytes.NewReader(body))
	req = withAPIRouteID(req, showID)
	rr = httptest.NewRecorder()
	s.handleUpdateNetworkShow(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update network show: got %d, want 200", rr.Code)
	}

	// Delete the show
	req = httptest.NewRequest("DELETE", "/"+showID, nil)
	req = withAPIRouteID(req, showID)
	rr = httptest.NewRecorder()
	s.handleDeleteNetworkShow(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete network show: got %d, want 200", rr.Code)
	}
}

func TestSyndicationAPI_Subscriptions(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	// Seed a network show
	show := models.NewNetworkShow("Syndicated Jazz Hour")
	if err := s.db.Create(show).Error; err != nil {
		t.Fatalf("seed network show: %v", err)
	}

	// List subscriptions (requires station_id)
	req := httptest.NewRequest("GET", "/?station_id=st1", nil)
	rr := httptest.NewRecorder()
	s.handleListSubscriptions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list subscriptions: got %d, want 200", rr.Code)
	}
	var listResp map[string]any
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	subs, _ := listResp["subscriptions"].([]any)
	if len(subs) != 0 {
		t.Fatalf("expected 0 subscriptions, got %d", len(subs))
	}

	// Create a subscription
	body, _ := json.Marshal(map[string]any{
		"station_id":      "st1",
		"network_show_id": show.ID,
		"local_time":      "19:00",
		"local_days":      "MO,WE,FR",
		"timezone":        "UTC",
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	s.handleCreateSubscription(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create subscription: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var createResp map[string]any
	json.NewDecoder(rr.Body).Decode(&createResp) //nolint:errcheck
	subMap, _ := createResp["subscription"].(map[string]any)
	subID, _ := subMap["id"].(string)
	if subID == "" {
		t.Fatal("expected subscription id in response")
	}

	// Delete the subscription
	req = httptest.NewRequest("DELETE", "/"+subID, nil)
	req = withAPIRouteID(req, subID)
	rr = httptest.NewRecorder()
	s.handleDeleteSubscription(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete subscription: got %d, want 200", rr.Code)
	}
}

func TestSyndicationAPI_Errors(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	t.Run("create network requires auth", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "No Auth"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		// No claims in context
		rr := httptest.NewRecorder()
		s.handleCreateNetwork(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("create network requires name", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"description": "No name"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withSyndicationAdminCtx(req)
		rr := httptest.NewRecorder()
		s.handleCreateNetwork(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("list subscriptions requires station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		s.handleListSubscriptions(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("get non-existent network", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/missing", nil)
		req = withAPIRouteID(req, "nonexistent-id")
		rr := httptest.NewRecorder()
		s.handleGetNetwork(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rr.Code)
		}
	})
}

func TestSyndicationAPI_Materialize(t *testing.T) {
	s, _ := newSyndicationAPITest(t)

	// Missing station_id → 400
	body, _ := json.Marshal(map[string]any{})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleMaterialize(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}

	// Valid with no subscriptions → 200, 0 instances
	body, _ = json.Marshal(map[string]any{"station_id": "s1", "start": "2025-01-06", "end": "2025-01-13"})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	s.handleMaterialize(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("materialize: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["instances_created"]; !ok {
		t.Fatal("expected instances_created key")
	}
}
