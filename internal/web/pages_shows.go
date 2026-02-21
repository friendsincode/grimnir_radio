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
	"github.com/teambition/rrule-go"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ShowsJSON returns shows as JSON for calendar and dropdowns
func (h *Handler) ShowsJSON(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	activeOnly := r.URL.Query().Get("active") == "true"

	query := h.db.Where("station_id = ?", station.ID).Preload("Host")
	if activeOnly {
		query = query.Where("active = ?", true)
	}

	var shows []models.Show
	query.Order("name ASC").Find(&shows)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"shows": shows})
}

// ShowInstanceEvents returns show instances as calendar events
func (h *Handler) ShowInstanceEvents(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	// Parse date range from FullCalendar
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")
	showID := r.URL.Query().Get("show_id")

	startTime, _ := time.Parse(time.RFC3339, start)
	endTime, _ := time.Parse(time.RFC3339, end)

	if startTime.IsZero() {
		startTime = time.Now().Add(-24 * time.Hour)
	}
	if endTime.IsZero() {
		endTime = time.Now().Add(7 * 24 * time.Hour)
	}

	// Limit materialization window
	if endTime.Sub(startTime) > 90*24*time.Hour {
		endTime = startTime.Add(90 * 24 * time.Hour)
	}

	// Fetch existing instances
	query := h.db.Where("station_id = ? AND starts_at >= ? AND starts_at <= ?",
		station.ID, startTime, endTime).
		Preload("Show").
		Preload("Host")

	if showID != "" {
		query = query.Where("show_id = ?", showID)
	}

	var instances []models.ShowInstance
	query.Order("starts_at ASC").Find(&instances)

	// Build map of existing instance times to avoid duplicates
	existingTimes := make(map[string]bool)
	for _, inst := range instances {
		key := inst.ShowID + "_" + inst.StartsAt.Format(time.RFC3339)
		existingTimes[key] = true
	}

	// Materialize instances from active shows that don't have instances yet
	showQuery := h.db.Where("station_id = ? AND active = ?", station.ID, true)
	if showID != "" {
		showQuery = showQuery.Where("id = ?", showID)
	}

	var shows []models.Show
	showQuery.Preload("Host").Find(&shows)

	for _, show := range shows {
		if show.RRule == "" {
			// Non-recurring show: check if DTStart is in range
			if show.DTStart.After(startTime) && show.DTStart.Before(endTime) {
				key := show.ID + "_" + show.DTStart.Format(time.RFC3339)
				if !existingTimes[key] {
					// Create virtual instance
					instances = append(instances, models.ShowInstance{
						ID:         "virtual_" + show.ID + "_" + show.DTStart.Format("20060102T150405"),
						ShowID:     show.ID,
						StationID:  show.StationID,
						StartsAt:   show.DTStart,
						EndsAt:     show.DTStart.Add(time.Duration(show.DefaultDurationMinutes) * time.Minute),
						HostUserID: show.HostUserID,
						Status:     models.ShowInstanceScheduled,
						Show:       &show,
						Host:       show.Host,
					})
				}
			}
			continue
		}

		// Parse RRULE and generate occurrences
		rr, err := rrule.StrToRRule(show.RRule)
		if err != nil {
			continue
		}
		rr.DTStart(show.DTStart)

		occurrences := rr.Between(startTime, endTime, true)
		duration := time.Duration(show.DefaultDurationMinutes) * time.Minute

		for _, occ := range occurrences {
			// Skip if past show end date
			if show.DTEnd != nil && occ.After(*show.DTEnd) {
				continue
			}

			key := show.ID + "_" + occ.Format(time.RFC3339)
			if existingTimes[key] {
				continue
			}

			// Create virtual instance
			instances = append(instances, models.ShowInstance{
				ID:         "virtual_" + show.ID + "_" + occ.Format("20060102T150405"),
				ShowID:     show.ID,
				StationID:  show.StationID,
				StartsAt:   occ,
				EndsAt:     occ.Add(duration),
				HostUserID: show.HostUserID,
				Status:     models.ShowInstanceScheduled,
				Show:       &show,
				Host:       show.Host,
			})
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
		Editable        bool   `json:"editable"`
		ExtendedProps   any    `json:"extendedProps,omitempty"`
	}

	events := make([]calendarEvent, 0, len(instances))
	for _, inst := range instances {
		title := "Show"
		color := "#6366f1" // Default indigo

		if inst.Show != nil {
			title = inst.Show.Name
			if inst.Show.Color != "" {
				color = inst.Show.Color
			}
		}

		// Add host name if available
		hostName := ""
		if inst.Host != nil {
			hostName = inst.Host.Email
		}

		// Mark cancelled/exception instances
		className := "event-show"
		if inst.IsCancelled() {
			className += " event-cancelled"
			title = "[Cancelled] " + title
		} else if inst.IsException() {
			className += " event-exception"
		}

		// Check if this is a virtual (not yet persisted) instance
		isVirtual := len(inst.ID) > 8 && inst.ID[:8] == "virtual_"

		event := calendarEvent{
			ID:              inst.ID,
			Title:           title,
			Start:           inst.StartsAt.Format(time.RFC3339),
			End:             inst.EndsAt.Format(time.RFC3339),
			BackgroundColor: color,
			BorderColor:     color,
			ClassName:       className,
			Editable:        !inst.IsCancelled(),
			ExtendedProps: map[string]any{
				"type":           "show",
				"show_id":        inst.ShowID,
				"instance_id":    inst.ID,
				"host_name":      hostName,
				"host_user_id":   inst.HostUserID,
				"status":         inst.Status,
				"exception_type": inst.ExceptionType,
				"exception_note": inst.ExceptionNote,
				"is_virtual":     isVirtual,
				"metadata":       inst.Metadata,
			},
		}

		events = append(events, event)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// ShowCreate creates a new show
func (h *Handler) ShowCreate(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	var input struct {
		Name                   string         `json:"name"`
		Description            string         `json:"description"`
		HostUserID             *string        `json:"host_user_id"`
		DefaultDurationMinutes int            `json:"default_duration_minutes"`
		Color                  string         `json:"color"`
		RRule                  string         `json:"rrule"`
		DTStart                string         `json:"dtstart"`
		DTEnd                  *string        `json:"dtend"`
		Timezone               string         `json:"timezone"`
		Metadata               map[string]any `json:"metadata"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if input.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	dtstart, err := time.Parse(time.RFC3339, input.DTStart)
	if err != nil {
		http.Error(w, "Invalid dtstart", http.StatusBadRequest)
		return
	}

	// Validate RRULE if provided
	if input.RRule != "" {
		if _, err := rrule.StrToRRule(input.RRule); err != nil {
			http.Error(w, "Invalid rrule: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	var dtend *time.Time
	if input.DTEnd != nil && *input.DTEnd != "" {
		parsed, err := time.Parse(time.RFC3339, *input.DTEnd)
		if err != nil {
			http.Error(w, "Invalid dtend", http.StatusBadRequest)
			return
		}
		dtend = &parsed
	}

	if input.DefaultDurationMinutes <= 0 {
		input.DefaultDurationMinutes = 60
	}
	if input.Timezone == "" {
		input.Timezone = "UTC"
	}
	if input.Color == "" {
		input.Color = "#6366f1"
	}

	show := models.Show{
		ID:                     uuid.NewString(),
		StationID:              station.ID,
		Name:                   input.Name,
		Description:            input.Description,
		HostUserID:             input.HostUserID,
		DefaultDurationMinutes: input.DefaultDurationMinutes,
		Color:                  input.Color,
		RRule:                  input.RRule,
		DTStart:                dtstart,
		DTEnd:                  dtend,
		Timezone:               input.Timezone,
		Active:                 true,
		Metadata:               input.Metadata,
	}

	if err := h.db.Create(&show).Error; err != nil {
		http.Error(w, "Failed to create show", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(show)
}

// ShowUpdate updates a show
func (h *Handler) ShowUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var show models.Show
	if err := h.db.First(&show, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	var input struct {
		Name                   *string        `json:"name"`
		Description            *string        `json:"description"`
		HostUserID             *string        `json:"host_user_id"`
		DefaultDurationMinutes *int           `json:"default_duration_minutes"`
		Color                  *string        `json:"color"`
		RRule                  *string        `json:"rrule"`
		DTStart                *string        `json:"dtstart"`
		DTEnd                  *string        `json:"dtend"`
		Timezone               *string        `json:"timezone"`
		Active                 *bool          `json:"active"`
		Metadata               map[string]any `json:"metadata"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	updates := make(map[string]any)

	if input.Name != nil {
		updates["name"] = *input.Name
	}
	if input.Description != nil {
		updates["description"] = *input.Description
	}
	if input.HostUserID != nil {
		updates["host_user_id"] = *input.HostUserID
	}
	if input.DefaultDurationMinutes != nil {
		updates["default_duration_minutes"] = *input.DefaultDurationMinutes
	}
	if input.Color != nil {
		updates["color"] = *input.Color
	}
	if input.RRule != nil {
		if *input.RRule != "" {
			if _, err := rrule.StrToRRule(*input.RRule); err != nil {
				http.Error(w, "Invalid rrule: "+err.Error(), http.StatusBadRequest)
				return
			}
		}
		updates["rrule"] = *input.RRule
	}
	if input.DTStart != nil {
		dtstart, err := time.Parse(time.RFC3339, *input.DTStart)
		if err != nil {
			http.Error(w, "Invalid dtstart", http.StatusBadRequest)
			return
		}
		updates["dtstart"] = dtstart
	}
	if input.DTEnd != nil {
		if *input.DTEnd == "" {
			updates["dtend"] = nil
		} else {
			dtend, err := time.Parse(time.RFC3339, *input.DTEnd)
			if err != nil {
				http.Error(w, "Invalid dtend", http.StatusBadRequest)
				return
			}
			updates["dtend"] = dtend
		}
	}
	if input.Timezone != nil {
		updates["timezone"] = *input.Timezone
	}
	if input.Active != nil {
		updates["active"] = *input.Active
	}
	if input.Metadata != nil {
		updates["metadata"] = input.Metadata
	}

	if len(updates) > 0 {
		if err := h.db.Model(&show).Updates(updates).Error; err != nil {
			http.Error(w, "Failed to update show", http.StatusInternalServerError)
			return
		}
	}

	h.db.Preload("Host").First(&show, "id = ?", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(show)
}

// ShowDelete deletes a show and future instances
func (h *Handler) ShowDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var show models.Show
	if err := h.db.First(&show, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	tx := h.db.Begin()

	// Delete future instances
	if err := tx.Where("show_id = ? AND starts_at > ?", id, time.Now()).Delete(&models.ShowInstance{}).Error; err != nil {
		tx.Rollback()
		http.Error(w, "Failed to delete instances", http.StatusInternalServerError)
		return
	}

	// Delete show
	if err := tx.Delete(&show).Error; err != nil {
		tx.Rollback()
		http.Error(w, "Failed to delete show", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		http.Error(w, "Failed to delete show", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ShowInstanceUpdate updates a single show instance
func (h *Handler) ShowInstanceUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	var input struct {
		StartsAt      *string                    `json:"starts_at"`
		EndsAt        *string                    `json:"ends_at"`
		HostUserID    *string                    `json:"host_user_id"`
		ExceptionType *models.ShowExceptionType  `json:"exception_type"`
		ExceptionNote *string                    `json:"exception_note"`
		Status        *models.ShowInstanceStatus `json:"status"`
		Metadata      map[string]any             `json:"metadata"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Check if this is a virtual instance that needs to be created
	isVirtual := len(id) > 8 && id[:8] == "virtual_"

	var instance models.ShowInstance
	if isVirtual {
		// Parse virtual instance ID: virtual_<showID>_<timestamp>
		// Need to create a real instance from the virtual one
		parts := id[8:] // Remove "virtual_" prefix
		showID := ""
		timestamp := ""

		// Find last underscore for timestamp separator
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] == '_' {
				showID = parts[:i]
				timestamp = parts[i+1:]
				break
			}
		}

		if showID == "" {
			http.Error(w, "Invalid virtual instance ID", http.StatusBadRequest)
			return
		}

		// Get the show
		var show models.Show
		if err := h.db.First(&show, "id = ?", showID).Error; err != nil {
			http.NotFound(w, r)
			return
		}

		// Parse timestamp (format: 20060102T150405)
		startsAt, err := time.Parse("20060102T150405", timestamp)
		if err != nil {
			http.Error(w, "Invalid virtual instance timestamp", http.StatusBadRequest)
			return
		}

		// Create the instance
		instance = models.ShowInstance{
			ID:         uuid.NewString(),
			ShowID:     showID,
			StationID:  station.ID,
			StartsAt:   startsAt,
			EndsAt:     startsAt.Add(time.Duration(show.DefaultDurationMinutes) * time.Minute),
			HostUserID: show.HostUserID,
			Status:     models.ShowInstanceScheduled,
		}
	} else {
		if err := h.db.First(&instance, "id = ?", id).Error; err != nil {
			http.NotFound(w, r)
			return
		}
	}

	updates := make(map[string]any)

	if input.StartsAt != nil {
		startsAt, err := time.Parse(time.RFC3339, *input.StartsAt)
		if err != nil {
			http.Error(w, "Invalid starts_at", http.StatusBadRequest)
			return
		}
		updates["starts_at"] = startsAt
		instance.StartsAt = startsAt

		// Mark as rescheduled if time changed
		if input.ExceptionType == nil {
			exType := models.ShowExceptionRescheduled
			updates["exception_type"] = exType
		}
	}
	if input.EndsAt != nil {
		endsAt, err := time.Parse(time.RFC3339, *input.EndsAt)
		if err != nil {
			http.Error(w, "Invalid ends_at", http.StatusBadRequest)
			return
		}
		updates["ends_at"] = endsAt
		instance.EndsAt = endsAt
	}
	if input.HostUserID != nil {
		updates["host_user_id"] = *input.HostUserID
		if input.ExceptionType == nil && *input.HostUserID != "" {
			exType := models.ShowExceptionSubstitute
			updates["exception_type"] = exType
		}
	}
	if input.ExceptionType != nil {
		updates["exception_type"] = *input.ExceptionType
	}
	if input.ExceptionNote != nil {
		updates["exception_note"] = *input.ExceptionNote
	}
	if input.Status != nil {
		updates["status"] = *input.Status
	}
	if input.Metadata != nil {
		updates["metadata"] = input.Metadata
	}

	if isVirtual {
		// Apply updates to the new instance
		for k, v := range updates {
			switch k {
			case "starts_at":
				instance.StartsAt = v.(time.Time)
			case "ends_at":
				instance.EndsAt = v.(time.Time)
			case "host_user_id":
				s := v.(string)
				instance.HostUserID = &s
			case "exception_type":
				instance.ExceptionType = v.(models.ShowExceptionType)
			case "exception_note":
				instance.ExceptionNote = v.(string)
			case "status":
				instance.Status = v.(models.ShowInstanceStatus)
			case "metadata":
				instance.Metadata = v.(map[string]any)
			}
		}

		if err := h.db.Create(&instance).Error; err != nil {
			http.Error(w, "Failed to create instance", http.StatusInternalServerError)
			return
		}
	} else {
		if err := h.db.Model(&instance).Updates(updates).Error; err != nil {
			http.Error(w, "Failed to update instance", http.StatusInternalServerError)
			return
		}
	}

	h.db.Preload("Show").Preload("Host").First(&instance, "id = ?", instance.ID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(instance)
}

// ShowInstanceCancel cancels a show instance
func (h *Handler) ShowInstanceCancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Handle virtual instances
	isVirtual := len(id) > 8 && id[:8] == "virtual_"
	if isVirtual {
		// For virtual instances, we need to create a cancelled instance
		station := h.GetStation(r)
		if station == nil {
			http.Error(w, "No station selected", http.StatusBadRequest)
			return
		}

		parts := id[8:]
		showID := ""
		timestamp := ""
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] == '_' {
				showID = parts[:i]
				timestamp = parts[i+1:]
				break
			}
		}

		var show models.Show
		if err := h.db.First(&show, "id = ?", showID).Error; err != nil {
			http.NotFound(w, r)
			return
		}

		startsAt, _ := time.Parse("20060102T150405", timestamp)

		instance := models.ShowInstance{
			ID:            uuid.NewString(),
			ShowID:        showID,
			StationID:     station.ID,
			StartsAt:      startsAt,
			EndsAt:        startsAt.Add(time.Duration(show.DefaultDurationMinutes) * time.Minute),
			HostUserID:    show.HostUserID,
			Status:        models.ShowInstanceCancelled,
			ExceptionType: models.ShowExceptionCancelled,
		}

		if err := h.db.Create(&instance).Error; err != nil {
			http.Error(w, "Failed to cancel instance", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
		return
	}

	var instance models.ShowInstance
	if err := h.db.First(&instance, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	updates := map[string]any{
		"status":         models.ShowInstanceCancelled,
		"exception_type": models.ShowExceptionCancelled,
	}

	if err := h.db.Model(&instance).Updates(updates).Error; err != nil {
		http.Error(w, "Failed to cancel instance", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

// ShowMaterialize generates instances for a show in a date range
func (h *Handler) ShowMaterialize(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var show models.Show
	if err := h.db.First(&show, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	var input struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	start, err := time.Parse(time.RFC3339, input.Start)
	if err != nil {
		http.Error(w, "Invalid start", http.StatusBadRequest)
		return
	}
	end, err := time.Parse(time.RFC3339, input.End)
	if err != nil {
		http.Error(w, "Invalid end", http.StatusBadRequest)
		return
	}

	// Limit to 1 year
	if end.Sub(start) > 365*24*time.Hour {
		end = start.Add(365 * 24 * time.Hour)
	}

	var occurrences []time.Time
	duration := time.Duration(show.DefaultDurationMinutes) * time.Minute

	if show.RRule == "" {
		if show.DTStart.After(start) && show.DTStart.Before(end) {
			occurrences = []time.Time{show.DTStart}
		}
	} else {
		rr, err := rrule.StrToRRule(show.RRule)
		if err != nil {
			http.Error(w, "Invalid rrule", http.StatusInternalServerError)
			return
		}
		rr.DTStart(show.DTStart)
		occurrences = rr.Between(start, end, true)
	}

	// Filter by show end date
	if show.DTEnd != nil {
		filtered := make([]time.Time, 0)
		for _, occ := range occurrences {
			if occ.Before(*show.DTEnd) || occ.Equal(*show.DTEnd) {
				filtered = append(filtered, occ)
			}
		}
		occurrences = filtered
	}

	var created []models.ShowInstance
	for _, startsAt := range occurrences {
		// Check if instance exists
		var existing models.ShowInstance
		err := h.db.Where("show_id = ? AND starts_at = ?", show.ID, startsAt).First(&existing).Error
		if err == nil {
			created = append(created, existing)
			continue
		}

		instance := models.ShowInstance{
			ID:         uuid.NewString(),
			ShowID:     show.ID,
			StationID:  show.StationID,
			StartsAt:   startsAt,
			EndsAt:     startsAt.Add(duration),
			HostUserID: show.HostUserID,
			Status:     models.ShowInstanceScheduled,
		}

		if err := h.db.Create(&instance).Error; err != nil {
			continue
		}
		created = append(created, instance)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"instances": created,
		"count":     len(created),
	})
}
