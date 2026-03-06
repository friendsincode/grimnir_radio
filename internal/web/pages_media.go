/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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

// MediaReanalyzeDurations queues analysis jobs for station media to refresh
// durations and metadata after imports with bad timing data.
func (h *Handler) MediaReanalyzeDurations(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}
	if !h.HasStationPermission(r, "edit_metadata") {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	// Used by the status endpoint to scope "this batch" of jobs.
	batchSince := time.Now().UTC().Add(-1 * time.Second)

	pendingStatuses := []string{"pending", "running"}

	// Queue only media that do not already have a pending/running analysis job.
	var mediaIDs []string
	if err := h.db.
		Table("media_items m").
		Select("m.id").
		Joins("LEFT JOIN analysis_jobs aj ON aj.media_id = m.id AND aj.status IN ?", pendingStatuses).
		Where("m.station_id = ? AND aj.id IS NULL", station.ID).
		Pluck("m.id", &mediaIDs).Error; err != nil {
		h.logger.Error().Err(err).Str("station_id", station.ID).Msg("failed to fetch media for duration reanalysis")
		http.Error(w, "Failed to queue duration refresh", http.StatusInternalServerError)
		return
	}

	if len(mediaIDs) == 0 {
		msg := "No media queued (already pending/running or no media found)"
		if r.Header.Get("HX-Request") == "true" {
			h.RenderPartial(w, r, "partials/duration-recalc-empty", map[string]any{
				"Message":   msg,
				"StationID": station.ID,
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"queued":  0,
			"message": msg,
		})
		return
	}

	jobs := make([]models.AnalysisJob, 0, len(mediaIDs))
	for _, mediaID := range mediaIDs {
		jobs = append(jobs, models.AnalysisJob{
			ID:      uuid.NewString(),
			MediaID: mediaID,
			Status:  "pending",
		})
	}

	if err := h.db.Create(&jobs).Error; err != nil {
		h.logger.Error().Err(err).Str("station_id", station.ID).Int("count", len(jobs)).Msg("failed to create duration reanalysis jobs")
		http.Error(w, "Failed to queue duration refresh", http.StatusInternalServerError)
		return
	}

	// Mark these media as pending so UI reflects that refresh is in progress.
	if err := h.db.Model(&models.MediaItem{}).
		Where("id IN ?", mediaIDs).
		Update("analysis_state", models.AnalysisPending).Error; err != nil {
		h.logger.Warn().Err(err).Str("station_id", station.ID).Int("count", len(mediaIDs)).Msg("failed to mark media as pending during duration refresh")
	}

	h.logger.Info().
		Str("station_id", station.ID).
		Int("queued", len(jobs)).
		Msg("queued media duration reanalysis jobs")

	if r.Header.Get("HX-Request") == "true" {
		h.RenderPartial(w, r, "partials/duration-recalc-started", map[string]any{
			"Queued":     len(jobs),
			"StationID":  station.ID,
			"BatchSince": batchSince.Format(time.RFC3339),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"queued": len(jobs),
	})
}

// MediaReanalyzeDurationsStatus returns progress for a duration reanalysis batch (HTMX).
func (h *Handler) MediaReanalyzeDurationsStatus(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}
	if !h.HasStationPermission(r, "edit_metadata") {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	sinceStr := r.URL.Query().Get("since")
	if sinceStr == "" {
		h.RenderPartial(w, r, "partials/duration-recalc-status", map[string]any{
			"HasBatch": false,
		})
		return
	}
	since, err := time.Parse(time.RFC3339, sinceStr)
	if err != nil {
		http.Error(w, "Invalid since time", http.StatusBadRequest)
		return
	}

	base := h.db.
		Table("analysis_jobs aj").
		Joins("JOIN media_items m ON m.id = aj.media_id").
		Where("m.station_id = ?", station.ID).
		Where("aj.created_at >= ?", since)

	var total, pending, running, complete, failed int64
	_ = base.Session(&gorm.Session{}).Count(&total).Error
	_ = base.Session(&gorm.Session{}).Where("aj.status = ?", "pending").Count(&pending).Error
	_ = base.Session(&gorm.Session{}).Where("aj.status = ?", "running").Count(&running).Error
	_ = base.Session(&gorm.Session{}).Where("aj.status = ?", "complete").Count(&complete).Error
	_ = base.Session(&gorm.Session{}).Where("aj.status = ?", "failed").Count(&failed).Error

	done := complete + failed
	percent := 0
	if total > 0 {
		percent = int((float64(done) / float64(total)) * 100.0)
		if percent > 100 {
			percent = 100
		}
	}

	// Surface a few recent failures for quick debugging.
	var recentFailed []models.AnalysisJob
	_ = h.db.
		WithContext(r.Context()).
		Joins("JOIN media_items m ON m.id = analysis_jobs.media_id").
		Where("m.station_id = ? AND analysis_jobs.created_at >= ? AND analysis_jobs.status = ?", station.ID, since, "failed").
		Order("analysis_jobs.updated_at DESC").
		Limit(10).
		Find(&recentFailed).Error

	h.RenderPartial(w, r, "partials/duration-recalc-status", map[string]any{
		"HasBatch":     true,
		"Since":        sinceStr,
		"Total":        total,
		"Pending":      pending,
		"Running":      running,
		"Complete":     complete,
		"Failed":       failed,
		"Done":         done,
		"Percent":      percent,
		"RecentFailed": recentFailed,
		"UpdatedAt":    time.Now().UTC(),
	})
}

// MediaReanalyzeDurationsCurrentStatus returns current station analyzer queue stats (HTMX).
func (h *Handler) MediaReanalyzeDurationsCurrentStatus(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}
	if !h.HasStationPermission(r, "edit_metadata") {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	type jobWithMedia struct {
		MediaID   string    `gorm:"column:media_id"`
		Error     string    `gorm:"column:error"`
		UpdatedAt time.Time `gorm:"column:updated_at"`
		Title     string    `gorm:"column:title"`
		Artist    string    `gorm:"column:artist"`
	}

	base := h.db.
		Table("analysis_jobs aj").
		Joins("JOIN media_items m ON m.id = aj.media_id").
		Where("m.station_id = ?", station.ID)

	var pending, running, complete, failed int64
	_ = base.Session(&gorm.Session{}).Where("aj.status = ?", "pending").Count(&pending).Error
	_ = base.Session(&gorm.Session{}).Where("aj.status = ?", "running").Count(&running).Error
	_ = base.Session(&gorm.Session{}).Where("aj.status = ?", "complete").Count(&complete).Error
	_ = base.Session(&gorm.Session{}).Where("aj.status = ?", "failed").Count(&failed).Error
	activeCount := pending + running

	// Recent failures (last 24h) for quick debugging.
	since := time.Now().UTC().Add(-24 * time.Hour)
	var recentFailed []models.AnalysisJob
	_ = h.db.
		WithContext(r.Context()).
		Joins("JOIN media_items m ON m.id = analysis_jobs.media_id").
		Where("m.station_id = ? AND analysis_jobs.updated_at >= ? AND analysis_jobs.status = ?", station.ID, since, "failed").
		Order("analysis_jobs.updated_at DESC").
		Limit(10).
		Find(&recentFailed).Error

	// Last complete/failed for quick confidence/debugging.
	var lastComplete jobWithMedia
	_ = h.db.
		WithContext(r.Context()).
		Table("analysis_jobs aj").
		Joins("JOIN media_items m ON m.id = aj.media_id").
		Select("aj.media_id, aj.updated_at, aj.error, m.title, m.artist").
		Where("m.station_id = ? AND aj.status = ?", station.ID, "complete").
		Order("aj.updated_at DESC").
		Limit(1).
		Scan(&lastComplete).Error

	var lastFailed jobWithMedia
	_ = h.db.
		WithContext(r.Context()).
		Table("analysis_jobs aj").
		Joins("JOIN media_items m ON m.id = aj.media_id").
		Select("aj.media_id, aj.updated_at, aj.error, m.title, m.artist").
		Where("m.station_id = ? AND aj.status = ?", station.ID, "failed").
		Order("aj.updated_at DESC").
		Limit(1).
		Scan(&lastFailed).Error

	view := r.URL.Query().Get("view")
	tpl := "partials/duration-recalc-queue-status"
	if view == "compact" {
		tpl = "partials/analyzer-queue-compact"
	}

	data := map[string]any{
		"Pending":      pending,
		"Running":      running,
		"ActiveCount":  activeCount,
		"Complete":     complete,
		"Failed":       failed,
		"RecentFailed": recentFailed,
		"LastComplete": lastComplete,
		"LastFailed":   lastFailed,
		"UpdatedAt":    time.Now().UTC(),
	}

	h.RenderPartial(w, r, tpl, data)
}

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
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 200 {
		perPage = 50
	}

	query := r.URL.Query().Get("q")
	genre := r.URL.Query().Get("genre")
	artist := r.URL.Query().Get("artist")
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "created_at"
	}
	sortOrder := r.URL.Query().Get("order")
	if sortOrder == "" {
		sortOrder = "desc"
	}

	// Validate sort column against allow-list.
	allowedSorts := map[string]bool{
		"title": true, "artist": true, "album": true,
		"duration": true, "created_at": true, "genre": true,
	}
	if !allowedSorts[sortBy] {
		sortBy = "created_at"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	var media []models.MediaItem
	var total int64

	dbQuery := h.db.Model(&models.MediaItem{}).Where("station_id = ?", station.ID)

	// Search filter with punctuation/spacing normalization.
	dbQuery = applyLooseMediaSearch(dbQuery, query)

	// Genre filter
	if genre != "" {
		dbQuery = dbQuery.Where("genre = ?", genre)
	}

	// Artist filter
	if artist != "" {
		dbQuery = dbQuery.Where("artist = ?", artist)
	}

	// Use Session clones to avoid Count mutating query state
	dbQuery.Session(&gorm.Session{}).Count(&total)

	// Sorting — omit large blobs from list queries
	orderClause := sortBy + " " + strings.ToUpper(sortOrder)
	dbQuery.Session(&gorm.Session{}).Omit("artwork", "waveform").Order(orderClause).
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&media)

	// Get unique genres for filter
	var genres []string
	h.db.Model(&models.MediaItem{}).
		Where("station_id = ? AND genre != ''", station.ID).
		Distinct().
		Order("genre ASC").
		Pluck("genre", &genres)

	// Get unique artists for filter
	var artists []string
	h.db.Model(&models.MediaItem{}).
		Where("station_id = ? AND artist != ''", station.ID).
		Distinct().
		Order("artist ASC").
		Pluck("artist", &artists)

	totalPages := int(total) / perPage
	if int(total)%perPage > 0 {
		totalPages++
	}

	// Build page number window for pagination.
	pageNumbers := buildPageNumbers(page, totalPages)

	data := map[string]any{
		"Media":       media,
		"Total":       total,
		"Page":        page,
		"PerPage":     perPage,
		"TotalPages":  totalPages,
		"PageNumbers": pageNumbers,
		"Query":       query,
		"Genre":       genre,
		"Artist":      artist,
		"SortBy":      sortBy,
		"SortOrder":   sortOrder,
		"Genres":      genres,
		"Artists":     artists,
	}

	// If HTMX request, render just the results partial.
	if r.Header.Get("HX-Request") == "true" {
		h.RenderPartial(w, r, "partials/media-library-results", data)
		return
	}

	h.Render(w, r, "pages/dashboard/media/list", PageData{
		Title:    "Media Library",
		Stations: h.LoadStations(r),
		Data:     data,
	})
}

