/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestShowWebHandlers_EdgeCases covers the not-found, bad-input, and delete
// paths of the show web handlers.
func TestShowWebHandlers_EdgeCases(t *testing.T) {
	h, stationID := newShowsWebPGTest(t)
	showID := dbtest.UUID("show")
	if err := h.db.Create(&models.Show{ID: showID, StationID: stationID, Name: "Morning"}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}

	t.Run("ShowUpdate not found", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ShowUpdate(rr, webReqWithID("PUT", dbtest.UUID("missing"), []byte(`{"name":"x"}`)))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})

	t.Run("ShowUpdate bad json", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ShowUpdate(rr, webReqWithID("PUT", showID, []byte(`{`)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400 (body=%s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("ShowUpdate invalid rrule", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ShowUpdate(rr, webReqWithID("PUT", showID, []byte(`{"rrule":"NOT-A-RRULE"}`)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400 (body=%s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("ShowInstanceUpdate no station", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ShowInstanceUpdate(rr, webReqWithID("PUT", dbtest.UUID("i"), []byte(`{}`)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400 (body=%s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("ShowDelete not found", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ShowDelete(rr, webReqWithID("DELETE", dbtest.UUID("missing"), nil))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})

	t.Run("ShowDelete ok", func(t *testing.T) {
		delID := dbtest.UUID("todelete")
		if err := h.db.Create(&models.Show{ID: delID, StationID: stationID, Name: "Gone"}).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
		rr := httptest.NewRecorder()
		h.ShowDelete(rr, webReqWithID("DELETE", delID, nil))
		if rr.Code != http.StatusNoContent {
			t.Fatalf("got %d, want 204 (body=%s)", rr.Code, rr.Body.String())
		}
		var n int64
		h.db.Model(&models.Show{}).Where("id = ?", delID).Count(&n)
		if n != 0 {
			t.Fatalf("show not deleted, %d rows remain", n)
		}
	})
}
