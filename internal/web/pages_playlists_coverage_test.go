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
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func playlistReqWithID(method, target string, user *models.User, station *models.Station, paramName, paramVal string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	ctx := req.Context()
	if user != nil {
		ctx = context.WithValue(ctx, ctxKeyUser, user)
	}
	if station != nil {
		ctx = context.WithValue(ctx, ctxKeyStation, station)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(paramName, paramVal)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

func playlistReqWithMultipleIDs(method, target string, user *models.User, station *models.Station, params map[string]string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	ctx := req.Context()
	if user != nil {
		ctx = context.WithValue(ctx, ctxKeyUser, user)
	}
	if station != nil {
		ctx = context.WithValue(ctx, ctxKeyStation, station)
	}
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

func seedPlaylist(t *testing.T, h *Handler, stationID, id, name string) models.Playlist {
	t.Helper()
	p := models.Playlist{
		ID:        id,
		StationID: stationID,
		Name:      name,
	}
	if err := h.db.Create(&p).Error; err != nil {
		t.Fatalf("seed playlist %s: %v", id, err)
	}
	return p
}

func seedPlaylistItem(t *testing.T, h *Handler, playlistID, mediaID string, position int) models.PlaylistItem {
	t.Helper()
	item := models.PlaylistItem{
		ID:         uuid.NewString(),
		PlaylistID: playlistID,
		MediaID:    mediaID,
		Position:   position,
	}
	if err := h.db.Create(&item).Error; err != nil {
		t.Fatalf("seed playlist item: %v", err)
	}
	return item
}

// ---------------------------------------------------------------------------
// PlaylistList
// ---------------------------------------------------------------------------

func TestPlaylistList_Redirects_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/dashboard/playlists", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistList(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestPlaylistList_EmptyStation(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/dashboard/playlists", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPlaylistList_WithPlaylistsAndItems(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	pl := seedPlaylist(t, h, station.ID, "pl-list-1", "My Playlist")
	m := seedMedia(t, h, station.ID, "pl-media-1", "Track 1")
	seedPlaylistItem(t, h, pl.ID, m.ID, 1)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/playlists", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PlaylistNew
// ---------------------------------------------------------------------------

func TestPlaylistNew_Renders(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/dashboard/playlists/new", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistNew(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PlaylistCreate
// ---------------------------------------------------------------------------

func TestPlaylistCreate_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/dashboard/playlists", strings.NewReader("name=Test"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistCreate_EmptyName(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/dashboard/playlists", strings.NewReader("name="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistCreate_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/dashboard/playlists", strings.NewReader("name=My+New+Playlist&description=Desc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistCreate(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPlaylistCreate_HtmxRedirects(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/dashboard/playlists", strings.NewReader("name=HTMX+Playlist"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistCreate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatal("expected HX-Redirect header")
	}
}

// ---------------------------------------------------------------------------
// PlaylistDetail
// ---------------------------------------------------------------------------

func TestPlaylistDetail_Redirects_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.PlaylistDetail(rr, playlistReqWithID(http.MethodGet, "/", &user, nil, "id", "x"))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestPlaylistDetail_NotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.PlaylistDetail(rr, playlistReqWithID(http.MethodGet, "/", &user, &station, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPlaylistDetail_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	pl := seedPlaylist(t, h, station.ID, "pl-detail-1", "Detail Playlist")
	m := seedMedia(t, h, station.ID, "pl-detail-media-1", "Track")
	seedPlaylistItem(t, h, pl.ID, m.ID, 1)

	rr := httptest.NewRecorder()
	h.PlaylistDetail(rr, playlistReqWithID(http.MethodGet, "/", &user, &station, "id", "pl-detail-1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PlaylistEdit
// ---------------------------------------------------------------------------

func TestPlaylistEdit_Redirects_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.PlaylistEdit(rr, playlistReqWithID(http.MethodGet, "/", &user, nil, "id", "x"))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestPlaylistEdit_NotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.PlaylistEdit(rr, playlistReqWithID(http.MethodGet, "/", &user, &station, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPlaylistEdit_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedPlaylist(t, h, station.ID, "pl-edit-1", "Edit Playlist")
	rr := httptest.NewRecorder()
	h.PlaylistEdit(rr, playlistReqWithID(http.MethodGet, "/", &user, &station, "id", "pl-edit-1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PlaylistUpdate
// ---------------------------------------------------------------------------

func TestPlaylistUpdate_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=Updated"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "x")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistUpdate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistUpdate_NotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=Updated"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nonexistent")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistUpdate(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPlaylistUpdate_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedPlaylist(t, h, station.ID, "pl-upd-1", "Original Name")

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=Updated+Name&description=New+Desc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "pl-upd-1")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.PlaylistUpdate(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPlaylistUpdate_HtmxRedirects(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedPlaylist(t, h, station.ID, "pl-upd-2", "HTMX Playlist")

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=HTMX+Updated"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "pl-upd-2")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.PlaylistUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatal("expected HX-Redirect header")
	}
}

// ---------------------------------------------------------------------------
// PlaylistDelete
// ---------------------------------------------------------------------------

func TestPlaylistDelete_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.PlaylistDelete(rr, playlistReqWithID(http.MethodDelete, "/", &user, nil, "id", "x"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistDelete_NotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.PlaylistDelete(rr, playlistReqWithID(http.MethodDelete, "/", &user, &station, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPlaylistDelete_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	pl := seedPlaylist(t, h, station.ID, "pl-del-1", "Delete Me")
	m := seedMedia(t, h, station.ID, "pl-del-media-1", "Track for deletion")
	seedPlaylistItem(t, h, pl.ID, m.ID, 1)

	rr := httptest.NewRecorder()
	h.PlaylistDelete(rr, playlistReqWithID(http.MethodDelete, "/", &user, &station, "id", "pl-del-1"))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPlaylistDelete_HtmxRedirects(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedPlaylist(t, h, station.ID, "pl-del-2", "Delete Me HTMX")

	req := playlistReqWithID(http.MethodDelete, "/", &user, &station, "id", "pl-del-2")
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.PlaylistDelete(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatal("expected HX-Redirect header")
	}
}

// ---------------------------------------------------------------------------
// PlaylistBulk
// ---------------------------------------------------------------------------

func TestPlaylistBulk_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	body := `{"action":"delete","ids":["x"]}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistBulk_InvalidJSON(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not-json"))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistBulk_EmptyIDs(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	body := `{"action":"delete","ids":[]}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistBulk_UnknownAction(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	body := `{"action":"unknown","ids":["x"]}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistBulk_Delete(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	pl1 := seedPlaylist(t, h, station.ID, "pl-bulk-del-1", "Bulk Delete 1")
	pl2 := seedPlaylist(t, h, station.ID, "pl-bulk-del-2", "Bulk Delete 2")

	body, _ := json.Marshal(map[string]interface{}{
		"action": "delete",
		"ids":    []string{pl1.ID, pl2.ID},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PlaylistAddItem
// ---------------------------------------------------------------------------

func TestPlaylistAddItem_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("media_id=m1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "pl-1")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistAddItem(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistAddItem_PlaylistNotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("media_id=m1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nonexistent-playlist")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistAddItem(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPlaylistAddItem_NoMediaID(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedPlaylist(t, h, station.ID, "pl-add-1", "Add Item Playlist")

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "pl-add-1")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistAddItem(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistAddItem_MediaNotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedPlaylist(t, h, station.ID, "pl-add-2", "Add Item Playlist 2")

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("media_id=nonexistent-media"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "pl-add-2")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistAddItem(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPlaylistAddItem_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedPlaylist(t, h, station.ID, "pl-add-3", "Add Item Playlist 3")
	seedMedia(t, h, station.ID, "pl-add-media-1", "Media for playlist")

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("media_id=pl-add-media-1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "pl-add-3")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistAddItem(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPlaylistAddItem_HtmxRequest(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedPlaylist(t, h, station.ID, "pl-add-4", "HTMX Add Playlist")
	seedMedia(t, h, station.ID, "pl-add-media-2", "HTMX Media")

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("media_id=pl-add-media-2"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "pl-add-4")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistAddItem(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPlaylistAddItem_MultipleMediaIDs(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedPlaylist(t, h, station.ID, "pl-add-5", "Multi Add Playlist")
	seedMedia(t, h, station.ID, "pl-multi-1", "Media 1")
	seedMedia(t, h, station.ID, "pl-multi-2", "Media 2")

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("media_ids=pl-multi-1%2Cpl-multi-2"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "pl-add-5")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistAddItem(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PlaylistRemoveItem
// ---------------------------------------------------------------------------

func TestPlaylistRemoveItem_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.PlaylistRemoveItem(rr, playlistReqWithMultipleIDs(http.MethodDelete, "/", &user, nil,
		map[string]string{"id": "pl-1", "itemID": "item-1"}))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistRemoveItem_PlaylistNotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.PlaylistRemoveItem(rr, playlistReqWithMultipleIDs(http.MethodDelete, "/", &user, &station,
		map[string]string{"id": "nonexistent", "itemID": "item-1"}))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPlaylistRemoveItem_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	pl := seedPlaylist(t, h, station.ID, "pl-remove-1", "Remove Item Playlist")
	m := seedMedia(t, h, station.ID, "pl-remove-media-1", "Remove Track")
	item := seedPlaylistItem(t, h, pl.ID, m.ID, 1)

	rr := httptest.NewRecorder()
	h.PlaylistRemoveItem(rr, playlistReqWithMultipleIDs(http.MethodDelete, "/", &user, &station,
		map[string]string{"id": pl.ID, "itemID": item.ID}))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPlaylistRemoveItem_HtmxRequest(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	pl := seedPlaylist(t, h, station.ID, "pl-remove-2", "HTMX Remove Playlist")
	m := seedMedia(t, h, station.ID, "pl-remove-media-2", "Remove Track 2")
	item := seedPlaylistItem(t, h, pl.ID, m.ID, 1)

	req := playlistReqWithMultipleIDs(http.MethodDelete, "/", &user, &station,
		map[string]string{"id": pl.ID, "itemID": item.ID})
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.PlaylistRemoveItem(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// PlaylistReorderItems
// ---------------------------------------------------------------------------

func TestPlaylistReorderItems_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	body := `[{"id":"item-1","position":1}]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "pl-1")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistReorderItems(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistReorderItems_PlaylistNotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	body := `[{"id":"item-1","position":1}]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nonexistent")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistReorderItems(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPlaylistReorderItems_InvalidJSON(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedPlaylist(t, h, station.ID, "pl-reorder-1", "Reorder Playlist")

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not-json"))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "pl-reorder-1")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistReorderItems(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistReorderItems_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	pl := seedPlaylist(t, h, station.ID, "pl-reorder-2", "Reorder Playlist 2")
	m1 := seedMedia(t, h, station.ID, "pl-reorder-media-1", "Track 1")
	m2 := seedMedia(t, h, station.ID, "pl-reorder-media-2", "Track 2")
	item1 := seedPlaylistItem(t, h, pl.ID, m1.ID, 1)
	item2 := seedPlaylistItem(t, h, pl.ID, m2.ID, 2)

	orderJSON, _ := json.Marshal([]map[string]interface{}{
		{"id": item1.ID, "position": 2},
		{"id": item2.ID, "position": 1},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(orderJSON))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", pl.ID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistReorderItems(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PlaylistCover
// ---------------------------------------------------------------------------

func TestPlaylistCover_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.PlaylistCover(rr, playlistReqWithID(http.MethodGet, "/", &user, nil, "id", "x"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistCover_NotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.PlaylistCover(rr, playlistReqWithID(http.MethodGet, "/", &user, &station, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPlaylistCover_NoCoverImage(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedPlaylist(t, h, station.ID, "pl-cover-1", "No Cover Playlist")
	rr := httptest.NewRecorder()
	h.PlaylistCover(rr, playlistReqWithID(http.MethodGet, "/", &user, &station, "id", "pl-cover-1"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPlaylistCover_WithCoverImage(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	pl := seedPlaylist(t, h, station.ID, "pl-cover-2", "Cover Playlist")
	fakeJPEG := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46}
	h.db.Model(&pl).Updates(map[string]interface{}{
		"cover_image":      fakeJPEG,
		"cover_image_mime": "image/jpeg",
	})
	rr := httptest.NewRecorder()
	h.PlaylistCover(rr, playlistReqWithID(http.MethodGet, "/", &user, &station, "id", "pl-cover-2"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %s", rr.Header().Get("Content-Type"))
	}
}

// ---------------------------------------------------------------------------
// PlaylistDeleteCover
// ---------------------------------------------------------------------------

func TestPlaylistDeleteCover_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.PlaylistDeleteCover(rr, playlistReqWithID(http.MethodDelete, "/", &user, nil, "id", "x"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistDeleteCover_NotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.PlaylistDeleteCover(rr, playlistReqWithID(http.MethodDelete, "/", &user, &station, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPlaylistDeleteCover_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	pl := seedPlaylist(t, h, station.ID, "pl-delcover-1", "Cover Playlist")
	fakeJPEG := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	h.db.Model(&pl).Updates(map[string]interface{}{
		"cover_image":      fakeJPEG,
		"cover_image_mime": "image/jpeg",
	})

	rr := httptest.NewRecorder()
	h.PlaylistDeleteCover(rr, playlistReqWithID(http.MethodDelete, "/", &user, &station, "id", "pl-delcover-1"))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPlaylistDeleteCover_HtmxRequest(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedPlaylist(t, h, station.ID, "pl-delcover-2", "HTMX Delete Cover")

	req := playlistReqWithID(http.MethodDelete, "/", &user, &station, "id", "pl-delcover-2")
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.PlaylistDeleteCover(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// PlaylistMediaSearch
// ---------------------------------------------------------------------------

func TestPlaylistMediaSearch_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistMediaSearch(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlaylistMediaSearch_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "search-m1", "Search Track")

	req := httptest.NewRequest(http.MethodGet, "/?q=Search", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistMediaSearch(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPlaylistMediaSearch_WithFilters(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "search-m2", "Filter Track")

	req := httptest.NewRequest(http.MethodGet, "/?genre=Jazz&artist=Test&mood=Happy&year_from=2000&year_to=2020&bpm_from=100&bpm_to=150&exclude_explicit=true", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistMediaSearch(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPlaylistMediaSearch_IncludeArchive(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "search-m3", "Archive Track")

	req := httptest.NewRequest(http.MethodGet, "/?include_archive=true", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.PlaylistMediaSearch(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}
