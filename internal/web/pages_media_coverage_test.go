/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ---------------------------------------------------------------------------
// helpers shared across media coverage tests
// ---------------------------------------------------------------------------

func mediaReq(method, target string, user *models.User, station *models.Station) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	ctx := req.Context()
	if user != nil {
		ctx = context.WithValue(ctx, ctxKeyUser, user)
	}
	if station != nil {
		ctx = context.WithValue(ctx, ctxKeyStation, station)
	}
	return req.WithContext(ctx)
}

func mediaReqWithID(method, target string, user *models.User, station *models.Station, paramName, paramVal string) *http.Request {
	req := mediaReq(method, target, user, station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(paramName, paramVal)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// seedMedia creates a media item in the DB and returns it.
func seedMedia(t *testing.T, h *Handler, stationID, id, title string) models.MediaItem {
	t.Helper()
	m := models.MediaItem{
		ID:            id,
		StationID:     stationID,
		Title:         title,
		Artist:        "Test Artist",
		Path:          id + ".mp3",
		AnalysisState: models.AnalysisComplete,
	}
	if err := h.db.Create(&m).Error; err != nil {
		t.Fatalf("seed media %s: %v", id, err)
	}
	return m
}

// ---------------------------------------------------------------------------
// MediaList
// ---------------------------------------------------------------------------

func TestMediaList_Redirects_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaList(rr, mediaReq(http.MethodGet, "/dashboard/media", &user, nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestMediaList_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "m1", "Song A")
	rr := httptest.NewRecorder()
	h.MediaList(rr, mediaReq(http.MethodGet, "/dashboard/media", &user, &station))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMediaList_HtmxRequest_ReturnsPartial(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "m2", "Song B")

	req := mediaReq(http.MethodGet, "/dashboard/media?q=Song", &user, &station)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.MediaList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestMediaList_WithFilters(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "m3", "Song C")

	req := mediaReq(http.MethodGet, "/dashboard/media?q=Song&genre=Rock&artist=Test+Artist&sort=title&order=asc&duplicates=1", &user, &station)
	rr := httptest.NewRecorder()
	h.MediaList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// MediaTablePartial / MediaGridPartial
// ---------------------------------------------------------------------------

func TestMediaTablePartial_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "m4", "Song D")
	rr := httptest.NewRecorder()
	h.MediaTablePartial(rr, mediaReq(http.MethodGet, "/", &user, &station))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestMediaGridPartial_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "m5", "Song E")
	rr := httptest.NewRecorder()
	h.MediaGridPartial(rr, mediaReq(http.MethodGet, "/", &user, &station))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// MediaSearchJSON
// ---------------------------------------------------------------------------

func TestMediaSearchJSON_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "m6", "Song F")
	rr := httptest.NewRecorder()
	h.MediaSearchJSON(rr, mediaReq(http.MethodGet, "/?q=Song", &user, &station))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var results []map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &results); err != nil {
		t.Fatalf("expected JSON array: %v", err)
	}
}

func TestMediaSearchJSON_IncludeArchive(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "m7", "Song G")
	rr := httptest.NewRecorder()
	h.MediaSearchJSON(rr, mediaReq(http.MethodGet, "/?include_archive=true", &user, &station))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// MediaUploadPage
// ---------------------------------------------------------------------------

