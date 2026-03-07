/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/scheduling"
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
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

type EffectiveSchedulePreviewData struct {
	WindowStart time.Time
	WindowEnd   time.Time
	Hours       int
	MountID     string
	MountName   string
	Items       []EffectiveSchedulePreviewItem
}

type EffectiveSchedulePreviewItem struct {
	ID                    string
	Title                 string
	SourceType            string
	SourceLabel           string
	MountName             string
	StartsAt              time.Time
	EndsAt                time.Time
	State                 string
	StatusLabel           string
	StatusTone            string
	StatusReason          string
	Headline              string
	RuntimeMismatch       bool
	RuntimeMismatchReason string
}

func classifyRuntimeMismatch(entry models.ScheduleEntry, mountState models.MountPlayoutState) (bool, string, string) {
	if mountState.SourceType != "" && mountState.SourceType != entry.SourceType {
		return true,
			fmt.Sprintf("Runtime source type is %s while schedule expects %s.", mountState.SourceType, entry.SourceType),
			"Wrong Source Type"
	}
	if entry.SourceType != "live" && entry.SourceID != "" && mountState.SourceID != "" && mountState.SourceID != entry.SourceID {
		return true,
			"Runtime source does not match the configured schedule source.",
			"Wrong Source"
	}
	return false, "", ""
}

func scheduleStatusForPreview(entry models.ScheduleEntry, runtimeMismatch bool, runtimeMismatchLabel string) (string, string, string) {
	if runtimeMismatch {
		return "mismatch", runtimeMismatchLabel, "The active playout state does not match this saved schedule block."
	}
	if entry.IsInstance && !isVirtualRecurringInstance(entry) {
		return "override", "Saved Override", "This occurrence was changed separately from the main recurring rule."
	}
	if isVirtualRecurringInstance(entry) {
		return "scheduled", "Recurring Block", "This occurrence was generated from the recurring schedule rule."
	}
	return "scheduled", "Scheduled", "This block is following the saved schedule as configured."
}

func previewStatusTone(state string) string {
	switch state {
	case "override":
		return "warning text-dark"
	case "mismatch":
		return "danger"
	default:
		return "secondary"
	}
}

func isVirtualRecurringInstance(entry models.ScheduleEntry) bool {
	if !entry.IsInstance || entry.RecurrenceParentID == nil {
		return false
	}
	parentID, _, ok := parseRecurringInstanceID(entry.ID)
	return ok && parentID == *entry.RecurrenceParentID
}

// ScheduleEffectivePreview renders the next window of effective schedule items for operator review.
func (h *Handler) ScheduleEffectivePreview(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	hours := 24
	mountID := strings.TrimSpace(r.URL.Query().Get("mount_id"))
	data := h.loadEffectiveSchedulePreviewData(r, station.ID, mountID, hours)
	h.RenderPartial(w, r, "partials/schedule-effective-preview", data)
}

func (h *Handler) loadEffectiveSchedulePreviewData(r *http.Request, stationID, mountID string, hours int) EffectiveSchedulePreviewData {
	now := time.Now().UTC()
	end := now.Add(time.Duration(hours) * time.Hour)
	data := EffectiveSchedulePreviewData{
		WindowStart: now,
		WindowEnd:   end,
		Hours:       hours,
		MountID:     mountID,
	}

	var mounts []models.Mount
	_ = h.db.WithContext(r.Context()).Where("station_id = ?", stationID).Order("name ASC").Find(&mounts).Error
	mountNames := make(map[string]string, len(mounts))
	for _, mount := range mounts {
		mountNames[mount.ID] = mount.Name
		if mount.ID == mountID {
			data.MountName = mount.Name
		}
	}

	query := h.db.WithContext(r.Context()).
		Where("station_id = ? AND starts_at < ? AND ends_at > ? AND (recurrence_type = '' OR recurrence_type IS NULL OR is_instance = true)", stationID, end, now)
	if mountID != "" {
		query = query.Where("mount_id = ?", mountID)
	}
	var entries []models.ScheduleEntry
	_ = query.Order("starts_at ASC").Find(&entries).Error

	instanceOverrides := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsInstance && entry.RecurrenceParentID != nil {
			instanceOverrides[recurrenceInstanceKey(*entry.RecurrenceParentID, entry.StartsAt)] = struct{}{}
		}
	}

	recurringQuery := h.db.WithContext(r.Context()).
		Where("station_id = ? AND recurrence_type != '' AND recurrence_type IS NOT NULL AND is_instance = false", stationID)
	if mountID != "" {
		recurringQuery = recurringQuery.Where("mount_id = ?", mountID)
	}
	var recurringEntries []models.ScheduleEntry
	_ = recurringQuery.Find(&recurringEntries).Error
	for _, re := range recurringEntries {
		entries = append(entries, h.expandRecurringEntry(re, now.Add(-24*time.Hour), end, instanceOverrides)...)
	}

	filtered := make([]models.ScheduleEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.EndsAt.After(now) && entry.StartsAt.Before(end) {
			filtered = append(filtered, entry)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].StartsAt.Equal(filtered[j].StartsAt) {
			return filtered[i].ID < filtered[j].ID
		}
		return filtered[i].StartsAt.Before(filtered[j].StartsAt)
	})
	if len(filtered) > 16 {
		filtered = filtered[:16]
	}

	activeMountStates := make(map[string]models.MountPlayoutState)
	var mountStates []models.MountPlayoutState
	if err := h.db.WithContext(r.Context()).Where("station_id = ?", stationID).Find(&mountStates).Error; err == nil {
		for _, state := range mountStates {
			activeMountStates[state.MountID] = state
		}
	}

	for _, entry := range filtered {
		title, label := h.resolveSchedulePreviewLabel(r, entry)
		headline := "Check this scheduled block before air."
		runtimeMismatch := false
		runtimeMismatchReason := ""
		runtimeMismatchLabel := ""
		switch entry.SourceType {
		case "live":
			headline = "A live source is expected to take this block."
		case "webstream":
			headline = "This block should relay the selected stream."
		case "smart_block":
			headline = "This block will be filled by the smart block rules."
		case "clock_template":
			headline = "This block will be filled by the clock's scheduled slots."
		case "playlist":
			headline = "This block will pull from the selected playlist."
		case "media":
			headline = "This block points to one fixed track."
		}
		if isVirtualRecurringInstance(entry) {
			headline = "This block was generated from the recurring schedule rule."
		}
		if entry.StartsAt.Before(now) && entry.EndsAt.After(now) {
			if mountState, ok := activeMountStates[entry.MountID]; ok {
				runtimeMismatch, runtimeMismatchReason, runtimeMismatchLabel = classifyRuntimeMismatch(entry, mountState)
			}
		}
		state, statusLabel, statusReason := scheduleStatusForPreview(entry, runtimeMismatch, runtimeMismatchLabel)
		if runtimeMismatch {
			headline = runtimeMismatchReason
		}
		data.Items = append(data.Items, EffectiveSchedulePreviewItem{
			ID:                    entry.ID,
			Title:                 title,
			SourceType:            entry.SourceType,
			SourceLabel:           label,
			MountName:             mountNames[entry.MountID],
			StartsAt:              entry.StartsAt,
			EndsAt:                entry.EndsAt,
			State:                 state,
			StatusLabel:           statusLabel,
			StatusTone:            previewStatusTone(state),
			StatusReason:          statusReason,
			Headline:              headline,
			RuntimeMismatch:       runtimeMismatch,
			RuntimeMismatchReason: runtimeMismatchReason,
		})
	}

	return data
}

