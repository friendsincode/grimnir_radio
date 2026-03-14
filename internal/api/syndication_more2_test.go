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

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/syndication"
)

func newSyndicationMore2Test(t *testing.T) (*SyndicationAPI, *gorm.DB) {
	t.Helper()
	dbPath := t.TempDir() + "/syn2.db"
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
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

// TestSyndicationAPI_ListNetworks_NonAdmin covers the non-admin ownerID branch.
func TestSyndicationAPI_ListNetworks_NonAdmin(t *testing.T) {
	s, _ := newSyndicationMore2Test(t)

	// Non-admin user (not platform admin) → ownerID is set to claims.UserID
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u-nonadmin",
		Roles:  []string{string(models.RoleManager)},
	}))
	rr := httptest.NewRecorder()
	s.handleListNetworks(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("non-admin list: got %d, want 200", rr.Code)
	}
}

// TestSyndicationAPI_ListNetworks_NilClaims covers the nil claims path.
func TestSyndicationAPI_ListNetworks_NilClaims(t *testing.T) {
	s, _ := newSyndicationMore2Test(t)

	req := httptest.NewRequest("GET", "/", nil)
	// No claims in context
	rr := httptest.NewRecorder()
	s.handleListNetworks(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("nil claims list: got %d, want 200", rr.Code)
	}
}

// TestSyndicationAPI_CreateNetwork_Unauthorized covers nil claims.
func TestSyndicationAPI_CreateNetwork_Unauthorized(t *testing.T) {
	s, _ := newSyndicationMore2Test(t)

	b, _ := json.Marshal(map[string]any{"name": "Net"})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
	// No claims → unauthorized
	rr := httptest.NewRecorder()
	s.handleCreateNetwork(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized: got %d, want 401", rr.Code)
	}
}

// TestSyndicationAPI_DeleteNetwork_Success covers the delete success path.
func TestSyndicationAPI_DeleteNetwork_Success(t *testing.T) {
	s, db := newSyndicationMore2Test(t)

	db.Create(&models.Network{ID: "net-del", Name: "ToDelete", OwnerID: "u1"}) //nolint:errcheck

	req := httptest.NewRequest("DELETE", "/", nil)
	req = withChiParam(req, "id", "net-del")
	rr := httptest.NewRecorder()
	s.handleDeleteNetwork(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete network: got %d, want 200", rr.Code)
	}
}

// TestSyndicationAPI_DeleteNetworkShow_Success covers the delete show success path.
func TestSyndicationAPI_DeleteNetworkShow_Success(t *testing.T) {
	s, db := newSyndicationMore2Test(t)

	netID := "net-x"
	db.Create(&models.NetworkShow{ID: "ns-del", NetworkID: &netID, Name: "ToDelete"}) //nolint:errcheck

	req := httptest.NewRequest("DELETE", "/", nil)
	req = withChiParam(req, "id", "ns-del")
	rr := httptest.NewRecorder()
	s.handleDeleteNetworkShow(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete network show: got %d, want 200", rr.Code)
	}
}

// TestSyndicationAPI_DeleteSubscription_Success covers the delete subscription success.
func TestSyndicationAPI_DeleteSubscription_Success(t *testing.T) {
	s, db := newSyndicationMore2Test(t)

	db.Create(&models.NetworkSubscription{ //nolint:errcheck
		ID:            "sub-del",
		NetworkShowID: "ns-some",
		StationID:     "s1",
	})

	req := httptest.NewRequest("DELETE", "/", nil)
	req = withChiParam(req, "id", "sub-del")
	rr := httptest.NewRecorder()
	s.handleDeleteSubscription(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete subscription: got %d, want 200", rr.Code)
	}
}

// TestSyndicationAPI_ListNetworkShows_NoNetworkID covers list with no filter.
func TestSyndicationAPI_ListNetworkShows_NoNetworkID(t *testing.T) {
	s, _ := newSyndicationMore2Test(t)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	s.handleListNetworkShows(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list shows no filter: got %d, want 200", rr.Code)
	}
}

// TestSyndicationAPI_GetNetwork_Success covers handleGetNetwork.
func TestSyndicationAPI_GetNetwork_Success(t *testing.T) {
	s, db := newSyndicationMore2Test(t)

	db.Create(&models.Network{ID: "net-get", Name: "GetMe", OwnerID: "u1"}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/", nil)
	req = withChiParam(req, "id", "net-get")
	rr := httptest.NewRecorder()
	s.handleGetNetwork(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get network: got %d, want 200", rr.Code)
	}
}

// TestSyndicationAPI_GetNetwork_NotFound covers the 404 path.
func TestSyndicationAPI_GetNetwork_NotFound(t *testing.T) {
	s, _ := newSyndicationMore2Test(t)

	req := httptest.NewRequest("GET", "/", nil)
	req = withChiParam(req, "id", "nonexistent")
	rr := httptest.NewRecorder()
	s.handleGetNetwork(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get network not found: got %d, want 404", rr.Code)
	}
}
