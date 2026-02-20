/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/go-chi/chi/v5"
)

// SettingsPage renders the system settings page
func (h *Handler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	// Load settings from database
	dbSettings, err := models.GetSystemSettings(h.db)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to load system settings")
		http.Error(w, "Failed to load settings", http.StatusInternalServerError)
		return
	}

	// Combine database settings with read-only config values
	settings := map[string]any{
		// Database-backed (editable)
		"scheduler_lookahead": dbSettings.SchedulerLookahead,
		"analysis_enabled":    dbSettings.AnalysisEnabled,
		"websocket_enabled":   dbSettings.WebsocketEnabled,
		"metrics_enabled":     dbSettings.MetricsEnabled,
		"log_level":           dbSettings.LogLevel,
		// Config-backed (read-only)
		"media_root":      h.mediaRoot,
		"leader_election": h.eventBus != nil, // Leader election requires event bus (Redis/NATS)
		// Validation options for dropdowns
		"valid_lookaheads": models.ValidSchedulerLookaheads,
		"valid_log_levels": models.ValidLogLevels,
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

	// Load current settings
	settings, err := models.GetSystemSettings(h.db)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to load system settings for update")
		if r.Header.Get("HX-Request") == "true" {
			w.Write([]byte(`<div class="alert alert-danger">Failed to load settings</div>`))
			return
		}
		http.Error(w, "Failed to load settings", http.StatusInternalServerError)
		return
	}

	// Parse and validate form values
	schedulerLookahead := r.FormValue("scheduler_lookahead")
	if schedulerLookahead != "" {
		if models.IsValidSchedulerLookahead(schedulerLookahead) {
			settings.SchedulerLookahead = schedulerLookahead
		} else {
			h.logger.Warn().Str("value", schedulerLookahead).Msg("invalid scheduler_lookahead value, using default")
			settings.SchedulerLookahead = "168h"
		}
	}

	logLevel := r.FormValue("log_level")
	if logLevel != "" {
		if models.IsValidLogLevel(logLevel) {
			settings.LogLevel = logLevel
		} else {
			h.logger.Warn().Str("value", logLevel).Msg("invalid log_level value, using default")
			settings.LogLevel = "info"
		}
	}

	// Boolean fields - checkboxes only send value when checked
	settings.AnalysisEnabled = r.FormValue("analysis_enabled") == "on"
	settings.WebsocketEnabled = r.FormValue("websocket_enabled") == "on"
	settings.MetricsEnabled = r.FormValue("metrics_enabled") == "on"

	// Save to database
	if err := h.db.Save(settings).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to save system settings")
		if r.Header.Get("HX-Request") == "true" {
			w.Write([]byte(`<div class="alert alert-danger">Failed to save settings</div>`))
			return
		}
		http.Error(w, "Failed to save settings", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("scheduler_lookahead", settings.SchedulerLookahead).
		Bool("analysis_enabled", settings.AnalysisEnabled).
		Bool("websocket_enabled", settings.WebsocketEnabled).
		Bool("metrics_enabled", settings.MetricsEnabled).
		Str("log_level", settings.LogLevel).
		Msg("system settings updated")

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
	// Default 100MB, configurable via GRIMNIR_MAX_UPLOAD_SIZE_MB.
	if err := r.ParseMultipartForm(h.multipartLimit(100 << 20)); err != nil {
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

	// Validate source type
	var migrationSourceType migration.SourceType
	switch sourceType {
	case "azuracast":
		migrationSourceType = migration.SourceTypeAzuraCast
	case "libretime":
		migrationSourceType = migration.SourceTypeLibreTime
	default:
		if r.Header.Get("HX-Request") == "true" {
			w.Write([]byte(`<div class="alert alert-danger">Unsupported source type for file import</div>`))
			return
		}
		http.Error(w, "Unsupported source type", http.StatusBadRequest)
		return
	}

	// Save uploaded file to temp directory
	tempDir := filepath.Join(os.TempDir(), "grimnir-imports")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		h.logger.Error().Err(err).Msg("failed to create temp directory")
		if r.Header.Get("HX-Request") == "true" {
			w.Write([]byte(`<div class="alert alert-danger">Failed to save import file</div>`))
			return
		}
		http.Error(w, "Failed to save import file", http.StatusInternalServerError)
		return
	}

	tempFile := filepath.Join(tempDir, fmt.Sprintf("%d-%s", time.Now().UnixNano(), header.Filename))
	dst, err := os.Create(tempFile)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to create temp file")
		if r.Header.Get("HX-Request") == "true" {
			w.Write([]byte(`<div class="alert alert-danger">Failed to save import file</div>`))
			return
		}
		http.Error(w, "Failed to save import file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		h.logger.Error().Err(err).Msg("failed to copy file contents")
		os.Remove(tempFile)
		if r.Header.Get("HX-Request") == "true" {
			w.Write([]byte(`<div class="alert alert-danger">Failed to save import file</div>`))
			return
		}
		http.Error(w, "Failed to save import file", http.StatusInternalServerError)
		return
	}

	// Get user for import tracking
	user := h.GetUser(r)
	userID := ""
	if user != nil {
		userID = user.ID
	}

	// Build migration options based on source type
	options := migration.Options{
		ImportingUserID: userID,
	}

	switch migrationSourceType {
	case migration.SourceTypeAzuraCast:
		options.AzuraCastBackupPath = tempFile
	case migration.SourceTypeLibreTime:
		// LibreTime file imports would need different handling
		// For now, we support AzuraCast backup files
		os.Remove(tempFile)
		if r.Header.Get("HX-Request") == "true" {
			w.Write([]byte(`<div class="alert alert-warning">LibreTime file import is not yet supported. Use API import instead.</div>`))
			return
		}
		http.Error(w, "LibreTime file import not supported", http.StatusBadRequest)
		return
	}

	// Create job through migration service
	ctx := r.Context()
	job, err := h.migrationService.CreateJob(ctx, migrationSourceType, options)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to create migration job")
		os.Remove(tempFile)
		if r.Header.Get("HX-Request") == "true" {
			w.Write([]byte(`<div class="alert alert-danger">Failed to create import job</div>`))
			return
		}
		http.Error(w, "Failed to create import job", http.StatusInternalServerError)
		return
	}

	// Start job in background
	if err := h.migrationService.StartJob(context.Background(), job.ID); err != nil {
		h.logger.Error().Err(err).Str("job_id", job.ID).Msg("failed to start migration job")
		if r.Header.Get("HX-Request") == "true" {
			w.Write([]byte(`<div class="alert alert-danger">Failed to start import job</div>`))
			return
		}
		http.Error(w, "Failed to start import job", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("job_id", job.ID).
		Str("source_type", sourceType).
		Str("file", tempFile).
		Msg("migration job created and started")

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(`<div class="alert alert-success">Import job started. Check progress in the job list below.</div>`))
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
	stagedMode := r.FormValue("staged_mode") == "on"
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
		StagedMode:        stagedMode,
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

	// Staged review mode: analyze asynchronously and present a review page once ready.
	if stagedMode {
		job, err := h.migrationService.CreateStagedJob(ctx, migration.SourceTypeAzuraCast, options)
		if err != nil {
			writeHTMXError(w, fmt.Sprintf("Failed to create staged import job: %v", err))
			return
		}
		if err := h.migrationService.StartStagedJob(context.Background(), job.ID); err != nil {
			writeHTMXError(w, fmt.Sprintf("Failed to start staged analysis: %v", err))
			return
		}

		h.logger.Info().Str("job_id", job.ID).Msg("AzuraCast staged import analysis started")
		html := fmt.Sprintf(`<div class="alert alert-success">
			<i class="bi bi-check-circle me-2"></i>
			<strong>Analysis started!</strong>
			<p class="mb-0 mt-2">We are analyzing your AzuraCast instance to prepare a reviewable import plan.</p>
			<p class="mb-0 mt-2"><a href="/dashboard/settings/migrations/status" class="alert-link">View import status</a> (you will get a Review button when ready)</p>
		</div>`)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
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

	job, err := h.migrationService.CreateJob(ctx, migration.SourceTypeAzuraCast, options)
	if err != nil {
		writeHTMXError(w, fmt.Sprintf("Failed to create import job: %v", err))
		return
	}
	if err := h.migrationService.StartJob(context.Background(), job.ID); err != nil {
		writeHTMXError(w, fmt.Sprintf("Failed to start import job: %v", err))
		return
	}

	h.logger.Info().Str("job_id", job.ID).Msg("AzuraCast API import job started")

	html := fmt.Sprintf(`<div class="alert alert-success">
		<i class="bi bi-check-circle me-2"></i>
		<strong>Import started!</strong>
		<p class="mb-0 mt-2">The import is running in the background. Media files are being downloaded and processed.</p>
		<p class="mb-0 mt-2"><a href="/dashboard/settings/migrations/status" class="alert-link">View import status</a></p>
	</div>`)

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// writeHTMXError writes an error as an HTMX-friendly HTML response
func writeHTMXError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(fmt.Sprintf(`<div class="alert alert-danger"><i class="bi bi-exclamation-triangle me-2"></i>%s</div>`, message)))
}

