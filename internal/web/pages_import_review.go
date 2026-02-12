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
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/go-chi/chi/v5"
)

// ImportReviewPage renders the staged import review page.
func (h *Handler) ImportReviewPage(w http.ResponseWriter, r *http.Request) {
	stagedID := chi.URLParam(r, "id")

	staged, err := h.migrationService.GetStagedImport(r.Context(), stagedID)
	if err != nil {
		h.logger.Error().Err(err).Str("staged_id", stagedID).Msg("failed to get staged import")
		http.Error(w, "Staged import not found", http.StatusNotFound)
		return
	}

	// Get the associated job for source type info
	job, _ := h.migrationService.GetJob(r.Context(), staged.JobID)

	h.Render(w, r, "pages/dashboard/settings/import-review", PageData{
		Title:    "Import Review",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Staged":           staged,
			"Job":              job,
			"MediaCount":       len(staged.StagedMedia),
			"PlaylistCount":    len(staged.StagedPlaylists),
			"ShowCount":        len(staged.StagedShows),
			"SmartBlockCount":  len(staged.StagedSmartBlocks),
			"WebstreamCount":   len(staged.StagedWebstreams),
			"DuplicateCount":   staged.DuplicateCount(),
			"OrphanMatchCount": staged.OrphanMatchCount(),
			"SelectedCount":    staged.SelectedCount(),
			"TotalCount":       staged.TotalCount(),
		},
	})
}

// ImportReviewMediaTab renders the media tab partial for HTMX.
func (h *Handler) ImportReviewMediaTab(w http.ResponseWriter, r *http.Request) {
	stagedID := chi.URLParam(r, "id")

	staged, err := h.migrationService.GetStagedImport(r.Context(), stagedID)
	if err != nil {
		writeHTMXError(w, "Staged import not found")
		return
	}

	h.RenderPartial(w, r, "partials/import-review-media", map[string]any{
		"Media":          staged.StagedMedia,
		"StagedID":       stagedID,
		"DuplicateCount": staged.DuplicateCount(),
	})
}

// ImportReviewShowsTab renders the shows tab partial for HTMX.
func (h *Handler) ImportReviewShowsTab(w http.ResponseWriter, r *http.Request) {
	stagedID := chi.URLParam(r, "id")

	staged, err := h.migrationService.GetStagedImport(r.Context(), stagedID)
	if err != nil {
		writeHTMXError(w, "Staged import not found")
		return
	}

	h.RenderPartial(w, r, "partials/import-review-shows", map[string]any{
		"Shows":    staged.StagedShows,
		"StagedID": stagedID,
	})
}

// ImportReviewUpdateSelections handles HTMX selection updates.
func (h *Handler) ImportReviewUpdateSelections(w http.ResponseWriter, r *http.Request) {
	stagedID := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		writeHTMXError(w, "Invalid form data")
		return
	}

	staged, err := h.migrationService.GetStagedImport(r.Context(), stagedID)
	if err != nil {
		writeHTMXError(w, "Staged import not found")
		return
	}

	// Build selections from form data
	selections := models.ImportSelections{
		MediaIDs:      r.Form["media_ids"],
		PlaylistIDs:   r.Form["playlist_ids"],
		SmartBlockIDs: r.Form["smartblock_ids"],
		ShowIDs:       r.Form["show_ids"],
		WebstreamIDs:  r.Form["webstream_ids"],
		ShowsAsShows:  nil,
		ShowsAsClocks: nil,
		CustomRRules:  make(map[string]string),
	}

	// Derive show-vs-clock preference from the show_type_{sourceID} radio input.
	for _, showID := range selections.ShowIDs {
		v := r.FormValue("show_type_" + showID)
		if v == "show" {
			selections.ShowsAsShows = append(selections.ShowsAsShows, showID)
		} else if v == "clock" {
			selections.ShowsAsClocks = append(selections.ShowsAsClocks, showID)
		}
	}

	// Extract custom RRULEs
	for key, values := range r.Form {
		if len(key) > 13 && key[:13] == "custom_rrule_" {
			showID := key[13:]
			if len(values) > 0 && values[0] != "" {
				selections.CustomRRules[showID] = values[0]
			}
		}
	}

	if err := h.migrationService.UpdateSelections(r.Context(), stagedID, selections); err != nil {
		writeHTMXError(w, fmt.Sprintf("Failed to update selections: %v", err))
		return
	}

	// Return updated count badge
	selectedCount := 0
	for _, m := range staged.StagedMedia {
		if containsString(selections.MediaIDs, m.SourceID) {
			selectedCount++
		}
	}
	for _, p := range staged.StagedPlaylists {
		if containsString(selections.PlaylistIDs, p.SourceID) {
			selectedCount++
		}
	}
	for _, s := range staged.StagedShows {
		if containsString(selections.ShowIDs, s.SourceID) {
			selectedCount++
		}
	}
	for _, sb := range staged.StagedSmartBlocks {
		if containsString(selections.SmartBlockIDs, sb.SourceID) {
			selectedCount++
		}
	}
	for _, ws := range staged.StagedWebstreams {
		if containsString(selections.WebstreamIDs, ws.SourceID) {
			selectedCount++
		}
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(fmt.Sprintf(`<span class="badge bg-primary" id="selectedCountBadge">%d selected</span>`, selectedCount)))
}

