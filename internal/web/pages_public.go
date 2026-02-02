/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/landingpage"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Landing renders the public platform landing page
func (h *Handler) Landing(w http.ResponseWriter, r *http.Request) {
	// Get platform landing page config
	var config map[string]any
	var theme *landingpage.Theme

	if h.landingPageSvc != nil {
		page, err := h.landingPageSvc.GetOrCreatePlatform(r.Context())
		if err == nil && page != nil {
			// Use PUBLISHED config (not draft) for public view
			config = page.PublishedConfig

			// Get theme from published config
			themeID := "daw-dark"
			if tid, ok := config["theme"].(string); ok && tid != "" {
				themeID = tid
			}
			theme = h.landingPageSvc.GetTheme(themeID)
			if theme == nil {
				theme = h.landingPageSvc.GetTheme("daw-dark")
			}
		}
	}

	// Get all public, approved, active stations for the stations grid
	var stations []models.Station
	h.db.Where("active = ? AND public = ? AND approved = ?", true, true, true).
		Order("sort_order, name").
		Find(&stations)

	// Order stations based on config
	orderedStations := orderStationsByConfig(stations, config)

	// Prepare stations with their mounts and stream URLs
	type stationWithStream struct {
		Station     models.Station
		StreamURL   string
		StreamURLLQ string
		MountName   string
	}

	var stationsWithStreams []stationWithStream
	for _, s := range orderedStations {
		var mount models.Mount
		h.db.Where("station_id = ?", s.ID).First(&mount)

		sw := stationWithStream{Station: s}
		if mount.ID != "" {
			sw.StreamURL = "/live/" + mount.Name
			sw.StreamURLLQ = "/live/" + mount.Name + "-lq"
			sw.MountName = mount.Name
		}
		stationsWithStreams = append(stationsWithStreams, sw)
	}

	// Get featured station for hero player (if any)
	var featuredStation *stationWithStream
	for i, s := range stationsWithStreams {
		if s.Station.Featured {
			featuredStation = &stationsWithStreams[i]
			break
		}
	}
	// Fallback to first station if none featured
	if featuredStation == nil && len(stationsWithStreams) > 0 {
		featuredStation = &stationsWithStreams[0]
	}

	// Render using the platform landing preview template (same as editor preview)
	h.Render(w, r, "pages/public/platform-landing-preview", PageData{
		Title: "Welcome",
		Data: map[string]any{
			"Config":          config,
			"Theme":           theme,
			"Stations":        stations,
			"OrderedStations": stationsWithStreams,
			"FeaturedStation": featuredStation,
			"IsPlatform":      true,
			"IsPreview":       false,
		},
	})
}

// Listen renders the public listening page
func (h *Handler) Listen(w http.ResponseWriter, r *http.Request) {
	// Get public, approved, active stations and their mounts
	var stations []models.Station
	h.db.Where("active = ? AND public = ? AND approved = ?", true, true, true).Find(&stations)

	type mountWithURL struct {
		models.Mount
		URL   string
		LQURL string // Low-quality stream URL
	}

	type stationWithMounts struct {
		Station models.Station
		Mounts  []mountWithURL
	}

	var data []stationWithMounts
	for _, s := range stations {
		var mounts []models.Mount
		h.db.Where("station_id = ?", s.ID).Find(&mounts)

		var mountsWithURLs []mountWithURL
		for _, m := range mounts {
			// Use the built-in broadcast server at /live/{mount}
			streamURL := "/live/" + m.Name
			lqStreamURL := "/live/" + m.Name + "-lq"
			mountsWithURLs = append(mountsWithURLs, mountWithURL{
				Mount: m,
				URL:   streamURL,
				LQURL: lqStreamURL,
			})
		}

		data = append(data, stationWithMounts{Station: s, Mounts: mountsWithURLs})
	}

	h.Render(w, r, "pages/public/listen", PageData{
		Title: "Listen Live",
		Data:  data,
	})
}