// MigrationStatusPage shows the status of all migration jobs
func (h *Handler) MigrationStatusPage(w http.ResponseWriter, r *http.Request) {
	// Get all migration jobs
	var jobs []migration.Job
	if err := h.db.Order("created_at DESC").Limit(20).Find(&jobs).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to load migration jobs")
		http.Error(w, "Failed to load migration jobs", http.StatusInternalServerError)
		return
	}

	// Calculate totals across all jobs
	var totalMedia, importedMedia, totalPlaylists, importedPlaylists int
	var hasRunning bool

	for _, job := range jobs {
		switch job.Status {
		case migration.JobStatusRunning:
			hasRunning = true
			totalMedia = job.Progress.MediaTotal
			importedMedia = job.Progress.MediaImported
			totalPlaylists = job.Progress.PlaylistsTotal
			importedPlaylists = job.Progress.PlaylistsImported
		case migration.JobStatusAnalyzing:
			hasRunning = true
		case migration.JobStatusCompleted:
			if job.Result != nil {
				importedMedia += job.Result.MediaItemsImported
				importedPlaylists += job.Result.PlaylistsCreated
			}
		case migration.JobStatusFailed:
			// Include progress from failed jobs so user can see what was imported
			importedMedia += job.Progress.MediaImported
			importedPlaylists += job.Progress.PlaylistsImported
		}
	}

	h.Render(w, r, "pages/dashboard/settings/migration-status", PageData{
		Title:    "Import Status",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Jobs":              jobs,
			"TotalMedia":        totalMedia,
			"ImportedMedia":     importedMedia,
			"TotalPlaylists":    totalPlaylists,
			"ImportedPlaylists": importedPlaylists,
			"HasRunning":        hasRunning,
		},
	})
}

