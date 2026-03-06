/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"database/sql"
	"net/http"
	"sort"
	"strconv"
	"strings"
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

	// Listener stats (current, peak today, avg 24h)
	now := time.Now()
	windowStart := now.Add(-24 * time.Hour)
	todayStartUTC := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)

	if h.director != nil {
		if listeners, err := h.director.ListenerCount(r.Context(), station.ID); err == nil {
			data.CurrentListeners = listeners
		}
	}

	data.PeakToday = data.CurrentListeners
	var peakTodayDB sql.NullInt64
	if err := h.db.Model(&models.ListenerSample{}).
		Select("MAX(listeners)").
		Where("station_id = ? AND captured_at >= ?", station.ID, todayStartUTC).
		Scan(&peakTodayDB).Error; err == nil && peakTodayDB.Valid && int(peakTodayDB.Int64) > data.PeakToday {
		data.PeakToday = int(peakTodayDB.Int64)
	}

	data.Avg24h = float64(data.CurrentListeners)
	var avg24hDB sql.NullFloat64
	if err := h.db.Model(&models.ListenerSample{}).
		Select("AVG(listeners)").
		Where("station_id = ? AND captured_at >= ?", station.ID, windowStart.UTC()).
		Scan(&avg24hDB).Error; err == nil && avg24hDB.Valid {
		data.Avg24h = avg24hDB.Float64
	}

	// Current/most-recent now playing item for IRT visibility on dashboard.
	var history models.PlayHistory
	if err := h.db.Where("station_id = ?", station.ID).Order("started_at DESC").First(&history).Error; err == nil {
		if history.EndedAt.IsZero() || history.EndedAt.After(now) {
			elapsed := now.Sub(history.StartedAt)
			if elapsed < 0 {
				elapsed = 0
			}
			duration := time.Duration(0)
			if !history.EndedAt.IsZero() && history.EndedAt.After(history.StartedAt) {
				duration = history.EndedAt.Sub(history.StartedAt)
			}
			data.NowPlaying = &NowPlayingInfo{
				Title:    history.Title,
				Artist:   history.Artist,
				Album:    history.Album,
				Duration: duration,
				Elapsed:  elapsed,
				MountID:  history.MountID,
			}
		}
	}

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

	instanceOverrides := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsInstance && entry.RecurrenceParentID != nil {
			instanceOverrides[recurrenceInstanceKey(*entry.RecurrenceParentID, entry.StartsAt)] = struct{}{}
		}
	}

	// Load recurring parent entries and expand virtual instances in-range.
	var recurringEntries []models.ScheduleEntry
	h.db.Where("station_id = ? AND recurrence_type != '' AND recurrence_type IS NOT NULL AND is_instance = false",
		stationID).
		Find(&recurringEntries)

	for _, re := range recurringEntries {
		instances := h.expandRecurringEntry(re, from, to, instanceOverrides)
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
	UpcomingEntries  []models.ScheduleEntry
	RecentMedia      []models.MediaItem
	LiveSessions     []models.LiveSession
	MediaCount       int64
	PlaylistCount    int64
	SmartBlockCount  int64
	CurrentListeners int
	PeakToday        int
	Avg24h           float64
	NowPlaying       *NowPlayingInfo
	Confidence       DashboardConfidenceData
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

type DashboardConfidenceData struct {
	RuntimeState  *models.ExecutorState
	CurrentMount  *dashboardMountRuntime
	QueuedByMount []dashboardQueuedMount
	RecentActions []dashboardActionEntry
}

type dashboardMountRuntime struct {
	MountID    string
	MountName  string
	MediaID    string
	SourceType string
	SourceID   string
	Position   int
	TotalItems int
	StartedAt  time.Time
	EndsAt     time.Time
}

type dashboardQueuedMount struct {
	MountID   string
	MountName string
	Items     []dashboardQueueItem
}

type dashboardQueueItem struct {
	ID        string
	MediaID   string
	Title     string
	Artist    string
	Position  int
	CreatedAt time.Time
}

type dashboardActionEntry struct {
	Action    models.AuditAction
	UserEmail string
	MountID   string
	MediaID   string
	Position  int
	Count     int
	CreatedAt time.Time
}

// DashboardPlayoutConfidence renders the runtime queue/health/action panel.
func (h *Handler) DashboardPlayoutConfidence(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	h.RenderPartial(w, r, "partials/dashboard-playout-confidence", h.loadDashboardConfidenceData(r, station.ID))
}

func (h *Handler) loadDashboardConfidenceData(r *http.Request, stationID string) DashboardConfidenceData {
	data := DashboardConfidenceData{}

	var runtimeState models.ExecutorState
	if err := h.db.WithContext(r.Context()).
		Where("station_id = ?", stationID).
		First(&runtimeState).Error; err == nil {
		data.RuntimeState = &runtimeState
	}

	var mountStates []models.MountPlayoutState
	_ = h.db.WithContext(r.Context()).
		Where("station_id = ?", stationID).
		Order("updated_at DESC").
		Find(&mountStates).Error

	var mounts []models.Mount
	_ = h.db.WithContext(r.Context()).
		Where("station_id = ?", stationID).
		Order("name ASC").
		Find(&mounts).Error
	mountNames := make(map[string]string, len(mounts))
	for _, mount := range mounts {
		mountNames[mount.ID] = mount.Name
	}

	if data.RuntimeState != nil && data.RuntimeState.MountID != "" {
		for _, state := range mountStates {
			if state.MountID != data.RuntimeState.MountID {
				continue
			}
			data.CurrentMount = &dashboardMountRuntime{
				MountID:    state.MountID,
				MountName:  mountNames[state.MountID],
				MediaID:    state.MediaID,
				SourceType: state.SourceType,
				SourceID:   state.SourceID,
				Position:   state.Position,
				TotalItems: state.TotalItems,
				StartedAt:  state.StartedAt,
				EndsAt:     state.EndsAt,
			}
			break
		}
	}
	if data.CurrentMount == nil && len(mountStates) > 0 {
		state := mountStates[0]
		data.CurrentMount = &dashboardMountRuntime{
			MountID:    state.MountID,
			MountName:  mountNames[state.MountID],
			MediaID:    state.MediaID,
			SourceType: state.SourceType,
			SourceID:   state.SourceID,
			Position:   state.Position,
			TotalItems: state.TotalItems,
			StartedAt:  state.StartedAt,
			EndsAt:     state.EndsAt,
		}
	}

	var queueRows []models.PlayoutQueueItem
	_ = h.db.WithContext(r.Context()).
		Where("station_id = ?", stationID).
		Order("mount_id ASC, position ASC, created_at ASC").
		Limit(12).
		Find(&queueRows).Error

	mediaIDs := make([]string, 0, len(queueRows))
	for _, row := range queueRows {
		mediaIDs = append(mediaIDs, row.MediaID)
	}
	mediaByID := map[string]models.MediaItem{}
	if len(mediaIDs) > 0 {
		var mediaItems []models.MediaItem
		_ = h.db.WithContext(r.Context()).
			Select("id, title, artist").
			Where("id IN ?", mediaIDs).
			Find(&mediaItems).Error
		for _, item := range mediaItems {
			mediaByID[item.ID] = item
		}
	}

	queueByMount := make(map[string][]dashboardQueueItem)
	mountOrder := make([]string, 0, len(queueRows))
	for _, row := range queueRows {
		if _, ok := queueByMount[row.MountID]; !ok {
			mountOrder = append(mountOrder, row.MountID)
		}
		media := mediaByID[row.MediaID]
		queueByMount[row.MountID] = append(queueByMount[row.MountID], dashboardQueueItem{
			ID:        row.ID,
			MediaID:   row.MediaID,
			Title:     media.Title,
			Artist:    media.Artist,
			Position:  row.Position,
			CreatedAt: row.CreatedAt,
		})
	}
	for _, mountID := range mountOrder {
		data.QueuedByMount = append(data.QueuedByMount, dashboardQueuedMount{
			MountID:   mountID,
			MountName: mountNames[mountID],
			Items:     queueByMount[mountID],
		})
	}

	var logs []models.AuditLog
	_ = h.db.WithContext(r.Context()).
		Where("station_id = ? AND action IN ?", stationID, []models.AuditAction{
			models.AuditActionPlayoutQueueAdd,
			models.AuditActionPlayoutQueueMove,
			models.AuditActionPlayoutQueueDelete,
			models.AuditActionPlayoutSkip,
			models.AuditActionPlayoutStop,
			models.AuditActionPlayoutReload,
			models.AuditActionScheduleRefresh,
			models.AuditActionScheduleUpdate,
		}).
		Order("timestamp DESC").
		Limit(8).
		Find(&logs).Error

	for _, log := range logs {
		entry := dashboardActionEntry{
			Action:    log.Action,
			UserEmail: log.UserEmail,
			CreatedAt: log.Timestamp,
		}
		if mountID, ok := log.Details["mount_id"].(string); ok {
			entry.MountID = mountID
		}
		if mediaID, ok := log.Details["media_id"].(string); ok {
			entry.MediaID = mediaID
		}
		if pos, ok := log.Details["position"].(float64); ok {
			entry.Position = int(pos)
		}
		if pos, ok := log.Details["position"].(int); ok {
			entry.Position = pos
		}
		if count, ok := log.Details["mount_count"].(float64); ok {
			entry.Count = int(count)
		}
		if count, ok := log.Details["mount_count"].(int); ok {
			entry.Count = count
		}
		data.RecentActions = append(data.RecentActions, entry)
	}

	return data
}

// StationSelect renders the station selection page
func (h *Handler) StationSelect(w http.ResponseWriter, r *http.Request) {
	// Use LoadStations which filters by user access
	stations := h.LoadStations(r)
	if len(stations) == 1 {
		h.SetStation(w, stations[0].ID)
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", "/dashboard")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

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

	// Validate against the same station list rendered on the selection page.
	// This keeps submit behavior aligned with what the user can actually pick.
	allowed := false
	for _, s := range h.LoadStations(r) {
		if s.ID == stationID {
			allowed = true
			break
		}
	}
	if !allowed {
		http.Error(w, "You don't have access to this station", http.StatusForbidden)
		return
	}

	h.SetStation(w, stationID)

	redirectTo := strings.TrimSpace(r.FormValue("redirect_to"))
	if redirectTo == "" {
		redirectTo = "/dashboard"
	}
	// Prevent open redirects by allowing only dashboard-relative paths.
	if !strings.HasPrefix(redirectTo, "/dashboard") {
		redirectTo = "/dashboard"
	}

	// Handle HTMX
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", redirectTo)
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, redirectTo, http.StatusSeeOther)
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

	// Invalidate all existing sessions (tokens issued before now are rejected)
	now := time.Now()
	user.TokenValidAfter = &now

	if err := h.db.Save(user).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to update user password")
		h.renderPasswordError(w, r, "Failed to update password")
		return
	}

	// Issue a fresh token so this browser stays logged in
	h.issueNewToken(w, user)

	// Handle HTMX
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Trigger", "passwordUpdated")
		w.Write([]byte(`<div class="alert alert-success">Password updated — all other sessions have been logged out</div>`))
		return
	}

	http.Redirect(w, r, "/dashboard/profile", http.StatusSeeOther)
}

// ProfileLogoutAllDevices invalidates all existing sessions for the user.
func (h *Handler) ProfileLogoutAllDevices(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	now := time.Now()
	user.TokenValidAfter = &now
	if err := h.db.Model(user).Update("token_valid_after", now).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to invalidate sessions")
		if r.Header.Get("HX-Request") == "true" {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`<div class="alert alert-danger">Failed to log out other devices</div>`))
			return
		}
		http.Redirect(w, r, "/dashboard/profile", http.StatusSeeOther)
		return
	}

	// Issue a fresh token so this browser stays logged in
	h.issueNewToken(w, user)

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(`<div class="alert alert-success">All other devices have been logged out</div>`))
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
