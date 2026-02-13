/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// DashboardHome renders the main dashboard overview
func (h *Handler) DashboardHome(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	// If no station selected, redirect to selection
	if station == nil {
		var stations []models.Station
		h.db.Where("active = ?", true).Find(&stations)

		if len(stations) == 1 {
			// Auto-select single station
			h.SetStation(w, stations[0].ID)
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	// Gather dashboard data
	var data DashboardData

	// Upcoming schedule (including recurring entries expanded into instances)
	// Schedule times are stored/compared in UTC throughout the system; use UTC here to avoid
	// empty dashboards when the server runs in a non-UTC timezone.
	// Use a full-day horizon so "Upcoming" is useful at any time of day.
	data.UpcomingEntries = h.loadDashboardUpcomingEntries(station.ID, time.Now().UTC(), 24*time.Hour, 10)
	data.UpcomingEntries = h.enrichDashboardUpcomingEntries(data.UpcomingEntries)

	// Recent media uploads
	h.db.Where("station_id = ?", station.ID).
		Order("created_at DESC").
		Limit(5).
		Find(&data.RecentMedia)

	// Active live sessions
	h.db.Where("station_id = ? AND active = ?", station.ID, true).
		Find(&data.LiveSessions)

	// Media stats
	h.db.Model(&models.MediaItem{}).Where("station_id = ?", station.ID).Count(&data.MediaCount)

	// Playlist count
	h.db.Model(&models.Playlist{}).Where("station_id = ?", station.ID).Count(&data.PlaylistCount)

	// Smart block count
	h.db.Model(&models.SmartBlock{}).Where("station_id = ?", station.ID).Count(&data.SmartBlockCount)

	h.Render(w, r, "pages/dashboard/home", PageData{
		Title:    "Dashboard",
		User:     user,
		Station:  station,
		Stations: h.LoadStations(r),
		Data:     data,
	})
}

func (h *Handler) loadDashboardUpcomingEntries(stationID string, from time.Time, horizon time.Duration, limit int) []models.ScheduleEntry {
	if limit <= 0 {
		limit = 10
	}
	from = from.UTC()
	to := from.Add(horizon).UTC()

	// Load non-recurring entries and already-materialized instances.
	var entries []models.ScheduleEntry
	h.db.Where("station_id = ? AND starts_at >= ? AND starts_at <= ? AND (recurrence_type = '' OR recurrence_type IS NULL OR is_instance = true)",
		stationID, from, to).
		Order("starts_at ASC").
		Find(&entries)

	// Load recurring parent entries and expand virtual instances in-range.
	var recurringEntries []models.ScheduleEntry
	h.db.Where("station_id = ? AND recurrence_type != '' AND recurrence_type IS NOT NULL AND is_instance = false",
		stationID).
		Find(&recurringEntries)

	for _, re := range recurringEntries {
		instances := h.expandRecurringEntry(re, from, to)
		entries = append(entries, instances...)
	}

	// Dedupe by source+mount+time window to avoid repeated rows when both materialized and virtual rows exist.
	type entryKey struct {
		mountID    string
		sourceType string
		sourceID   string
		startUnix  int64
		endUnix    int64
	}
	seen := make(map[entryKey]struct{}, len(entries))
	deduped := make([]models.ScheduleEntry, 0, len(entries))
	for _, e := range entries {
		k := entryKey{
			mountID:    e.MountID,
			sourceType: e.SourceType,
			sourceID:   e.SourceID,
			startUnix:  e.StartsAt.Unix(),
			endUnix:    e.EndsAt.Unix(),
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		deduped = append(deduped, e)
	}

	sort.Slice(deduped, func(i, j int) bool {
		return deduped[i].StartsAt.Before(deduped[j].StartsAt)
	})
	if len(deduped) > limit {
		deduped = deduped[:limit]
	}
	return deduped
}

func (h *Handler) enrichDashboardUpcomingEntries(entries []models.ScheduleEntry) []models.ScheduleEntry {
	if len(entries) == 0 {
		return entries
	}

	playlistNames := make(map[string]string)
	smartBlockNames := make(map[string]string)
	clockNames := make(map[string]string)
	webstreamNames := make(map[string]string)
	mediaNames := make(map[string]string)

	var playlistIDs, smartBlockIDs, clockIDs, webstreamIDs, mediaIDs []string
	for _, entry := range entries {
		switch entry.SourceType {
		case "playlist":
			if entry.SourceID != "" {
				playlistIDs = append(playlistIDs, entry.SourceID)
			}
		case "smart_block":
			if entry.SourceID != "" {
				smartBlockIDs = append(smartBlockIDs, entry.SourceID)
			}
		case "clock_template":
			if entry.SourceID != "" {
				clockIDs = append(clockIDs, entry.SourceID)
			}
		case "webstream":
			if entry.SourceID != "" {
				webstreamIDs = append(webstreamIDs, entry.SourceID)
			}
		case "media":
			if entry.SourceID != "" {
				mediaIDs = append(mediaIDs, entry.SourceID)
			}
		}
	}

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

	for i := range entries {
		title := ""
		switch entries[i].SourceType {
		case "playlist":
			title = playlistNames[entries[i].SourceID]
		case "smart_block":
			title = smartBlockNames[entries[i].SourceID]
		case "clock_template":
			title = clockNames[entries[i].SourceID]
		case "webstream":
			title = webstreamNames[entries[i].SourceID]
		case "media":
			title = mediaNames[entries[i].SourceID]
		case "live":
			if entries[i].Metadata != nil {
				if sessionName, ok := entries[i].Metadata["session_name"].(string); ok {
					title = sessionName
				}
			}
			if title == "" {
				title = "Live Session"
			}
		case "stopset":
			title = "Stopset"
		}

		if title == "" {
			continue
		}
		if entries[i].Metadata == nil {
			entries[i].Metadata = make(map[string]any)
		}
		if _, ok := entries[i].Metadata["title"]; !ok {
			entries[i].Metadata["title"] = title
		}
	}

	return entries
}

// DashboardData holds data for the dashboard overview
type DashboardData struct {
	UpcomingEntries []models.ScheduleEntry
	RecentMedia     []models.MediaItem
	LiveSessions    []models.LiveSession
	MediaCount      int64
	PlaylistCount   int64
	SmartBlockCount int64
	NowPlaying      *NowPlayingInfo
}

// NowPlayingInfo holds current playback info
type NowPlayingInfo struct {
	Title    string
	Artist   string
	Album    string
	Duration time.Duration
	Elapsed  time.Duration
	MountID  string
}

// StationSelect renders the station selection page
func (h *Handler) StationSelect(w http.ResponseWriter, r *http.Request) {
	// Use LoadStations which filters by user access
	stations := h.LoadStations(r)

	h.Render(w, r, "pages/dashboard/station-select", PageData{
		Title:    "Select Station",
		Stations: stations,
	})
}

// StationSelectSubmit handles station selection
func (h *Handler) StationSelectSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	stationID := r.FormValue("station_id")
	if stationID == "" {
		http.Error(w, "Station required", http.StatusBadRequest)
		return
	}

	// Verify station exists
	var station models.Station
	if err := h.db.First(&station, "id = ?", stationID).Error; err != nil {
		http.Error(w, "Station not found", http.StatusNotFound)
		return
	}

	// Verify user has access to this station
	user := h.GetUser(r)
	if user == nil {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	if !h.HasStationAccess(user, stationID) {
		http.Error(w, "You don't have access to this station", http.StatusForbidden)
		return
	}

	h.SetStation(w, stationID)

	// Handle HTMX
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// ProfilePage renders the user's profile page
func (h *Handler) ProfilePage(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	h.Render(w, r, "pages/dashboard/profile", PageData{
		Title:    "Profile",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"User": user,
		},
	})
}

// ProfileUpdate handles profile updates
func (h *Handler) ProfileUpdate(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderProfileError(w, r, "Invalid form data")
		return
	}

	// Update allowed fields
	email := r.FormValue("email")
	if email == "" {
		h.renderProfileError(w, r, "Email is required")
		return
	}

	// Check if email changed and is unique
	if email != user.Email {
		var existing models.User
		if err := h.db.Where("email = ? AND id != ?", email, user.ID).First(&existing).Error; err == nil {
			h.renderProfileError(w, r, "Email already in use")
			return
		}
	}

	user.Email = email

	// Update calendar color theme if provided
	calendarColorTheme := r.FormValue("calendar_color_theme")
	if calendarColorTheme != "" {
		// Validate the theme is one of our presets
		validThemes := map[string]bool{
			"default": true, "ocean": true, "forest": true, "sunset": true,
			"berry": true, "earth": true, "neon": true, "pastel": true,
		}
		if validThemes[calendarColorTheme] {
			user.CalendarColorTheme = calendarColorTheme
		}
	}

	if err := h.db.Save(user).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to update user")
		h.renderProfileError(w, r, "Failed to update profile")
		return
	}

	// Handle HTMX
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Trigger", "profileUpdated")
		w.Write([]byte(`<div class="alert alert-success">Profile updated successfully</div>`))
		return
	}

	http.Redirect(w, r, "/dashboard/profile", http.StatusSeeOther)
}