// MigrationJobRestart restarts a failed migration job
func (h *Handler) MigrationJobRestart(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	if jobID == "" {
		writeHTMXError(w, "Job ID is required")
		return
	}

	ctx := r.Context()

	// Get the failed job
	job, err := h.migrationService.GetJob(ctx, jobID)
	if err != nil {
		writeHTMXError(w, fmt.Sprintf("Job not found: %v", err))
		return
	}

	if job.Status != migration.JobStatusFailed && job.Status != migration.JobStatusCancelled {
		writeHTMXError(w, "Only failed or cancelled jobs can be restarted")
		return
	}

	// Create a new job with the same options
	newJob, err := h.migrationService.CreateJob(ctx, job.SourceType, job.Options)
	if err != nil {
		writeHTMXError(w, fmt.Sprintf("Failed to create new job: %v", err))
		return
	}

	// Start the new job
	if err := h.migrationService.StartJob(context.Background(), newJob.ID); err != nil {
		writeHTMXError(w, fmt.Sprintf("Failed to start job: %v", err))
		return
	}

	h.logger.Info().
		Str("old_job_id", jobID).
		Str("new_job_id", newJob.ID).
		Msg("migration job restarted")

	// Redirect to status page
	w.Header().Set("HX-Redirect", "/dashboard/settings/migrations/status")
	w.WriteHeader(http.StatusOK)
}

// MigrationJobDelete deletes a migration job
func (h *Handler) MigrationJobDelete(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	if jobID == "" {
		writeHTMXError(w, "Job ID is required")
		return
	}

	ctx := r.Context()

	if err := h.migrationService.DeleteJob(ctx, jobID); err != nil {
		writeHTMXError(w, fmt.Sprintf("Failed to delete job: %v", err))
		return
	}

	h.logger.Info().Str("job_id", jobID).Msg("migration job deleted")

	// Redirect to status page
	w.Header().Set("HX-Redirect", "/dashboard/settings/migrations/status")
	w.WriteHeader(http.StatusOK)
}

