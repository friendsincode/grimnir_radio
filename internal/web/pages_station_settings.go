/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// StationSettings renders the station settings page
func (h *Handler) StationSettings(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	// Check if user can manage station settings
	if !h.canManageStationSettings(user, station) {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	// Get station mounts
	var mounts []models.Mount
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&mounts)

	h.Render(w, r, "pages/dashboard/station/settings", PageData{
		Title:    "Station Settings - " + station.Name,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Station": station,
			"Mounts":  mounts,
		},
	})
}

// StationSettingsUpdate handles station settings updates
func (h *Handler) StationSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	if !h.canManageStationSettings(user, station) {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	station.Name = r.FormValue("name")
	station.Description = r.FormValue("description")
	station.Timezone = r.FormValue("timezone")

	if station.Name == "" {
		if r.Header.Get("HX-Request") == "true" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`<div class="alert alert-danger">Name is required</div>`))
			return
		}
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	if err := h.db.Save(station).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to update station settings")
		http.Error(w, "Failed to update settings", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("station_id", station.ID).
		Str("updated_by", user.ID).
		Msg("station settings updated")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Trigger", "settingsUpdated")
		w.Write([]byte(`<div class="alert alert-success">Settings saved successfully</div>`))
		return
	}
	http.Redirect(w, r, "/dashboard/station/settings", http.StatusSeeOther)
}

// canManageStationSettings checks if user can manage station settings
func (h *Handler) canManageStationSettings(user *models.User, station *models.Station) bool {
	if user == nil || station == nil {
		return false
	}

	// Platform admins can manage all stations
	if user.IsPlatformAdmin() {
		return true
	}

	// Check station role
	stationUser := h.GetStationRole(user, station.ID)
	if stationUser == nil {
		return false
	}

	// Owner and admin can manage settings
	return stationUser.Role == models.StationRoleOwner || stationUser.Role == models.StationRoleAdmin
}