// ImportReviewCommit handles the commit action for a staged import.
func (h *Handler) ImportReviewCommit(w http.ResponseWriter, r *http.Request) {
	stagedID := chi.URLParam(r, "id")

	staged, err := h.migrationService.GetStagedImport(r.Context(), stagedID)
	if err != nil {
		writeHTMXError(w, "Staged import not found")
		return
	}

	if staged.Status != models.StagedImportStatusReady {
		writeHTMXError(w, "Staged import is not ready for commit")
		return
	}

	// Start the staged commit in background.
	if err := h.migrationService.CommitStagedImport(context.Background(), stagedID); err != nil {
		writeHTMXError(w, fmt.Sprintf("Failed to start staged import: %v", err))
		return
	}

	// Redirect to status page
	w.Header().Set("HX-Redirect", "/dashboard/settings/migrations/status")
	w.WriteHeader(http.StatusOK)
}

// ImportReviewReject handles the reject action for a staged import.
func (h *Handler) ImportReviewReject(w http.ResponseWriter, r *http.Request) {
	stagedID := chi.URLParam(r, "id")

	if err := h.migrationService.RejectStagedImport(r.Context(), stagedID); err != nil {
		writeHTMXError(w, fmt.Sprintf("Failed to reject import: %v", err))
		return
	}

	// Redirect to migrations page
	w.Header().Set("HX-Redirect", "/dashboard/settings/migrations")
	w.WriteHeader(http.StatusOK)
}

// ImportHistoryPage renders the import history page.
func (h *Handler) ImportHistoryPage(w http.ResponseWriter, r *http.Request) {
	// Get all migration jobs
	jobs, err := h.migrationService.ListJobs(r.Context())
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to load migration jobs")
		http.Error(w, "Failed to load import history", http.StatusInternalServerError)
		return
	}

	// Categorize jobs
	var completedJobs, rolledBackJobs, failedJobs []*migration.Job
	for _, job := range jobs {
		switch job.Status {
		case migration.JobStatusCompleted:
			completedJobs = append(completedJobs, job)
		case migration.JobStatusRolledBack:
			rolledBackJobs = append(rolledBackJobs, job)
		case migration.JobStatusFailed:
			failedJobs = append(failedJobs, job)
		}
	}

	h.Render(w, r, "pages/dashboard/settings/import-history", PageData{
		Title:    "Import History",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Jobs":           jobs,
			"CompletedJobs":  completedJobs,
			"RolledBackJobs": rolledBackJobs,
			"FailedJobs":     failedJobs,
		},
	})
}

// ImportHistoryRollback handles rolling back an import.
func (h *Handler) ImportHistoryRollback(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	if err := h.migrationService.RollbackImport(r.Context(), jobID); err != nil {
		writeHTMXError(w, fmt.Sprintf("Failed to rollback import: %v", err))
		return
	}

	h.logger.Warn().Str("job_id", jobID).Msg("import rolled back")

	html := `<div class="alert alert-success">
		<i class="bi bi-check-circle me-2"></i>
		<strong>Rollback complete!</strong>
		<p class="mb-0 mt-2">All items from this import have been removed.</p>
	</div>`

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// ImportHistoryRedo handles creating a redo job.
func (h *Handler) ImportHistoryRedo(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	newJob, err := h.migrationService.CloneJobForRedo(r.Context(), jobID)
	if err != nil {
		writeHTMXError(w, fmt.Sprintf("Failed to create redo job: %v", err))
		return
	}

	h.logger.Info().
		Str("original_job_id", jobID).
		Str("new_job_id", newJob.ID).
		Msg("redo job created")

	// Redirect to migrations page with the new job
	w.Header().Set("HX-Redirect", "/dashboard/settings/migrations/status")
	w.WriteHeader(http.StatusOK)
}

// ImportHistoryItems shows items created by an import.
func (h *Handler) ImportHistoryItems(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	items, err := h.migrationService.GetImportedItems(r.Context(), jobID)
	if err != nil {
		writeHTMXError(w, fmt.Sprintf("Failed to get imported items: %v", err))
		return
	}

	h.RenderPartial(w, r, "partials/import-history-items", map[string]any{
		"Items": items,
		"JobID": jobID,
	})
}

// containsString checks if a string slice contains a specific string.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
