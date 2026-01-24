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

	h.Render(w, r, "pages/dashboard/schedule/calendar", PageData{
		Title:    "Schedule",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Mounts": mounts,
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

	query := h.db.Where("station_id = ? AND starts_at >= ? AND starts_at <= ?",
		station.ID, startTime, endTime)

	if mountID != "" {
		query = query.Where("mount_id = ?", mountID)
	}

	var entries []models.ScheduleEntry
	query.Order("starts_at ASC").Find(&entries)

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
		title := ""
		if entry.Metadata != nil {
			if t, ok := entry.Metadata["title"].(string); ok {
				title = t
			}
			if a, ok := entry.Metadata["artist"].(string); ok && a != "" {
				title = a + " - " + title
			}
		}
		if title == "" {
			title = string(entry.SourceType)
		}

		event := calendarEvent{
			ID:        entry.ID,
			Title:     title,
			Start:     entry.StartsAt.Format(time.RFC3339),
			End:       entry.EndsAt.Format(time.RFC3339),
			ClassName: "event-" + entry.SourceType,
			Extendedprops: map[string]any{
				"source_type": entry.SourceType,
				"source_id":   entry.SourceID,
				"mount_id":    entry.MountID,
				"metadata":    entry.Metadata,
			},
		}

		events = append(events, event)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// ScheduleCreateEntry creates a new schedule entry
func (h *Handler) ScheduleCreateEntry(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	var input struct {
		MountID    string         `json:"mount_id"`
		StartsAt   time.Time      `json:"starts_at"`
		EndsAt     time.Time      `json:"ends_at"`
		SourceType string         `json:"source_type"`
		SourceID   string         `json:"source_id"`
		Metadata   map[string]any `json:"metadata"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	entry := models.ScheduleEntry{
		ID:         uuid.New().String(),
		StationID:  station.ID,
		MountID:    input.MountID,
		StartsAt:   input.StartsAt,
		EndsAt:     input.EndsAt,
		SourceType: input.SourceType,
		SourceID:   input.SourceID,
		Metadata:   input.Metadata,
	}

	if err := h.db.Create(&entry).Error; err != nil {
		http.Error(w, "Failed to create entry", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

// ScheduleUpdateEntry updates a schedule entry (drag-drop)
func (h *Handler) ScheduleUpdateEntry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var entry models.ScheduleEntry
	if err := h.db.First(&entry, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	var input struct {
		StartsAt time.Time `json:"starts_at"`
		EndsAt   time.Time `json:"ends_at"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	entry.StartsAt = input.StartsAt
	entry.EndsAt = input.EndsAt

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