// StationLanding renders the public landing page for a specific station by shortcode
func (h *Handler) StationLanding(w http.ResponseWriter, r *http.Request) {
	shortcode := chi.URLParam(r, "shortcode")
	if shortcode == "" {
		http.NotFound(w, r)
		return
	}

	// Find station by shortcode
	var station models.Station
	err := h.db.Where("shortcode = ? AND active = ? AND public = ? AND approved = ?",
		shortcode, true, true, true).First(&station).Error
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Get station's mount for the player
	var mount models.Mount
	h.db.Where("station_id = ?", station.ID).First(&mount)

	// Build stream URLs
	streamURL := ""
	streamURLLQ := ""
	if mount.ID != "" {
		streamURL = "/live/" + mount.Name
		streamURLLQ = "/live/" + mount.Name + "-lq"
	}

	// Get station's landing page config
	var config map[string]any
	if h.landingPageSvc != nil {
		page, err := h.landingPageSvc.Get(r.Context(), station.ID)
		if err == nil && page != nil {
			config = page.PublishedConfig
		}
	}

	h.Render(w, r, "pages/public/station-landing", PageData{
		Title: station.Name,
		Data: map[string]any{
			"Station":     station,
			"StationID":   station.ID,
			"StationName": station.Name,
			"MountName":   mount.Name,
			"StreamURL":   streamURL,
			"StreamURLLQ": streamURLLQ,
			"Config":      config,
		},
	})
}

