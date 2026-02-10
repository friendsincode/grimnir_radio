/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// MediaList renders the media library
func (h *Handler) MediaList(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	// Pagination and filters
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage := 24

	query := r.URL.Query().Get("q")
	genre := r.URL.Query().Get("genre")
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "created_at"
	}
	sortOrder := r.URL.Query().Get("order")
	if sortOrder == "" {
		sortOrder = "desc"
	}

	var media []models.MediaItem
	var total int64

	dbQuery := h.db.Model(&models.MediaItem{}).Where("station_id = ?", station.ID)

	// Search filter (use LOWER for cross-database compatibility)
	if query != "" {
		searchPattern := "%" + strings.ToLower(query) + "%"
		dbQuery = dbQuery.Where(
			"LOWER(title) LIKE ? OR LOWER(artist) LIKE ? OR LOWER(album) LIKE ?",
			searchPattern, searchPattern, searchPattern,
		)
	}

	// Genre filter
	if genre != "" {
		dbQuery = dbQuery.Where("genre = ?", genre)
	}

	// Use Session clones to avoid Count mutating query state
	dbQuery.Session(&gorm.Session{}).Count(&total)

	// Sorting
	orderClause := sortBy + " " + strings.ToUpper(sortOrder)
	dbQuery.Session(&gorm.Session{}).Order(orderClause).
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&media)

	// Get unique genres for filter
	var genres []string
	h.db.Model(&models.MediaItem{}).
		Where("station_id = ? AND genre != ''", station.ID).
		Distinct().
		Pluck("genre", &genres)

	totalPages := int(total) / perPage
	if int(total)%perPage > 0 {
		totalPages++
	}

	h.Render(w, r, "pages/dashboard/media/list", PageData{
		Title:    "Media Library",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Media":      media,
			"Total":      total,
			"Page":       page,
			"PerPage":    perPage,
			"TotalPages": totalPages,
			"Query":      query,
			"Genre":      genre,
			"SortBy":     sortBy,
			"SortOrder":  sortOrder,
			"Genres":     genres,
		},
	})
}

// MediaTablePartial renders just the media table (for HTMX)
func (h *Handler) MediaTablePartial(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	query := r.URL.Query().Get("q")

	var media []models.MediaItem
	dbQuery := h.db.Where("station_id = ?", station.ID)

	if query != "" {
		searchPattern := "%" + strings.ToLower(query) + "%"
		dbQuery = dbQuery.Where(
			"LOWER(title) LIKE ? OR LOWER(artist) LIKE ? OR LOWER(album) LIKE ?",
			searchPattern, searchPattern, searchPattern,
		)
	}

	dbQuery.Order("created_at DESC").Limit(50).Find(&media)

	h.RenderPartial(w, r, "partials/media-table", media)
}

// MediaGridPartial renders media as cards (for HTMX)
func (h *Handler) MediaGridPartial(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	query := r.URL.Query().Get("q")

	var media []models.MediaItem
	dbQuery := h.db.Where("station_id = ?", station.ID)

	if query != "" {
		searchPattern := "%" + strings.ToLower(query) + "%"
		dbQuery = dbQuery.Where(
			"LOWER(title) LIKE ? OR LOWER(artist) LIKE ? OR LOWER(album) LIKE ?",
			searchPattern, searchPattern, searchPattern,
		)
	}

	dbQuery.Order("created_at DESC").Limit(50).Find(&media)

	h.RenderPartial(w, r, "partials/media-grid", media)
}

// MediaUploadPage renders the upload page
func (h *Handler) MediaUploadPage(w http.ResponseWriter, r *http.Request) {
	h.Render(w, r, "pages/dashboard/media/upload", PageData{
		Title:    "Upload Media",
		Stations: h.LoadStations(r),
	})
}

