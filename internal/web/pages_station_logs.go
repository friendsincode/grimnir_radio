/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"
)

// StationLogs renders the station logs viewer page
func (h *Handler) StationLogs(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Any station member can view logs
	stationUser := h.GetStationRole(user, station.ID)
	if stationUser == nil && !user.IsPlatformAdmin() {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	h.Render(w, r, "pages/dashboard/station/logs", PageData{
		Title:    "Station Logs - " + station.Name,
		Stations: h.LoadStations(r),
	})
}