// Archive renders the public archive browser
func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	// Pagination
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	perPage := 24

	// Get public stations for filtering
	var publicStations []models.Station
	h.db.Where("active = ? AND public = ? AND approved = ?", true, true, true).Find(&publicStations)

	// Build list of public station IDs
	var publicStationIDs []string
	for _, s := range publicStations {
		publicStationIDs = append(publicStationIDs, s.ID)
	}

	// Base query for archive media
	baseQuery := h.db.Model(&models.MediaItem{}).Where("show_in_archive = ?", true)
	if len(publicStationIDs) > 0 {
		baseQuery = baseQuery.Where("station_id IN ?", publicStationIDs)
	} else {
		baseQuery = baseQuery.Where("1=0")
	}

	// Fetch distinct values for filter dropdowns (only if there are public stations)
	var genres []string
	var years []string
	var artists []string
	if len(publicStationIDs) > 0 {
		h.db.Model(&models.MediaItem{}).
			Where("show_in_archive = ? AND station_id IN ? AND genre != '' AND genre IS NOT NULL", true, publicStationIDs).
			Distinct().Pluck("genre", &genres)

		h.db.Model(&models.MediaItem{}).
			Where("show_in_archive = ? AND station_id IN ? AND year != '' AND year IS NOT NULL", true, publicStationIDs).
			Distinct().Order("year DESC").Pluck("year", &years)

		h.db.Model(&models.MediaItem{}).
			Where("show_in_archive = ? AND station_id IN ? AND artist != '' AND artist IS NOT NULL", true, publicStationIDs).
			Distinct().Order("artist ASC").Pluck("artist", &artists)
	}

	var media []models.MediaItem
	var total int64

	// Clone base query for filtering (must use Session to get a true clone)
	query := baseQuery.Session(&gorm.Session{})

	// Get filter parameters
	stationID := r.URL.Query().Get("station")
	genre := r.URL.Query().Get("genre")
	year := r.URL.Query().Get("year")
	artist := r.URL.Query().Get("artist")
	sortBy := r.URL.Query().Get("sort")
	duration := r.URL.Query().Get("duration")
	searchQuery := r.URL.Query().Get("q")

	// Station filter
	if stationID != "" {
		isPublic := false
		for _, id := range publicStationIDs {
			if id == stationID {
				isPublic = true
				break
			}
		}
		if isPublic {
			query = query.Where("station_id = ?", stationID)
		}
	}

	// Genre filter
	if genre != "" {
		query = query.Where("genre = ?", genre)
	}

	// Year filter
	if year != "" {
		query = query.Where("year = ?", year)
	}

	// Artist filter
	if artist != "" {
		query = query.Where("artist = ?", artist)
	}

	// Duration filter (Duration is time.Duration stored as nanoseconds)
	switch duration {
	case "short": // Under 3 minutes
		query = query.Where("duration < ?", 3*time.Minute)
	case "medium": // 3-6 minutes
		query = query.Where("duration >= ? AND duration <= ?", 3*time.Minute, 6*time.Minute)
	case "long": // Over 6 minutes
		query = query.Where("duration > ?", 6*time.Minute)
	}

	// Search filter (use LOWER for cross-database compatibility)
	if searchQuery != "" {
		searchPattern := "%" + strings.ToLower(searchQuery) + "%"
		query = query.Where("LOWER(title) LIKE ? OR LOWER(artist) LIKE ? OR LOWER(album) LIKE ?",
			searchPattern, searchPattern, searchPattern)
	}

	// Sort order
	orderClause := "created_at DESC" // default: newest first
	switch sortBy {
	case "oldest":
		orderClause = "created_at ASC"
	case "title":
		orderClause = "title ASC"
	case "artist":
		orderClause = "artist ASC, title ASC"
	case "duration":
		orderClause = "duration DESC"
	}

	// Count total results (use Session clone to avoid mutating query state)
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		h.logger.Error().Err(err).Str("search", searchQuery).Msg("archive count query failed")
	}

	// Fetch paginated results (exclude large binary fields for performance)
	if err := query.Session(&gorm.Session{}).
		Omit("artwork", "waveform").
		Order(orderClause).
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&media).Error; err != nil {
		h.logger.Error().Err(err).Str("search", searchQuery).Msg("archive search query failed")
	}

	// Build pagination query string preserving all filters
	var paginationParams []string
	if searchQuery != "" {
		paginationParams = append(paginationParams, "q="+searchQuery)
	}
	if stationID != "" {
		paginationParams = append(paginationParams, "station="+stationID)
	}
	if genre != "" {
		paginationParams = append(paginationParams, "genre="+genre)
	}
	if year != "" {
		paginationParams = append(paginationParams, "year="+year)
	}
	if artist != "" {
		paginationParams = append(paginationParams, "artist="+artist)
	}
	if duration != "" {
		paginationParams = append(paginationParams, "duration="+duration)
	}
	if sortBy != "" {
		paginationParams = append(paginationParams, "sort="+sortBy)
	}
	filterParams := ""
	if len(paginationParams) > 0 {
		filterParams = "&" + strings.Join(paginationParams, "&")
	}

	h.Render(w, r, "pages/public/archive", PageData{
		Title: "Archive",
		Data: map[string]any{
			"Media":        media,
			"Total":        total,
			"Page":         page,
			"PerPage":      perPage,
			"Query":        searchQuery,
			"Stations":     publicStations,
			"StationID":    stationID,
			"Genres":       genres,
			"Genre":        genre,
			"Years":        years,
			"Year":         year,
			"Artists":      artists,
			"Artist":       artist,
			"Sort":         sortBy,
			"Duration":     duration,
			"FilterParams": filterParams,
		},
	})
}

// ArchiveDetail renders a single media item page
func (h *Handler) ArchiveDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var media models.MediaItem
	if err := h.db.Omit("artwork", "waveform").First(&media, "id = ? AND show_in_archive = ?", id, true).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Verify the media belongs to a public station
	var station models.Station
	if err := h.db.First(&station, "id = ? AND active = ? AND public = ? AND approved = ?",
		media.StationID, true, true, true).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	h.Render(w, r, "pages/public/archive-detail", PageData{
		Title: media.Title,
		Data: map[string]any{
			"Media":   media,
			"Station": station,
		},
	})
}