func (h *Handler) resolveSchedulePreviewLabel(r *http.Request, entry models.ScheduleEntry) (string, string) {
	switch entry.SourceType {
	case "playlist":
		var playlist models.Playlist
		if err := h.db.WithContext(r.Context()).Select("id, name").First(&playlist, "id = ?", entry.SourceID).Error; err == nil {
			return playlist.Name, "Playlist"
		}
		return "Playlist", "Playlist"
	case "smart_block":
		var block models.SmartBlock
		if err := h.db.WithContext(r.Context()).Select("id, name").First(&block, "id = ?", entry.SourceID).Error; err == nil {
			return block.Name, "Smart Block"
		}
		return "Smart Block", "Smart Block"
	case "clock_template":
		var clock models.ClockHour
		if err := h.db.WithContext(r.Context()).Select("id, name").First(&clock, "id = ?", entry.SourceID).Error; err == nil {
			return clock.Name, "Clock"
		}
		return "Clock", "Clock"
	case "webstream":
		var stream models.Webstream
		if err := h.db.WithContext(r.Context()).Select("id, name").First(&stream, "id = ?", entry.SourceID).Error; err == nil {
			return stream.Name, "Webstream"
		}
		return "Webstream", "Webstream"
	case "media":
		var media models.MediaItem
		if err := h.db.WithContext(r.Context()).Select("id, title, artist").First(&media, "id = ?", entry.SourceID).Error; err == nil {
			if media.Artist != "" {
				return media.Artist + " - " + media.Title, "Track"
			}
			return media.Title, "Track"
		}
		return "Track", "Track"
	case "live":
		if entry.Metadata != nil {
			if name, ok := entry.Metadata["session_name"].(string); ok && strings.TrimSpace(name) != "" {
				return name, "Live"
			}
		}
		return "Live Session", "Live"
	default:
		return entry.SourceType, entry.SourceType
	}
}

