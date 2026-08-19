/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/syndication"
)

// withUUIDClaims is like withAdminClaims but the UserID is a real uuid, needed
// when the handler persists it into a uuid column (e.g. Network.OwnerID).
func withUUIDClaims(req *http.Request) *http.Request {
	return req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: dbtest.UUID("admin"),
		Roles:  []string{string(models.PlatformRoleAdmin)},
	}))
}

func newSyndTest(t *testing.T) *SyndicationAPI {
	t.Helper()
	database := dbtest.Open(t, &models.Station{}, &models.Network{}, &models.NetworkShow{}, &models.NetworkSubscription{})
	api := &API{db: database, bus: events.NewBus(), logger: zerolog.Nop()}
	return NewSyndicationAPI(api, syndication.NewService(database, zerolog.Nop()))
}

// TestSyndicationHandlers_EdgeCases covers the auth, validation, and not-found
// guards across the network/subscription handlers.
func TestSyndicationHandlers_EdgeCases(t *testing.T) {
	s := newSyndTest(t)

	t.Run("CreateNetwork requires auth", func(t *testing.T) {
		rr := httptest.NewRecorder()
		s.handleCreateNetwork(rr, postJSON("/", `{"name":"Indie"}`))
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401 (body=%s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("CreateNetwork requires name", func(t *testing.T) {
		rr := httptest.NewRecorder()
		s.handleCreateNetwork(rr, withAdminClaims(postJSON("/", `{"name":""}`)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400 (body=%s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("CreateNetwork ok", func(t *testing.T) {
		rr := httptest.NewRecorder()
		s.handleCreateNetwork(rr, withUUIDClaims(postJSON("/", `{"name":"Indie"}`)))
		if rr.Code != http.StatusCreated {
			t.Fatalf("got %d, want 201 (body=%s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("GetNetwork not found", func(t *testing.T) {
		rr := httptest.NewRecorder()
		s.handleGetNetwork(rr, withChiParam(httptest.NewRequest("GET", "/", nil), "id", dbtest.UUID("missing")))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})

	t.Run("CreateNetworkShow requires name", func(t *testing.T) {
		rr := httptest.NewRecorder()
		s.handleCreateNetworkShow(rr, postJSON("/", `{"name":""}`))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400 (body=%s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("CreateSubscription requires ids", func(t *testing.T) {
		for _, body := range []string{`{}`, `{"station_id":"x"}`, `{"network_show_id":"y"}`} {
			rr := httptest.NewRecorder()
			s.handleCreateSubscription(rr, postJSON("/", body))
			if rr.Code != http.StatusBadRequest {
				t.Errorf("body %s: got %d, want 400", body, rr.Code)
			}
		}
	})

	t.Run("ListSubscriptions requires station_id", func(t *testing.T) {
		rr := httptest.NewRecorder()
		s.handleListSubscriptions(rr, httptest.NewRequest("GET", "/", nil))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("Materialize requires station_id", func(t *testing.T) {
		rr := httptest.NewRecorder()
		s.handleMaterialize(rr, postJSON("/", `{}`))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}