// ArchiveStream serves the audio file for a public archive item
func (h *Handler) ArchiveStream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var media models.MediaItem
	if err := h.db.Select("id", "path", "title", "artist", "station_id", "show_in_archive", "allow_download").First(&media, "id = ? AND show_in_archive = ?", id, true).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Check if this is a download request (attachment header)
	isDownload := r.URL.Query().Get("download") == "1"
	if isDownload && !media.AllowDownload {
		http.Error(w, "Downloads not allowed for this item", http.StatusForbidden)
		return
	}

	// Verify the media belongs to a public station
	var station models.Station
	if err := h.db.First(&station, "id = ? AND active = ? AND public = ? AND approved = ?",
		media.StationID, true, true, true).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if media.Path == "" {
		http.Error(w, "No media file available", http.StatusNotFound)
		return
	}

	fullPath := h.mediaRoot + "/" + media.Path

	// Set content type based on file extension
	ext := media.Path[len(media.Path)-3:]
	contentTypes := map[string]string{
		"mp3": "audio/mpeg",
		"lac": "audio/flac",
		"wav": "audio/wav",
		"ogg": "audio/ogg",
		"m4a": "audio/mp4",
	}
	if ct, ok := contentTypes[ext]; ok {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Accept-Ranges", "bytes")

	// Set download header if this is a download request
	if isDownload {
		filename := media.Title
		if media.Artist != "" {
			filename = media.Artist + " - " + filename
		}
		filename = filename + "." + ext
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	}

	http.ServeFile(w, r, fullPath)
}