// MediaTablePartial renders just the media table (for HTMX)
func (h *Handler) MediaTablePartial(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	media := h.queryFilteredMedia(r, station.ID)
	h.RenderPartial(w, r, "partials/media-table", media)
}

// MediaGridPartial renders media as cards (for HTMX)
func (h *Handler) MediaGridPartial(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	media := h.queryFilteredMedia(r, station.ID)
	h.RenderPartial(w, r, "partials/media-grid", media)
}

// MediaSearchJSON returns media items as JSON for media pickers (inline search).
func (h *Handler) MediaSearchJSON(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	query := r.URL.Query().Get("q")
	genre := r.URL.Query().Get("genre")
	artist := r.URL.Query().Get("artist")
	includeArchive := r.URL.Query().Get("include_archive") == "true"
	limit := 50

	dbQuery := h.db.Model(&models.MediaItem{})
	if includeArchive {
		dbQuery = dbQuery.Where("station_id = ? OR show_in_archive = ?", station.ID, true)
		dbQuery = dbQuery.Preload("Station")
	} else {
		dbQuery = dbQuery.Where("station_id = ?", station.ID)
	}

	dbQuery = applyLooseMediaSearch(dbQuery, query)

	if genre != "" {
		dbQuery = dbQuery.Where("genre = ?", genre)
	}
	if artist != "" {
		dbQuery = dbQuery.Where("artist = ?", artist)
	}

	// Only include analysed media with valid duration.
	dbQuery = dbQuery.Where("analysis_state != 'failed' AND duration > 0")

	var items []models.MediaItem
	dbQuery.Omit("artwork", "waveform", "lyrics").
		Order("artist ASC, title ASC").
		Limit(limit).
		Find(&items)

	type mediaResult struct {
		ID          string  `json:"id"`
		Title       string  `json:"title"`
		Artist      string  `json:"artist"`
		Album       string  `json:"album"`
		Genre       string  `json:"genre"`
		Duration    float64 `json:"duration"`
		StationName string  `json:"station_name,omitempty"`
		IsArchive   bool    `json:"is_archive,omitempty"`
	}

	results := make([]mediaResult, len(items))
	for i, item := range items {
		results[i] = mediaResult{
			ID:       item.ID,
			Title:    item.Title,
			Artist:   item.Artist,
			Album:    item.Album,
			Genre:    item.Genre,
			Duration: item.Duration.Seconds(),
		}
		if includeArchive && item.StationID != station.ID {
			results[i].IsArchive = true
			if item.Station != nil {
				results[i].StationName = item.Station.Name
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// queryFilteredMedia applies common search/filter/sort/limit params from the
// request and returns matching media items. Used by table/grid partials.
func (h *Handler) queryFilteredMedia(r *http.Request, stationID string) []models.MediaItem {
	query := r.URL.Query().Get("q")
	genre := r.URL.Query().Get("genre")
	artist := r.URL.Query().Get("artist")
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "created_at"
	}
	sortOrder := r.URL.Query().Get("order")
	if sortOrder == "" {
		sortOrder = "desc"
	}

	allowedSorts := map[string]bool{
		"title": true, "artist": true, "album": true,
		"duration": true, "created_at": true, "genre": true,
	}
	if !allowedSorts[sortBy] {
		sortBy = "created_at"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	dbQuery := h.db.Where("station_id = ?", stationID)
	dbQuery = applyLooseMediaSearch(dbQuery, query)

	if genre != "" {
		dbQuery = dbQuery.Where("genre = ?", genre)
	}
	if artist != "" {
		dbQuery = dbQuery.Where("artist = ?", artist)
	}

	var media []models.MediaItem
	orderClause := sortBy + " " + strings.ToUpper(sortOrder)
	dbQuery.Omit("artwork", "waveform").Order(orderClause).Limit(100).Find(&media)
	return media
}

// MediaUploadPage renders the upload page
func (h *Handler) MediaUploadPage(w http.ResponseWriter, r *http.Request) {
	maxUploadBytes := h.multipartLimit(1 << 30)
	h.Render(w, r, "pages/dashboard/media/upload", PageData{
		Title:    "Upload Media",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"MaxUploadBytes": maxUploadBytes,
		},
	})
}

// MediaUpload handles file uploads
func (h *Handler) MediaUpload(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	// Parse multipart form (default 1GB, configurable via GRIMNIR_MAX_UPLOAD_SIZE_MB)
	maxUploadBytes := h.multipartLimit(1 << 30)
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	// ParseMultipartForm's argument is in-memory buffer, not total body size.
	// Keep memory bounded and let net/http spill larger parts to temp files.
	parseMemoryBytes := int64(32 << 20) // 32 MiB
	if maxUploadBytes > 0 && maxUploadBytes < parseMemoryBytes {
		parseMemoryBytes = maxUploadBytes
	}
	if err := r.ParseMultipartForm(parseMemoryBytes); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) || strings.Contains(strings.ToLower(err.Error()), "request body too large") {
			http.Error(w, fmt.Sprintf("File too large. Maximum allowed size is %s.", humanReadableBytes(maxUploadBytes)), http.StatusBadRequest)
			return
		}
		http.Error(w, "Invalid upload form data", http.StatusBadRequest)
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
	hasher := sha256.New()
	written, err := io.Copy(dst, io.TeeReader(file, hasher))
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to write media file")
		os.Remove(fullPath) // Clean up partial file
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	contentHash := hex.EncodeToString(hasher.Sum(nil))

	// Sync file to disk to ensure it's fully written before analysis
	if err := dst.Sync(); err != nil {
		h.logger.Warn().Err(err).Msg("failed to sync media file")
	}

	// Reject duplicate content in the same station.
	var existing models.MediaItem
	err = h.db.Select("id, title, artist").Where("station_id = ? AND content_hash = ?", station.ID, contentHash).First(&existing).Error
	if err == nil {
		_ = os.Remove(fullPath)
		msg := "Duplicate file already exists in media library"
		if existing.Title != "" || existing.Artist != "" {
			msg = fmt.Sprintf("This file already exists as \"%s\" by \"%s\"",
				existing.Title, existing.Artist)
		}
		http.Error(w, msg, http.StatusConflict)
		return
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		h.logger.Error().Err(err).Str("station_id", station.ID).Str("content_hash", contentHash).Msg("failed duplicate media lookup")
		_ = os.Remove(fullPath)
		http.Error(w, "Failed to validate upload", http.StatusInternalServerError)
		return
	}

	// Create media item record with station's default archive settings
	var createdBy *string
	if user := h.GetUser(r); user != nil && strings.TrimSpace(user.ID) != "" {
		createdBy = &user.ID
	}
	media := models.MediaItem{
		ID:               mediaID,
		StationID:        station.ID,
		CreatedBy:        createdBy,
		Title:            strings.TrimSuffix(header.Filename, ext),
		Path:             relPath,
		ContentHash:      contentHash,
		OriginalFilename: header.Filename,
		ShowInArchive:    station.DefaultShowInArchive,
		AllowDownload:    station.DefaultAllowDownload,
		AnalysisState:    models.AnalysisPending,
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
	if r.Header.Get("HX-Request") == "true" || r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(fmt.Sprintf(`<div class="alert alert-success">File uploaded successfully! <a href="/dashboard/media/%s">View details</a></div>`, mediaID)))
		return
	}

	http.Redirect(w, r, "/dashboard/media/"+mediaID, http.StatusSeeOther)
}

func humanReadableBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	value = math.Round(value*10) / 10
	suffix := "KMGTPE"[exp]
	return fmt.Sprintf("%.1f %ciB", value, suffix)
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

	// Load mounts for queue actions
	var mounts []models.Mount
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&mounts)

	// Load usage references (playlists, schedule entries, clock slots)
	usageMap := h.loadMediaUsage(r, station.ID, []string{id})
	usage := usageMap[id]

	// Check for duplicates (same hash)
	var hashDuplicates []models.MediaItem
	if media.ContentHash != "" {
		h.db.Omit("artwork", "waveform").
			Where("station_id = ? AND content_hash = ? AND id != ?", station.ID, media.ContentHash, id).
			Limit(10).
			Find(&hashDuplicates)
	}

	h.Render(w, r, "pages/dashboard/media/detail", PageData{
		Title:    media.Title,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Media":          media,
			"History":        history,
			"Mounts":         mounts,
			"Usage":          usage,
			"HashDuplicates": hashDuplicates,
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

	fileFormat := "-"
	fileSize := int64(0)
	if strings.TrimSpace(media.Path) != "" {
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(media.Path)), ".")
		if ext != "" {
			fileFormat = strings.ToUpper(ext)
		}
		fullPath := filepath.Join(h.mediaRoot, media.Path)
		if stat, err := os.Stat(fullPath); err == nil {
			fileSize = stat.Size()
		}
	}

	type mediaEditView struct {
		models.MediaItem
		Format string
		Size   int64
	}
	view := mediaEditView{
		MediaItem: media,
		Format:    fileFormat,
		Size:      fileSize,
	}

	h.Render(w, r, "pages/dashboard/media/edit", PageData{
		Title:    "Edit: " + media.Title,
		Stations: h.LoadStations(r),
		Data:     view,
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

	// Support multipart submissions for artwork updates while still handling standard form posts.
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		if !errors.Is(err, http.ErrNotMultipart) {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Invalid form", http.StatusBadRequest)
				return
			}
		}
	}

	media.Title = r.FormValue("title")
	media.Artist = r.FormValue("artist")
	media.Album = r.FormValue("album")
	media.Genre = r.FormValue("genre")
	media.Year = r.FormValue("year")
	media.Label = r.FormValue("label")
	media.ISRC = r.FormValue("isrc")
	media.Composer = r.FormValue("composer")
	media.Conductor = r.FormValue("conductor")
	media.Copyright = r.FormValue("copyright")
	media.Publisher = r.FormValue("publisher")
	media.OriginalArtist = r.FormValue("original_artist")
	media.AlbumArtist = r.FormValue("album_artist")
	media.Comment = r.FormValue("comment")
	media.Lyrics = r.FormValue("lyrics")
	media.Language = r.FormValue("language")
	media.Mood = r.FormValue("mood")
	media.TrackNumber = parseInt(r.FormValue("track_number"), 0)
	media.DiscNumber = parseInt(r.FormValue("disc_number"), 0)
	media.Explicit = r.FormValue("explicit") == "on"
	media.ShowInArchive = r.FormValue("show_in_archive") == "on"
	media.AllowDownload = r.FormValue("allow_download") == "on"

	if r.FormValue("remove_artwork") == "on" {
		media.Artwork = nil
		media.ArtworkMime = ""
	}
	if file, _, err := r.FormFile("artwork_file"); err == nil {
		defer file.Close()
		const maxArtworkSize = 8 << 20 // 8 MiB
		data, readErr := io.ReadAll(io.LimitReader(file, maxArtworkSize+1))
		if readErr != nil {
			http.Error(w, "Failed to read artwork file", http.StatusBadRequest)
			return
		}
		if len(data) == 0 {
			http.Error(w, "Artwork file is empty", http.StatusBadRequest)
			return
		}
		if len(data) > maxArtworkSize {
			http.Error(w, "Artwork file is too large (max 8 MiB)", http.StatusBadRequest)
			return
		}
		mime := http.DetectContentType(data)
		switch mime {
		case "image/jpeg", "image/png", "image/webp", "image/gif":
			media.Artwork = data
			media.ArtworkMime = mime
		default:
			http.Error(w, "Unsupported artwork format. Use JPEG, PNG, WEBP, or GIF.", http.StatusBadRequest)
			return
		}
	} else if err != nil && !errors.Is(err, http.ErrMissingFile) {
		http.Error(w, "Invalid artwork upload", http.StatusBadRequest)
		return
	}

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

	// Delete from database within a transaction, cleaning up all references first.
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := adminDeleteMediaReferences(tx, []string{id}); err != nil {
			return err
		}
		return tx.Delete(&models.MediaItem{}, "id = ? AND station_id = ?", id, station.ID).Error
	}); err != nil {
		http.Error(w, "Failed to delete media", http.StatusInternalServerError)
		return
	}

	// Best-effort file deletion outside transaction.
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
				ID:         uuid.NewString(),
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