// MediaUpload handles file uploads
func (h *Handler) MediaUpload(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	// Parse multipart form (1GB max)
	if err := r.ParseMultipartForm(1 << 30); err != nil {
		http.Error(w, "File too large", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "No file uploaded", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file type
	ext := strings.ToLower(filepath.Ext(header.Filename))
	validExts := map[string]bool{".mp3": true, ".flac": true, ".wav": true, ".ogg": true, ".m4a": true}
	if !validExts[ext] {
		http.Error(w, "Invalid file type", http.StatusBadRequest)
		return
	}

	// Generate unique filename and path
	mediaID := uuid.New().String()
	// Organize files by station and date
	dateDir := time.Now().Format("2006/01")
	relPath := filepath.Join(station.ID, dateDir, mediaID+ext)
	fullPath := filepath.Join(h.mediaRoot, relPath)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		h.logger.Error().Err(err).Str("path", fullPath).Msg("failed to create media directory")
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(fmt.Sprintf(`<div class="alert alert-danger">Failed to create storage directory. Check that the media root (%s) is writable.</div>`, h.mediaRoot)))
			return
		}
		http.Error(w, fmt.Sprintf("Failed to create storage directory. Check that %s is writable.", h.mediaRoot), http.StatusInternalServerError)
		return
	}

	// Create destination file
	dst, err := os.Create(fullPath)
	if err != nil {
		h.logger.Error().Err(err).Str("path", fullPath).Msg("failed to create media file")
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	// Copy file content
	written, err := io.Copy(dst, file)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to write media file")
		os.Remove(fullPath) // Clean up partial file
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	// Create media item record with station's default archive settings
	media := models.MediaItem{
		ID:            mediaID,
		StationID:     station.ID,
		Title:         strings.TrimSuffix(header.Filename, ext),
		Path:          relPath,
		ShowInArchive: station.DefaultShowInArchive,
		AllowDownload: station.DefaultAllowDownload,
	}

	if err := h.db.Create(&media).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to create media record")
		os.Remove(fullPath) // Clean up file if DB insert fails
		http.Error(w, "Failed to save media", http.StatusInternalServerError)
		return
	}

	// Queue for analysis (metadata extraction, loudness, duration, etc.)
	analysisJob := models.AnalysisJob{
		ID:      uuid.New().String(),
		MediaID: mediaID,
		Status:  "pending",
	}
	if err := h.db.Create(&analysisJob).Error; err != nil {
		h.logger.Warn().Err(err).Str("media_id", mediaID).Msg("failed to queue analysis job")
		// Don't fail the upload, just log the warning
	}

	h.logger.Info().
		Str("media_id", mediaID).
		Str("path", relPath).
		Int64("size", written).
		Msg("media file uploaded")

	// Return success
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(fmt.Sprintf(`<div class="alert alert-success">File uploaded successfully! <a href="/dashboard/media/%s">View details</a></div>`, mediaID)))
		return
	}

	http.Redirect(w, r, "/dashboard/media/"+mediaID, http.StatusSeeOther)
}

// MediaDetail renders media details page
func (h *Handler) MediaDetail(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	id := chi.URLParam(r, "id")

	var media models.MediaItem
	if err := h.db.Preload("Tags").First(&media, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Get play history for this media (only from current station)
	var history []models.PlayHistory
	h.db.Where("media_id = ? AND station_id = ?", id, station.ID).Order("started_at DESC").Limit(20).Find(&history)

	h.Render(w, r, "pages/dashboard/media/detail", PageData{
		Title:    media.Title,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Media":   media,
			"History": history,
		},
	})
}