// ArchiveArtwork serves the album art for a public archive item
func (h *Handler) ArchiveArtwork(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var media models.MediaItem
	if err := h.db.Select("id", "artwork", "artwork_mime", "station_id", "show_in_archive").First(&media, "id = ? AND show_in_archive = ?", id, true).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Verify the media belongs to a public station
	var station models.Station
	if err := h.db.First(&station, "id = ? AND active = ? AND public = ? AND approved = ?",
		media.StationID, true, true, true).Error; err != nil {
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
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(media.Artwork)
}

// PublicSchedule renders the public schedule view
func (h *Handler) PublicSchedule(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")

	// Only show public, approved, active stations
	var stations []models.Station
	h.db.Where("active = ? AND public = ? AND approved = ?", true, true, true).Find(&stations)

	// Build list of public station IDs
	var publicStationIDs []string
	for _, s := range stations {
		publicStationIDs = append(publicStationIDs, s.ID)
	}

	var entries []models.ScheduleEntry
	query := h.db.Where("starts_at >= ? AND starts_at <= ? AND source_type != ?",
		time.Now(), time.Now().Add(48*time.Hour), "media")

	// Only show schedule for public stations
	if len(publicStationIDs) > 0 {
		query = query.Where("station_id IN ?", publicStationIDs)
	} else {
		query = query.Where("1=0")
	}

	if stationID != "" {
		// Verify station is in public list
		isPublic := false
		for _, id := range publicStationIDs {
			if id == stationID {
				isPublic = true
				break
			}
		}
		if isPublic {
			query = query.Where("station_id = ?", stationID)
		}
	}

	query.Order("starts_at ASC").Limit(100).Find(&entries)

	// Get user's color theme if logged in
	colorTheme := "default"
	if user := h.GetUser(r); user != nil && user.CalendarColorTheme != "" {
		colorTheme = user.CalendarColorTheme
	}

	h.Render(w, r, "pages/public/schedule", PageData{
		Title: "Schedule",
		Data: map[string]any{
			"Stations":   stations,
			"Entries":    entries,
			"StationID":  stationID,
			"ColorTheme": colorTheme,
		},
	})
}

// PublicScheduleEvents returns schedule entries as JSON for FullCalendar (public, view-only)
func (h *Handler) PublicScheduleEvents(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")

	// Parse date range from FullCalendar
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")

	startTime, _ := time.Parse(time.RFC3339, start)
	endTime, _ := time.Parse(time.RFC3339, end)

	if startTime.IsZero() {
		startTime = time.Now().Add(-24 * time.Hour)
	}
	if endTime.IsZero() {
		endTime = time.Now().Add(30 * 24 * time.Hour) // Default to 30 days
	}

	// Only show public, approved, active stations
	var stations []models.Station
	if stationID != "" {
		h.db.Where("id = ? AND active = ? AND public = ? AND approved = ?", stationID, true, true, true).Find(&stations)
	} else {
		h.db.Where("active = ? AND public = ? AND approved = ?", true, true, true).Order("sort_order ASC, name ASC").Find(&stations)
	}

	// Get user's color theme if logged in
	colorTheme := r.URL.Query().Get("theme")
	if colorTheme == "" {
		if user := h.GetUser(r); user != nil && user.CalendarColorTheme != "" {
			colorTheme = user.CalendarColorTheme
		}
	}
	if colorTheme == "" {
		colorTheme = "default"
	}

	// Color themes
	colorThemes := map[string][]string{
		"default": {"#6366f1", "#10b981", "#f59e0b", "#ef4444", "#8b5cf6", "#06b6d4", "#ec4899", "#84cc16"},
		"ocean":   {"#0ea5e9", "#14b8a6", "#06b6d4", "#3b82f6", "#0891b2", "#22d3ee", "#0284c7", "#2dd4bf"},
		"forest":  {"#22c55e", "#10b981", "#84cc16", "#16a34a", "#059669", "#4ade80", "#15803d", "#a3e635"},
		"sunset":  {"#f97316", "#ef4444", "#ec4899", "#f43f5e", "#fb923c", "#e11d48", "#f472b6", "#dc2626"},
		"berry":   {"#a855f7", "#8b5cf6", "#d946ef", "#c026d3", "#9333ea", "#e879f9", "#7c3aed", "#f0abfc"},
		"earth":   {"#92400e", "#f59e0b", "#78716c", "#b45309", "#d97706", "#a16207", "#fbbf24", "#6b7280"},
		"neon":    {"#00ff88", "#ff0088", "#00ffff", "#ffff00", "#ff00ff", "#88ff00", "#0088ff", "#ff8800"},
		"pastel":  {"#93c5fd", "#a5b4fc", "#c4b5fd", "#f9a8d4", "#fca5a5", "#fed7aa", "#fde68a", "#bbf7d0"},
	}
	colors := colorThemes[colorTheme]
	if colors == nil {
		colors = colorThemes["default"]
	}

	// Build list of public station IDs
	var publicStationIDs []string
	stationNames := make(map[string]string)
	stationColors := make(map[string]string)
	for i, s := range stations {
		publicStationIDs = append(publicStationIDs, s.ID)
		stationNames[s.ID] = s.Name
		stationColors[s.ID] = colors[i%len(colors)]
	}

	if len(publicStationIDs) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}

	// Fetch non-recurring entries within range
	// Exclude 'media' source type - public schedule shows programs, not individual tracks
	var entries []models.ScheduleEntry
	h.db.Where("station_id IN ? AND starts_at >= ? AND starts_at <= ? AND source_type != ? AND (recurrence_type = '' OR recurrence_type IS NULL OR is_instance = true)",
		publicStationIDs, startTime, endTime, "media").
		Order("starts_at ASC").Find(&entries)

	// Also fetch recurring entries (excluding media)
	var recurringEntries []models.ScheduleEntry
	h.db.Where("station_id IN ? AND source_type != ? AND recurrence_type != '' AND recurrence_type IS NOT NULL AND is_instance = false",
		publicStationIDs, "media").Find(&recurringEntries)

	// Expand recurring entries
	for _, re := range recurringEntries {
		instances := h.expandRecurringEntry(re, startTime, endTime)
		entries = append(entries, instances...)
	}

	// Fetch mount names
	mountNames := make(map[string]string)
	var mounts []models.Mount
	if len(publicStationIDs) > 0 {
		h.db.Where("station_id IN ?", publicStationIDs).Find(&mounts)
		for _, m := range mounts {
			mountNames[m.ID] = m.Name
		}
	}

	// Build lookup maps for source names
	playlistNames := make(map[string]string)
	smartBlockNames := make(map[string]string)
	clockNames := make(map[string]string)
	webstreamNames := make(map[string]string)
	mediaNames := make(map[string]string)

	// Collect IDs by type
	var playlistIDs, smartBlockIDs, clockIDs, webstreamIDs, mediaIDs []string
	for _, entry := range entries {
		switch entry.SourceType {
		case "playlist":
			playlistIDs = append(playlistIDs, entry.SourceID)
		case "smart_block":
			smartBlockIDs = append(smartBlockIDs, entry.SourceID)
		case "clock_template":
			clockIDs = append(clockIDs, entry.SourceID)
		case "webstream":
			webstreamIDs = append(webstreamIDs, entry.SourceID)
		case "media":
			mediaIDs = append(mediaIDs, entry.SourceID)
		}
	}

	// Fetch names in bulk
	if len(playlistIDs) > 0 {
		var playlists []models.Playlist
		h.db.Select("id, name").Where("id IN ?", playlistIDs).Find(&playlists)
		for _, p := range playlists {
			playlistNames[p.ID] = p.Name
		}
	}
	if len(smartBlockIDs) > 0 {
		var blocks []models.SmartBlock
		h.db.Select("id, name").Where("id IN ?", smartBlockIDs).Find(&blocks)
		for _, b := range blocks {
			smartBlockNames[b.ID] = b.Name
		}
	}
	if len(clockIDs) > 0 {
		var clocks []models.ClockHour
		h.db.Select("id, name").Where("id IN ?", clockIDs).Find(&clocks)
		for _, c := range clocks {
			clockNames[c.ID] = c.Name
		}
	}
	if len(webstreamIDs) > 0 {
		var streams []models.Webstream
		h.db.Select("id, name").Where("id IN ?", webstreamIDs).Find(&streams)
		for _, ws := range streams {
			webstreamNames[ws.ID] = ws.Name
		}
	}
	if len(mediaIDs) > 0 {
		var items []models.MediaItem
		h.db.Select("id, title, artist").Where("id IN ?", mediaIDs).Find(&items)
		for _, m := range items {
			if m.Artist != "" {
				mediaNames[m.ID] = m.Artist + " - " + m.Title
			} else {
				mediaNames[m.ID] = m.Title
			}
		}
	}

	// Convert to FullCalendar event format
	type calendarEvent struct {
		ID              string `json:"id"`
		Title           string `json:"title"`
		Start           string `json:"start"`
		End             string `json:"end"`
		BackgroundColor string `json:"backgroundColor,omitempty"`
		BorderColor     string `json:"borderColor,omitempty"`
		TextColor       string `json:"textColor,omitempty"`
		ClassName       string `json:"className,omitempty"`
		Extendedprops   any    `json:"extendedProps,omitempty"`
	}

	events := make([]calendarEvent, 0, len(entries))
	for _, entry := range entries {
		// Get title based on source type
		var title string
		var sourceLabel string

		switch entry.SourceType {
		case "playlist":
			title = playlistNames[entry.SourceID]
			sourceLabel = "Playlist"
		case "smart_block":
			title = smartBlockNames[entry.SourceID]
			sourceLabel = "Smart Block"
		case "clock_template":
			title = clockNames[entry.SourceID]
			sourceLabel = "Clock"
		case "webstream":
			title = webstreamNames[entry.SourceID]
			sourceLabel = "Webstream"
		case "media":
			title = mediaNames[entry.SourceID]
			sourceLabel = "Track"
		case "live":
			if entry.Metadata != nil {
				if name, ok := entry.Metadata["session_name"].(string); ok {
					title = name
				}
			}
			if title == "" {
				title = "Live Session"
			}
			sourceLabel = "Live"
		default:
			title = entry.SourceType
			sourceLabel = entry.SourceType
		}

		// Fallback
		if title == "" {
			if entry.Metadata != nil {
				if t, ok := entry.Metadata["title"].(string); ok {
					title = t
				}
			}
			if title == "" {
				title = sourceLabel
			}
		}

		event := calendarEvent{
			ID:              entry.ID,
			Title:           title,
			Start:           entry.StartsAt.Format(time.RFC3339),
			End:             entry.EndsAt.Format(time.RFC3339),
			BackgroundColor: stationColors[entry.StationID],
			BorderColor:     stationColors[entry.StationID],
			ClassName:       "event-" + entry.SourceType,
			Extendedprops: map[string]any{
				"source_type":  entry.SourceType,
				"source_label": sourceLabel,
				"source_name":  title,
				"station_id":   entry.StationID,
				"station_name": stationNames[entry.StationID],
				"mount_id":     entry.MountID,
				"mount_name":   mountNames[entry.MountID],
			},
		}

		events = append(events, event)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// StationInfo renders a station info page
func (h *Handler) StationInfo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var station models.Station
	// Only show public, approved, active stations
	if err := h.db.First(&station, "id = ? AND active = ? AND public = ? AND approved = ?",
		id, true, true, true).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	var mounts []models.Mount
	h.db.Where("station_id = ?", station.ID).Find(&mounts)

	// Build stream URLs for each mount
	type mountWithURL struct {
		models.Mount
		URL   string
		LQURL string
	}

	var mountsWithURLs []mountWithURL
	for _, m := range mounts {
		mountsWithURLs = append(mountsWithURLs, mountWithURL{
			Mount: m,
			URL:   "/live/" + m.Name,
			LQURL: "/live/" + m.Name + "-lq",
		})
	}

	h.Render(w, r, "pages/public/station", PageData{
		Title: station.Name,
		Data: map[string]any{
			"Station": station,
			"Mounts":  mountsWithURLs,
		},
	})
}

// LoginPage renders the login form
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	// Redirect if already logged in
	if h.GetUser(r) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	h.Render(w, r, "pages/public/login", PageData{
		Title: "Login",
		Data: map[string]any{
			"Redirect": r.URL.Query().Get("redirect"),
		},
	})
}

