/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// WebDJConsole renders the WebDJ console page.
func (h *Handler) WebDJConsole(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	user := h.GetUser(r)

	// Check for existing active session
	var existingSession models.WebDJSession
	hasSession := h.db.Where("station_id = ? AND user_id = ? AND active = ?",
		station.ID, user.ID, true).First(&existingSession).Error == nil

	// Get recent media for quick load
	var recentMedia []models.MediaItem
	h.db.Where("station_id = ?", station.ID).
		Order("updated_at DESC").
		Limit(20).
		Find(&recentMedia)

	// Get playlists
	var playlists []models.Playlist
	h.db.Where("station_id = ?", station.ID).
		Order("name ASC").
		Find(&playlists)

	// Get mounts for live broadcast
	var mounts []models.Mount
	h.db.Where("station_id = ?", station.ID).
		Order("name ASC").
		Find(&mounts)

	h.Render(w, r, "pages/dashboard/webdj", PageData{
		Title:    "WebDJ Console",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"HasSession":      hasSession,
			"ExistingSession": existingSession,
			"RecentMedia":     recentMedia,
			"Playlists":       playlists,
			"Mounts":          mounts,
		},
	})
}

// WebDJLibrarySearch handles media library search for WebDJ.
func (h *Handler) WebDJLibrarySearch(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	query := r.URL.Query().Get("q")
	genre := r.URL.Query().Get("genre")
	playlistID := r.URL.Query().Get("playlist_id")

	var media []models.MediaItem

	db := h.db.Where("station_id = ?", station.ID)

	if query != "" {
		db = applyLooseMediaSearch(db, query)
	}

	if genre != "" {
		db = db.Where("genre = ?", genre)
	}

	if playlistID != "" {
		// Search within playlist
		subQuery := h.db.Table("playlist_items").
			Select("media_id").
			Where("playlist_id = ?", playlistID)
		db = db.Where("id IN (?)", subQuery)
	}

	db.Order("artist ASC, title ASC").
		Limit(100).
		Find(&media)

	// Return JSON response
	type mediaResult struct {
		ID         string  `json:"id"`
		Title      string  `json:"title"`
		Artist     string  `json:"artist"`
		Album      string  `json:"album"`
		Genre      string  `json:"genre"`
		DurationMS int     `json:"duration_ms"`
		BPM        float64 `json:"bpm"`
		HasArtwork bool    `json:"has_artwork"`
	}

	results := make([]mediaResult, len(media))
	for i, m := range media {
		results[i] = mediaResult{
			ID:         m.ID,
			Title:      m.Title,
			Artist:     m.Artist,
			Album:      m.Album,
			Genre:      m.Genre,
			DurationMS: int(m.Duration.Milliseconds()),
			BPM:        m.BPM,
			HasArtwork: len(m.Artwork) > 0,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// WebDJGenres returns available genres for filtering.
func (h *Handler) WebDJGenres(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	var genres []string
	h.db.Model(&models.MediaItem{}).
		Where("station_id = ? AND genre != ''", station.ID).
		Distinct("genre").
		Order("genre ASC").
		Pluck("genre", &genres)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(genres)
}

// WebDJPlaylists returns playlists for the station.
func (h *Handler) WebDJPlaylists(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	var playlists []models.Playlist
	h.db.Where("station_id = ?", station.ID).
		Order("name ASC").
		Find(&playlists)

	type playlistResult struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		ItemCount int    `json:"item_count"`
	}

	results := make([]playlistResult, len(playlists))
	for i, p := range playlists {
		var count int64
		h.db.Model(&models.PlaylistItem{}).Where("playlist_id = ?", p.ID).Count(&count)
		results[i] = playlistResult{
			ID:        p.ID,
			Name:      p.Name,
			ItemCount: int(count),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// WebDJPlaylistItems returns tracks in a playlist.
func (h *Handler) WebDJPlaylistItems(w http.ResponseWriter, r *http.Request) {
	playlistID := chi.URLParam(r, "id")
	if playlistID == "" {
		http.Error(w, "Playlist ID required", http.StatusBadRequest)
		return
	}

	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	// Verify playlist belongs to station
	var playlist models.Playlist
	if err := h.db.First(&playlist, "id = ? AND station_id = ?", playlistID, station.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "Playlist not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Get playlist items with media info
	var items []struct {
		models.PlaylistItem
		Title      string  `json:"title"`
		Artist     string  `json:"artist"`
		Album      string  `json:"album"`
		DurationMS int     `json:"duration_ms"`
		BPM        float64 `json:"bpm"`
	}

	h.db.Table("playlist_items").
		Select("playlist_items.*, media_items.title, media_items.artist, media_items.album, media_items.duration_ms, media_items.bpm").
		Joins("LEFT JOIN media_items ON playlist_items.media_id = media_items.id").
		Where("playlist_items.playlist_id = ?", playlistID).
		Order("playlist_items.position ASC").
		Scan(&items)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// WebDJMediaArtwork serves artwork for a media item (for WebDJ preview).
func (h *Handler) WebDJMediaArtwork(w http.ResponseWriter, r *http.Request) {
	mediaID := chi.URLParam(r, "id")
	if mediaID == "" {
		http.Error(w, "Media ID required", http.StatusBadRequest)
		return
	}

	var media models.MediaItem
	if err := h.db.First(&media, "id = ?", mediaID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "Media not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if len(media.Artwork) == 0 {
		// Return default placeholder
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="200" height="200" viewBox="0 0 200 200">
			<rect width="200" height="200" fill="#1a1a2e"/>
			<circle cx="100" cy="100" r="60" fill="#16213e" stroke="#0f3460" stroke-width="2"/>
			<circle cx="100" cy="100" r="20" fill="#e94560"/>
		</svg>`))
		return
	}

	// Serve artwork directly from database
	mimeType := media.ArtworkMime
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(media.Artwork)
}

// WebDJMediaStream streams a media file for WebDJ preview.
func (h *Handler) WebDJMediaStream(w http.ResponseWriter, r *http.Request) {
	mediaID := chi.URLParam(r, "id")
	if mediaID == "" {
		http.Error(w, "Media ID required", http.StatusBadRequest)
		return
	}

	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	var media models.MediaItem
	if err := h.db.First(&media, "id = ? AND station_id = ?", mediaID, station.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "Media not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Redirect to the media stream endpoint
	http.Redirect(w, r, "/dashboard/media/"+mediaID+"/stream", http.StatusTemporaryRedirect)
}
