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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ---------------------------------------------------------------------------
// ensureAtLeastOneAdmin
// ---------------------------------------------------------------------------

func TestEnsureAtLeastOneAdmin_WithAdminExists_ReturnsEmpty(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	// Exclude a different ID — admin still exists
	msg := h.ensureAtLeastOneAdmin([]string{"some-other-id"})
	if msg != "" {
		t.Fatalf("expected empty msg (admin still exists), got: %q", msg)
	}

	// Exclude the actual admin — no admins remain
	msg = h.ensureAtLeastOneAdmin([]string{admin.ID})
	if msg == "" {
		t.Fatal("expected error message when no admins remain")
	}
}

func TestEnsureAtLeastOneAdmin_NoAdmins_ReturnsError(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	// No users created at all
	msg := h.ensureAtLeastOneAdmin([]string{})
	if msg == "" {
		t.Fatal("expected error message when no admins exist")
	}
}

func TestEnsureAtLeastOneAdmin_MultipleAdmins_ReturnsEmpty(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin1 := seedAdminUser(t, db)
	// Second admin
	admin2 := models.User{ID: "admin2", Email: "admin2@example.com", Password: "x", PlatformRole: models.PlatformRoleAdmin}
	if err := db.Create(&admin2).Error; err != nil {
		t.Fatalf("seed admin2: %v", err)
	}

	// Exclude admin1 — admin2 still exists
	msg := h.ensureAtLeastOneAdmin([]string{admin1.ID})
	if msg != "" {
		t.Fatalf("expected empty msg (admin2 still exists), got: %q", msg)
	}
}

// ---------------------------------------------------------------------------
// promoteFirstUserToAdmin
// ---------------------------------------------------------------------------

func TestPromoteFirstUserToAdmin_NoAdmins_PromotesFirstUser(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	user := seedRegularUser(t, db)

	h.promoteFirstUserToAdmin()

	var updated models.User
	db.First(&updated, "id = ?", user.ID)
	if updated.PlatformRole != models.PlatformRoleAdmin {
		t.Fatalf("expected user to be promoted to admin, got: %q", updated.PlatformRole)
	}
}

func TestPromoteFirstUserToAdmin_AdminAlreadyExists_NoChange(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	user := seedRegularUser(t, db)

	h.promoteFirstUserToAdmin()

	// Admin should remain admin
	var updatedAdmin models.User
	db.First(&updatedAdmin, "id = ?", admin.ID)
	if updatedAdmin.PlatformRole != models.PlatformRoleAdmin {
		t.Fatal("admin should remain admin")
	}

	// Regular user should remain regular
	var updatedUser models.User
	db.First(&updatedUser, "id = ?", user.ID)
	if updatedUser.PlatformRole != models.PlatformRoleUser {
		t.Fatalf("regular user should not be promoted, got: %q", updatedUser.PlatformRole)
	}
}

func TestPromoteFirstUserToAdmin_NoUsers_NoError(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)

	// Should not panic or error when no users exist
	h.promoteFirstUserToAdmin()
}

// ---------------------------------------------------------------------------
// AdminMediaTogglePublic
// ---------------------------------------------------------------------------