func TestMediaUploadPage_Renders(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaUploadPage(rr, mediaReq(http.MethodGet, "/dashboard/media/upload", &user, &station))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// MediaEdit
// ---------------------------------------------------------------------------

func TestMediaEdit_Redirects_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaEdit(rr, mediaReqWithID(http.MethodGet, "/", &user, nil, "id", "no-media"))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestMediaEdit_NotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaEdit(rr, mediaReqWithID(http.MethodGet, "/", &user, &station, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestMediaEdit_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "med-edit-1", "Edit Song")
	rr := httptest.NewRecorder()
	h.MediaEdit(rr, mediaReqWithID(http.MethodGet, "/", &user, &station, "id", "med-edit-1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// MediaUpdate
// ---------------------------------------------------------------------------

func TestMediaUpdate_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaUpdate(rr, mediaReqWithID(http.MethodPost, "/", &user, nil, "id", "no-media"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaUpdate_NotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaUpdate(rr, mediaReqWithID(http.MethodPost, "/", &user, &station, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// buildMediaUpdateMultipart creates a multipart form body for MediaUpdate tests.
func buildMediaUpdateMultipart(t *testing.T, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write multipart field %s: %v", k, err)
		}
	}
	w.Close()
	return &buf, w.FormDataContentType()
}

func TestMediaUpdate_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "med-upd-1", "Original Title")

	body, ct := buildMediaUpdateMultipart(t, map[string]string{
		"title":  "Updated Title",
		"artist": "Updated Artist",
	})
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", ct)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "med-upd-1")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.MediaUpdate(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMediaUpdate_HtmxRedirects(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "med-upd-2", "Title")

	body, ct := buildMediaUpdateMultipart(t, map[string]string{"title": "New Title"})
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("HX-Request", "true")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "med-upd-2")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.MediaUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatal("expected HX-Redirect header")
	}
}

// ---------------------------------------------------------------------------
// MediaDelete
// ---------------------------------------------------------------------------

func TestMediaDelete_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaDelete(rr, mediaReqWithID(http.MethodDelete, "/", &user, nil, "id", "x"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaDelete_NotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaDelete(rr, mediaReqWithID(http.MethodDelete, "/", &user, &station, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestMediaDelete_HappyPath(t *testing.T) {
	// Use cascade test DB which has all tables needed by adminDeleteMediaReferences
	db := newCascadeTestDB(t)
	user := models.User{ID: "u-del", Email: "del@example.com", Password: "x"}
	station := models.Station{ID: "s-del", Name: "Del Station", Active: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.StationUser{
		ID: "su-del", UserID: user.ID, StationID: station.ID, Role: models.StationRoleManager,
	}).Error; err != nil {
		t.Fatalf("create station user: %v", err)
	}

	h, err := NewHandler(db, []byte("test"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	m := models.MediaItem{
		ID: "med-del-1", StationID: station.ID, Title: "Delete Me",
		Path: "del.mp3", AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&m).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}
	rr := httptest.NewRecorder()
	h.MediaDelete(rr, mediaReqWithID(http.MethodDelete, "/", &user, &station, "id", "med-del-1"))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMediaDelete_HtmxRedirects(t *testing.T) {
	db := newCascadeTestDB(t)
	user := models.User{ID: "u-del2", Email: "del2@example.com", Password: "x"}
	station := models.Station{ID: "s-del2", Name: "Del Station 2", Active: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.StationUser{
		ID: "su-del2", UserID: user.ID, StationID: station.ID, Role: models.StationRoleManager,
	}).Error; err != nil {
		t.Fatalf("create station user: %v", err)
	}

	h, err := NewHandler(db, []byte("test"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	m := models.MediaItem{
		ID: "med-del-2", StationID: station.ID, Title: "Delete Me 2",
		Path: "del2.mp3", AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&m).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}
	req := mediaReqWithID(http.MethodDelete, "/", &user, &station, "id", "med-del-2")
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.MediaDelete(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatal("expected HX-Redirect header")
	}
}

// ---------------------------------------------------------------------------
// MediaBulk
// ---------------------------------------------------------------------------

func TestMediaBulk_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	body := `{"action":"delete","ids":["x"]}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaBulk_InvalidJSON(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not-json"))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaBulk_EmptyIDs(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	body := `{"action":"delete","ids":[]}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaBulk_UnknownAction(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	body := `{"action":"unknown","ids":["x"]}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaBulk_Delete(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "bulk-del-1", "Bulk Delete 1")
	seedMedia(t, h, station.ID, "bulk-del-2", "Bulk Delete 2")

	body, _ := json.Marshal(map[string]interface{}{
		"action": "delete",
		"ids":    []string{"bulk-del-1", "bulk-del-2"},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMediaBulk_SetGenre(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "bulk-genre-1", "Genre Song")

	body, _ := json.Marshal(map[string]interface{}{
		"action": "set_genre",
		"ids":    []string{"bulk-genre-1"},
		"value":  "Jazz",
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMediaBulk_ToggleExplicit(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "bulk-expl-1", "Explicit Song")

	body, _ := json.Marshal(map[string]interface{}{
		"action": "toggle_explicit",
		"ids":    []string{"bulk-expl-1"},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMediaBulk_AddToPlaylist_NoPlaylistID(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "bulk-pl-1", "Playlist Song")

	body, _ := json.Marshal(map[string]interface{}{
		"action": "add_to_playlist",
		"ids":    []string{"bulk-pl-1"},
		"value":  "",
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaBulk_AddToPlaylist_PlaylistNotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "bulk-pl-2", "Playlist Song 2")

	body, _ := json.Marshal(map[string]interface{}{
		"action": "add_to_playlist",
		"ids":    []string{"bulk-pl-2"},
		"value":  "nonexistent-playlist",
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaBulk(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestMediaBulk_AddToPlaylist_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "bulk-pl-3", "Playlist Song 3")
	playlist := models.Playlist{
		ID:        "pl-bulk-1",
		StationID: station.ID,
		Name:      "Bulk Test Playlist",
	}
	if err := h.db.Create(&playlist).Error; err != nil {
		t.Fatalf("seed playlist: %v", err)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"action": "add_to_playlist",
		"ids":    []string{"bulk-pl-3"},
		"value":  "pl-bulk-1",
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// MediaGenres
// ---------------------------------------------------------------------------

func TestMediaGenres_Redirects_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaGenres(rr, mediaReq(http.MethodGet, "/dashboard/media/genres", &user, nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestMediaGenres_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	m := seedMedia(t, h, station.ID, "genre-m1", "Genre Track")
	h.db.Model(&m).Update("genre", "Rock")

	rr := httptest.NewRecorder()
	h.MediaGenres(rr, mediaReq(http.MethodGet, "/dashboard/media/genres", &user, &station))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// MediaGenreReassign
// ---------------------------------------------------------------------------

func TestMediaGenreReassign_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("old_genre=Rock&new_genre=Jazz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaGenreReassign(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaGenreReassign_MissingOldGenre(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("new_genre=Jazz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaGenreReassign(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaGenreReassign_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	m := seedMedia(t, h, station.ID, "genre-r1", "Reassign Track")
	h.db.Model(&m).Update("genre", "Rock")

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("old_genre=Rock&new_genre=Pop"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaGenreReassign(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// MediaWaveform
// ---------------------------------------------------------------------------

func TestMediaWaveform_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaWaveform(rr, mediaReqWithID(http.MethodGet, "/", &user, nil, "id", "x"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaWaveform_NotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaWaveform(rr, mediaReqWithID(http.MethodGet, "/", &user, &station, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestMediaWaveform_NoWaveformData(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "wav-m1", "Waveform Track")
	rr := httptest.NewRecorder()
	h.MediaWaveform(rr, mediaReqWithID(http.MethodGet, "/", &user, &station, "id", "wav-m1"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestMediaWaveform_WithData(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "wav-m2", "Waveform Track 2")
	h.db.Model(&models.MediaItem{}).Where("id = ?", "wav-m2").Update("waveform", []byte{0x01, 0x02, 0x03})
	rr := httptest.NewRecorder()
	h.MediaWaveform(rr, mediaReqWithID(http.MethodGet, "/", &user, &station, "id", "wav-m2"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"data"`) {
		t.Fatalf("expected JSON waveform response, got %s", rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// MediaArtwork
// ---------------------------------------------------------------------------

func TestMediaArtwork_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaArtwork(rr, mediaReqWithID(http.MethodGet, "/", &user, nil, "id", "x"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaArtwork_NotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaArtwork(rr, mediaReqWithID(http.MethodGet, "/", &user, &station, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestMediaArtwork_NoArtworkData(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "art-m1", "Artwork Track")
	rr := httptest.NewRecorder()
	h.MediaArtwork(rr, mediaReqWithID(http.MethodGet, "/", &user, &station, "id", "art-m1"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestMediaArtwork_WithData(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "art-m2", "Artwork Track 2")
	fakeJPEG := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}
	h.db.Model(&models.MediaItem{}).Where("id = ?", "art-m2").Updates(map[string]interface{}{
		"artwork":      fakeJPEG,
		"artwork_mime": "image/jpeg",
	})
	rr := httptest.NewRecorder()
	h.MediaArtwork(rr, mediaReqWithID(http.MethodGet, "/", &user, &station, "id", "art-m2"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("expected image/jpeg content type, got %s", rr.Header().Get("Content-Type"))
	}
}

// ---------------------------------------------------------------------------
// MediaStream
// ---------------------------------------------------------------------------

func TestMediaStream_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaStream(rr, mediaReqWithID(http.MethodGet, "/", &user, nil, "id", "x"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaStream_NotFound(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaStream(rr, mediaReqWithID(http.MethodGet, "/", &user, &station, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestMediaStream_NoPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	m := models.MediaItem{
		ID:            "stream-no-path",
		StationID:     station.ID,
		Title:         "No Path",
		AnalysisState: models.AnalysisComplete,
	}
	if err := h.db.Create(&m).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}
	rr := httptest.NewRecorder()
	h.MediaStream(rr, mediaReqWithID(http.MethodGet, "/", &user, &station, "id", "stream-no-path"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestMediaStream_FileNotOnDisk(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	m := models.MediaItem{
		ID:            "stream-missing-file",
		StationID:     station.ID,
		Title:         "Missing File",
		Path:          "nonexistent/path/file.mp3",
		AnalysisState: models.AnalysisComplete,
	}
	if err := h.db.Create(&m).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}
	rr := httptest.NewRecorder()
	h.MediaStream(rr, mediaReqWithID(http.MethodGet, "/", &user, &station, "id", "stream-missing-file"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// MediaDuplicates
// ---------------------------------------------------------------------------

func TestMediaDuplicates_Redirects_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaDuplicates(rr, mediaReq(http.MethodGet, "/dashboard/media/duplicates", &user, nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestMediaDuplicates_EmptyStation(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaDuplicates(rr, mediaReq(http.MethodGet, "/dashboard/media/duplicates", &user, &station))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMediaDuplicates_WithHashDuplicates(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	m1 := models.MediaItem{
		ID: "dup-hash-1", StationID: station.ID, Title: "Dup Track", Artist: "Artist",
		Path: "dup1.mp3", ContentHash: "abc123hash", AnalysisState: models.AnalysisComplete,
	}
	m2 := models.MediaItem{
		ID: "dup-hash-2", StationID: station.ID, Title: "Dup Track Copy", Artist: "Artist",
		Path: "dup2.mp3", ContentHash: "abc123hash", AnalysisState: models.AnalysisComplete,
	}
	if err := h.db.Create(&m1).Error; err != nil {
		t.Fatalf("seed dup1: %v", err)
	}
	if err := h.db.Create(&m2).Error; err != nil {
		t.Fatalf("seed dup2: %v", err)
	}
	rr := httptest.NewRecorder()
	h.MediaDuplicates(rr, mediaReq(http.MethodGet, "/dashboard/media/duplicates", &user, &station))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// MediaPurgeDuplicates
// ---------------------------------------------------------------------------

func TestMediaPurgeDuplicates_Returns400_WhenNoStation(t *testing.T) {
	h, _, user, _ := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("ids=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaPurgeDuplicates(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaPurgeDuplicates_NoPermission(t *testing.T) {
	h, _, _, station := newMediaDetailTestHandler(t)
	noPermsUser := models.User{ID: "noperms-user", Email: "noperms@example.com", Password: "x"}
	if err := h.db.Create(&noPermsUser).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("ids=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &noPermsUser)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaPurgeDuplicates(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestMediaPurgeDuplicates_EmptyIDs(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaPurgeDuplicates(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
}

func TestMediaPurgeDuplicates_HappyPath(t *testing.T) {
	h, _, user, station := newMediaDetailTestHandler(t)
	seedMedia(t, h, station.ID, "purge-m1", "Purge Track")

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("ids=purge-m1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaPurgeDuplicates(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// newMediaAnalysisTestHandler returns a handler+DB with analysis_jobs migrated.
func newMediaAnalysisTestHandler(t *testing.T) (*Handler, models.User, models.Station) {
	t.Helper()
	// Use cascade DB which includes analysis_jobs and mount_playout_states
	db := newCascadeTestDB(t)
	if err := db.AutoMigrate(&models.AnalysisJob{}); err != nil {
		t.Fatalf("migrate analysis_jobs: %v", err)
	}

	user := models.User{ID: "u-ana", Email: "ana@example.com", Password: "x"}
	station := models.Station{ID: "s-ana", Name: "Ana Station", Active: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.StationUser{
		ID: "su-ana", UserID: user.ID, StationID: station.ID, Role: models.StationRoleManager,
	}).Error; err != nil {
		t.Fatalf("create station user: %v", err)
	}

	h, err := NewHandler(db, []byte("test"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h, user, station
}

// ---------------------------------------------------------------------------
// MediaReanalyzeDurations
// ---------------------------------------------------------------------------

func TestMediaReanalyzeDurations_Returns400_WhenNoStation(t *testing.T) {
	h, user, _ := newMediaAnalysisTestHandler(t)
	req := mediaReq(http.MethodPost, "/", &user, nil)
	rr := httptest.NewRecorder()
	h.MediaReanalyzeDurations(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaReanalyzeDurations_NoPermission(t *testing.T) {
	h, _, station := newMediaAnalysisTestHandler(t)
	noPermsUser := models.User{ID: "noperms-user-2", Email: "noperms2@example.com", Password: "x"}
	if err := h.db.Create(&noPermsUser).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	ctx := context.WithValue(req.Context(), ctxKeyUser, &noPermsUser)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.MediaReanalyzeDurations(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestMediaReanalyzeDurations_NoMedia_ReturnsOK(t *testing.T) {
	h, user, station := newMediaAnalysisTestHandler(t)
	req := mediaReq(http.MethodPost, "/", &user, &station)
	rr := httptest.NewRecorder()
	h.MediaReanalyzeDurations(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMediaReanalyzeDurations_WithMedia(t *testing.T) {
	h, user, station := newMediaAnalysisTestHandler(t)
	m := models.MediaItem{
		ID: "reanalyze-m1", StationID: station.ID, Title: "Reanalyze Track",
		Path: "reanalyze.mp3", AnalysisState: models.AnalysisComplete,
	}
	if err := h.db.Create(&m).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}
	req := mediaReq(http.MethodPost, "/", &user, &station)
	rr := httptest.NewRecorder()
	h.MediaReanalyzeDurations(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// MediaReanalyzeDurationsStatus
// ---------------------------------------------------------------------------

func TestMediaReanalyzeDurationsStatus_Returns400_WhenNoStation(t *testing.T) {
	h, user, _ := newMediaAnalysisTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaReanalyzeDurationsStatus(rr, mediaReq(http.MethodGet, "/", &user, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaReanalyzeDurationsStatus_NoSinceParam(t *testing.T) {
	h, user, station := newMediaAnalysisTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaReanalyzeDurationsStatus(rr, mediaReq(http.MethodGet, "/", &user, &station))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMediaReanalyzeDurationsStatus_InvalidSince(t *testing.T) {
	h, user, station := newMediaAnalysisTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaReanalyzeDurationsStatus(rr, mediaReq(http.MethodGet, "/?since=not-a-time", &user, &station))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaReanalyzeDurationsStatus_ValidSince(t *testing.T) {
	h, user, station := newMediaAnalysisTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaReanalyzeDurationsStatus(rr, mediaReq(http.MethodGet, "/?since=2026-01-01T00:00:00Z", &user, &station))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// MediaReanalyzeDurationsCurrentStatus
// ---------------------------------------------------------------------------

func TestMediaReanalyzeDurationsCurrentStatus_Returns400_WhenNoStation(t *testing.T) {
	h, user, _ := newMediaAnalysisTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaReanalyzeDurationsCurrentStatus(rr, mediaReq(http.MethodGet, "/", &user, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMediaReanalyzeDurationsCurrentStatus_HappyPath(t *testing.T) {
	h, user, station := newMediaAnalysisTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaReanalyzeDurationsCurrentStatus(rr, mediaReq(http.MethodGet, "/", &user, &station))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMediaReanalyzeDurationsCurrentStatus_CompactView(t *testing.T) {
	h, user, station := newMediaAnalysisTestHandler(t)
	rr := httptest.NewRecorder()
	h.MediaReanalyzeDurationsCurrentStatus(rr, mediaReq(http.MethodGet, "/?view=compact", &user, &station))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// buildPageNumbers (unit test)
// ---------------------------------------------------------------------------

func TestBuildPageNumbers_SinglePage(t *testing.T) {
	pages := buildPageNumbers(1, 1)
	if len(pages) != 0 {
		t.Fatalf("expected no pages for total=1, got %v", pages)
	}
}

func TestBuildPageNumbers_TwoPages(t *testing.T) {
	pages := buildPageNumbers(1, 2)
	if len(pages) == 0 {
		t.Fatal("expected pages for total=2")
	}
}

func TestBuildPageNumbers_LargeRange(t *testing.T) {
	pages := buildPageNumbers(5, 20)
	if len(pages) == 0 {
		t.Fatal("expected pages")
	}
	hasFirst := false
	hasLast := false
	for _, p := range pages {
		if p == 1 {
			hasFirst = true
		}
		if p == 20 {
			hasLast = true
		}
	}
	if !hasFirst || !hasLast {
		t.Fatalf("expected first and last page in results, got %v", pages)
	}
}