// LoginSubmit handles the login form submission
func (h *Handler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderLoginError(w, r, "Invalid form data")
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")
	redirect := r.FormValue("redirect")

	if email == "" || password == "" {
		h.renderLoginError(w, r, "Email and password are required")
		return
	}

	// Find user
	var user models.User
	if err := h.db.First(&user, "email = ?", email).Error; err != nil {
		h.renderLoginError(w, r, "Invalid email or password")
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		h.renderLoginError(w, r, "Invalid email or password")
		return
	}

	// Generate JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"roles":   []string{string(user.PlatformRole)},
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
		"iat":     time.Now().Unix(),
		"sub":     user.ID,
	})

	tokenStr, err := token.SignedString(h.jwtSecret)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to sign JWT")
		h.renderLoginError(w, r, "Authentication failed")
		return
	}

	// Set cookie (24 hours)
	h.SetAuthToken(w, tokenStr, 86400)

	// Handle HTMX request
	if r.Header.Get("HX-Request") == "true" {
		if redirect != "" && redirect != "/login" {
			w.Header().Set("HX-Redirect", redirect)
		} else {
			w.Header().Set("HX-Redirect", "/dashboard")
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	// Standard redirect
	if redirect != "" && redirect != "/login" {
		http.Redirect(w, r, redirect, http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	}
}

func (h *Handler) renderLoginError(w http.ResponseWriter, r *http.Request, message string) {
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`<div class="alert alert-danger" role="alert">` + message + `</div>`))
		return
	}

	h.Render(w, r, "pages/public/login", PageData{
		Title: "Login",
		Flash: &FlashMessage{Type: "error", Message: message},
		Data: map[string]any{
			"Email":    r.FormValue("email"),
			"Redirect": r.FormValue("redirect"),
		},
	})
}

// Logout clears the auth cookie and redirects to login
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	h.ClearAuthToken(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