// ProfileUpdatePassword handles password changes
func (h *Handler) ProfileUpdatePassword(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderPasswordError(w, r, "Invalid form data")
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	// Verify current password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(currentPassword)); err != nil {
		h.renderPasswordError(w, r, "Current password is incorrect")
		return
	}

	// Validate new password
	if len(newPassword) < 8 {
		h.renderPasswordError(w, r, "New password must be at least 8 characters")
		return
	}

	if newPassword != confirmPassword {
		h.renderPasswordError(w, r, "New passwords do not match")
		return
	}

	// Hash and save new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to hash password")
		h.renderPasswordError(w, r, "Failed to update password")
		return
	}

	user.Password = string(hashedPassword)

	if err := h.db.Save(user).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to update user password")
		h.renderPasswordError(w, r, "Failed to update password")
		return
	}

	// Handle HTMX
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Trigger", "passwordUpdated")
		w.Write([]byte(`<div class="alert alert-success">Password updated successfully</div>`))
		return
	}

	http.Redirect(w, r, "/dashboard/profile", http.StatusSeeOther)
}

func (h *Handler) renderProfileError(w http.ResponseWriter, r *http.Request, message string) {
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<div class="alert alert-danger">` + message + `</div>`))
		return
	}

	user := h.GetUser(r)
	h.Render(w, r, "pages/dashboard/profile", PageData{
		Title:    "Profile",
		Flash:    &FlashMessage{Type: "error", Message: message},
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"User": user,
		},
	})
}

func (h *Handler) renderPasswordError(w http.ResponseWriter, r *http.Request, message string) {
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<div class="alert alert-danger">` + message + `</div>`))
		return
	}

	user := h.GetUser(r)
	h.Render(w, r, "pages/dashboard/profile", PageData{
		Title:    "Profile",
		Flash:    &FlashMessage{Type: "error", Message: message},
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"User":          user,
			"PasswordError": true,
		},
	})
}

