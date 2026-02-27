/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// PreviewItem represents a media item for playlist hover preview
type PreviewItem struct {
	Title    string
	Duration string // Formatted duration string
}

// PlaylistWithPreview includes playlist data with item count and preview items
type PlaylistWithPreview struct {
	models.Playlist
	ItemCount     int64
	PreviewItems  []PreviewItem
	HasCover      bool
	TotalDuration time.Duration
}

// PlaylistList renders the playlists page
func (h *Handler) PlaylistList(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	var playlists []models.Playlist
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&playlists)

	// Collect all playlist IDs
	playlistIDs := make([]string, len(playlists))
	for i, p := range playlists {
		playlistIDs[i] = p.ID
	}

	// Fetch all playlist items in one query
	var allPlaylistItems []models.PlaylistItem
	if len(playlistIDs) > 0 {
		h.db.Where("playlist_id IN ?", playlistIDs).Order("playlist_id, position ASC").Find(&allPlaylistItems)
	}

	// Collect all unique media IDs
	mediaIDSet := make(map[string]struct{})
	for _, item := range allPlaylistItems {
		mediaIDSet[item.MediaID] = struct{}{}
	}
	mediaIDs := make([]string, 0, len(mediaIDSet))
	for id := range mediaIDSet {
		mediaIDs = append(mediaIDs, id)
	}

	// Fetch all media in one query
	mediaMap := make(map[string]models.MediaItem)
	if len(mediaIDs) > 0 {
		var mediaItems []models.MediaItem
		h.db.Select("id", "title", "duration").Where("id IN ?", mediaIDs).Find(&mediaItems)
		for _, m := range mediaItems {
			mediaMap[m.ID] = m
		}
	}

	// Group playlist items by playlist ID
	itemsByPlaylist := make(map[string][]models.PlaylistItem)
	for _, item := range allPlaylistItems {
		itemsByPlaylist[item.PlaylistID] = append(itemsByPlaylist[item.PlaylistID], item)
	}

	// Build preview data for each playlist
	var playlistsWithPreviews []PlaylistWithPreview
	for _, p := range playlists {
		items := itemsByPlaylist[p.ID]
		var previewItems []PreviewItem
		var totalDuration time.Duration

		for i, item := range items {
			if media, ok := mediaMap[item.MediaID]; ok {
				totalDuration += media.Duration

				// Only include first 10 items in preview
				if i < 10 {
					dur := media.Duration
					mins := int(dur.Minutes())
					secs := int(dur.Seconds()) % 60
					durationStr := fmt.Sprintf("%d:%02d", mins, secs)

					previewItems = append(previewItems, PreviewItem{
						Title:    media.Title,
						Duration: durationStr,
					})
				}
			}
		}

		playlistsWithPreviews = append(playlistsWithPreviews, PlaylistWithPreview{
			Playlist:      p,
			ItemCount:     int64(len(items)),
			PreviewItems:  previewItems,
			HasCover:      len(p.CoverImage) > 0,
			TotalDuration: totalDuration,
		})
	}

	h.Render(w, r, "pages/dashboard/playlists/list", PageData{
		Title:    "Playlists",
		Stations: h.LoadStations(r),
		Data:     playlistsWithPreviews,
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
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	id := chi.URLParam(r, "id")

	var playlist models.Playlist
	if err := h.db.First(&playlist, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Get playlist items with media info
	var items []models.PlaylistItem
	h.db.Where("playlist_id = ?", id).Order("position ASC").Find(&items)

	// Collect all media IDs and fetch in one query
	mediaIDs := make([]string, len(items))
	for i, item := range items {
		mediaIDs[i] = item.MediaID
	}

	mediaMap := make(map[string]models.MediaItem)
	if len(mediaIDs) > 0 {
		var mediaItems []models.MediaItem
		h.db.Where("id IN ?", mediaIDs).Find(&mediaItems)
		for _, m := range mediaItems {
			mediaMap[m.ID] = m
		}
	}

	// Build items with media
	type itemWithMedia struct {
		models.PlaylistItem
		Media models.MediaItem
	}

	var itemsWithMedia []itemWithMedia
	var totalDuration time.Duration
	for _, item := range items {
		media := mediaMap[item.MediaID]
		itemsWithMedia = append(itemsWithMedia, itemWithMedia{item, media})
		totalDuration += media.Duration
	}
	var availableMedia []models.MediaItem
	h.db.Where("station_id = ?", station.ID).Order("artist ASC, title ASC").Limit(500).Find(&availableMedia)

	// Get unique genres for filter dropdown (include archive genres)
	var genres []string
	h.db.Model(&models.MediaItem{}).
		Where("(station_id = ? OR show_in_archive = ?) AND genre != ''", station.ID, true).
		Distinct().Pluck("genre", &genres)

	// Get unique artists for filter dropdown (include archive artists)
	var artists []string
	h.db.Model(&models.MediaItem{}).
		Where("(station_id = ? OR show_in_archive = ?) AND artist != ''", station.ID, true).
		Distinct().Order("artist ASC").Pluck("artist", &artists)

	// Get unique moods for filter dropdown (include archive moods)
	var moods []string
	h.db.Model(&models.MediaItem{}).
		Where("(station_id = ? OR show_in_archive = ?) AND mood != ''", station.ID, true).
		Distinct().Pluck("mood", &moods)

	// Get smart blocks for quick filter selection
	var smartBlocks []models.SmartBlock
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&smartBlocks)

	h.Render(w, r, "pages/dashboard/playlists/detail", PageData{
		Title:    playlist.Name,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Playlist":       playlist,
			"Items":          itemsWithMedia,
			"AvailableMedia": availableMedia,
			"TotalDuration":  totalDuration,
			"Genres":         genres,
			"Artists":        artists,
			"Moods":          moods,
			"SmartBlocks":    smartBlocks,
		},
	})
}

