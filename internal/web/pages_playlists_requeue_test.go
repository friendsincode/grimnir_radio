/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func requeueTestHandler(t *testing.T) (*Handler, *models.Station) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}, &models.Playlist{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	station := &models.Station{ID: "st-1", Name: "S"}
	if err := db.Create(station).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	if err := db.Create(&models.Playlist{ID: "pl-1", StationID: "st-1", Name: "Mine"}).Error; err != nil {
		t.Fatalf("seed playlist: %v", err)
	}
	// A playlist owned by a different station, to prove scoping.
	if err := db.Create(&models.Playlist{ID: "pl-foreign", StationID: "st-2", Name: "Theirs"}).Error; err != nil {
		t.Fatalf("seed foreign playlist: %v", err)
	}
	return &Handler{db: db, logger: zerolog.Nop()}, station
}

func requeueRequest(id string, station *models.Station) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/dashboard/playlists/"+id+"/requeue", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, ctxKeyStation, station)
	return req.WithContext(ctx)
}

// A playlist that exists in the operator's station returns 503 when the director
// is absent (the playout system is down), rather than a 404 or a panic.
func TestPlaylistRequeue_NilDirector(t *testing.T) {
	h, station := requeueTestHandler(t)
	w := httptest.NewRecorder()
	h.PlaylistRequeue(w, requeueRequest("pl-1", station))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (nil director), got %d: %s", w.Code, w.Body.String())
	}
}

// A playlist owned by another station is never re-queued through this station's
// context: it 404s before the director is ever consulted.
func TestPlaylistRequeue_ForeignPlaylist404(t *testing.T) {
	h, station := requeueTestHandler(t)
	w := httptest.NewRecorder()
	h.PlaylistRequeue(w, requeueRequest("pl-foreign", station))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (cross-station), got %d", w.Code)
	}
}