// APIKeysSection renders the API keys section for the profile page
func (h *Handler) APIKeysSection(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	keys, err := auth.ListAPIKeys(h.db, user.ID)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to list api keys")
		http.Error(w, "Failed to load API keys", http.StatusInternalServerError)
		return
	}

	// Render partial for HTMX
	h.RenderPartial(w, r, "partials/api-keys-list", map[string]any{
		"APIKeys":           keys,
		"ExpirationOptions": auth.APIKeyExpirationOptions,
	})
}

// APIKeyGenerate generates a new API key for the user
func (h *Handler) APIKeyGenerate(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		name = "API Key"
	}

	expirationDays := r.FormValue("expiration_days")
	days := 90 // default
	if expirationDays != "" {
		if parsed, err := strconv.Atoi(expirationDays); err == nil && parsed > 0 && parsed <= 365 {
			days = parsed
		}
	}

	expiration := time.Duration(days) * 24 * time.Hour

	plaintextKey, apiKey, err := auth.GenerateAPIKey(user.ID, name, expiration)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to generate api key")
		http.Error(w, "Failed to generate API key", http.StatusInternalServerError)
		return
	}

	if err := h.db.Create(apiKey).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to save api key")
		http.Error(w, "Failed to save API key", http.StatusInternalServerError)
		return
	}

	// Publish audit event
	h.eventBus.Publish(events.EventAuditAPIKeyCreate, events.Payload{
		"user_id":       user.ID,
		"user_email":    user.Email,
		"resource_type": "apikey",
		"resource_id":   apiKey.ID,
		"key_name":      apiKey.Name,
		"ip_address":    r.RemoteAddr,
		"user_agent":    r.UserAgent(),
	})

	// Get all keys including the new one for the list
	keys, _ := auth.ListAPIKeys(h.db, user.ID)

	// Return both the new key reveal and the updated list
	h.RenderPartial(w, r, "partials/api-key-created", map[string]any{
		"NewKey":            plaintextKey,
		"APIKey":            apiKey,
		"APIKeys":           keys,
		"ExpirationOptions": auth.APIKeyExpirationOptions,
	})
}

// APIKeyRevoke revokes an API key
func (h *Handler) APIKeyRevoke(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	keyID := chi.URLParam(r, "id")
	if keyID == "" {
		http.Error(w, "Key ID required", http.StatusBadRequest)
		return
	}

	if err := auth.RevokeAPIKey(h.db, keyID, user.ID); err != nil {
		if err == auth.ErrAPIKeyNotFound {
			http.Error(w, "API key not found", http.StatusNotFound)
			return
		}
		h.logger.Error().Err(err).Msg("failed to revoke api key")
		http.Error(w, "Failed to revoke API key", http.StatusInternalServerError)
		return
	}

	// Publish audit event
	h.eventBus.Publish(events.EventAuditAPIKeyRevoke, events.Payload{
		"user_id":       user.ID,
		"user_email":    user.Email,
		"resource_type": "apikey",
		"resource_id":   keyID,
		"ip_address":    r.RemoteAddr,
		"user_agent":    r.UserAgent(),
	})

	// Return updated list
	keys, _ := auth.ListAPIKeys(h.db, user.ID)

	h.RenderPartial(w, r, "partials/api-keys-list", map[string]any{
		"APIKeys":           keys,
		"ExpirationOptions": auth.APIKeyExpirationOptions,
		"Flash":             &FlashMessage{Type: "success", Message: "API key revoked successfully"},
	})
}