// MediaEdit renders the media edit form
func (h *Handler) MediaEdit(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	id := chi.URLParam(r, "id")

	var media models.MediaItem
	if err := h.db.First(&media, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	h.Render(w, r, "pages/dashboard/media/edit", PageData{
		Title:    "Edit: " + media.Title,
		Stations: h.LoadStations(r),
		Data:     media,
	})
}

// MediaUpdate handles media metadata updates
func (h *Handler) MediaUpdate(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	var media models.MediaItem
	if err := h.db.First(&media, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	media.Title = r.FormValue("title")
	media.Artist = r.FormValue("artist")
	media.Album = r.FormValue("album")
	media.Genre = r.FormValue("genre")
	media.Year = r.FormValue("year")
	media.Label = r.FormValue("label")
	media.Language = r.FormValue("language")
	media.Mood = r.FormValue("mood")
	media.Explicit = r.FormValue("explicit") == "on"
	media.ShowInArchive = r.FormValue("show_in_archive") == "on"
	media.AllowDownload = r.FormValue("allow_download") == "on"

	if err := h.db.Save(&media).Error; err != nil {
		http.Error(w, "Failed to update media", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/media/"+id)
		return
	}

	http.Redirect(w, r, "/dashboard/media/"+id, http.StatusSeeOther)
}

// MediaDelete handles media deletion
func (h *Handler) MediaDelete(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	// Get media item to find file path (verify station ownership)
	var media models.MediaItem
	if err := h.db.First(&media, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Delete from database (station already verified above)
	if err := h.db.Delete(&models.MediaItem{}, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.Error(w, "Failed to delete media", http.StatusInternalServerError)
		return
	}

	// Delete file from disk
	if media.Path != "" {
		fullPath := filepath.Join(h.mediaRoot, media.Path)
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			h.logger.Warn().Err(err).Str("path", fullPath).Msg("failed to delete media file")
		}
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/media")
		return
	}

	http.Redirect(w, r, "/dashboard/media", http.StatusSeeOther)
}

// MediaBulk handles bulk actions on media items
func (h *Handler) MediaBulk(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	var req struct {
		Action string   `json:"action"`
		IDs    []string `json:"ids"`
		Value  string   `json:"value,omitempty"`
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
	case "set_genre":
		result := h.db.Model(&models.MediaItem{}).
			Where("id IN ? AND station_id = ?", req.IDs, station.ID).
			Update("genre", req.Value)
		affected, err = result.RowsAffected, result.Error

	case "toggle_explicit":
		// Toggle explicit flag using raw SQL
		result := h.db.Exec(
			"UPDATE media_items SET explicit = NOT explicit WHERE id IN ? AND station_id = ?",
			req.IDs, station.ID)
		affected, err = result.RowsAffected, result.Error

	case "add_to_playlist":
		if req.Value == "" {
			http.Error(w, "No playlist selected", http.StatusBadRequest)
			return
		}
		// Verify playlist belongs to station
		var playlist models.Playlist
		if err := h.db.First(&playlist, "id = ? AND station_id = ?", req.Value, station.ID).Error; err != nil {
			http.Error(w, "Playlist not found", http.StatusNotFound)
			return
		}
		// Get current max position
		var maxPos int
		h.db.Model(&models.PlaylistItem{}).
			Where("playlist_id = ?", playlist.ID).
			Select("COALESCE(MAX(position), 0)").
			Scan(&maxPos)

		// Add each media item to playlist
		for i, mediaID := range req.IDs {
			item := models.PlaylistItem{
				PlaylistID: playlist.ID,
				MediaID:    mediaID,
				Position:   maxPos + i + 1,
			}
			if err := h.db.Create(&item).Error; err != nil {
				h.logger.Warn().Err(err).Str("media_id", mediaID).Msg("failed to add to playlist")
				continue
			}
			affected++
		}

	case "delete":
		// Get media items to delete files
		var mediaItems []models.MediaItem
		h.db.Where("id IN ? AND station_id = ?", req.IDs, station.ID).Find(&mediaItems)

		// Delete from database
		result := h.db.Where("id IN ? AND station_id = ?", req.IDs, station.ID).Delete(&models.MediaItem{})
		affected, err = result.RowsAffected, result.Error

		// Delete files from disk
		for _, media := range mediaItems {
			if media.Path != "" {
				fullPath := filepath.Join(h.mediaRoot, media.Path)
				if rmErr := os.Remove(fullPath); rmErr != nil && !os.IsNotExist(rmErr) {
					h.logger.Warn().Err(rmErr).Str("path", fullPath).Msg("failed to delete media file")
				}
			}
		}

	default:
		http.Error(w, "Unknown action", http.StatusBadRequest)
		return
	}

	if err != nil {
		h.logger.Error().Err(err).Str("action", req.Action).Msg("bulk media action failed")
		http.Error(w, "Operation failed", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("action", req.Action).
		Int64("affected", affected).
		Str("station_id", station.ID).
		Msg("bulk media action completed")

	w.WriteHeader(http.StatusOK)
}

// MediaWaveform returns the waveform data for a media item
func (h *Handler) MediaWaveform(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	var media models.MediaItem
	if err := h.db.Select("id", "waveform").First(&media, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if len(media.Waveform) == 0 {
		http.Error(w, "No waveform data", http.StatusNotFound)
		return
	}

	// Return as base64 encoded JSON for WaveSurfer
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"data":"` + base64.StdEncoding.EncodeToString(media.Waveform) + `"}`))
}

// MediaArtwork returns the album art for a media item
func (h *Handler) MediaArtwork(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	var media models.MediaItem
	if err := h.db.Select("id", "artwork", "artwork_mime").First(&media, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if len(media.Artwork) == 0 {
		http.Error(w, "No artwork available", http.StatusNotFound)
		return
	}

	contentType := media.ArtworkMime
	if contentType == "" {
		contentType = "image/jpeg"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400") // Cache for 24 hours
	w.Write(media.Artwork)
}

// MediaStream serves the actual audio file for playback
func (h *Handler) MediaStream(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	var media models.MediaItem
	if err := h.db.Select("id", "path", "title", "artist").First(&media, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if media.Path == "" {
		http.Error(w, "No media file available", http.StatusNotFound)
		return
	}

	fullPath := filepath.Join(h.mediaRoot, media.Path)

	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		h.logger.Error().Str("path", fullPath).Msg("media file not found on disk")
		http.Error(w, "Media file not found", http.StatusNotFound)
		return
	}

	// Derive format from file extension
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(media.Path), "."))

	// Set content type based on format
	contentTypes := map[string]string{
		"mp3":  "audio/mpeg",
		"flac": "audio/flac",
		"wav":  "audio/wav",
		"ogg":  "audio/ogg",
		"m4a":  "audio/mp4",
	}
	contentType := contentTypes[ext]
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")

	// Set a friendly filename for downloads
	filename := media.Title
	if media.Artist != "" {
		filename = media.Artist + " - " + filename
	}
	filename = strings.ReplaceAll(filename, `"`, `'`) + "." + ext
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filename))

	// Serve the file (supports range requests for seeking)
	http.ServeFile(w, r, fullPath)
}
