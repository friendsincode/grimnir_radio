/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import "net/http"

// AdminIntegrity renders the platform integrity findings and repair page.
func (h *Handler) AdminIntegrity(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	h.Render(w, r, "pages/dashboard/admin/integrity", PageData{
		Title:    "Platform Integrity",
		Stations: h.LoadStations(r),
	})
}
