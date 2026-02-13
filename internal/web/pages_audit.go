/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// AdminAudit renders the platform-wide audit log page (platform admin only).
func (h *Handler) AdminAudit(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	h.Render(w, r, "pages/dashboard/admin/audit", PageData{
		Title:    "Platform Audit Logs",
		Stations: h.LoadStations(r),
	})
}

// StationAudit renders the station-specific audit log page.
func (h *Handler) StationAudit(w http.ResponseWriter, r *http.Request) {
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

	// Only admin/manager can view audit logs
	stationUser := h.GetStationRole(user, station.ID)
	if !user.IsPlatformAdmin() {
		if stationUser == nil {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if stationUser.Role != models.StationRoleOwner &&
			stationUser.Role != models.StationRoleAdmin &&
			stationUser.Role != models.StationRoleManager {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	h.Render(w, r, "pages/dashboard/station/audit", PageData{
		Title:    "Station Audit Logs - " + station.Name,
		Stations: h.LoadStations(r),
	})
}