// MediaGenres renders the genre management page.
func (h *Handler) MediaGenres(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	type genreRow struct {
		Genre string
		Count int64
	}

	var rows []genreRow
	h.db.Model(&models.MediaItem{}).
		Select("genre, COUNT(*) as count").
		Where("station_id = ? AND genre != ''", station.ID).
		Group("genre").
		Order("genre ASC").
		Scan(&rows)

	// Also count items with no genre
	var noGenreCount int64
	h.db.Model(&models.MediaItem{}).
		Where("station_id = ? AND (genre = '' OR genre IS NULL)", station.ID).
		Count(&noGenreCount)

	h.Render(w, r, "pages/dashboard/media/genres", PageData{
		Title:    "Manage Genres",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Genres":       rows,
			"NoGenreCount": noGenreCount,
		},
	})
}

// MediaGenreReassign batch-reassigns all media from one genre to another.
func (h *Handler) MediaGenreReassign(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	oldGenre := strings.TrimSpace(r.FormValue("old_genre"))
	newGenre := strings.TrimSpace(r.FormValue("new_genre"))

	if oldGenre == "" {
		http.Error(w, "No genre specified", http.StatusBadRequest)
		return
	}

	result := h.db.Model(&models.MediaItem{}).
		Where("station_id = ? AND genre = ?", station.ID, oldGenre).
		Update("genre", newGenre)

	if result.Error != nil {
		h.logger.Error().Err(result.Error).Str("old", oldGenre).Str("new", newGenre).Msg("genre reassign failed")
		http.Error(w, "Update failed", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("old_genre", oldGenre).
		Str("new_genre", newGenre).
		Int64("affected", result.RowsAffected).
		Str("station_id", station.ID).
		Msg("genre reassigned")

	http.Redirect(w, r, "/dashboard/media/genres", http.StatusSeeOther)
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

// buildPageNumbers returns a slice of page numbers (with 0 as ellipsis
// placeholder) for a pagination widget. It always includes the first and
// last page and up to two neighbours of the current page.
func buildPageNumbers(current, total int) []int {
	if total <= 1 {
		return nil
	}
	seen := map[int]bool{}
	add := func(n int) {
		if n >= 1 && n <= total {
			seen[n] = true
		}
	}
	add(1)
	add(total)
	for d := -2; d <= 2; d++ {
		add(current + d)
	}
	pages := make([]int, 0, len(seen))
	for p := range seen {
		pages = append(pages, p)
	}
	// Sort
	for i := 0; i < len(pages); i++ {
		for j := i + 1; j < len(pages); j++ {
			if pages[j] < pages[i] {
				pages[i], pages[j] = pages[j], pages[i]
			}
		}
	}
	// Insert 0 (ellipsis) where there are gaps > 1
	result := make([]int, 0, len(pages)*2)
	for i, p := range pages {
		if i > 0 && p-pages[i-1] > 1 {
			result = append(result, 0)
		}
		result = append(result, p)
	}
	return result
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

// ── Duplicate Detection ──────────────────────────────────────────

type duplicateGroup struct {
	Key   string // hash or "artist – title"
	Count int
	Items []duplicateItem
}

// duplicateItem is a media item enriched with usage information.
type duplicateItem struct {
	models.MediaItem
	Playlists       []playlistRef
	ScheduleEntries []scheduleRef
	ClockSlots      []clockSlotRef
}

type playlistRef struct {
	PlaylistID   string
	PlaylistName string
}

type scheduleRef struct {
	EntryID  string
	StartsAt time.Time
}

type clockSlotRef struct {
	ClockHourID string
	ClockName   string
	Position    int
}

// MediaDuplicates renders the station-level duplicate finder page.
func (h *Handler) MediaDuplicates(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	notice := strings.TrimSpace(r.URL.Query().Get("notice"))
	errMsg := strings.TrimSpace(r.URL.Query().Get("error"))

	// ── Hash-based duplicates (exact file match) ──
	type hashCount struct {
		ContentHash string
		Count       int64
	}
	var hashCounts []hashCount
	if err := h.db.Model(&models.MediaItem{}).
		Select("content_hash, COUNT(*) as count").
		Where("station_id = ? AND content_hash IS NOT NULL AND content_hash <> ''", station.ID).
		Group("content_hash").
		Having("COUNT(*) > 1").
		Order("count DESC").
		Limit(200).
		Scan(&hashCounts).Error; err != nil {
		h.logger.Error().Err(err).Str("station_id", station.ID).Msg("failed to load hash duplicate groups")
		http.Error(w, "Failed to load duplicates", http.StatusInternalServerError)
		return
	}

	hashes := make([]string, 0, len(hashCounts))
	for _, hc := range hashCounts {
		hashes = append(hashes, hc.ContentHash)
	}

	var hashGroups []duplicateGroup
	if len(hashes) > 0 {
		var items []models.MediaItem
		if err := h.db.Omit("artwork", "waveform").
			Where("station_id = ? AND content_hash IN ?", station.ID, hashes).
			Order("content_hash ASC, created_at ASC").
			Find(&items).Error; err != nil {
			h.logger.Error().Err(err).Msg("failed to load hash duplicate items")
			http.Error(w, "Failed to load duplicates", http.StatusInternalServerError)
			return
		}

		mediaIDs := make([]string, 0, len(items))
		for _, item := range items {
			mediaIDs = append(mediaIDs, item.ID)
		}
		usageMap := h.loadMediaUsage(r, station.ID, mediaIDs)

		byHash := make(map[string][]duplicateItem)
		for _, item := range items {
			di := duplicateItem{MediaItem: item}
			if u, ok := usageMap[item.ID]; ok {
				di.Playlists = u.Playlists
				di.ScheduleEntries = u.ScheduleEntries
				di.ClockSlots = u.ClockSlots
			}
			byHash[item.ContentHash] = append(byHash[item.ContentHash], di)
		}
		for _, hc := range hashCounts {
			groupItems := byHash[hc.ContentHash]
			if len(groupItems) < 2 {
				continue
			}
			hashGroups = append(hashGroups, duplicateGroup{
				Key:   hc.ContentHash,
				Count: len(groupItems),
				Items: groupItems,
			})
		}
	}

	// ── Metadata-based duplicates (same artist + title) ──
	type metaKey struct {
		Artist string
		Title  string
		Count  int64
	}
	var metaCounts []metaKey
	if err := h.db.Model(&models.MediaItem{}).
		Select("LOWER(TRIM(artist)) as artist, LOWER(TRIM(title)) as title, COUNT(*) as count").
		Where("station_id = ? AND artist <> '' AND title <> ''", station.ID).
		Group("LOWER(TRIM(artist)), LOWER(TRIM(title))").
		Having("COUNT(*) > 1").
		Order("count DESC").
		Limit(200).
		Scan(&metaCounts).Error; err != nil {
		h.logger.Error().Err(err).Str("station_id", station.ID).Msg("failed to load metadata duplicate groups")
		http.Error(w, "Failed to load duplicates", http.StatusInternalServerError)
		return
	}

	var metaGroups []duplicateGroup
	if len(metaCounts) > 0 {
		for _, mk := range metaCounts {
			var items []models.MediaItem
			if err := h.db.Omit("artwork", "waveform").
				Where("station_id = ? AND LOWER(TRIM(artist)) = ? AND LOWER(TRIM(title)) = ?",
					station.ID, mk.Artist, mk.Title).
				Order("created_at ASC").
				Find(&items).Error; err != nil {
				continue
			}
			if len(items) < 2 {
				continue
			}

			mediaIDs := make([]string, 0, len(items))
			for _, item := range items {
				mediaIDs = append(mediaIDs, item.ID)
			}
			usageMap := h.loadMediaUsage(r, station.ID, mediaIDs)

			var groupItems []duplicateItem
			for _, item := range items {
				di := duplicateItem{MediaItem: item}
				if u, ok := usageMap[item.ID]; ok {
					di.Playlists = u.Playlists
					di.ScheduleEntries = u.ScheduleEntries
					di.ClockSlots = u.ClockSlots
				}
				groupItems = append(groupItems, di)
			}

			label := items[0].Artist + " \u2013 " + items[0].Title
			metaGroups = append(metaGroups, duplicateGroup{
				Key:   label,
				Count: len(groupItems),
				Items: groupItems,
			})
		}
	}

	// Count items missing hashes in this station.
	var missingHashCount int64
	_ = h.db.Model(&models.MediaItem{}).
		Where("station_id = ? AND (content_hash IS NULL OR content_hash = '')", station.ID).
		Count(&missingHashCount).Error

	h.Render(w, r, "pages/dashboard/media/duplicates", PageData{
		Title:    "Find Duplicates",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"HashGroups":       hashGroups,
			"HashGroupCount":   len(hashGroups),
			"MetaGroups":       metaGroups,
			"MetaGroupCount":   len(metaGroups),
			"MissingHashCount": missingHashCount,
			"Notice":           notice,
			"Error":            errMsg,
		},
	})
}

// mediaUsage aggregates usage references for a single media item.
type mediaUsage struct {
	Playlists       []playlistRef
	ScheduleEntries []scheduleRef
	ClockSlots      []clockSlotRef
}

// loadMediaUsage loads playlist, schedule, and clock slot references for a set of media IDs.
func (h *Handler) loadMediaUsage(r *http.Request, stationID string, mediaIDs []string) map[string]mediaUsage {
	result := make(map[string]mediaUsage, len(mediaIDs))
	if len(mediaIDs) == 0 {
		return result
	}

	// Playlist references
	type plRow struct {
		MediaID      string `gorm:"column:media_id"`
		PlaylistID   string `gorm:"column:playlist_id"`
		PlaylistName string `gorm:"column:name"`
	}
	var plRows []plRow
	_ = h.db.WithContext(r.Context()).
		Table("playlist_items pi").
		Select("pi.media_id, pi.playlist_id, p.name").
		Joins("JOIN playlists p ON p.id = pi.playlist_id").
		Where("pi.media_id IN ? AND p.station_id = ?", mediaIDs, stationID).
		Scan(&plRows).Error
	for _, row := range plRows {
		u := result[row.MediaID]
		u.Playlists = append(u.Playlists, playlistRef{
			PlaylistID:   row.PlaylistID,
			PlaylistName: row.PlaylistName,
		})
		result[row.MediaID] = u
	}

	// Schedule entries (upcoming, source_type = "media")
	now := time.Now().UTC()
	var seRows []models.ScheduleEntry
	_ = h.db.WithContext(r.Context()).
		Where("station_id = ? AND source_type = ? AND source_id IN ? AND ends_at > ?",
			stationID, "media", mediaIDs, now).
		Order("starts_at ASC").
		Limit(100).
		Find(&seRows).Error
	for _, se := range seRows {
		u := result[se.SourceID]
		u.ScheduleEntries = append(u.ScheduleEntries, scheduleRef{
			EntryID:  se.ID,
			StartsAt: se.StartsAt,
		})
		result[se.SourceID] = u
	}

	// Clock slots (hard_item with media_id in payload JSON)
	type csRow struct {
		MediaID     string `gorm:"column:media_id"`
		ClockHourID string `gorm:"column:clock_hour_id"`
		ClockName   string `gorm:"column:name"`
		Position    int    `gorm:"column:position"`
	}
	var csRows []csRow
	_ = h.db.WithContext(r.Context()).
		Table("clock_slots cs").
		Select("cs.payload->>'media_id' as media_id, cs.clock_hour_id, ch.name, cs.position").
		Joins("JOIN clock_hours ch ON ch.id = cs.clock_hour_id").
		Where("ch.station_id = ? AND cs.type = ? AND cs.payload->>'media_id' IN ?",
			stationID, "hard_item", mediaIDs).
		Scan(&csRows).Error
	for _, row := range csRows {
		u := result[row.MediaID]
		u.ClockSlots = append(u.ClockSlots, clockSlotRef{
			ClockHourID: row.ClockHourID,
			ClockName:   row.ClockName,
			Position:    row.Position,
		})
		result[row.MediaID] = u
	}

	return result
}

// MediaPurgeDuplicates deletes selected duplicate media items within the station.
func (h *Handler) MediaPurgeDuplicates(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}
	if !h.HasStationPermission(r, "delete_media") {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	ids := r.Form["ids"]
	if len(ids) == 0 {
		http.Redirect(w, r, "/dashboard/media/duplicates?error=No+items+selected", http.StatusSeeOther)
		return
	}

	// Verify all selected items belong to this station.
	var selected []models.MediaItem
	if err := h.db.Select("id, path, content_hash").
		Where("id IN ? AND station_id = ?", ids, station.ID).
		Find(&selected).Error; err != nil {
		http.Error(w, "Failed to load selected media", http.StatusInternalServerError)
		return
	}
	if len(selected) == 0 {
		http.Redirect(w, r, "/dashboard/media/duplicates?error=No+matching+media+found", http.StatusSeeOther)
		return
	}

	validIDs := make([]string, 0, len(selected))
	for _, item := range selected {
		validIDs = append(validIDs, item.ID)
	}

	var deleted int64
	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := adminDeleteMediaReferences(tx, validIDs); err != nil {
			return err
		}
		result := tx.Where("id IN ? AND station_id = ?", validIDs, station.ID).Delete(&models.MediaItem{})
		if result.Error != nil {
			return result.Error
		}
		deleted = result.RowsAffected
		return nil
	})
	if err != nil {
		h.logger.Error().Err(err).Int("selected", len(validIDs)).Str("station_id", station.ID).Msg("duplicate purge failed")
		http.Redirect(w, r, "/dashboard/media/duplicates?error=Failed+to+purge+selected+duplicates", http.StatusSeeOther)
		return
	}

	// Best-effort file deletion outside transaction.
	for _, item := range selected {
		if strings.TrimSpace(item.Path) == "" {
			continue
		}
		fullPath := filepath.Join(h.mediaRoot, item.Path)
		if rmErr := os.Remove(fullPath); rmErr != nil && !os.IsNotExist(rmErr) {
			h.logger.Warn().Err(rmErr).Str("path", fullPath).Msg("failed to delete media file during duplicate purge")
		}
	}

	h.logger.Info().
		Str("station_id", station.ID).
		Int64("deleted", deleted).
		Msg("station duplicate purge completed")

	msg := fmt.Sprintf("Purged %d duplicate media items.", deleted)
	http.Redirect(w, r, "/dashboard/media/duplicates?notice="+msg, http.StatusSeeOther)
}

// MediaBackfillHashes computes missing SHA-256 hashes for station media.
func (h *Handler) MediaBackfillHashes(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}
	if !h.HasStationPermission(r, "edit_metadata") {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	var media []models.MediaItem
	if err := h.db.Select("id, path, content_hash").
		Where("station_id = ? AND (content_hash IS NULL OR content_hash = '')", station.ID).
		Find(&media).Error; err != nil {
		http.Error(w, "Failed to load media", http.StatusInternalServerError)
		return
	}

	updated, missing, failed := 0, 0, 0
	for _, item := range media {
		if strings.TrimSpace(item.Path) == "" {
			failed++
			continue
		}
		fullPath := filepath.Join(h.mediaRoot, item.Path)
		hash, err := computeSHA256File(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				missing++
			} else {
				failed++
			}
			continue
		}
		if hash == "" {
			failed++
			continue
		}
		if err := h.db.Model(&models.MediaItem{}).Where("id = ?", item.ID).Update("content_hash", hash).Error; err != nil {
			failed++
			continue
		}
		updated++
	}

	msg := fmt.Sprintf("Hash backfill: %d updated, %d missing files, %d failed", updated, missing, failed)
	http.Redirect(w, r, "/dashboard/media/duplicates?notice="+msg, http.StatusSeeOther)
}
