/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ScheduleCalendar renders the schedule calendar page
func (h *Handler) ScheduleCalendar(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	// Get mounts for filtering
	var mounts []models.Mount
	h.db.Where("station_id = ?", station.ID).Find(&mounts)

	// Get user's color theme
	colorTheme := "default"
	if user := h.GetUser(r); user != nil && user.CalendarColorTheme != "" {
		colorTheme = user.CalendarColorTheme
	}

	h.Render(w, r, "pages/dashboard/schedule/calendar", PageData{
		Title:    "Schedule",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"StationID":  station.ID,
			"Mounts":     mounts,
			"ColorTheme": colorTheme,
		},
	})
}

// ScheduleEvents returns schedule entries as JSON for FullCalendar
func (h *Handler) ScheduleEvents(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	// Parse date range from FullCalendar
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")
	mountID := r.URL.Query().Get("mount_id")

	startTime, _ := time.Parse(time.RFC3339, start)
	endTime, _ := time.Parse(time.RFC3339, end)

	if startTime.IsZero() {
		startTime = time.Now().Add(-24 * time.Hour)
	}
	if endTime.IsZero() {
		endTime = time.Now().Add(48 * time.Hour)
	}

	// Fetch non-recurring entries and instances within range
	query := h.db.Where("station_id = ? AND starts_at >= ? AND starts_at <= ? AND (recurrence_type = '' OR recurrence_type IS NULL OR is_instance = true)",
		station.ID, startTime, endTime)

	if mountID != "" {
		query = query.Where("mount_id = ?", mountID)
	}

	var entries []models.ScheduleEntry
	query.Order("starts_at ASC").Find(&entries)

	// Also fetch recurring entries that could generate instances in this range
	var recurringEntries []models.ScheduleEntry
	recurringQuery := h.db.Where("station_id = ? AND recurrence_type != '' AND recurrence_type IS NOT NULL AND is_instance = false",
		station.ID)
	if mountID != "" {
		recurringQuery = recurringQuery.Where("mount_id = ?", mountID)
	}
	recurringQuery.Find(&recurringEntries)

	// Expand recurring entries into virtual instances
	for _, re := range recurringEntries {
		instances := h.expandRecurringEntry(re, startTime, endTime)
		entries = append(entries, instances...)
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
		for _, w := range streams {
			webstreamNames[w.ID] = w.Name
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

		// Fallback to metadata or source type
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

		// Build recurrence info
		var recurrenceInfo map[string]any
		if entry.RecurrenceType != "" {
			recurrenceInfo = map[string]any{
				"type": string(entry.RecurrenceType),
				"days": entry.RecurrenceDays,
			}
			if entry.RecurrenceEndDate != nil {
				recurrenceInfo["end_date"] = entry.RecurrenceEndDate.Format("2006-01-02")
			}
			if entry.RecurrenceParentID != nil {
				recurrenceInfo["parent_id"] = *entry.RecurrenceParentID
			}
		}

		event := calendarEvent{
			ID:        entry.ID,
			Title:     title,
			Start:     entry.StartsAt.Format(time.RFC3339),
			End:       entry.EndsAt.Format(time.RFC3339),
			ClassName: "event-" + entry.SourceType,
			Extendedprops: map[string]any{
				"source_type":  entry.SourceType,
				"source_id":    entry.SourceID,
				"source_label": sourceLabel,
				"source_name":  title,
				"mount_id":     entry.MountID,
				"metadata":     entry.Metadata,
				"recurrence":   recurrenceInfo,
				"is_instance":  entry.IsInstance,
			},
		}

		events = append(events, event)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// expandRecurringEntry generates virtual instances for a recurring entry within a date range
func (h *Handler) expandRecurringEntry(entry models.ScheduleEntry, rangeStart, rangeEnd time.Time) []models.ScheduleEntry {
	var instances []models.ScheduleEntry
	duration := entry.EndsAt.Sub(entry.StartsAt)

	// Start from the original entry's date
	current := entry.StartsAt

	// If the entry starts before our range, find the first occurrence in range
	for current.Before(rangeStart) {
		current = h.nextOccurrence(entry, current)
		if current.IsZero() {
			return instances
		}
	}

	// Generate instances within range (limit to prevent infinite loops)
	maxInstances := 100
	for i := 0; i < maxInstances && !current.After(rangeEnd); i++ {
		// Check if we've passed the recurrence end date
		if entry.RecurrenceEndDate != nil && current.After(*entry.RecurrenceEndDate) {
			break
		}

		// Check if this day matches the recurrence pattern
		if h.matchesRecurrence(entry, current) {
			instance := entry
			instance.ID = entry.ID + "_" + current.Format("20060102")
			instance.StartsAt = current
			instance.EndsAt = current.Add(duration)
			instance.IsInstance = true
			instance.RecurrenceParentID = &entry.ID
			instances = append(instances, instance)
		}

		current = h.nextOccurrence(entry, current)
		if current.IsZero() {
			break
		}
	}

	return instances
}

// nextOccurrence finds the next potential occurrence date
func (h *Handler) nextOccurrence(entry models.ScheduleEntry, from time.Time) time.Time {
	switch entry.RecurrenceType {
	case models.RecurrenceDaily:
		return from.AddDate(0, 0, 1)
	case models.RecurrenceWeekdays:
		next := from.AddDate(0, 0, 1)
		// Skip weekends
		for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
			next = next.AddDate(0, 0, 1)
		}
		return next
	case models.RecurrenceWeekly:
		return from.AddDate(0, 0, 7)
	case models.RecurrenceCustom:
		// For custom, advance day by day
		return from.AddDate(0, 0, 1)
	}
	return time.Time{}
}

// matchesRecurrence checks if a date matches the recurrence pattern
func (h *Handler) matchesRecurrence(entry models.ScheduleEntry, date time.Time) bool {
	switch entry.RecurrenceType {
	case models.RecurrenceDaily:
		return true
	case models.RecurrenceWeekdays:
		wd := date.Weekday()
		return wd != time.Saturday && wd != time.Sunday
	case models.RecurrenceWeekly:
		// Same day of week as original
		return date.Weekday() == entry.StartsAt.Weekday()
	case models.RecurrenceCustom:
		// Check if this weekday is in the allowed days
		if len(entry.RecurrenceDays) == 0 {
			return true
		}
		wd := int(date.Weekday())
		for _, d := range entry.RecurrenceDays {
			if d == wd {
				return true
			}
		}
		return false
	}
	return false
}

// ScheduleCreateEntry creates a new schedule entry
func (h *Handler) ScheduleCreateEntry(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	var input struct {
		MountID           string         `json:"mount_id"`
		StartsAt          time.Time      `json:"starts_at"`
		EndsAt            time.Time      `json:"ends_at"`
		SourceType        string         `json:"source_type"`
		SourceID          string         `json:"source_id"`
		Metadata          map[string]any `json:"metadata"`
		RecurrenceType    string         `json:"recurrence_type"`
		RecurrenceDays    []int          `json:"recurrence_days"`
		RecurrenceEndDate *string        `json:"recurrence_end_date"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// If no mount specified, use the first mount for this station
	mountID := input.MountID
	if mountID == "" {
		var mount models.Mount
		if err := h.db.Where("station_id = ?", station.ID).First(&mount).Error; err == nil {
			mountID = mount.ID
		} else {
			http.Error(w, "No mount available for this station", http.StatusBadRequest)
			return
		}
	}

	entry := models.ScheduleEntry{
		ID:             uuid.New().String(),
		StationID:      station.ID,
		MountID:        mountID,
		StartsAt:       input.StartsAt,
		EndsAt:         input.EndsAt,
		SourceType:     input.SourceType,
		SourceID:       input.SourceID,
		Metadata:       input.Metadata,
		RecurrenceType: models.RecurrenceType(input.RecurrenceType),
		RecurrenceDays: input.RecurrenceDays,
	}

	// Parse recurrence end date if provided
	if input.RecurrenceEndDate != nil && *input.RecurrenceEndDate != "" {
		if endDate, err := time.Parse("2006-01-02", *input.RecurrenceEndDate); err == nil {
			entry.RecurrenceEndDate = &endDate
		}
	}

	if err := h.db.Create(&entry).Error; err != nil {
		http.Error(w, "Failed to create entry", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

// ScheduleUpdateEntry updates a schedule entry
func (h *Handler) ScheduleUpdateEntry(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	// Handle virtual instance IDs (parentID_YYYYMMDD)
	realID := id
	instanceDate := ""
	if idx := len(id) - 9; idx > 0 && id[idx] == '_' {
		realID = id[:idx]
		instanceDate = id[idx+1:]
	}

	var entry models.ScheduleEntry
	if err := h.db.First(&entry, "id = ?", realID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	var input struct {
		StartsAt          time.Time      `json:"starts_at"`
		EndsAt            time.Time      `json:"ends_at"`
		SourceType        *string        `json:"source_type"`
		SourceID          *string        `json:"source_id"`
		Metadata          map[string]any `json:"metadata"`
		RecurrenceType    *string        `json:"recurrence_type"`
		RecurrenceDays    []int          `json:"recurrence_days"`
		RecurrenceEndDate *string        `json:"recurrence_end_date"`
		EditMode          string         `json:"edit_mode"` // "single" or "all"
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// If editing a single instance of a recurring entry, create an exception
	if instanceDate != "" && input.EditMode == "single" {
		// Create a new one-off entry for this instance
		parentID := entry.ID
		newEntry := models.ScheduleEntry{
			ID:                 uuid.New().String(),
			StationID:          station.ID,
			MountID:            entry.MountID,
			StartsAt:           input.StartsAt,
			EndsAt:             input.EndsAt,
			SourceType:         entry.SourceType,
			SourceID:           entry.SourceID,
			Metadata:           entry.Metadata,
			IsInstance:         true,
			RecurrenceParentID: &parentID,
		}

		// Override with provided values
		if input.SourceType != nil {
			newEntry.SourceType = *input.SourceType
		}
		if input.SourceID != nil {
			newEntry.SourceID = *input.SourceID
		}
		if input.Metadata != nil {
			newEntry.Metadata = input.Metadata
		}

		if err := h.db.Create(&newEntry).Error; err != nil {
			http.Error(w, "Failed to create instance", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(newEntry)
		return
	}

	// Update the main entry (all occurrences or non-recurring)
	entry.StartsAt = input.StartsAt
	entry.EndsAt = input.EndsAt

	// Update source if provided
	if input.SourceType != nil {
		entry.SourceType = *input.SourceType
	}
	if input.SourceID != nil {
		entry.SourceID = *input.SourceID
	}
	if input.Metadata != nil {
		entry.Metadata = input.Metadata
	}

	// Update recurrence if provided
	if input.RecurrenceType != nil {
		entry.RecurrenceType = models.RecurrenceType(*input.RecurrenceType)
	}
	if input.RecurrenceDays != nil {
		entry.RecurrenceDays = input.RecurrenceDays
	}
	if input.RecurrenceEndDate != nil {
		if *input.RecurrenceEndDate == "" {
			entry.RecurrenceEndDate = nil
		} else if endDate, err := time.Parse("2006-01-02", *input.RecurrenceEndDate); err == nil {
			entry.RecurrenceEndDate = &endDate
		}
	}

	if err := h.db.Save(&entry).Error; err != nil {
		http.Error(w, "Failed to update entry", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

// ScheduleDeleteEntry deletes a schedule entry
func (h *Handler) ScheduleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.db.Delete(&models.ScheduleEntry{}, "id = ?", id).Error; err != nil {
		http.Error(w, "Failed to delete entry", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ScheduleRefresh triggers a schedule refresh
func (h *Handler) ScheduleRefresh(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	// TODO: Call scheduler service to refresh
	// For now, just return success

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(`<div class="alert alert-success">Schedule refresh queued</div>`))
		return
	}

	w.WriteHeader(http.StatusOK)
}

// SchedulePlaylistsJSON returns playlists as JSON for schedule dropdowns
func (h *Handler) SchedulePlaylistsJSON(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	var playlists []models.Playlist
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&playlists)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"playlists": playlists})
}

// ScheduleSmartBlocksJSON returns smart blocks as JSON for schedule dropdowns
func (h *Handler) ScheduleSmartBlocksJSON(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	var smartBlocks []models.SmartBlock
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&smartBlocks)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"smart_blocks": smartBlocks})
}

// ScheduleClocksJSON returns clock templates as JSON for schedule dropdowns
func (h *Handler) ScheduleClocksJSON(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	var clocks []models.ClockHour
	h.db.Where("station_id = ?", station.ID).Preload("Slots").Order("name ASC").Find(&clocks)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"clocks": clocks})
}

// ScheduleWebstreamsJSON returns webstreams as JSON for schedule dropdowns
func (h *Handler) ScheduleWebstreamsJSON(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	var webstreams []models.Webstream
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&webstreams)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"webstreams": webstreams})
}

// ScheduleMediaSearchJSON searches media items for schedule dropdowns
func (h *Handler) ScheduleMediaSearchJSON(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	query := r.URL.Query().Get("q")
	includeArchive := r.URL.Query().Get("include_archive") == "true"
	limit := 20

	var items []models.MediaItem
	dbQuery := h.db.Model(&models.MediaItem{})

	// Include public archive media from other stations if requested
	if includeArchive {
		dbQuery = dbQuery.Where("station_id = ? OR show_in_archive = ?", station.ID, true)
		dbQuery = dbQuery.Preload("Station")
	} else {
		dbQuery = dbQuery.Where("station_id = ?", station.ID)
	}

	if query != "" {
		searchPattern := "%" + strings.ToLower(query) + "%"
		dbQuery = dbQuery.Where("LOWER(title) LIKE ? OR LOWER(artist) LIKE ? OR LOWER(album) LIKE ?",
			searchPattern, searchPattern, searchPattern)
	}

	dbQuery.Order("title ASC").Limit(limit).Find(&items)

	// Return simplified response for dropdown
	type mediaResponse struct {
		ID          string  `json:"id"`
		Title       string  `json:"title"`
		Artist      string  `json:"artist"`
		Duration    float64 `json:"duration"`
		StationName string  `json:"station_name,omitempty"`
		IsArchive   bool    `json:"is_archive,omitempty"`
	}

	response := make([]mediaResponse, len(items))
	for i, item := range items {
		resp := mediaResponse{
			ID:       item.ID,
			Title:    item.Title,
			Artist:   item.Artist,
			Duration: item.Duration.Seconds(),
		}
		// Mark items from other stations as archive
		if includeArchive && item.StationID != station.ID {
			resp.IsArchive = true
			if item.Station != nil {
				resp.StationName = item.Station.Name
			}
		}
		response[i] = resp
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"items": response})
}
