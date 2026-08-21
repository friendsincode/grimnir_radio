/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/db"
	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newShowsWebPGTest(t *testing.T) (*Handler, string) {
	t.Helper()
	database := dbtest.Open(t)
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	stationID := dbtest.UUID("st")
	if err := database.Create(&models.Station{ID: stationID, OwnerID: dbtest.UUID("owner"), Name: "S"}).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	return &Handler{db: database, logger: zerolog.Nop()}, stationID
}

func webReqWithID(method, id string, body []byte) *http.Request {
	req := httptest.NewRequest(method, "/"+id, bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestShowUpdate_PersistsMetadata guards Show.metadata (serializer:json) on the
// web handler: the update must persist the metadata map through the struct
// field, not fail the raw-map jsonb write.
func TestShowUpdate_PersistsMetadata(t *testing.T) {
	h, stationID := newShowsWebPGTest(t)
	showID := dbtest.UUID("show")
	if err := h.db.Create(&models.Show{ID: showID, StationID: stationID, Name: "Morning"}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}

	rr := httptest.NewRecorder()
	h.ShowUpdate(rr, withStationCtx(webReqWithID("PUT", showID, []byte(`{"metadata":{"color":"blue"}}`)), &models.Station{ID: stationID}))
	if rr.Code != http.StatusOK {
		t.Fatalf("ShowUpdate: got %d, body=%s", rr.Code, rr.Body.String())
	}

	var got models.Show
	h.db.First(&got, "id = ?", showID)
	if got.Metadata["color"] != "blue" {
		t.Fatalf("Show.Metadata = %v, want color=blue", got.Metadata)
	}
}

// TestShowInstanceUpdate_PersistsMetadata guards ShowInstance.metadata on the
// non-virtual web update path.
func TestShowInstanceUpdate_PersistsMetadata(t *testing.T) {
	h, stationID := newShowsWebPGTest(t)
	showID := dbtest.UUID("show")
	if err := h.db.Create(&models.Show{ID: showID, StationID: stationID, Name: "Morning"}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	instID := dbtest.UUID("inst")
	if err := h.db.Create(&models.ShowInstance{
		ID: instID, ShowID: showID, StationID: stationID,
		StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour),
		Status: models.ShowInstanceScheduled,
	}).Error; err != nil {
		t.Fatalf("seed instance: %v", err)
	}

	req := webReqWithID("PUT", instID, []byte(`{"metadata":{"note":"swap"}}`))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &models.Station{ID: stationID}))
	rr := httptest.NewRecorder()
	h.ShowInstanceUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ShowInstanceUpdate: got %d, body=%s", rr.Code, rr.Body.String())
	}

	var got models.ShowInstance
	h.db.First(&got, "id = ?", instID)
	if got.Metadata["note"] != "swap" {
		t.Fatalf("ShowInstance.Metadata = %v, want note=swap", got.Metadata)
	}
}
