/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

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

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Multi-select track removal (operator request 2026-07-04): one request
// removes a batch of selected items, scoped to the playlist & station so a
// crafted id list can't reach into other playlists.
func TestPlaylistRemoveItems_BulkScopedToPlaylist(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}, &models.Playlist{}, &models.PlaylistItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	station := models.Station{ID: "st-1", Name: "S"}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	for _, p := range []models.Playlist{
		{ID: "pl-1", StationID: "st-1", Name: "Mine"},
		{ID: "pl-2", StationID: "st-1", Name: "Other"},
	} {
		if err := db.Create(&p).Error; err != nil {
			t.Fatalf("seed playlist: %v", err)
		}
	}
	for _, it := range []models.PlaylistItem{
		{ID: "it-1", PlaylistID: "pl-1", MediaID: "m-1", Position: 1},
		{ID: "it-2", PlaylistID: "pl-1", MediaID: "m-2", Position: 2},
		{ID: "it-3", PlaylistID: "pl-1", MediaID: "m-3", Position: 3},
		{ID: "it-other", PlaylistID: "pl-2", MediaID: "m-4", Position: 1},
	} {
		if err := db.Create(&it).Error; err != nil {
			t.Fatalf("seed item: %v", err)
		}
	}

	h := &Handler{db: db, logger: zerolog.Nop()}

	// Remove two of pl-1's items AND sneak in another playlist's item id:
	// the foreign id must be ignored by the playlist_id scope.
	body, _ := json.Marshal(map[string]any{"ids": []string{"it-1", "it-3", "it-other"}})
	req := httptest.NewRequest(http.MethodPost, "/dashboard/playlists/pl-1/items/bulk-remove", bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "pl-1")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.PlaylistRemoveItems(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["removed"] != float64(2) {
		t.Errorf("removed = %v, want 2 (foreign id ignored)", resp["removed"])
	}

	var remaining []models.PlaylistItem
	db.Order("id").Find(&remaining)
	if len(remaining) != 2 {
		t.Fatalf("remaining = %d items, want 2", len(remaining))
	}
	if remaining[0].ID != "it-2" || remaining[1].ID != "it-other" {
		t.Errorf("survivors = %s,%s want it-2,it-other", remaining[0].ID, remaining[1].ID)
	}

	// Empty selection is a 400, not a no-op delete.
	req2 := httptest.NewRequest(http.MethodPost, "/dashboard/playlists/pl-1/items/bulk-remove", bytes.NewReader([]byte(`{"ids":[]}`)))
	rctx2 := chi.NewRouteContext()
	rctx2.URLParams.Add("id", "pl-1")
	ctx2 := context.WithValue(req2.Context(), chi.RouteCtxKey, rctx2)
	ctx2 = context.WithValue(ctx2, ctxKeyStation, &station)
	rr2 := httptest.NewRecorder()
	h.PlaylistRemoveItems(rr2, req2.WithContext(ctx2))
	if rr2.Code != http.StatusBadRequest {
		t.Errorf("empty selection status = %d, want 400", rr2.Code)
	}

	// Foreign station's playlist: 404.
	other := models.Station{ID: "st-2", Name: "X"}
	req3 := httptest.NewRequest(http.MethodPost, "/dashboard/playlists/pl-1/items/bulk-remove", bytes.NewReader(body))
	rctx3 := chi.NewRouteContext()
	rctx3.URLParams.Add("id", "pl-1")
	ctx3 := context.WithValue(req3.Context(), chi.RouteCtxKey, rctx3)
	ctx3 = context.WithValue(ctx3, ctxKeyStation, &other)
	rr3 := httptest.NewRecorder()
	h.PlaylistRemoveItems(rr3, req3.WithContext(ctx3))
	if rr3.Code != http.StatusNotFound {
		t.Errorf("foreign station status = %d, want 404", rr3.Code)
	}
}