// MigrationResetData clears all imported data from the database
func (h *Handler) MigrationResetData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := h.migrationService.ResetImportedData(ctx); err != nil {
		h.logger.Error().Err(err).Msg("failed to reset imported data")
		writeHTMXError(w, fmt.Sprintf("Failed to reset data: %v", err))
		return
	}

	h.logger.Warn().Msg("all imported data has been reset")

	html := `<div class="alert alert-success">
		<i class="bi bi-check-circle me-2"></i>
		<strong>Reset complete!</strong>
		<p class="mb-0 mt-2">All imported data has been cleared. You can now run a fresh import.</p>
	</div>`

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// LibreTimeAPITest tests the connection to a LibreTime instance
func (h *Handler) LibreTimeAPITest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeHTMXError(w, "Invalid form data")
		return
	}

	apiURL := r.FormValue("libretime_url")
	apiKey := r.FormValue("api_key")

	if apiURL == "" {
		writeHTMXError(w, "URL is required")
		return
	}

	if apiKey == "" {
		writeHTMXError(w, "API key is required")
		return
	}

	client, err := migration.NewLibreTimeAPIClient(apiURL, apiKey)
	if err != nil {
		writeHTMXError(w, fmt.Sprintf("Failed to create client: %v", err))
		return
	}

	ctx := context.Background()

	// Test connection
	status, err := client.TestConnection(ctx)
	if err != nil {
		writeHTMXError(w, fmt.Sprintf("Connection failed: %v", err))
		return
	}

	// Get files to show count
	files, err := client.GetFiles(ctx)
	if err != nil {
		writeHTMXError(w, fmt.Sprintf("Could not fetch files: %v", err))
		return
	}

	// Count accessible files
	accessibleFiles := 0
	for _, f := range files {
		if !f.Hidden && f.FileExists {
			accessibleFiles++
		}
	}

	// Get playlists
	playlists, _ := client.GetPlaylists(ctx)

	// Get shows
	shows, _ := client.GetShows(ctx)

	// Build success response
	html := fmt.Sprintf(`<div class="alert alert-success">
		<i class="bi bi-check-circle me-2"></i>
		<strong>Connection successful!</strong>
		<p class="mb-1 mt-2">Server online: %v</p>
		<p class="mb-1">Files accessible: %v</p>`, status.Online, status.FilesAccessible)

	if status.Warning != "" {
		html += fmt.Sprintf(`<p class="text-warning mb-1"><i class="bi bi-exclamation-triangle me-1"></i>%s</p>`, status.Warning)
	}

	html += fmt.Sprintf(`<hr class="my-2">
		<p class="mb-0">Found:</p>
		<ul class="mb-0">
			<li><strong>%d</strong> media files</li>
			<li><strong>%d</strong> playlists</li>
			<li><strong>%d</strong> shows</li>
		</ul>
	</div>`, accessibleFiles, len(playlists), len(shows))

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// LibreTimeAPIImport starts an import from a LibreTime instance via API
func (h *Handler) LibreTimeAPIImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeHTMXError(w, "Invalid form data")
		return
	}

	apiURL := r.FormValue("libretime_url")
	apiKey := r.FormValue("api_key")
	targetStationID := r.FormValue("target_station_id")
	stagedMode := r.FormValue("staged_mode") == "on"
	skipMedia := r.FormValue("skip_media") == "on"
	skipPlaylists := r.FormValue("skip_playlists") == "on"
	skipSchedules := r.FormValue("skip_schedules") == "on"
	skipSmartblocks := r.FormValue("skip_smartblocks") == "on"
	skipWebstreams := r.FormValue("skip_webstreams") == "on"
	dryRun := r.FormValue("dry_run") == "on"

	if apiURL == "" {
		writeHTMXError(w, "URL is required")
		return
	}

	if apiKey == "" {
		writeHTMXError(w, "API key is required")
		return
	}

	h.logger.Info().
		Str("url", apiURL).
		Str("target_station", targetStationID).
		Bool("skip_media", skipMedia).
		Bool("skip_playlists", skipPlaylists).
		Bool("skip_schedules", skipSchedules).
		Bool("skip_smartblocks", skipSmartblocks).
		Bool("skip_webstreams", skipWebstreams).
		Bool("dry_run", dryRun).
		Msg("starting LibreTime API import")

	// Get current user for ownership
	user := h.GetUser(r)
	var importingUserID string
	if user != nil {
		importingUserID = user.ID
	}

	// Create import options
	options := migration.Options{
		LibreTimeAPIURL: apiURL,
		LibreTimeAPIKey: apiKey,
		TargetStationID: targetStationID,
		StagedMode:      stagedMode,
		SkipMedia:       skipMedia,
		SkipPlaylists:   skipPlaylists,
		SkipSchedules:   skipSchedules,
		SkipSmartblocks: skipSmartblocks,
		SkipWebstreams:  skipWebstreams,
		ImportingUserID: importingUserID,
	}

	ctx := context.Background()
	importer := migration.NewLibreTimeImporter(h.db, h.mediaService, h.logger)

	// Validate first
	if err := importer.Validate(ctx, options); err != nil {
		writeHTMXError(w, fmt.Sprintf("Validation failed: %v", err))
		return
	}

	// Staged review mode: analyze asynchronously and present a review page once ready.
	if stagedMode {
		job, err := h.migrationService.CreateStagedJob(ctx, migration.SourceTypeLibreTime, options)
		if err != nil {
			writeHTMXError(w, fmt.Sprintf("Failed to create staged import job: %v", err))
			return
		}
		if err := h.migrationService.StartStagedJob(context.Background(), job.ID); err != nil {
			writeHTMXError(w, fmt.Sprintf("Failed to start staged analysis: %v", err))
			return
		}

		h.logger.Info().Str("job_id", job.ID).Msg("LibreTime staged import analysis started")
		html := fmt.Sprintf(`<div class="alert alert-success">
			<i class="bi bi-check-circle me-2"></i>
			<strong>Analysis started!</strong>
			<p class="mb-0 mt-2">We are analyzing your LibreTime instance to prepare a reviewable import plan.</p>
			<p class="mb-0 mt-2"><a href="/dashboard/settings/migrations/status" class="alert-link">View import status</a> (you will get a Review button when ready)</p>
		</div>`)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
		return
	}

	// Dry run (non-staged) - just analyze with detailed report
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
						<tr><td class="text-body-secondary">Media Files:</td><td><strong>%d</strong></td></tr>
						<tr><td class="text-body-secondary">Playlists:</td><td><strong>%d</strong></td></tr>
						<tr><td class="text-body-secondary">Shows:</td><td><strong>%d</strong></td></tr>
					</table>
				</div>
				<div class="col-md-6">
					<table class="table table-sm table-borderless mb-0">
						<tr><td class="text-body-secondary">Est. Storage:</td><td><strong>%s</strong></td></tr>
					</table>
				</div>
			</div>`,
			report.TotalFiles, report.TotalPlaylists, report.TotalShows, report.EstimatedStorageHuman)

		// Playlists
		if len(report.Playlists) > 0 {
			html += `<h6 class="mt-3 mb-2">Playlists</h6><ul class="small mb-0">`
			for _, pl := range report.Playlists {
				// Build item breakdown string
				var parts []string
				if pl.FileCount > 0 {
					parts = append(parts, fmt.Sprintf("%d files", pl.FileCount))
				}
				if pl.BlockCount > 0 {
					parts = append(parts, fmt.Sprintf("%d smart blocks", pl.BlockCount))
				}
				if pl.StreamCount > 0 {
					parts = append(parts, fmt.Sprintf("%d streams", pl.StreamCount))
				}
				itemsDesc := "empty"
				if len(parts) > 0 {
					itemsDesc = strings.Join(parts, ", ")
				}
				html += fmt.Sprintf(`<li>%s <span class="text-body-secondary">(%s, %s)</span></li>`,
					pl.Name, itemsDesc, pl.Length)
			}
			html += `</ul>`
		}

		// Shows
		if len(report.Shows) > 0 {
			html += `<h6 class="mt-3 mb-2">Shows (will become Clocks)</h6><ul class="small mb-0">`
			for _, show := range report.Shows {
				desc := show.Description
				if len(desc) > 50 {
					desc = desc[:50] + "..."
				}
				html += fmt.Sprintf(`<li>%s`, show.Name)
				if desc != "" {
					html += fmt.Sprintf(` <span class="text-body-secondary">- %s</span>`, desc)
				}
				html += `</li>`
			}
			html += `</ul>`
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
			</div></div>`

		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
		return
	}

	// Create job through migration service for tracking
	job, err := h.migrationService.CreateJob(ctx, migration.SourceTypeLibreTime, options)
	if err != nil {
		writeHTMXError(w, fmt.Sprintf("Failed to create import job: %v", err))
		return
	}

	// Start job in background
	if err := h.migrationService.StartJob(context.Background(), job.ID); err != nil {
		writeHTMXError(w, fmt.Sprintf("Failed to start import job: %v", err))
		return
	}

	h.logger.Info().Str("job_id", job.ID).Msg("LibreTime API import job started")

	html := fmt.Sprintf(`<div class="alert alert-success">
		<i class="bi bi-check-circle me-2"></i>
		<strong>Import started!</strong>
		<p class="mb-0 mt-2">The import is running in the background. Media files are being downloaded and processed.</p>
		<p class="mb-0 mt-2"><a href="/dashboard/settings/migrations/status" class="alert-link">View import status</a></p>
	</div>`)

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}