// PlaylistEdit renders the playlist edit form
func (h *Handler) PlaylistEdit(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	id := chi.URLParam(r, "id")

	var playlist models.Playlist
	if err := h.db.First(&playlist, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
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
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	var playlist models.Playlist
	if err := h.db.First(&playlist, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
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
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	// Verify playlist belongs to station
	var playlist models.Playlist
	if err := h.db.First(&playlist, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Delete items first
	h.db.Delete(&models.PlaylistItem{}, "playlist_id = ?", id)

	// Delete playlist
	if err := h.db.Delete(&models.Playlist{}, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.Error(w, "Failed to delete playlist", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/playlists")
		return
	}

	http.Redirect(w, r, "/dashboard/playlists", http.StatusSeeOther)
}

// PlaylistBulk handles bulk actions on playlists
func (h *Handler) PlaylistBulk(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	var req struct {
		Action string   `json:"action"`
		IDs    []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if len(req.IDs) == 0 {
		http.Error(w, "No items selected", http.StatusBadRequest)
		return
	}

	var affected int64
	var err error

	switch req.Action {
	case "delete":
		// Delete items first
		h.db.Where("playlist_id IN ?", req.IDs).Delete(&models.PlaylistItem{})
		// Delete playlists
		result := h.db.Where("id IN ? AND station_id = ?", req.IDs, station.ID).Delete(&models.Playlist{})
		affected, err = result.RowsAffected, result.Error
	default:
		http.Error(w, "Unknown action", http.StatusBadRequest)
		return
	}

	if err != nil {
		h.logger.Error().Err(err).Str("action", req.Action).Msg("bulk playlist action failed")
		http.Error(w, "Operation failed", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("action", req.Action).
		Int64("affected", affected).
		Str("station_id", station.ID).
		Msg("bulk playlist action completed")

	w.WriteHeader(http.StatusOK)
}

// PlaylistAddItem adds a media item to a playlist
func (h *Handler) PlaylistAddItem(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	// Verify playlist belongs to station
	var playlist models.Playlist
	if err := h.db.First(&playlist, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	var mediaIDs []string
	for _, raw := range r.Form["media_ids"] {
		for _, id := range strings.Split(raw, ",") {
			trimmed := strings.TrimSpace(id)
			if trimmed != "" {
				mediaIDs = append(mediaIDs, trimmed)
			}
		}
	}
	if len(mediaIDs) == 0 {
		singleMediaID := strings.TrimSpace(r.FormValue("media_id"))
		if singleMediaID != "" {
			mediaIDs = append(mediaIDs, singleMediaID)
		}
	}
	if len(mediaIDs) == 0 {
		http.Error(w, "Media ID required", http.StatusBadRequest)
		return
	}

	// De-duplicate while preserving request order.
	seen := make(map[string]struct{}, len(mediaIDs))
	deduped := make([]string, 0, len(mediaIDs))
	for _, mediaID := range mediaIDs {
		if _, ok := seen[mediaID]; ok {
			continue
		}
		seen[mediaID] = struct{}{}
		deduped = append(deduped, mediaID)
	}
	mediaIDs = deduped

	// Verify all media IDs belong to the station or are public archive items.
	var mediaCount int64
	h.db.Model(&models.MediaItem{}).
		Where("id IN ? AND (station_id = ? OR show_in_archive = ?)", mediaIDs, station.ID, true).
		Count(&mediaCount)
	if mediaCount != int64(len(mediaIDs)) {
		http.Error(w, "One or more media items not found", http.StatusNotFound)
		return
	}

	// Insert all items in one transaction so positions are contiguous.
	tx := h.db.Begin()
	if tx.Error != nil {
		http.Error(w, "Failed to add items", http.StatusInternalServerError)
		return
	}

	var maxPos int
	if err := tx.Model(&models.PlaylistItem{}).Where("playlist_id = ?", id).
		Select("COALESCE(MAX(position), 0)").Scan(&maxPos).Error; err != nil {
		tx.Rollback()
		http.Error(w, "Failed to add items", http.StatusInternalServerError)
		return
	}

	items := make([]models.PlaylistItem, 0, len(mediaIDs))
	for idx, mediaID := range mediaIDs {
		items = append(items, models.PlaylistItem{
			ID:         uuid.New().String(),
			PlaylistID: id,
			MediaID:    mediaID,
			Position:   maxPos + idx + 1,
		})
	}

	if err := tx.Create(&items).Error; err != nil {
		tx.Rollback()
		http.Error(w, "Failed to add items", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		http.Error(w, "Failed to add items", http.StatusInternalServerError)
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
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")
	itemID := chi.URLParam(r, "itemID")

	// Verify playlist belongs to station
	var playlist models.Playlist
	if err := h.db.First(&playlist, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := h.db.Delete(&models.PlaylistItem{}, "id = ? AND playlist_id = ?", itemID, id).Error; err != nil {
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
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	// Verify playlist belongs to station
	var playlist models.Playlist
	if err := h.db.First(&playlist, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

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
	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		http.Error(w, "Failed to reorder", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// PlaylistCover serves the playlist cover image
func (h *Handler) PlaylistCover(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	var playlist models.Playlist
	if err := h.db.Select("id", "cover_image", "cover_image_mime").First(&playlist, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if len(playlist.CoverImage) == 0 {
		http.Error(w, "No cover image", http.StatusNotFound)
		return
	}

	contentType := playlist.CoverImageMime
	if contentType == "" {
		contentType = "image/jpeg"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(playlist.CoverImage)
}

// PlaylistUploadCover handles cover image upload
func (h *Handler) PlaylistUploadCover(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	var playlist models.Playlist
	if err := h.db.First(&playlist, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Parse multipart form (default 5MB, configurable via GRIMNIR_MAX_UPLOAD_SIZE_MB)
	if err := r.ParseMultipartForm(h.multipartLimit(5 << 20)); err != nil {
		http.Error(w, "File too large", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("cover")
	if err != nil {
		http.Error(w, "No file uploaded", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read file content
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}

	// Detect content type
	contentType := http.DetectContentType(data)
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/gif" && contentType != "image/webp" {
		http.Error(w, "Invalid image type. Use JPEG, PNG, GIF, or WebP", http.StatusBadRequest)
		return
	}

	playlist.CoverImage = data
	playlist.CoverImageMime = contentType

	if err := h.db.Save(&playlist).Error; err != nil {
		http.Error(w, "Failed to save cover image", http.StatusInternalServerError)
		return
	}

	h.logger.Info().Str("playlist_id", id).Str("filename", header.Filename).Msg("playlist cover uploaded")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		return
	}

	http.Redirect(w, r, "/dashboard/playlists/"+id, http.StatusSeeOther)
}

// PlaylistDeleteCover removes the playlist cover image
func (h *Handler) PlaylistDeleteCover(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	// Verify playlist belongs to station
	var playlist models.Playlist
	if err := h.db.First(&playlist, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := h.db.Model(&models.Playlist{}).Where("id = ? AND station_id = ?", id, station.ID).
		Updates(map[string]any{"cover_image": nil, "cover_image_mime": ""}).Error; err != nil {
		http.Error(w, "Failed to delete cover", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		return
	}

	http.Redirect(w, r, "/dashboard/playlists/"+id, http.StatusSeeOther)
}

// PlaylistMediaSearch returns filtered media items for the playlist media picker (HTMX)
func (h *Handler) PlaylistMediaSearch(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	// Get filter parameters
	query := r.URL.Query().Get("q")
	genre := r.URL.Query().Get("genre")
	artist := r.URL.Query().Get("artist")
	mood := r.URL.Query().Get("mood")
	yearFrom := r.URL.Query().Get("year_from")
	yearTo := r.URL.Query().Get("year_to")
	bpmFrom := r.URL.Query().Get("bpm_from")
	bpmTo := r.URL.Query().Get("bpm_to")
	excludeExplicit := r.URL.Query().Get("exclude_explicit") == "true"
	includeArchive := r.URL.Query().Get("include_archive") == "true"

	// Build query - include public archive media from other stations if requested
	dbQuery := h.db.Model(&models.MediaItem{})
	if includeArchive {
		// Include own station's media OR public archive media from any station
		dbQuery = dbQuery.Where("station_id = ? OR show_in_archive = ?", station.ID, true)
	} else {
		// Only own station's media
		dbQuery = dbQuery.Where("station_id = ?", station.ID)
	}

	// Text search with punctuation/spacing normalization.
	dbQuery = applyLooseMediaSearch(dbQuery, query)

	// Genre filter
	if genre != "" {
		dbQuery = dbQuery.Where("genre = ?", genre)
	}

	// Artist filter
	if artist != "" {
		dbQuery = dbQuery.Where("artist = ?", artist)
	}

	// Mood filter
	if mood != "" {
		dbQuery = dbQuery.Where("mood = ?", mood)
	}

	// Year range
	if yearFrom != "" {
		dbQuery = dbQuery.Where("year >= ?", yearFrom)
	}
	if yearTo != "" {
		dbQuery = dbQuery.Where("year <= ?", yearTo)
	}

	// BPM range
	if bpmFrom != "" {
		dbQuery = dbQuery.Where("bpm >= ?", bpmFrom)
	}
	if bpmTo != "" {
		dbQuery = dbQuery.Where("bpm <= ?", bpmTo)
	}

	// Exclude explicit
	if excludeExplicit {
		dbQuery = dbQuery.Where("explicit = ?", false)
	}

	// Fetch results - limit to 200 but allow searching all
	var media []models.MediaItem

	// Preload station when including archive (to show source station in UI)
	if includeArchive {
		dbQuery = dbQuery.Preload("Station")
	}

	dbQuery.Order("artist ASC, title ASC").Limit(1000).Find(&media)

	// Pass current station ID to template for comparison
	type templateData struct {
		Media            []models.MediaItem
		CurrentStationID string
		IncludeArchive   bool
	}

	h.RenderPartial(w, r, "partials/playlist-media-items", templateData{
		Media:            media,
		CurrentStationID: station.ID,
		IncludeArchive:   includeArchive,
	})
}
