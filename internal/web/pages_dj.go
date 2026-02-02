/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"encoding/json"
	"net/http"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// DJAvailability renders the DJ availability management page.
func (h *Handler) DJAvailability(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	user := h.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Get user's availability for this station
	var availability []models.DJAvailability
	h.db.Where("user_id = ? AND (station_id = ? OR station_id IS NULL)", user.ID, station.ID).
		Order("day_of_week ASC, start_time ASC").
		Find(&availability)

	h.Render(w, r, "pages/dashboard/dj/availability", PageData{
		Title:    "My Availability",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"StationID":    station.ID,
			"Availability": availability,
		},
	})
}

// DJRequests renders the schedule requests page.
func (h *Handler) DJRequests(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	user := h.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Check if user is manager+
	isManager := false
	var stationUser models.StationUser
	if h.db.Where("user_id = ? AND station_id = ?", user.ID, station.ID).First(&stationUser).Error == nil {
		isManager = stationUser.Role == models.StationRoleOwner ||
			stationUser.Role == models.StationRoleAdmin ||
			stationUser.Role == models.StationRoleManager
	}
	if user.IsPlatformAdmin() {
		isManager = true
	}

	// Get requests based on role
	var requests []models.ScheduleRequest
	query := h.db.Where("station_id = ?", station.ID).
		Preload("Requester").
		Preload("TargetInstance").
		Preload("TargetInstance.Show").
		Preload("SwapWithUser").
		Preload("Reviewer").
		Order("created_at DESC")

	if !isManager {
		query = query.Where("requester_id = ?", user.ID)
	}

	query.Find(&requests)

	// Get shows for new request form
	var shows []models.Show
	h.db.Where("station_id = ? AND active = ?", station.ID, true).
		Order("name ASC").
		Find(&shows)

	// Get DJs for swap requests
	var stationUsers []models.StationUser
	h.db.Where("station_id = ?", station.ID).
		Preload("User").
		Find(&stationUsers)

	h.Render(w, r, "pages/dashboard/dj/requests", PageData{
		Title:    "Schedule Requests",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"StationID":    station.ID,
			"Requests":     requests,
			"Shows":        shows,
			"StationUsers": stationUsers,
			"IsManager":    isManager,
		},
	})
}

// DJAvailabilityJSON returns availability as JSON.
func (h *Handler) DJAvailabilityJSON(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	user := h.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var availability []models.DJAvailability
	h.db.Where("user_id = ? AND (station_id = ? OR station_id IS NULL)", user.ID, station.ID).
		Order("day_of_week ASC, start_time ASC").
		Find(&availability)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"availability": availability})
}
