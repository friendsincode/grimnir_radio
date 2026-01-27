/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"fmt"
	"net/http"

	"github.com/friendsincode/grimnir_radio/internal/migration"
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

// AzuraCastAPITest tests the connection to an AzuraCast instance
func (h *Handler) AzuraCastAPITest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeHTMXError(w, "Invalid form data")
		return
	}

	apiURL := r.FormValue("azuracast_url")
	apiKey := r.FormValue("api_key")
	username := r.FormValue("username")
	password := r.FormValue("password")

	if apiURL == "" {
		writeHTMXError(w, "URL is required")
		return
	}

	hasAPIKey := apiKey != ""
	hasCredentials := username != "" && password != ""

	if !hasAPIKey && !hasCredentials {
		writeHTMXError(w, "Either API key or username/password is required")
		return
	}

	var client *migration.AzuraCastAPIClient
	var err error

	if hasAPIKey {
		client, err = migration.NewAzuraCastAPIClient(apiURL, apiKey)
	} else {
		client, err = migration.NewAzuraCastAPIClientWithCredentials(apiURL, username, password)
	}
	if err != nil {
		writeHTMXError(w, fmt.Sprintf("Authentication failed: %v", err))
		return
	}

	ctx := context.Background()

	// Test connection
	status, err := client.TestConnection(ctx)
	if err != nil {
		writeHTMXError(w, fmt.Sprintf("Connection failed: %v", err))
		return
	}

	// Get stations to show what will be imported
	stations, err := client.GetStations(ctx)
	if err != nil {
		writeHTMXError(w, fmt.Sprintf("Could not fetch stations: %v", err))
		return
	}

	// Build success response
	html := fmt.Sprintf(`<div class="alert alert-success">
		<i class="bi bi-check-circle me-2"></i>
		<strong>Connection successful!</strong>
		<p class="mb-1 mt-2">Server online: %v</p>
		<p class="mb-0">Found <strong>%d station(s)</strong> accessible with this API key:</p>
		<ul class="mb-0 mt-2">`, status.Online, len(stations))

	for _, station := range stations {
		html += fmt.Sprintf(`<li>%s</li>`, station.Name)
	}
	html += `</ul></div>`

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// AzuraCastAPIImport starts an import from an AzuraCast instance via API
func (h *Handler) AzuraCastAPIImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeHTMXError(w, "Invalid form data")
		return
	}

	apiURL := r.FormValue("azuracast_url")
	apiKey := r.FormValue("api_key")
	username := r.FormValue("username")
	password := r.FormValue("password")
	skipMedia := r.FormValue("skip_media") == "on"
	skipUsers := r.FormValue("skip_users") == "on"
	dryRun := r.FormValue("dry_run") == "on"

	if apiURL == "" {
		writeHTMXError(w, "URL is required")
		return
	}

	hasAPIKey := apiKey != ""
	hasCredentials := username != "" && password != ""

	if !hasAPIKey && !hasCredentials {
		writeHTMXError(w, "Either API key or username/password is required")
		return
	}

	h.logger.Info().
		Str("url", apiURL).
		Bool("skip_media", skipMedia).
		Bool("skip_users", skipUsers).
		Bool("dry_run", dryRun).
		Msg("starting AzuraCast API import")

	// Get current user for ownership
	user := h.GetUser(r)
	var importingUserID string
	if user != nil {
		importingUserID = user.ID
	}

	// Create import options
	options := migration.Options{
		AzuraCastAPIURL:   apiURL,
		AzuraCastAPIKey:   apiKey,
		AzuraCastUsername: username,
		AzuraCastPassword: password,
		SkipMedia:         skipMedia,
		SkipUsers:         skipUsers,
		ImportingUserID:   importingUserID,
	}

	ctx := context.Background()
	importer := migration.NewAzuraCastImporter(h.db, h.mediaService, h.logger)

	// Validate first
	if err := importer.Validate(ctx, options); err != nil {
		writeHTMXError(w, fmt.Sprintf("Validation failed: %v", err))
		return
	}

	// Dry run - just analyze with detailed report
	if dryRun {
		report, err := importer.AnalyzeDetailed(ctx, options)
		if err != nil {
			writeHTMXError(w, fmt.Sprintf("Analysis failed: %v", err))
			return
		}

		html := `<div class="card border-info">
			<div class="card-header bg-info text-white">
				<i class="bi bi-info-circle me-2"></i><strong>Dry Run Analysis Complete</strong>
			</div>
			<div class="card-body">`

		// Summary section
		html += fmt.Sprintf(`
			<h6 class="mb-3">Summary</h6>
			<div class="row mb-3">
				<div class="col-md-6">
					<table class="table table-sm table-borderless mb-0">
						<tr><td class="text-body-secondary">Stations:</td><td><strong>%d</strong></td></tr>
						<tr><td class="text-body-secondary">Media Files:</td><td><strong>%d</strong></td></tr>
						<tr><td class="text-body-secondary">Playlists:</td><td><strong>%d</strong></td></tr>
					</table>
				</div>
				<div class="col-md-6">
					<table class="table table-sm table-borderless mb-0">
						<tr><td class="text-body-secondary">Schedules:</td><td><strong>%d</strong></td></tr>
						<tr><td class="text-body-secondary">Users/DJs:</td><td><strong>%d</strong></td></tr>
						<tr><td class="text-body-secondary">Est. Storage:</td><td><strong>%s</strong></td></tr>
					</table>
				</div>
			</div>`,
			report.TotalStations, report.TotalMedia, report.TotalPlaylists,
			report.TotalSchedules, report.TotalStreamers, report.EstimatedStorageHuman)

		// Detailed station breakdown
		if len(report.Stations) > 0 {
			html += `<h6 class="mt-3 mb-3">Station Details</h6>
				<div class="accordion" id="stationAccordion">`

			for i, station := range report.Stations {
				collapseID := fmt.Sprintf("collapse%d", i)
				headingID := fmt.Sprintf("heading%d", i)

				html += fmt.Sprintf(`
					<div class="accordion-item">
						<h2 class="accordion-header" id="%s">
							<button class="accordion-button collapsed" type="button" data-bs-toggle="collapse" data-bs-target="#%s">
								<i class="bi bi-broadcast me-2"></i>
								<strong>%s</strong>
								<span class="badge bg-secondary ms-2">%d media</span>
							</button>
						</h2>
						<div id="%s" class="accordion-collapse collapse" data-bs-parent="#stationAccordion">
							<div class="accordion-body">`,
					headingID, collapseID, station.Name, station.MediaCount, collapseID)

				if station.Description != "" {
					html += fmt.Sprintf(`<p class="text-body-secondary small mb-2">%s</p>`, station.Description)
				}

				// Playlists
				if len(station.Playlists) > 0 {
					html += `<div class="mb-2"><strong class="small">Playlists:</strong><ul class="small mb-0">`
					for _, pl := range station.Playlists {
						html += fmt.Sprintf(`<li>%s <span class="text-body-secondary">(%s, %d items)</span></li>`,
							pl.Name, pl.Type, pl.ItemCount)
					}
					html += `</ul></div>`
				}

				// Mounts
				if len(station.Mounts) > 0 {
					html += `<div class="mb-2"><strong class="small">Mounts:</strong><ul class="small mb-0">`
					for _, mt := range station.Mounts {
						html += fmt.Sprintf(`<li>%s <span class="text-body-secondary">(%s, %d kbps)</span></li>`,
							mt.Name, mt.Format, mt.Bitrate)
					}
					html += `</ul></div>`
				}

				// Streamers
				if len(station.Streamers) > 0 {
					html += `<div class="mb-0"><strong class="small">Streamers/DJs:</strong><ul class="small mb-0">`
					for _, st := range station.Streamers {
						name := st.DisplayName
						if name == "" {
							name = st.Username
						}
						html += fmt.Sprintf(`<li>%s</li>`, name)
					}
					html += `</ul></div>`
				}

				html += `</div></div></div>` // Close accordion-body, accordion-collapse, accordion-item
			}

			html += `</div>` // Close accordion
		}

		// Warnings
		if len(report.Warnings) > 0 {
			html += `<div class="alert alert-warning mt-3 mb-0"><strong>Warnings:</strong><ul class="mb-0">`
			for _, warning := range report.Warnings {
				html += fmt.Sprintf(`<li>%s</li>`, warning)
			}
			html += `</ul></div>`
		}

		html += `<p class="mt-3 mb-0 text-body-secondary small">Uncheck "Dry run" and click Import to perform the actual import.</p>
			</div></div>` // Close card-body, card

		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
		return
	}

	// Real import - run in background
	go func() {
		progressCallback := func(progress migration.Progress) {
			h.logger.Debug().
				Str("phase", progress.Phase).
				Float64("percentage", progress.Percentage).
				Str("step", progress.CurrentStep).
				Msg("import progress")
		}

		result, err := importer.Import(context.Background(), options, progressCallback)
		if err != nil {
			h.logger.Error().Err(err).Msg("AzuraCast API import failed")
			return
		}

		h.logger.Info().
			Int("stations", result.StationsCreated).
			Int("media", result.MediaItemsImported).
			Int("playlists", result.PlaylistsCreated).
			Float64("duration_seconds", result.DurationSeconds).
			Msg("AzuraCast API import completed")
	}()

	html := `<div class="alert alert-success">
		<i class="bi bi-check-circle me-2"></i>
		<strong>Import started!</strong>
		<p class="mb-0 mt-2">The import is running in the background. Media files are being downloaded and processed.
		Check the logs for progress updates.</p>
	</div>`

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// writeHTMXError writes an error as an HTMX-friendly HTML response
func writeHTMXError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(fmt.Sprintf(`<div class="alert alert-danger"><i class="bi bi-exclamation-triangle me-2"></i>%s</div>`, message)))
}