func TestAdminMediaTogglePublic_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	user := seedRegularUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminMediaTogglePublic(rr, adminReqWithID(http.MethodPost, "/", &user, "id", "m1"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminMediaTogglePublic_MediaNotFound_Returns404(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminMediaTogglePublic(rr, adminReqWithID(http.MethodPost, "/", &admin, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestAdminMediaTogglePublic_Admin_TogglesAndRedirects(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "s1", "Station")

	media := models.MediaItem{
		ID:            "mtp1",
		StationID:     "s1",
		Title:         "Track",
		Path:          "s1/track.mp3",
		AnalysisState: "complete",
		ShowInArchive: true,
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	rr := httptest.NewRecorder()
	h.AdminMediaTogglePublic(rr, adminReqWithID(http.MethodPost, "/", &admin, "id", "mtp1"))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}

	var updated models.MediaItem
	db.First(&updated, "id = ?", "mtp1")
	if updated.ShowInArchive {
		t.Fatal("expected ShowInArchive to be toggled to false")
	}
}

func TestAdminMediaTogglePublic_HXRequest_SetsHXRefresh(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "s1tx", "Station")

	media := models.MediaItem{
		ID:            "mtp2",
		StationID:     "s1tx",
		Title:         "Track HX",
		Path:          "s1tx/track.mp3",
		AnalysisState: "complete",
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("HX-Request", "true")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "mtp2")
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.AdminMediaTogglePublic(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HX, got %d", rr.Code)
	}
	if rr.Header().Get("HX-Refresh") != "true" {
		t.Fatal("expected HX-Refresh: true header")
	}
}

// ---------------------------------------------------------------------------
// AdminMediaMove
// ---------------------------------------------------------------------------

func TestAdminMediaMove_NonAdmin_Returns403(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	user := seedRegularUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminMediaMove(rr, adminReqWithID(http.MethodPost, "/", &user, "id", "m1"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminMediaMove_MediaNotFound_Returns404(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminMediaMove(rr, adminReqWithID(http.MethodPost, "/", &admin, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestAdminMediaMove_NoTargetStation_Returns400(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "src1", "Source Station")

	media := models.MediaItem{
		ID:            "mmv1",
		StationID:     "src1",
		Title:         "Move Track",
		Path:          "src1/track.mp3",
		AnalysisState: "complete",
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	form := url.Values{"station_id": {""}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "mmv1")
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.AdminMediaMove(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestAdminMediaMove_TargetStationNotFound_Returns400(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "src2", "Source Station 2")

	media := models.MediaItem{
		ID:            "mmv2",
		StationID:     "src2",
		Title:         "Move Track 2",
		Path:          "src2/track.mp3",
		AnalysisState: "complete",
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	form := url.Values{"station_id": {"nonexistent-station"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "mmv2")
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.AdminMediaMove(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestAdminMediaMove_Valid_MovesMediaAndRedirects(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "src3", "Source Station 3")
	seedStation(t, db, "dst3", "Destination Station 3")

	media := models.MediaItem{
		ID:            "mmv3",
		StationID:     "src3",
		Title:         "Move Track 3",
		Path:          "src3/track.mp3",
		AnalysisState: "complete",
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	form := url.Values{"station_id": {"dst3"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "mmv3")
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.AdminMediaMove(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}

	var updated models.MediaItem
	db.First(&updated, "id = ?", "mmv3")
	if updated.StationID != "dst3" {
		t.Fatalf("expected StationID to be 'dst3', got %q", updated.StationID)
	}
}

func TestAdminMediaMove_HXRequest_SetsHXRefresh(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "src4", "Source Station 4")
	seedStation(t, db, "dst4", "Destination Station 4")

	media := models.MediaItem{
		ID:            "mmv4",
		StationID:     "src4",
		Title:         "Move Track HX",
		Path:          "src4/track.mp3",
		AnalysisState: "complete",
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	form := url.Values{"station_id": {"dst4"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "mmv4")
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.AdminMediaMove(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HX, got %d", rr.Code)
	}
	if rr.Header().Get("HX-Refresh") != "true" {
		t.Fatal("expected HX-Refresh: true header")
	}
}

// newAdminMediaTestDB returns an in-memory SQLite DB with the tables needed
// by adminDeleteMediaReferences / adminDeleteMediaByIDsTx (includes schedule_entries,
// playlist_items, mount_playout_states, play_histories, etc.)
func newAdminMediaTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// Reuse the full cascade test DB — it already has all the tables we need.
	return newCascadeTestDB(t)
}

// ---------------------------------------------------------------------------
// AdminMediaDelete (happy path / not found)
// ---------------------------------------------------------------------------

func TestAdminMediaDelete_Admin_DeletesAndRedirects(t *testing.T) {
	db := newAdminMediaTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "sdel1", "Delete Station")

	media := models.MediaItem{
		ID:            "mdel1",
		StationID:     "sdel1",
		Title:         "Delete Track",
		Path:          "sdel1/track.mp3",
		AnalysisState: "complete",
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	rr := httptest.NewRecorder()
	h.AdminMediaDelete(rr, adminReqWithID(http.MethodPost, "/", &admin, "id", "mdel1"))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}

	var count int64
	db.Model(&models.MediaItem{}).Where("id = ?", "mdel1").Count(&count)
	if count != 0 {
		t.Fatal("media item should be deleted")
	}
}

func TestAdminMediaDelete_NotFound_Returns404(t *testing.T) {
	db := newAdminMediaTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminMediaDelete(rr, adminReqWithID(http.MethodPost, "/", &admin, "id", "nonexistent"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestAdminMediaDelete_HXRequest_SetsHXRefresh(t *testing.T) {
	db := newAdminMediaTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "sdel2", "Delete Station 2")

	media := models.MediaItem{
		ID:            "mdel2",
		StationID:     "sdel2",
		Title:         "Delete Track HX",
		Path:          "sdel2/track.mp3",
		AnalysisState: "complete",
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("HX-Request", "true")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "mdel2")
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.AdminMediaDelete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HX, got %d", rr.Code)
	}
	if rr.Header().Get("HX-Refresh") != "true" {
		t.Fatal("expected HX-Refresh: true header")
	}
}

// ---------------------------------------------------------------------------
// AdminMediaPurgeDuplicates (exercises computeSHA256File, adminDeleteMediaByIDs,
// adminDeleteMediaByIDsTx, adminRemapMediaReferences, deduplicatePlaylistItems,
// adminDeleteMediaReferences)
// ---------------------------------------------------------------------------

func TestAdminMediaPurgeDuplicates_NoIDs_Redirects(t *testing.T) {
	db := newAdminMediaTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/admin/media/duplicates/purge", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))

	rr := httptest.NewRecorder()
	h.AdminMediaPurgeDuplicates(rr, req)

	// Should redirect to duplicates page with error
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "error") {
		t.Fatalf("expected error in redirect location, got %q", loc)
	}
}

func TestAdminMediaPurgeDuplicates_IDsWithNoHashes_Redirects(t *testing.T) {
	db := newAdminMediaTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "sdup1", "Dup Station")

	// Create media item without content hash
	media := models.MediaItem{
		ID:            "noHash1",
		StationID:     "sdup1",
		Title:         "No Hash Track",
		Path:          "sdup1/track.mp3",
		AnalysisState: "complete",
		ContentHash:   "",
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	form := url.Values{"ids": {"noHash1"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))

	rr := httptest.NewRecorder()
	h.AdminMediaPurgeDuplicates(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "error") {
		t.Fatalf("expected error redirect, got %q", loc)
	}
}

func TestAdminMediaPurgeDuplicates_AllCopiesSelected_Redirects(t *testing.T) {
	db := newAdminMediaTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "sdup2", "Dup Station 2")

	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	media1 := models.MediaItem{
		ID:            "dup1a",
		StationID:     "sdup2",
		Title:         "Dup Track",
		Path:          "sdup2/track1.mp3",
		AnalysisState: "complete",
		ContentHash:   hash,
	}
	media2 := models.MediaItem{
		ID:            "dup1b",
		StationID:     "sdup2",
		Title:         "Dup Track Copy",
		Path:          "sdup2/track2.mp3",
		AnalysisState: "complete",
		ContentHash:   hash,
	}
	if err := db.Create(&media1).Error; err != nil {
		t.Fatalf("create media1: %v", err)
	}
	if err := db.Create(&media2).Error; err != nil {
		t.Fatalf("create media2: %v", err)
	}

	// Try to purge BOTH — should fail (no survivors)
	form := url.Values{"ids": {"dup1a", "dup1b"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))

	rr := httptest.NewRecorder()
	h.AdminMediaPurgeDuplicates(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "error") {
		t.Fatalf("expected error redirect (all copies selected), got %q", loc)
	}
}

func TestAdminMediaPurgeDuplicates_PurgesOneDuplicate_Succeeds(t *testing.T) {
	db := newAdminMediaTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "sdup3", "Dup Station 3")

	hash := "deadbeef1234567890abcdef1234567890abcdef1234567890abcdef12345678"
	media1 := models.MediaItem{
		ID:            "purge1a",
		StationID:     "sdup3",
		Title:         "Keep Track",
		Path:          "sdup3/keep.mp3",
		AnalysisState: "complete",
		ContentHash:   hash,
	}
	media2 := models.MediaItem{
		ID:            "purge1b",
		StationID:     "sdup3",
		Title:         "Delete Track",
		Path:          "sdup3/delete.mp3",
		AnalysisState: "complete",
		ContentHash:   hash,
	}
	if err := db.Create(&media1).Error; err != nil {
		t.Fatalf("create media1: %v", err)
	}
	if err := db.Create(&media2).Error; err != nil {
		t.Fatalf("create media2: %v", err)
	}

	// Purge only media2 — media1 survives
	form := url.Values{"ids": {"purge1b"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))

	rr := httptest.NewRecorder()
	h.AdminMediaPurgeDuplicates(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "notice") {
		t.Fatalf("expected success notice redirect, got %q", loc)
	}
	decodedLoc, _ := url.QueryUnescape(loc)
	if !strings.Contains(decodedLoc, "Purged") {
		t.Fatalf("expected 'Purged' in redirect message, got %q", decodedLoc)
	}

	// media1 should still exist
	var count int64
	db.Model(&models.MediaItem{}).Where("id = ?", "purge1a").Count(&count)
	if count == 0 {
		t.Fatal("survivor media1 should still exist")
	}

	// media2 should be gone
	db.Model(&models.MediaItem{}).Where("id = ?", "purge1b").Count(&count)
	if count != 0 {
		t.Fatal("duplicate media2 should be deleted")
	}
}

func TestAdminMediaPurgeDuplicates_WithPlaylistRemap_Succeeds(t *testing.T) {
	db := newAdminMediaTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "sdup4", "Dup Station 4")

	hash := "cafebabe1234567890abcdef1234567890abcdef1234567890abcdef12345678"
	media1 := models.MediaItem{
		ID:            "remap1a",
		StationID:     "sdup4",
		Title:         "Keep",
		Path:          "sdup4/keep.mp3",
		AnalysisState: "complete",
		ContentHash:   hash,
	}
	media2 := models.MediaItem{
		ID:            "remap1b",
		StationID:     "sdup4",
		Title:         "Delete",
		Path:          "sdup4/delete.mp3",
		AnalysisState: "complete",
		ContentHash:   hash,
	}
	if err := db.Create(&media1).Error; err != nil {
		t.Fatalf("create media1: %v", err)
	}
	if err := db.Create(&media2).Error; err != nil {
		t.Fatalf("create media2: %v", err)
	}

	// Create a playlist referencing media2
	playlist := models.Playlist{ID: "pl4", StationID: "sdup4", Name: "Test Playlist"}
	if err := db.Create(&playlist).Error; err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	pitem := models.PlaylistItem{ID: "pi4", PlaylistID: "pl4", MediaID: "remap1b", Position: 1}
	if err := db.Create(&pitem).Error; err != nil {
		t.Fatalf("create playlist item: %v", err)
	}

	// Purge media2 — references should be remapped to media1
	form := url.Values{"ids": {"remap1b"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))

	rr := httptest.NewRecorder()
	h.AdminMediaPurgeDuplicates(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}

	// Playlist item should now point to media1
	var updatedItem models.PlaylistItem
	db.First(&updatedItem, "id = ?", "pi4")
	if updatedItem.MediaID != "remap1a" {
		t.Fatalf("expected playlist item remapped to remap1a, got %q", updatedItem.MediaID)
	}
}

// ---------------------------------------------------------------------------
// computeSHA256File (direct test)
// ---------------------------------------------------------------------------

func TestComputeSHA256File_ValidFile_ReturnsHash(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.bin")
	if err := os.WriteFile(filePath, []byte("hello grimnir"), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	hash, err := computeSHA256File(filePath)
	if err != nil {
		t.Fatalf("computeSHA256File: %v", err)
	}
	if len(hash) != 64 {
		t.Fatalf("expected 64-char hex hash, got %d chars: %s", len(hash), hash)
	}
}

func TestComputeSHA256File_NonexistentFile_ReturnsError(t *testing.T) {
	_, err := computeSHA256File("/nonexistent/path/file.mp3")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestComputeSHA256File_Deterministic(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "det.bin")
	content := []byte("deterministic content")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	h1, _ := computeSHA256File(filePath)
	h2, _ := computeSHA256File(filePath)
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %s vs %s", h1, h2)
	}
}

// ---------------------------------------------------------------------------
// AdminUserUpdate (increases coverage from 10.3%)
// (403 case already covered in pages_admin_test.go)
// ---------------------------------------------------------------------------

func TestAdminUserUpdate_UserNotFound_Returns404(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminUserUpdate(rr, adminReqWithID(http.MethodPost, "/", &admin, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestAdminUserUpdate_ValidRole_Redirects(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	other := seedRegularUser(t, db)

	form := url.Values{"platform_role": {string(models.PlatformRoleAdmin)}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", other.ID)
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.AdminUserUpdate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}
	var updated models.User
	db.First(&updated, "id = ?", other.ID)
	if updated.PlatformRole != models.PlatformRoleAdmin {
		t.Fatalf("expected admin role, got %q", updated.PlatformRole)
	}
}

func TestAdminUserUpdate_InvalidRole_Returns400(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	other := seedRegularUser(t, db)

	form := url.Values{"platform_role": {"superuser"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", other.ID)
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.AdminUserUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid role, got %d", rr.Code)
	}
}

func TestAdminUserUpdate_DemoteLastAdmin_Returns400(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	// Create a second admin to update
	targetAdmin := models.User{ID: "admin2x", Email: "admin2x@example.com", Password: "x", PlatformRole: models.PlatformRoleAdmin}
	if err := db.Create(&targetAdmin).Error; err != nil {
		t.Fatalf("create target admin: %v", err)
	}

	// Demote admin1 first so only admin2x remains, then demote admin2x
	db.Model(&models.User{}).Where("id = ?", admin.ID).Update("platform_role", models.PlatformRoleUser)

	form := url.Values{"platform_role": {string(models.PlatformRoleUser)}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", targetAdmin.ID)
	// Use admin as the requester (even though they're now a user in DB, we test via context)
	// Actually we need admin to still be admin in context for the auth check
	adminCtx := models.User{ID: admin.ID, Email: admin.Email, PlatformRole: models.PlatformRoleAdmin}
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &adminCtx,
	))

	rr := httptest.NewRecorder()
	h.AdminUserUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 demoting last admin, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminUserUpdate_HXRequest_SetsHXRedirect(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	other := seedRegularUser(t, db)

	form := url.Values{"platform_role": {string(models.PlatformRoleMod)}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", other.ID)
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.AdminUserUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HX, got %d", rr.Code)
	}
	if rr.Header().Get("HX-Redirect") == "" {
		t.Fatal("expected HX-Redirect header")
	}
}

// ---------------------------------------------------------------------------
// AdminUserResetPassword (increases coverage from 11.4%)
// ---------------------------------------------------------------------------

func TestAdminUserResetPassword_TooShortPassword_Returns400(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	other := seedRegularUser(t, db)

	form := url.Values{"new_password": {"short"}, "confirm_password": {"short"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", other.ID)
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.AdminUserResetPassword(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestAdminUserResetPassword_MismatchedPasswords_Returns400(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	other := seedRegularUser(t, db)

	form := url.Values{"new_password": {"password123"}, "confirm_password": {"password456"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", other.ID)
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.AdminUserResetPassword(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestAdminUserResetPassword_Valid_Succeeds(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	other := seedRegularUser(t, db)

	form := url.Values{"new_password": {"newpassword123"}, "confirm_password": {"newpassword123"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", other.ID)
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.AdminUserResetPassword(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "success") {
		t.Fatalf("expected success message, got: %s", rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// AdminUserDelete (increases coverage from 16.7%)
// ---------------------------------------------------------------------------

func TestAdminUserDelete_SelfDelete_Returns400(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminUserDelete(rr, adminReqWithID(http.MethodPost, "/", &admin, "id", admin.ID))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for self-delete, got %d", rr.Code)
	}
}

func TestAdminUserDelete_UserNotFound_Returns404(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminUserDelete(rr, adminReqWithID(http.MethodPost, "/", &admin, "id", "nonexistent"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestAdminUserDelete_RegularUser_Redirects(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	other := seedRegularUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminUserDelete(rr, adminReqWithID(http.MethodPost, "/", &admin, "id", other.ID))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rr.Code, rr.Body.String())
	}

	var count int64
	db.Model(&models.User{}).Where("id = ?", other.ID).Count(&count)
	if count != 0 {
		t.Fatal("expected user to be deleted")
	}
}

func TestAdminUserDelete_LastAdmin_Returns400(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	// Create second admin user to delete
	adminToDelete := models.User{ID: "admin-del", Email: "admindel@example.com", Password: "x", PlatformRole: models.PlatformRoleAdmin}
	if err := db.Create(&adminToDelete).Error; err != nil {
		t.Fatalf("create adminToDelete: %v", err)
	}

	// Demote admin1 in DB so only adminToDelete is admin
	db.Model(&models.User{}).Where("id = ?", admin.ID).Update("platform_role", models.PlatformRoleUser)

	adminCtx := models.User{ID: admin.ID, Email: admin.Email, PlatformRole: models.PlatformRoleAdmin}
	rr := httptest.NewRecorder()
	h.AdminUserDelete(rr, adminReqWithID(http.MethodPost, "/", &adminCtx, "id", adminToDelete.ID))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for last admin delete, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminUserDelete_HXRequest_SetsHXRefresh(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	other := seedRegularUser(t, db)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("HX-Request", "true")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", other.ID)
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &admin,
	))

	rr := httptest.NewRecorder()
	h.AdminUserDelete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for HX, got %d", rr.Code)
	}
	if rr.Header().Get("HX-Refresh") != "true" {
		t.Fatal("expected HX-Refresh: true header")
	}
}

func TestAdminUserDelete_AdminWithOtherAdmins_Succeeds(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	// Seed a second admin to delete — ensureAtLeastOneAdmin passes because admin still exists
	admin2 := models.User{ID: "admin-del2", Email: "admindel2@example.com", Password: "x", PlatformRole: models.PlatformRoleAdmin}
	if err := db.Create(&admin2).Error; err != nil {
		t.Fatalf("create admin2: %v", err)
	}

	rr := httptest.NewRecorder()
	h.AdminUserDelete(rr, adminReqWithID(http.MethodDelete, "/", &admin, "id", admin2.ID))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 deleting second admin when first still exists, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// AdminMediaList (increases coverage from 5.6%)
// ---------------------------------------------------------------------------

func TestAdminMediaList_Admin_Renders200(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	h.AdminMediaList(rr, adminReq(http.MethodGet, "/dashboard/admin/media", &admin))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminMediaList_Admin_WithQueryFilter_Renders200(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "smedia1", "Media Station")
	if err := db.Create(&models.MediaItem{
		ID:            "ml1",
		StationID:     "smedia1",
		Title:         "Searchable Track",
		Path:          "smedia1/track.mp3",
		AnalysisState: "complete",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/admin/media?q=Searchable&station=smedia1&public=true&sort=title&order=asc", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	h.AdminMediaList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminMediaList_Admin_WithPublicFalseFilter_Renders200(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/admin/media?public=false", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	h.AdminMediaList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for public=false filter, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// AdminUsersBulk - additional actions (set_role_mod, set_role_user, delete)
// ---------------------------------------------------------------------------

func TestAdminUsersBulk_SetRoleMod_Succeeds(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	other := seedRegularUser(t, db)

	body, _ := json.Marshal(BulkRequest{IDs: []string{other.ID}, Action: "set_role_mod"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminUsersBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var updated models.User
	db.First(&updated, "id = ?", other.ID)
	if updated.PlatformRole != models.PlatformRoleMod {
		t.Fatalf("expected mod role, got %q", updated.PlatformRole)
	}
}

func TestAdminUsersBulk_SetRoleUser_Succeeds(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	// Create a second admin that we'll demote (admin stays as primary admin)
	target := models.User{ID: "admin3", Email: "admin3@example.com", Password: "x", PlatformRole: models.PlatformRoleAdmin}
	if err := db.Create(&target).Error; err != nil {
		t.Fatalf("create target: %v", err)
	}

	body, _ := json.Marshal(BulkRequest{IDs: []string{target.ID}, Action: "set_role_user"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminUsersBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminUsersBulk_DeleteUser_Succeeds(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	other := seedRegularUser(t, db)

	body, _ := json.Marshal(BulkRequest{IDs: []string{other.ID}, Action: "delete"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminUsersBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var count int64
	db.Model(&models.User{}).Where("id = ?", other.ID).Count(&count)
	if count != 0 {
		t.Fatal("user should be deleted")
	}
}

func TestAdminUsersBulk_UnknownAction_Returns400(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	other := seedRegularUser(t, db)

	body, _ := json.Marshal(BulkRequest{IDs: []string{other.ID}, Action: "bogus_action"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminUsersBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// AdminStationsBulk - additional actions (deactivate, make_public, make_private, approve, unapprove)
// ---------------------------------------------------------------------------

func TestAdminStationsBulk_Admin_DeactivatesStations(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "sbulk1", "Bulk Deactivate Station")

	body, _ := json.Marshal(BulkRequest{IDs: []string{"sbulk1"}, Action: "deactivate"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminStationsBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var updated models.Station
	db.First(&updated, "id = ?", "sbulk1")
	if updated.Active {
		t.Fatal("expected station to be inactive")
	}
}

func TestAdminStationsBulk_Admin_MakesPublic(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "sbulk2", "Bulk Public Station")

	body, _ := json.Marshal(BulkRequest{IDs: []string{"sbulk2"}, Action: "make_public"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminStationsBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var updated models.Station
	db.First(&updated, "id = ?", "sbulk2")
	if !updated.Public {
		t.Fatal("expected station to be public")
	}
}

func TestAdminStationsBulk_Admin_MakesPrivate(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	// Create station that's already public
	s := models.Station{ID: "sbulk3", Name: "Bulk Private Station", Active: true, Public: true}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	body, _ := json.Marshal(BulkRequest{IDs: []string{"sbulk3"}, Action: "make_private"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminStationsBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var updated models.Station
	db.First(&updated, "id = ?", "sbulk3")
	if updated.Public {
		t.Fatal("expected station to be private")
	}
}

func TestAdminStationsBulk_Admin_Approves(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "sbulk4", "Approve Station")

	body, _ := json.Marshal(BulkRequest{IDs: []string{"sbulk4"}, Action: "approve"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminStationsBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var updated models.Station
	db.First(&updated, "id = ?", "sbulk4")
	if !updated.Approved {
		t.Fatal("expected station to be approved")
	}
}

func TestAdminStationsBulk_Admin_Unapproves(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	s := models.Station{ID: "sbulk5", Name: "Unapprove Station", Active: true, Approved: true}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	body, _ := json.Marshal(BulkRequest{IDs: []string{"sbulk5"}, Action: "unapprove"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminStationsBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var updated models.Station
	db.First(&updated, "id = ?", "sbulk5")
	if updated.Approved {
		t.Fatal("expected station to be unapproved")
	}
}

// ---------------------------------------------------------------------------
// AdminMediaBulk (increases coverage from 10.5%)
// (403 case already covered in pages_admin_test.go)
// ---------------------------------------------------------------------------

func TestAdminMediaBulk_Admin_DeleteMedia_Succeeds(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)
	seedStation(t, db, "smb1", "Media Bulk Station")

	media := models.MediaItem{
		ID:            "bulk-del-1",
		StationID:     "smb1",
		Title:         "Bulk Delete Track",
		Path:          "smb1/track.mp3",
		AnalysisState: "complete",
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	body, _ := json.Marshal(BulkRequest{IDs: []string{"bulk-del-1"}, Action: "delete"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminMediaBulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var count int64
	db.Model(&models.MediaItem{}).Where("id = ?", "bulk-del-1").Count(&count)
	if count != 0 {
		t.Fatal("media should be deleted")
	}
}

func TestAdminMediaBulk_Admin_EmptyIDs_Returns400(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	body, _ := json.Marshal(BulkRequest{IDs: []string{}, Action: "delete"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	h.AdminMediaBulk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// AdminMediaDuplicates (was at 23.2%)
// ---------------------------------------------------------------------------

func TestAdminMediaDuplicates_NoAuth(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	req := adminReq("GET", "/admin/media/duplicates", nil)
	rr := httptest.NewRecorder()
	h.AdminMediaDuplicates(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminMediaDuplicates_Admin_NoData(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	user := seedAdminUser(t, db)
	req := adminReq("GET", "/admin/media/duplicates", &user)
	rr := httptest.NewRecorder()
	h.AdminMediaDuplicates(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAdminMediaDuplicates_Admin_WithDuplicates(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	user := seedAdminUser(t, db)

	// Seed two items with the same content hash
	hash := "abc123"
	db.Create(&models.MediaItem{ID: "dup-1", Path: "a/b/c/file1.mp3", ContentHash: hash, AnalysisState: "complete"})
	db.Create(&models.MediaItem{ID: "dup-2", Path: "a/b/c/file2.mp3", ContentHash: hash, AnalysisState: "complete"})

	req := adminReq("GET", "/admin/media/duplicates", &user)
	rr := httptest.NewRecorder()
	h.AdminMediaDuplicates(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAdminMediaDuplicates_DBError(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	user := seedAdminUser(t, db)
	sqlDB, _ := db.DB()
	sqlDB.Close()
	req := adminReq("GET", "/admin/media/duplicates", &user)
	rr := httptest.NewRecorder()
	h.AdminMediaDuplicates(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// AdminStationDelete (was at 40.9%)
// ---------------------------------------------------------------------------

func TestAdminStationDelete_NoAuth(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	req := adminReqWithID("DELETE", "/admin/stations/s1", nil, "id", "s1")
	rr := httptest.NewRecorder()
	h.AdminStationDelete(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminStationDelete_NotFound(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	user := seedAdminUser(t, db)
	req := adminReqWithID("DELETE", "/admin/stations/no-such", &user, "id", "no-such")
	rr := httptest.NewRecorder()
	h.AdminStationDelete(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestAdminStationDelete_HxRequest(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	user := seedAdminUser(t, db)
	req := adminReqWithID("DELETE", "/admin/stations/no-such-hx", &user, "id", "no-such-hx")
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.AdminStationDelete(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}
