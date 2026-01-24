/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"
)

// SettingsPage renders the system settings page
func (h *Handler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	// TODO: Load actual system settings from config/database

	settings := map[string]any{
		"scheduler_lookahead":  "48h",
		"media_root":           "/var/lib/grimnir/media",
		"analysis_enabled":     true,
		"websocket_enabled":    true,
		"leader_election":      false,
		"metrics_enabled":      true,
		"log_level":            "info",
	}

	h.Render(w, r, "pages/dashboard/settings/index", PageData{
		Title:    "Settings",
		Stations: h.LoadStations(r),
		Data:     settings,
	})
}

// SettingsUpdate handles settings updates
func (h *Handler) SettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	// TODO: Actually save settings

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(`<div class="alert alert-success">Settings saved</div>`))
		return
	}

	http.Redirect(w, r, "/dashboard/settings", http.StatusSeeOther)
}

// MigrationsPage renders the migrations/import page
func (h *Handler) MigrationsPage(w http.ResponseWriter, r *http.Request) {
	h.Render(w, r, "pages/dashboard/settings/migrations", PageData{
		Title:    "Migrations",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"SupportedSources": []string{
				"LibreTime",
				"AzuraCast",
				"RadioBoss",
				"PlayoutONE",
				"CSV Import",
			},
		},
	})
}

// MigrationsImport handles import file upload
func (h *Handler) MigrationsImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(100 << 20); err != nil { // 100MB max
		http.Error(w, "File too large", http.StatusBadRequest)
		return
	}

	sourceType := r.FormValue("source_type")
	file, header, err := r.FormFile("import_file")
	if err != nil {
		http.Error(w, "No file uploaded", http.StatusBadRequest)
		return
	}
	defer file.Close()

	h.logger.Info().
		Str("source_type", sourceType).
		Str("filename", header.Filename).
		Int64("size", header.Size).
		Msg("migration import started")

	// TODO: Process import file based on source type
	// This would typically queue a background job

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(`<div class="alert alert-info">Import queued. Check progress in the background tasks.</div>`))
		return
	}

	http.Redirect(w, r, "/dashboard/settings/migrations", http.StatusSeeOther)
}