// ScheduleValidate validates the schedule for the current station (web-authenticated).
// This mirrors the API endpoint but works with the dashboard session auth (no JWT required).
func (h *Handler) ScheduleValidate(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	// Parse date range
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	start := time.Now()
	end := start.Add(7 * 24 * time.Hour) // Default: next 7 days

	if startStr != "" {
		if parsed, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = parsed
		}
	}
	if endStr != "" {
		if parsed, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = parsed
		}
	}

	// Limit validation range to 90 days
	if end.Sub(start) > 90*24*time.Hour {
		end = start.Add(90 * 24 * time.Hour)
	}

	validator := scheduling.NewValidator(h.db, zerolog.Nop())
	result, err := validator.Validate(station.ID, start, end)
	if err != nil {
		h.logger.Error().
			Err(err).
			Str("station_id", station.ID).
			Time("range_start", start).
			Time("range_end", end).
			Msg("schedule validation failed")
		http.Error(w, "Validation failed", http.StatusInternalServerError)
		return
	}
	h.logValidationSummary(station.ID, start, end, result)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
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

	// Track concrete instance overrides so we don't also render generated
	// virtual instances for the same parent/date.
	instanceOverrides := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsInstance && entry.RecurrenceParentID != nil {
			instanceOverrides[recurrenceInstanceKey(*entry.RecurrenceParentID, entry.StartsAt)] = struct{}{}
		}
	}

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
		instances := h.expandRecurringEntry(re, startTime, endTime, instanceOverrides)
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

	// Only expand smart_block entries into individual track events on day/time views,
	// not on week or month views where they'd clutter the calendar.
	calendarView := r.URL.Query().Get("view")
	expandTracks := calendarView == "" || calendarView == "timeGridDay" || calendarView == "listDay" || calendarView == "listWeek"

	engine := smartblock.New(h.db, h.logger)
	var expandedEntries []models.ScheduleEntry
	for _, entry := range entries {
		if !expandTracks || entry.SourceType != "smart_block" {
			continue
		}
		targetDuration := entry.EndsAt.Sub(entry.StartsAt)
		if targetDuration <= 0 {
			continue
		}
		result, err := engine.Generate(r.Context(), smartblock.GenerateRequest{
			SmartBlockID: entry.SourceID,
			Seed:         entry.StartsAt.Unix(),
			Duration:     targetDuration.Milliseconds(),
			StationID:    station.ID,
			MountID:      entry.MountID,
		})
		if err != nil {
			continue
		}
		for _, item := range result.Items {
			child := models.ScheduleEntry{
				ID:         entry.ID + "-t-" + item.MediaID,
				StationID:  station.ID,
				MountID:    entry.MountID,
				StartsAt:   entry.StartsAt.Add(time.Duration(item.StartsAtMS) * time.Millisecond),
				EndsAt:     entry.StartsAt.Add(time.Duration(item.EndsAtMS) * time.Millisecond),
				SourceType: "media",
				SourceID:   item.MediaID,
				Metadata: map[string]any{
					"smart_block_id": entry.SourceID,
					"expanded":       true,
				},
			}
			expandedEntries = append(expandedEntries, child)
			mediaIDs = append(mediaIDs, item.MediaID)
		}
	}

	// Fetch names for expanded media items
	if len(expandedEntries) > 0 {
		var items []models.MediaItem
		h.db.Select("id, title, artist").Where("id IN ?", mediaIDs).Find(&items)
		for _, m := range items {
			if m.Artist != "" {
				mediaNames[m.ID] = m.Artist + " - " + m.Title
			} else {
				mediaNames[m.ID] = m.Title
			}
		}
		entries = append(entries, expandedEntries...)
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

	nowUTC := time.Now().UTC()
	activeMountStates := make(map[string]models.MountPlayoutState)
	var mountStates []models.MountPlayoutState
	h.db.Where("station_id = ?", station.ID).Find(&mountStates)
	for _, state := range mountStates {
		activeMountStates[state.MountID] = state
	}

	events := make([]calendarEvent, 0, len(entries))
	for _, entry := range entries {
		// Get title based on source type and detect orphaned references
		var title string
		var sourceLabel string
		orphaned := false

		switch entry.SourceType {
		case "playlist":
			title = playlistNames[entry.SourceID]
			sourceLabel = "Playlist"
			if title == "" && entry.SourceID != "" {
				orphaned = true
			}
		case "smart_block":
			title = smartBlockNames[entry.SourceID]
			sourceLabel = "Smart Block"
			if title == "" && entry.SourceID != "" {
				orphaned = true
			}
		case "clock_template":
			title = clockNames[entry.SourceID]
			sourceLabel = "Clock"
			if title == "" && entry.SourceID != "" {
				orphaned = true
			}
		case "webstream":
			title = webstreamNames[entry.SourceID]
			sourceLabel = "Webstream"
			if title == "" && entry.SourceID != "" {
				orphaned = true
			}
		case "media":
			title = mediaNames[entry.SourceID]
			sourceLabel = "Track"
			if title == "" && entry.SourceID != "" {
				orphaned = true
			}
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
		case "show":
			// Imported shows without a clock - get title from metadata
			if entry.Metadata != nil {
				if name, ok := entry.Metadata["show_name"].(string); ok {
					title = name
				} else if t, ok := entry.Metadata["title"].(string); ok {
					title = t
				}
			}
			sourceLabel = "Show"
		default:
			title = entry.SourceType
			sourceLabel = entry.SourceType
		}

		// Orphaned entries get error styling
		if orphaned {
			title = "\u26a0 MISSING " + sourceLabel
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

		className := "event-" + entry.SourceType
		health := "green"
		runtimeMismatch := false
		runtimeMismatchReason := ""
		runtimeMismatchLabel := ""
		statusLabel := ""
		statusReason := ""
		if orphaned {
			className = "event-orphaned"
			health = "red"
			statusLabel = "Missing Source"
			statusReason = "This block points to a source the system cannot load."
		} else if entry.Metadata != nil {
			if _, ok := entry.Metadata["emergency_fallback"]; ok {
				health = "yellow"
				statusLabel = "Fallback Active"
				statusReason = "This block is running with emergency fallback metadata."
			} else if _, ok := entry.Metadata["constraint_relaxed"]; ok {
				health = "yellow"
				statusLabel = "Constraint Relaxed"
				statusReason = "Rules had to be relaxed to build this block."
			}
		}
		if entry.StartsAt.Before(nowUTC) && entry.EndsAt.After(nowUTC) {
			if mountState, ok := activeMountStates[entry.MountID]; ok {
				runtimeMismatch, runtimeMismatchReason, runtimeMismatchLabel = classifyRuntimeMismatch(entry, mountState)
			}
		}
		if runtimeMismatch && health == "green" {
			health = "yellow"
		}
		if runtimeMismatch {
			statusLabel = runtimeMismatchLabel
			statusReason = "The active playout state does not match the saved schedule block."
		} else if statusLabel == "" && entry.IsInstance && !isVirtualRecurringInstance(entry) {
			statusLabel = "Saved Override"
			statusReason = "This occurrence was changed separately from the main recurring rule."
		}

		event := calendarEvent{
			ID:        entry.ID,
			Title:     title,
			Start:     entry.StartsAt.Format(time.RFC3339),
			End:       entry.EndsAt.Format(time.RFC3339),
			ClassName: className,
			Extendedprops: map[string]any{
				"source_type":             entry.SourceType,
				"source_id":               entry.SourceID,
				"source_label":            sourceLabel,
				"source_name":             title,
				"mount_id":                entry.MountID,
				"metadata":                entry.Metadata,
				"recurrence":              recurrenceInfo,
				"is_instance":             entry.IsInstance,
				"orphaned":                orphaned,
				"health":                  health,
				"runtime_mismatch":        runtimeMismatch,
				"runtime_mismatch_reason": runtimeMismatchReason,
				"status_label":            statusLabel,
				"status_reason":           statusReason,
			},
		}

		events = append(events, event)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// expandRecurringEntry generates virtual instances for a recurring entry within a date range
func (h *Handler) expandRecurringEntry(entry models.ScheduleEntry, rangeStart, rangeEnd time.Time, overrides map[string]struct{}) []models.ScheduleEntry {
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
			if _, overridden := overrides[recurrenceInstanceKey(entry.ID, current)]; overridden {
				current = h.nextOccurrence(entry, current)
				if current.IsZero() {
					break
				}
				continue
			}

			instance := entry
			instance.ID = recurrenceInstanceKey(entry.ID, current)
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

func recurrenceInstanceKey(parentID string, at time.Time) string {
	return parentID + "_" + at.Format("20060102")
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

	if !input.EndsAt.After(input.StartsAt) {
		http.Error(w, "End time must be after start time", http.StatusBadRequest)
		return
	}

	hasOverlap, err := h.scheduleOverlaps(station.ID, input.StartsAt, input.EndsAt, "")
	if err != nil {
		http.Error(w, "Failed to validate schedule", http.StatusInternalServerError)
		return
	}
	if hasOverlap {
		writeScheduleJSONError(w, http.StatusConflict, "This time slot overlaps with another scheduled item. Open Validate to see the exact conflict and move one item so only one program is scheduled at a time.")
		return
	}

	mountID, err := h.resolveScheduleMountID(station, input.MountID)
	if err != nil {
		http.Error(w, "Failed to resolve schedule mount", http.StatusInternalServerError)
		return
	}

	entryID := uuid.New().String()
	sourceID := normalizeScheduleSourceID(input.SourceType, input.SourceID, entryID)
	entry := models.ScheduleEntry{
		ID:             entryID,
		StationID:      station.ID,
		MountID:        mountID,
		StartsAt:       input.StartsAt,
		EndsAt:         input.EndsAt,
		SourceType:     input.SourceType,
		SourceID:       sourceID,
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

	if h.eventBus != nil {
		h.eventBus.Publish(events.EventScheduleUpdate, events.Payload{
			"entry_id":    entry.ID,
			"station_id":  entry.StationID,
			"mount_id":    entry.MountID,
			"starts_at":   entry.StartsAt,
			"ends_at":     entry.EndsAt,
			"source_type": entry.SourceType,
			"source_id":   entry.SourceID,
			"metadata":    entry.Metadata,
			"event":       "create",
		})
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

	if !input.EndsAt.After(input.StartsAt) {
		http.Error(w, "End time must be after start time", http.StatusBadRequest)
		return
	}

	// If editing a single instance of a recurring entry, create an exception
	if instanceDate != "" && input.EditMode == "single" {
		hasOverlap, err := h.scheduleOverlaps(station.ID, input.StartsAt, input.EndsAt, realID)
		if err != nil {
			http.Error(w, "Failed to validate schedule", http.StatusInternalServerError)
			return
		}
		if hasOverlap {
			writeScheduleJSONError(w, http.StatusConflict, "This time slot overlaps with another scheduled item. Open Validate to see the exact conflict and move one item so only one program is scheduled at a time.")
			return
		}

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

		if h.eventBus != nil {
			h.eventBus.Publish(events.EventScheduleUpdate, events.Payload{
				"entry_id":    newEntry.ID,
				"station_id":  newEntry.StationID,
				"mount_id":    newEntry.MountID,
				"starts_at":   newEntry.StartsAt,
				"ends_at":     newEntry.EndsAt,
				"source_type": newEntry.SourceType,
				"source_id":   newEntry.SourceID,
				"metadata":    newEntry.Metadata,
				"event":       "create_instance",
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(newEntry)
		return
	}

	hasOverlap, err := h.scheduleOverlaps(station.ID, input.StartsAt, input.EndsAt, entry.ID)
	if err != nil {
		http.Error(w, "Failed to validate schedule", http.StatusInternalServerError)
		return
	}
	if hasOverlap {
		writeScheduleJSONError(w, http.StatusConflict, "This time slot overlaps with another scheduled item. Open Validate to see the exact conflict and move one item so only one program is scheduled at a time.")
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
	entry.SourceID = normalizeScheduleSourceID(entry.SourceType, entry.SourceID, entry.ID)
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

	if h.eventBus != nil {
		h.eventBus.Publish(events.EventScheduleUpdate, events.Payload{
			"entry_id":    entry.ID,
			"station_id":  entry.StationID,
			"mount_id":    entry.MountID,
			"starts_at":   entry.StartsAt,
			"ends_at":     entry.EndsAt,
			"source_type": entry.SourceType,
			"source_id":   entry.SourceID,
			"metadata":    entry.Metadata,
			"event":       "update",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

// ScheduleDeleteEntry deletes a schedule entry
func (h *Handler) ScheduleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	var entry models.ScheduleEntry
	targetID := id
	err := h.db.First(&entry, "id = ? AND station_id = ?", targetID, station.ID).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "Failed to load entry", http.StatusInternalServerError)
			return
		}
		// If this is a virtual recurring instance ID, attempt to map it to a
		// concrete overridden instance for that parent/day.
		parentID, dayStart, ok := parseRecurringInstanceID(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		dayEnd := dayStart.Add(24 * time.Hour)
		if err := h.db.Where("station_id = ? AND recurrence_parent_id = ? AND starts_at >= ? AND starts_at < ?",
			station.ID, parentID, dayStart, dayEnd).
			Order("starts_at ASC").
			First(&entry).Error; err != nil {
			http.NotFound(w, r)
			return
		}
		targetID = entry.ID
	}

	if err := h.db.Delete(&models.ScheduleEntry{}, "id = ? AND station_id = ?", targetID, station.ID).Error; err != nil {
		http.Error(w, "Failed to delete entry", http.StatusInternalServerError)
		return
	}

	if h.eventBus != nil {
		h.eventBus.Publish(events.EventScheduleUpdate, events.Payload{
			"entry_id":    entry.ID,
			"station_id":  station.ID,
			"mount_id":    entry.MountID,
			"starts_at":   entry.StartsAt,
			"ends_at":     entry.EndsAt,
			"source_type": entry.SourceType,
			"source_id":   entry.SourceID,
			"metadata":    entry.Metadata,
			"event":       "delete",
		})
	}

	w.WriteHeader(http.StatusNoContent)
}

func parseRecurringInstanceID(id string) (string, time.Time, bool) {
	if len(id) < 10 {
		return "", time.Time{}, false
	}
	idx := len(id) - 9
	if idx <= 0 || id[idx] != '_' {
		return "", time.Time{}, false
	}
	parentID := id[:idx]
	day, err := time.Parse("20060102", id[idx+1:])
	if err != nil {
		return "", time.Time{}, false
	}
	return parentID, day, true
}

func (h *Handler) scheduleOverlaps(stationID string, startsAt, endsAt time.Time, excludeID string) (bool, error) {
	q := h.db.Model(&models.ScheduleEntry{}).
		Where("station_id = ?", stationID).
		Where("starts_at < ? AND ends_at > ?", endsAt, startsAt)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func normalizeScheduleSourceID(sourceType, sourceID, fallbackUUID string) string {
	if strings.TrimSpace(sourceID) != "" {
		return sourceID
	}
	if sourceType == "live" {
		return fallbackUUID
	}
	return sourceID
}

func (h *Handler) resolveScheduleMountID(station *models.Station, requestedMountID string) (string, error) {
	if requestedMountID != "" {
		return requestedMountID, nil
	}

	var mount models.Mount
	err := h.db.Where("station_id = ?", station.ID).Order("created_at ASC").First(&mount).Error
	if err == nil {
		return mount.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}

	mountName := models.GenerateMountName(station.Name)
	defaultMount := models.Mount{
		ID:         uuid.New().String(),
		StationID:  station.ID,
		Name:       mountName,
		URL:        "/" + mountName,
		Format:     "mp3",
		Bitrate:    128,
		Channels:   2,
		SampleRate: 44100,
	}
	if err := h.db.Create(&defaultMount).Error; err != nil {
		return "", err
	}

	h.logger.Warn().
		Str("station_id", station.ID).
		Str("mount_id", defaultMount.ID).
		Msg("auto-created missing station mount for schedule entry")

	return defaultMount.ID, nil
}

func (h *Handler) logValidationSummary(stationID string, start, end time.Time, result *models.ValidationResult) {
	if result == nil {
		return
	}

	overlapViolations := make([]models.ValidationViolation, 0)
	collect := func(items []models.ValidationViolation) {
		for _, item := range items {
			if item.RuleType == models.RuleTypeOverlap {
				overlapViolations = append(overlapViolations, item)
			}
		}
	}
	collect(result.Errors)
	collect(result.Warnings)
	collect(result.Info)

	logger := h.logger.With().
		Str("station_id", stationID).
		Time("range_start", start).
		Time("range_end", end).
		Int("overlap_count", len(overlapViolations)).
		Bool("valid", result.Valid).
		Logger()

	if len(overlapViolations) == 0 {
		logger.Info().Msg("schedule validation completed with no overlaps")
		return
	}

	logger.Warn().Msg("schedule validation detected overlaps")
	for i, v := range overlapViolations {
		entry := logger.Warn().
			Int("overlap_index", i+1).
			Time("starts_at", v.StartsAt).
			Time("ends_at", v.EndsAt).
			Strs("affected_ids", v.AffectedIDs)
		if minutes, ok := v.Details["overlap_minutes"]; ok {
			entry = entry.Interface("overlap_minutes", minutes)
		}
		entry.Msg(v.Message)
	}
}

func writeScheduleJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (h *Handler) schedulerLookaheadDuration() time.Duration {
	lookahead := 168 * time.Hour
	if h.db == nil {
		return lookahead
	}

	settings, err := models.GetSystemSettings(h.db)
	if err != nil {
		return lookahead
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(settings.SchedulerLookahead))
	if err != nil || parsed <= 0 {
		return lookahead
	}
	return parsed
}

func deterministicSmartBlockSeed(entry models.ScheduleEntry, blockID string) int64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(entry.ID))
	_, _ = hasher.Write([]byte(blockID))
	_, _ = hasher.Write([]byte(entry.StartsAt.UTC().Format(time.RFC3339Nano)))
	_, _ = hasher.Write([]byte(entry.EndsAt.UTC().Format(time.RFC3339Nano)))
	_, _ = hasher.Write([]byte(entry.StationID))
	_, _ = hasher.Write([]byte(entry.MountID))
	return int64(hasher.Sum64() & 0x7fffffffffffffff)
}

// ScheduleEntryDetails returns detailed info about a schedule entry including what will be played
func (h *Handler) ScheduleEntryDetails(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var entry models.ScheduleEntry
	if err := h.db.First(&entry, "id = ?", id).Error; err != nil {
		http.Error(w, "Entry not found", http.StatusNotFound)
		return
	}

	response := map[string]any{
		"id":          entry.ID,
		"source_type": entry.SourceType,
		"source_id":   entry.SourceID,
		"starts_at":   entry.StartsAt,
		"ends_at":     entry.EndsAt,
		"metadata":    entry.Metadata,
	}

	// Get detailed content based on source type
	switch entry.SourceType {
	case "clock_template":
		var clockHour models.ClockHour
		if err := h.db.Preload("Slots").First(&clockHour, "id = ?", entry.SourceID).Error; err == nil {
			response["clock"] = map[string]any{
				"id":   clockHour.ID,
				"name": clockHour.Name,
			}

			slotsSorted := append([]models.ClockSlot(nil), clockHour.Slots...)
			sort.Slice(slotsSorted, func(i, j int) bool {
				return slotsSorted[i].Position < slotsSorted[j].Position
			})

			lookahead := h.schedulerLookaheadDuration()
			now := time.Now().UTC()
			clockTrace := map[string]any{
				"entry_start":  entry.StartsAt,
				"entry_end":    entry.EndsAt,
				"queued_slots": []map[string]any{},
			}

			// Past playback trace for this clock entry window.
			var historyRows []models.PlayHistory
			h.db.
				Where("station_id = ? AND mount_id = ? AND started_at >= ? AND started_at <= ?",
					entry.StationID, entry.MountID, entry.StartsAt, entry.EndsAt).
				Order("started_at ASC").
				Find(&historyRows)

			playedTracks := make([]map[string]any, 0, len(historyRows))
			for _, ph := range historyRows {
				if ph.MetadataString("clock_id") != clockHour.ID {
					continue
				}
				playedTracks = append(playedTracks, map[string]any{
					"started_at":     ph.StartedAt,
					"ended_at":       ph.EndedAt,
					"media_id":       ph.MediaID,
					"title":          ph.Title,
					"artist":         ph.Artist,
					"duration":       int64(ph.EndedAt.Sub(ph.StartedAt).Seconds()),
					"smart_block_id": ph.MetadataString("smart_block_id"),
					"playlist_id":    ph.MetadataString("playlist_id"),
					"clock_id":       ph.MetadataString("clock_id"),
				})
			}
			clockTrace["played_tracks"] = playedTracks

			// Get slot details and queued traces
			var slots []map[string]any
			queuedSlots := make([]map[string]any, 0, len(slotsSorted))
			for i, slot := range slotsSorted {
				slotStart := entry.StartsAt.Add(slot.Offset)
				slotEnd := entry.EndsAt
				if i+1 < len(slotsSorted) {
					nextStart := entry.StartsAt.Add(slotsSorted[i+1].Offset)
					if nextStart.Before(slotEnd) {
						slotEnd = nextStart
					}
				}
				if slotStart.After(entry.EndsAt) || !slotStart.Before(slotEnd) {
					continue
				}

				slotInfo := map[string]any{
					"id":       slot.ID,
					"position": slot.Position,
					"offset":   slot.Offset,
					"type":     slot.Type,
				}
				slotTrace := map[string]any{
					"id":         slot.ID,
					"position":   slot.Position,
					"offset":     slot.Offset,
					"type":       slot.Type,
					"starts_at":  slotStart,
					"ends_at":    slotEnd,
					"source_map": "clock -> slot source -> media",
				}

				// Get content for each slot type
				switch slot.Type {
				case models.SlotTypePlaylist:
					if playlistID, ok := slot.Payload["playlist_id"].(string); ok {
						var playlist models.Playlist
						if h.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
							return db.Order("position ASC").Limit(20)
						}).Preload("Items.Media").First(&playlist, "id = ?", playlistID).Error == nil {
							var tracks []map[string]any
							var totalDuration int64
							for _, item := range playlist.Items {
								durSec := int64(item.Media.Duration.Seconds())
								tracks = append(tracks, map[string]any{
									"media_id": item.Media.ID,
									"title":    item.Media.Title,
									"artist":   item.Media.Artist,
									"duration": durSec,
								})
								totalDuration += durSec
							}
							slotInfo["playlist"] = map[string]any{
								"id":             playlist.ID,
								"name":           playlist.Name,
								"track_count":    len(playlist.Items),
								"total_duration": totalDuration,
								"tracks":         tracks,
							}
							slotTrace["playlist"] = slotInfo["playlist"]
						}
					}
				case models.SlotTypeSmartBlock:
					if smartBlockID, ok := slot.Payload["smart_block_id"].(string); ok {
						var smartBlock models.SmartBlock
						if h.db.First(&smartBlock, "id = ?", smartBlockID).Error == nil {
							sbInfo := map[string]any{
								"id":   smartBlock.ID,
								"name": smartBlock.Name,
							}
							slotInfo["smart_block"] = sbInfo
							slotTrace["smart_block"] = sbInfo

							materializeAt := slotStart.Add(-lookahead)
							if now.Before(materializeAt) {
								slotTrace["smart_block_preview"] = map[string]any{
									"status":      "pending_materialization",
									"message":     "This section has not been created yet.",
									"expected_at": materializeAt,
									"help":        "It will be built when this slot enters the scheduler lookahead window.",
								}
							} else {
								targetDuration := entry.EndsAt.Sub(entry.StartsAt)
								if targetDuration < 0 {
									targetDuration = 0
								}
								engine := smartblock.New(h.db, h.logger)
								seed := deterministicSmartBlockSeed(entry, smartBlock.ID)
								result, genErr := engine.Generate(r.Context(), smartblock.GenerateRequest{
									SmartBlockID: smartBlock.ID,
									Seed:         seed,
									Duration:     targetDuration.Milliseconds(),
									StationID:    entry.StationID,
									MountID:      entry.MountID,
								})

								preview := map[string]any{
									"status":            "ready",
									"seed":              seed,
									"target_duration_s": int64(targetDuration.Seconds()),
								}
								if genErr != nil {
									preview["status"] = "error"
									preview["error"] = "Unable to generate track selection for this slot yet."
									preview["cause"] = genErr.Error()
									slotTrace["smart_block_preview"] = preview
								} else {
									mediaIDs := make([]string, 0, len(result.Items))
									for _, item := range result.Items {
										if item.MediaID != "" {
											mediaIDs = append(mediaIDs, item.MediaID)
										}
									}
									mediaByID := make(map[string]models.MediaItem, len(mediaIDs))
									if len(mediaIDs) > 0 {
										var mediaItems []models.MediaItem
										h.db.Select("id, title, artist, duration").Where("id IN ?", mediaIDs).Find(&mediaItems)
										for _, m := range mediaItems {
											mediaByID[m.ID] = m
										}
									}
									tracks := make([]map[string]any, 0, len(result.Items))
									for _, item := range result.Items {
										media := mediaByID[item.MediaID]
										tracks = append(tracks, map[string]any{
											"media_id":    item.MediaID,
											"title":       media.Title,
											"artist":      media.Artist,
											"duration":    int64(media.Duration.Seconds()),
											"starts_at_s": item.StartsAtMS / 1000,
											"ends_at_s":   item.EndsAtMS / 1000,
											"is_bumper":   item.IsBumper,
										})
									}
									preview["track_count"] = len(tracks)
									preview["total_duration_s"] = result.TotalMS / 1000
									preview["bumper_count"] = result.BumperCount
									preview["bumper_limit"] = result.BumperLimit
									preview["bumper_limit_reached"] = result.BumperLimitReached
									preview["warnings"] = result.Warnings
									preview["tracks"] = tracks
									slotTrace["smart_block_preview"] = preview
								}
							}
						}
					}
				case models.SlotTypeHardItem:
					if mediaID, ok := slot.Payload["media_id"].(string); ok {
						var media models.MediaItem
						if h.db.Select("id, title, artist, duration").First(&media, "id = ?", mediaID).Error == nil {
							track := map[string]any{
								"media_id": media.ID,
								"title":    media.Title,
								"artist":   media.Artist,
								"duration": int64(media.Duration.Seconds()),
							}
							slotTrace["media"] = track
						}
					}
				case models.SlotTypeWebstream:
					if webstreamID, ok := slot.Payload["webstream_id"].(string); ok {
						var ws models.Webstream
						if h.db.First(&ws, "id = ?", webstreamID).Error == nil {
							url := ""
							if len(ws.URLs) > 0 {
								url = ws.URLs[0]
							}
							slotTrace["webstream"] = map[string]any{
								"id":   ws.ID,
								"name": ws.Name,
								"url":  url,
							}
						}
					}
				}
				slots = append(slots, slotInfo)
				queuedSlots = append(queuedSlots, slotTrace)
			}
			response["slots"] = slots
			clockTrace["queued_slots"] = queuedSlots
			response["clock_trace"] = clockTrace
		}

	case "playlist":
		var playlist models.Playlist
		if err := h.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
			return db.Order("position ASC").Limit(50)
		}).Preload("Items.Media").First(&playlist, "id = ?", entry.SourceID).Error; err == nil {
			var tracks []map[string]any
			var totalDuration int64
			for _, item := range playlist.Items {
				durSec := int64(item.Media.Duration.Seconds())
				tracks = append(tracks, map[string]any{
					"title":    item.Media.Title,
					"artist":   item.Media.Artist,
					"duration": durSec,
				})
				totalDuration += durSec
			}
			response["playlist"] = map[string]any{
				"id":             playlist.ID,
				"name":           playlist.Name,
				"track_count":    len(playlist.Items),
				"total_duration": totalDuration,
				"tracks":         tracks,
			}
		}

	case "smart_block":
		var smartBlock models.SmartBlock
		if err := h.db.First(&smartBlock, "id = ?", entry.SourceID).Error; err == nil {
			response["smart_block"] = map[string]any{
				"id":    smartBlock.ID,
				"name":  smartBlock.Name,
				"rules": smartBlock.Rules,
			}

			lookahead := h.schedulerLookaheadDuration()
			materializeAt := entry.StartsAt.Add(-lookahead)
			if time.Now().Before(materializeAt) {
				response["smart_block_preview"] = map[string]any{
					"status":      "pending_materialization",
					"message":     "This section has not been created yet.",
					"expected_at": materializeAt,
					"help":        "It is scheduled to be built when it enters the scheduler lookahead window. Please check back after the expected time.",
				}
				break
			}

			targetDuration := entry.EndsAt.Sub(entry.StartsAt)
			if targetDuration < 0 {
				targetDuration = 0
			}

			engine := smartblock.New(h.db, h.logger)
			result, genErr := engine.Generate(r.Context(), smartblock.GenerateRequest{
				SmartBlockID: smartBlock.ID,
				Seed:         entry.StartsAt.Unix(),
				Duration:     targetDuration.Milliseconds(),
				StationID:    entry.StationID,
				MountID:      entry.MountID,
			})

			preview := map[string]any{
				"status":            "ready",
				"seed":              entry.StartsAt.Unix(),
				"target_duration_s": int64(targetDuration.Seconds()),
			}

			if genErr != nil {
				preview["status"] = "error"
				preview["error"] = "Unable to generate track selection for this time window."
				preview["cause"] = genErr.Error()
				response["smart_block_preview"] = preview
				break
			}

			mediaIDs := make([]string, 0, len(result.Items))
			for _, item := range result.Items {
				if item.MediaID != "" {
					mediaIDs = append(mediaIDs, item.MediaID)
				}
			}

			mediaByID := make(map[string]models.MediaItem, len(mediaIDs))
			if len(mediaIDs) > 0 {
				var mediaItems []models.MediaItem
				h.db.Select("id, title, artist, duration").
					Where("id IN ?", mediaIDs).
					Find(&mediaItems)
				for _, m := range mediaItems {
					mediaByID[m.ID] = m
				}
			}

			tracks := make([]map[string]any, 0, len(result.Items))
			for _, item := range result.Items {
				media := mediaByID[item.MediaID]
				tracks = append(tracks, map[string]any{
					"media_id":    item.MediaID,
					"title":       media.Title,
					"artist":      media.Artist,
					"duration":    int64(media.Duration.Seconds()),
					"starts_at_s": item.StartsAtMS / 1000,
					"ends_at_s":   item.EndsAtMS / 1000,
					"is_bumper":   item.IsBumper,
				})
			}

			preview["total_duration_s"] = result.TotalMS / 1000
			preview["track_count"] = len(tracks)
			preview["bumper_count"] = result.BumperCount
			preview["bumper_limit"] = result.BumperLimit
			preview["bumper_limit_reached"] = result.BumperLimitReached
			preview["warnings"] = result.Warnings
			preview["tracks"] = tracks
			response["smart_block_preview"] = preview
		}

	case "media":
		var media models.MediaItem
		if err := h.db.First(&media, "id = ?", entry.SourceID).Error; err == nil {
			response["media"] = map[string]any{
				"id":       media.ID,
				"title":    media.Title,
				"artist":   media.Artist,
				"album":    media.Album,
				"duration": int64(media.Duration.Seconds()),
				"genre":    media.Genre,
			}
		}

	case "webstream":
		var webstream models.Webstream
		if err := h.db.First(&webstream, "id = ?", entry.SourceID).Error; err == nil {
			var url string
			if len(webstream.URLs) > 0 {
				url = webstream.URLs[0]
			}
			response["webstream"] = map[string]any{
				"id":   webstream.ID,
				"name": webstream.Name,
				"url":  url,
			}
		}
	}

	response["resolution_summary"] = buildScheduleResolutionSummary(entry, response)
	response["effective_preview"] = buildEffectiveEntryPreview(entry, response)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func buildScheduleResolutionSummary(entry models.ScheduleEntry, response map[string]any) map[string]any {
	summary := map[string]any{
		"configured_type": entry.SourceType,
		"configured_id":   entry.SourceID,
		"duration_s":      int64(entry.EndsAt.Sub(entry.StartsAt).Seconds()),
	}

	checks := make([]string, 0, 4)
	state := "ok"
	headline := "This block is ready as currently configured."
	resolvedAs := entry.SourceType

	switch entry.SourceType {
	case "clock_template":
		if clock, ok := response["clock"].(map[string]any); ok {
			if name, ok := clock["name"].(string); ok && strings.TrimSpace(name) != "" {
				headline = "This clock will fill this time block using the clock's slots and their assigned sources."
				resolvedAs = name
			}
		}
		if trace, ok := response["clock_trace"].(map[string]any); ok {
			if queued, ok := trace["queued_slots"].([]map[string]any); ok {
				summary["queued_slot_count"] = len(queued)
				checks = append(checks, fmt.Sprintf("Make sure the %d clock slot(s) match what you expect in this block.", len(queued)))
			} else if queuedAny, ok := trace["queued_slots"].([]any); ok {
				summary["queued_slot_count"] = len(queuedAny)
				checks = append(checks, fmt.Sprintf("Make sure the %d clock slot(s) match what you expect in this block.", len(queuedAny)))
			}
			if playedAny, ok := trace["played_tracks"].([]any); ok && len(playedAny) > 0 {
				checks = append(checks, "Compare what already played against the current clock plan for this block.")
			}
		}
	case "playlist":
		if playlist, ok := response["playlist"].(map[string]any); ok {
			if name, ok := playlist["name"].(string); ok && strings.TrimSpace(name) != "" {
				resolvedAs = name
			}
			if count, ok := playlist["track_count"].(int); ok {
				summary["track_count"] = count
				headline = fmt.Sprintf("This block will pull from %d track(s) in the selected playlist.", count)
			} else if count, ok := playlist["track_count"].(float64); ok {
				summary["track_count"] = int(count)
				headline = fmt.Sprintf("This block will pull from %d track(s) in the selected playlist.", int(count))
			}
			checks = append(checks, "Make sure the playlist order and any track swaps look right before air.")
		}
	case "smart_block":
		if block, ok := response["smart_block"].(map[string]any); ok {
			if name, ok := block["name"].(string); ok && strings.TrimSpace(name) != "" {
				resolvedAs = name
			}
		}
		if preview, ok := response["smart_block_preview"].(map[string]any); ok {
			if status, _ := preview["status"].(string); status != "" {
				summary["preview_status"] = status
				switch status {
				case "pending_materialization":
					state = "pending"
					headline = "This smart block has not built its track plan for this block yet."
					checks = append(checks, "Wait until this block is closer to air time, then review the generated track list.")
				case "error":
					state = "attention"
					headline = "This smart block could not build a playable plan for this block."
					checks = append(checks, "Review the smart block rules or available matching tracks before trusting this block.")
				case "ready":
					if count, ok := preview["track_count"].(float64); ok {
						summary["track_count"] = int(count)
						headline = fmt.Sprintf("This smart block currently plans %d track(s) for this block.", int(count))
					}
					if bumperCount, ok := preview["bumper_count"].(float64); ok {
						summary["bumper_count"] = int(bumperCount)
						if bumperLimit, ok := preview["bumper_limit"].(float64); ok && bumperLimit > 0 {
							summary["bumper_limit"] = int(bumperLimit)
							checks = append(checks, fmt.Sprintf("Check the bumper fill: %d of %d allowed bumper slots were used.", int(bumperCount), int(bumperLimit)))
						} else {
							checks = append(checks, fmt.Sprintf("Check the bumper fill: %d extra bumper track(s) were used.", int(bumperCount)))
						}
					}
					if reached, ok := preview["bumper_limit_reached"].(bool); ok && reached {
						summary["bumper_limit_reached"] = true
						state = "attention"
						checks = append(checks, "The bumper cap was hit before the full block was filled. Review the smart block timing plan.")
					}
					checks = append(checks, "Make sure the generated track list still fits the block length and rotation you expect.")
				}
			}
		}
	case "media":
		if media, ok := response["media"].(map[string]any); ok {
			title, _ := media["title"].(string)
			artist, _ := media["artist"].(string)
			if artist != "" && title != "" {
				resolvedAs = artist + " - " + title
			} else if title != "" {
				resolvedAs = title
			}
			headline = "This block points to one fixed track."
			checks = append(checks, "Make sure the exact track and duration are the ones you want in this block.")
		}
	case "webstream":
		if webstream, ok := response["webstream"].(map[string]any); ok {
			if name, ok := webstream["name"].(string); ok && strings.TrimSpace(name) != "" {
				resolvedAs = name
			}
		}
		headline = "This block relays an external stream instead of selecting tracks."
		checks = append(checks, "Make sure the stream source is available and the right relay is selected.")
	case "live":
		headline = "This block expects a live operator or live source takeover."
		checks = append(checks, "Make sure the live source is ready before this block starts.")
	default:
		headline = "Review this block carefully before air."
	}

	if entry.MountID != "" {
		checks = append(checks, "Make sure this block is scheduled on the correct mount.")
	}

	summary["state"] = state
	summary["headline"] = headline
	summary["resolved_as"] = resolvedAs
	summary["checks"] = checks
	return summary
}

func buildEffectiveEntryPreview(entry models.ScheduleEntry, response map[string]any) map[string]any {
	preview := map[string]any{
		"entry_id":     entry.ID,
		"source_type":  entry.SourceType,
		"starts_at":    entry.StartsAt,
		"ends_at":      entry.EndsAt,
		"window_s":     int64(entry.EndsAt.Sub(entry.StartsAt).Seconds()),
		"sections":     []map[string]any{},
		"empty_reason": "",
	}

	sections := make([]map[string]any, 0, 8)
	addSection := func(title, kind string, items []map[string]any, note string) {
		sections = append(sections, map[string]any{
			"title": title,
			"kind":  kind,
			"items": items,
			"note":  note,
		})
	}
	collectItems := func(raw any) []map[string]any {
		switch items := raw.(type) {
		case []map[string]any:
			return items
		case []any:
			out := make([]map[string]any, 0, len(items))
			for _, itemRaw := range items {
				if item, ok := itemRaw.(map[string]any); ok {
					out = append(out, item)
				}
			}
			return out
		default:
			return nil
		}
	}
	collectSections := func(raw any) []map[string]any {
		switch sectionsRaw := raw.(type) {
		case []map[string]any:
			return sectionsRaw
		case []any:
			out := make([]map[string]any, 0, len(sectionsRaw))
			for _, sectionRaw := range sectionsRaw {
				if section, ok := sectionRaw.(map[string]any); ok {
					out = append(out, section)
				}
			}
			return out
		default:
			return nil
		}
	}

	switch entry.SourceType {
	case "clock_template":
		if trace, ok := response["clock_trace"].(map[string]any); ok {
			for _, slot := range collectSections(trace["queued_slots"]) {
				items := make([]map[string]any, 0, 6)
				note := ""
				if playlist, ok := slot["playlist"].(map[string]any); ok {
					items = append(items, collectItems(playlist["tracks"])...)
				}
				if media, ok := slot["media"].(map[string]any); ok {
					items = append(items, media)
				}
				if previewMap, ok := slot["smart_block_preview"].(map[string]any); ok {
					items = append(items, collectItems(previewMap["tracks"])...)
					if len(items) == 0 {
						note, _ = previewMap["message"].(string)
						if note == "" {
							note, _ = previewMap["error"].(string)
						}
						addSection(fmt.Sprintf("Slot %v", slot["position"]), "slot", items, note)
						continue
					}
					if bumperCount, ok := previewMap["bumper_count"].(float64); ok && bumperCount > 0 {
						note = fmt.Sprintf("Includes %d bumper track(s) in the resolved smart block plan.", int(bumperCount))
						if bumperLimit, ok := previewMap["bumper_limit"].(float64); ok && bumperLimit > 0 {
							note = fmt.Sprintf("%s Max allowed: %d.", note, int(bumperLimit))
						}
					}
				}
				if webstream, ok := slot["webstream"].(map[string]any); ok {
					items = append(items, webstream)
				}
				title := fmt.Sprintf("Slot %v", slot["position"])
				if sourceMap, ok := slot["source_map"].(string); ok {
					if note == "" {
						note = sourceMap
					} else {
						note = note + " " + sourceMap
					}
				}
				addSection(title, "slot", items, note)
			}
		}
	case "playlist":
		if playlist, ok := response["playlist"].(map[string]any); ok {
			items := collectItems(playlist["tracks"])
			addSection("Playlist Plan", "tracks", items, "Tracks from the selected playlist for this block.")
		}
	case "smart_block":
		if previewMap, ok := response["smart_block_preview"].(map[string]any); ok {
			items := collectItems(previewMap["tracks"])
			note := ""
			if len(items) == 0 {
				note, _ = previewMap["message"].(string)
				if note == "" {
					note, _ = previewMap["error"].(string)
				}
			}
			if bumperCount, ok := previewMap["bumper_count"].(float64); ok && bumperCount > 0 {
				note = fmt.Sprintf("Includes %d bumper track(s) in the resolved plan.", int(bumperCount))
				if bumperLimit, ok := previewMap["bumper_limit"].(float64); ok && bumperLimit > 0 {
					note = fmt.Sprintf("%s Max allowed: %d.", note, int(bumperLimit))
				}
			}
			addSection("Smart Block Plan", "tracks", items, note)
		}
	case "media":
		if media, ok := response["media"].(map[string]any); ok {
			addSection("Fixed Track", "track", []map[string]any{media}, "One fixed track for this block.")
		}
	case "webstream":
		if webstream, ok := response["webstream"].(map[string]any); ok {
			addSection("Relay Source", "relay", []map[string]any{webstream}, "External stream relay for this window.")
		}
	case "live":
		title := ""
		if entry.Metadata != nil {
			if sessionName, ok := entry.Metadata["session_name"].(string); ok {
				title = sessionName
			}
		}
		addSection("Live Window", "live", []map[string]any{{
			"title":    title,
			"duration": int64(entry.EndsAt.Sub(entry.StartsAt).Seconds()),
		}}, "Human/live source must take over for this window.")
	}

	if len(sections) == 0 {
		preview["empty_reason"] = "No playback plan is available for this block yet."
	} else {
		preview["sections"] = sections
	}
	return preview
}

// ScheduleRefresh triggers a schedule refresh
func (h *Handler) ScheduleRefresh(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	// Call scheduler service to refresh
	if h.scheduler != nil {
		if err := h.scheduler.RefreshStation(r.Context(), station.ID); err != nil {
			h.logger.Error().Err(err).Str("station_id", station.ID).Msg("failed to refresh schedule")
			if r.Header.Get("HX-Request") == "true" {
				w.Write([]byte(`<div class="alert alert-danger">Failed to refresh schedule</div>`))
				return
			}
			http.Error(w, "Failed to refresh schedule", http.StatusInternalServerError)
			return
		}
		h.logger.Info().Str("station_id", station.ID).Msg("schedule refresh triggered")
	} else {
		h.logger.Warn().Msg("scheduler service not available")
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(`<div class="alert alert-success">Schedule refresh queued</div>`))
		return
	}

	w.WriteHeader(http.StatusOK)
}

// ScheduleSourceTracks returns the track list for a given source type and ID.
// This works for any source regardless of how it was scheduled (direct, recurring, etc).
func (h *Handler) ScheduleSourceTracks(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	sourceType := r.URL.Query().Get("source_type")
	sourceID := r.URL.Query().Get("source_id")
	startsAtStr := r.URL.Query().Get("starts_at")
	endsAtStr := r.URL.Query().Get("ends_at")
	mountID := r.URL.Query().Get("mount_id")
	entryID := r.URL.Query().Get("entry_id")

	if sourceType == "" || sourceID == "" {
		http.Error(w, "source_type and source_id required", http.StatusBadRequest)
		return
	}

	startsAt, _ := time.Parse(time.RFC3339, startsAtStr)
	endsAt, _ := time.Parse(time.RFC3339, endsAtStr)
	duration := endsAt.Sub(startsAt)
	if duration <= 0 {
		duration = time.Hour
	}

	// Look up existing track overrides from the entry metadata
	var trackOverrides map[string]string
	if entryID != "" {
		var entry models.ScheduleEntry
		if h.db.Select("id, metadata").First(&entry, "id = ?", entryID).Error == nil && entry.Metadata != nil {
			if overridesRaw, ok := entry.Metadata["track_overrides"]; ok {
				if ovMap, ok := overridesRaw.(map[string]any); ok {
					trackOverrides = make(map[string]string, len(ovMap))
					for k, v := range ovMap {
						if s, ok := v.(string); ok {
							trackOverrides[k] = s
						}
					}
				}
			}
		}
	}

	type trackInfo struct {
		MediaID  string `json:"media_id"`
		Title    string `json:"title"`
		Artist   string `json:"artist"`
		Duration int64  `json:"duration"`
		IsBumper bool   `json:"is_bumper,omitempty"`
	}

	response := map[string]any{
		"source_type": sourceType,
		"source_id":   sourceID,
	}

	var tracks []trackInfo

	switch sourceType {
	case "playlist":
		var playlist models.Playlist
		if err := h.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
			return db.Order("position ASC")
		}).Preload("Items.Media").First(&playlist, "id = ?", sourceID).Error; err == nil {
			response["source_name"] = playlist.Name
			for _, item := range playlist.Items {
				tracks = append(tracks, trackInfo{
					MediaID:  item.Media.ID,
					Title:    item.Media.Title,
					Artist:   item.Media.Artist,
					Duration: int64(item.Media.Duration.Seconds()),
				})
			}
		}

	case "smart_block":
		var sb models.SmartBlock
		if err := h.db.First(&sb, "id = ?", sourceID).Error; err == nil {
			response["source_name"] = sb.Name
			engine := smartblock.New(h.db, h.logger)
			result, err := engine.Generate(r.Context(), smartblock.GenerateRequest{
				SmartBlockID: sb.ID,
				Seed:         startsAt.Unix(),
				Duration:     duration.Milliseconds(),
				StationID:    station.ID,
				MountID:      mountID,
			})
			if err == nil {
				mediaIDs := make([]string, 0, len(result.Items))
				for _, item := range result.Items {
					if item.MediaID != "" {
						mediaIDs = append(mediaIDs, item.MediaID)
					}
				}
				mediaByID := make(map[string]models.MediaItem, len(mediaIDs))
				if len(mediaIDs) > 0 {
					var mediaItems []models.MediaItem
					h.db.Select("id, title, artist, duration").Where("id IN ?", mediaIDs).Find(&mediaItems)
					for _, m := range mediaItems {
						mediaByID[m.ID] = m
					}
				}
				for _, item := range result.Items {
					media := mediaByID[item.MediaID]
					tracks = append(tracks, trackInfo{
						MediaID:  item.MediaID,
						Title:    media.Title,
						Artist:   media.Artist,
						Duration: int64(media.Duration.Seconds()),
						IsBumper: item.IsBumper,
					})
				}
				response["bumper_count"] = result.BumperCount
				response["bumper_limit"] = result.BumperLimit
				response["bumper_limit_reached"] = result.BumperLimitReached
				response["warnings"] = result.Warnings
			} else {
				response["error"] = err.Error()
			}
		}

	case "clock_template":
		var clockHour models.ClockHour
		if err := h.db.Preload("Slots").First(&clockHour, "id = ?", sourceID).Error; err == nil {
			response["source_name"] = clockHour.Name
			slotsSorted := append([]models.ClockSlot(nil), clockHour.Slots...)
			sort.Slice(slotsSorted, func(i, j int) bool {
				return slotsSorted[i].Position < slotsSorted[j].Position
			})
			for _, slot := range slotsSorted {
				switch slot.Type {
				case models.SlotTypePlaylist:
					if playlistID, ok := slot.Payload["playlist_id"].(string); ok {
						var pl models.Playlist
						if h.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
							return db.Order("position ASC")
						}).Preload("Items.Media").First(&pl, "id = ?", playlistID).Error == nil {
							for _, item := range pl.Items {
								tracks = append(tracks, trackInfo{
									MediaID:  item.Media.ID,
									Title:    item.Media.Title,
									Artist:   item.Media.Artist,
									Duration: int64(item.Media.Duration.Seconds()),
								})
							}
						}
					}
				case models.SlotTypeSmartBlock:
					if sbID, ok := slot.Payload["smart_block_id"].(string); ok {
						var sb models.SmartBlock
						if h.db.First(&sb, "id = ?", sbID).Error == nil {
							slotStart := startsAt.Add(slot.Offset)
							slotEnd := endsAt
							// Find the end of this slot (next slot start or entry end)
							for _, ns := range slotsSorted {
								if ns.Position > slot.Position {
									nextStart := startsAt.Add(ns.Offset)
									if nextStart.Before(slotEnd) {
										slotEnd = nextStart
									}
									break
								}
							}
							slotDuration := slotEnd.Sub(slotStart)
							if slotDuration <= 0 {
								slotDuration = duration
							}
							engine := smartblock.New(h.db, h.logger)
							result, err := engine.Generate(r.Context(), smartblock.GenerateRequest{
								SmartBlockID: sb.ID,
								Seed:         slotStart.Unix(),
								Duration:     slotDuration.Milliseconds(),
								StationID:    station.ID,
								MountID:      mountID,
							})
							if err == nil {
								mediaIDs := make([]string, 0, len(result.Items))
								for _, item := range result.Items {
									if item.MediaID != "" {
										mediaIDs = append(mediaIDs, item.MediaID)
									}
								}
								mediaByID := make(map[string]models.MediaItem, len(mediaIDs))
								if len(mediaIDs) > 0 {
									var mediaItems []models.MediaItem
									h.db.Select("id, title, artist, duration").Where("id IN ?", mediaIDs).Find(&mediaItems)
									for _, m := range mediaItems {
										mediaByID[m.ID] = m
									}
								}
								for _, item := range result.Items {
									media := mediaByID[item.MediaID]
									tracks = append(tracks, trackInfo{
										MediaID:  item.MediaID,
										Title:    media.Title,
										Artist:   media.Artist,
										Duration: int64(media.Duration.Seconds()),
										IsBumper: item.IsBumper,
									})
								}
							}
						}
					}
				case models.SlotTypeHardItem:
					if mediaID, ok := slot.Payload["media_id"].(string); ok {
						var media models.MediaItem
						if h.db.Select("id, title, artist, duration").First(&media, "id = ?", mediaID).Error == nil {
							tracks = append(tracks, trackInfo{
								MediaID:  media.ID,
								Title:    media.Title,
								Artist:   media.Artist,
								Duration: int64(media.Duration.Seconds()),
							})
						}
					}
				}
			}
		}

	case "media":
		var media models.MediaItem
		if err := h.db.Select("id, title, artist, duration").First(&media, "id = ?", sourceID).Error; err == nil {
			tracks = append(tracks, trackInfo{
				MediaID:  media.ID,
				Title:    media.Title,
				Artist:   media.Artist,
				Duration: int64(media.Duration.Seconds()),
			})
		}

	case "webstream":
		var ws models.Webstream
		if err := h.db.First(&ws, "id = ?", sourceID).Error; err == nil {
			response["source_name"] = ws.Name
			if len(ws.URLs) > 0 {
				response["url"] = ws.URLs[0]
			}
		}
	}

	// Apply track overrides from entry metadata
	if len(trackOverrides) > 0 && len(tracks) > 0 {
		// Collect replacement media IDs
		var replacementIDs []string
		for _, mediaID := range trackOverrides {
			if mediaID != "" && mediaID != "__remove__" {
				replacementIDs = append(replacementIDs, mediaID)
			}
		}
		replacementMedia := make(map[string]models.MediaItem, len(replacementIDs))
		if len(replacementIDs) > 0 {
			var items []models.MediaItem
			h.db.Select("id, title, artist, duration").Where("id IN ?", replacementIDs).Find(&items)
			for _, m := range items {
				replacementMedia[m.ID] = m
			}
		}

		var finalTracks []trackInfo
		for i, t := range tracks {
			idxStr := fmt.Sprintf("%d", i)
			if replacement, ok := trackOverrides[idxStr]; ok {
				if replacement == "__remove__" {
					continue // Skip removed tracks
				}
				if media, ok := replacementMedia[replacement]; ok {
					finalTracks = append(finalTracks, trackInfo{
						MediaID:  media.ID,
						Title:    media.Title,
						Artist:   media.Artist,
						Duration: int64(media.Duration.Seconds()),
					})
					continue
				}
			}
			finalTracks = append(finalTracks, t)
		}
		tracks = finalTracks
	}

	response["tracks"] = tracks
	response["track_count"] = len(tracks)

	var totalDuration int64
	bumperCount := 0
	for _, t := range tracks {
		totalDuration += t.Duration
		if t.IsBumper {
			bumperCount++
		}
	}
	response["total_duration"] = totalDuration
	if bumperCount > 0 {
		response["bumper_count"] = bumperCount
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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
