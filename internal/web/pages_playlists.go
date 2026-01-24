/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// PlaylistList renders the playlists page
func (h *Handler) PlaylistList(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	var playlists []models.Playlist
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&playlists)

	// Get item counts for each playlist
	type playlistWithCount struct {
		models.Playlist
		ItemCount int64
	}

	var playlistsWithCounts []playlistWithCount
	for _, p := range playlists {
		var count int64
		h.db.Model(&models.PlaylistItem{}).Where("playlist_id = ?", p.ID).Count(&count)
		playlistsWithCounts = append(playlistsWithCounts, playlistWithCount{p, count})
	}

	h.Render(w, r, "pages/dashboard/playlists/list", PageData{
		Title:    "Playlists",
		Stations: h.LoadStations(r),
		Data:     playlistsWithCounts,
	})
}

// PlaylistNew renders the new playlist form
func (h *Handler) PlaylistNew(w http.ResponseWriter, r *http.Request) {
	h.Render(w, r, "pages/dashboard/playlists/form", PageData{
		Title:    "New Playlist",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Playlist": models.Playlist{},
			"IsNew":    true,
		},
	})
}

// PlaylistCreate handles new playlist creation
func (h *Handler) PlaylistCreate(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	playlist := models.Playlist{
		ID:          uuid.New().String(),
		StationID:   station.ID,
		Name:        r.FormValue("name"),
		Description: r.FormValue("description"),
	}

	if playlist.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	if err := h.db.Create(&playlist).Error; err != nil {
		http.Error(w, "Failed to create playlist", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/playlists/"+playlist.ID)
		return
	}

	http.Redirect(w, r, "/dashboard/playlists/"+playlist.ID, http.StatusSeeOther)
}

// PlaylistDetail renders the playlist detail/editor
func (h *Handler) PlaylistDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var playlist models.Playlist
	if err := h.db.First(&playlist, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Get playlist items with media info
	var items []models.PlaylistItem
	h.db.Where("playlist_id = ?", id).Order("position ASC").Find(&items)

	// Load media for each item
	type itemWithMedia struct {
		models.PlaylistItem
		Media models.MediaItem
	}

	var itemsWithMedia []itemWithMedia
	var totalDuration time.Duration
	for _, item := range items {
		var media models.MediaItem
		h.db.First(&media, "id = ?", item.MediaID)
		itemsWithMedia = append(itemsWithMedia, itemWithMedia{item, media})
		totalDuration += media.Duration
	}

	// Get available media for adding
	station := h.GetStation(r)
	var availableMedia []models.MediaItem
	h.db.Where("station_id = ?", station.ID).Order("artist ASC, title ASC").Limit(100).Find(&availableMedia)

	h.Render(w, r, "pages/dashboard/playlists/detail", PageData{
		Title:    playlist.Name,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Playlist":       playlist,
			"Items":          itemsWithMedia,
			"AvailableMedia": availableMedia,
			"TotalDuration":  totalDuration,
		},
	})
}

// PlaylistEdit renders the playlist edit form
func (h *Handler) PlaylistEdit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var playlist models.Playlist
	if err := h.db.First(&playlist, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	h.Render(w, r, "pages/dashboard/playlists/form", PageData{
		Title:    "Edit: " + playlist.Name,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Playlist": playlist,
			"IsNew":    false,
		},
	})
}

// PlaylistUpdate handles playlist updates
func (h *Handler) PlaylistUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var playlist models.Playlist
	if err := h.db.First(&playlist, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	playlist.Name = r.FormValue("name")
	playlist.Description = r.FormValue("description")

	if err := h.db.Save(&playlist).Error; err != nil {
		http.Error(w, "Failed to update playlist", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/playlists/"+id)
		return
	}

	http.Redirect(w, r, "/dashboard/playlists/"+id, http.StatusSeeOther)
}

// PlaylistDelete handles playlist deletion
func (h *Handler) PlaylistDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Delete items first
	h.db.Delete(&models.PlaylistItem{}, "playlist_id = ?", id)

	// Delete playlist
	if err := h.db.Delete(&models.Playlist{}, "id = ?", id).Error; err != nil {
		http.Error(w, "Failed to delete playlist", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/playlists")
		return
	}

	http.Redirect(w, r, "/dashboard/playlists", http.StatusSeeOther)
}

// PlaylistAddItem adds a media item to a playlist
func (h *Handler) PlaylistAddItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	mediaID := r.FormValue("media_id")
	if mediaID == "" {
		http.Error(w, "Media ID required", http.StatusBadRequest)
		return
	}

	// Get current max position
	var maxPos int
	h.db.Model(&models.PlaylistItem{}).Where("playlist_id = ?", id).
		Select("COALESCE(MAX(position), 0)").Scan(&maxPos)

	item := models.PlaylistItem{
		ID:         uuid.New().String(),
		PlaylistID: id,
		MediaID:    mediaID,
		Position:   maxPos + 1,
	}

	if err := h.db.Create(&item).Error; err != nil {
		http.Error(w, "Failed to add item", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		return
	}

	http.Redirect(w, r, "/dashboard/playlists/"+id, http.StatusSeeOther)
}

// PlaylistRemoveItem removes a media item from a playlist
func (h *Handler) PlaylistRemoveItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	itemID := chi.URLParam(r, "itemID")

	if err := h.db.Delete(&models.PlaylistItem{}, "id = ?", itemID).Error; err != nil {
		http.Error(w, "Failed to remove item", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		return
	}

	http.Redirect(w, r, "/dashboard/playlists/"+id, http.StatusSeeOther)
}

// PlaylistReorderItems handles drag-drop reordering
func (h *Handler) PlaylistReorderItems(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var order []struct {
		ID       string `json:"id"`
		Position int    `json:"position"`
	}

	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Update positions in transaction
	tx := h.db.Begin()
	for _, item := range order {
		if err := tx.Model(&models.PlaylistItem{}).
			Where("id = ? AND playlist_id = ?", item.ID, id).
			Update("position", item.Position).Error; err != nil {
			tx.Rollback()
			http.Error(w, "Failed to reorder", http.StatusInternalServerError)
			return
		}
	}
	tx.Commit()

	w.WriteHeader(http.StatusOK)
}
