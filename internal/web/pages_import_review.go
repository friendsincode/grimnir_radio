/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

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
			"StationFilters":   extractStationFilters(staged),
			"SelectedStations": selectedStationMap(staged.Selections.StationIDs),
			"AnomalyCards":     buildStagedAnomalyCards(staged),
		},
	})
}

// ImportReviewByJobRedirect resolves staged import by job ID and redirects to review page.
func (h *Handler) ImportReviewByJobRedirect(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if strings.TrimSpace(jobID) == "" {
		writeHTMXError(w, "Missing migration job ID")
		return
	}

	staged, err := h.migrationService.GetStagedImportByJobID(r.Context(), jobID)
	if err != nil || staged == nil || staged.ID == "" {
		h.logger.Error().Err(err).Str("job_id", jobID).Msg("failed to resolve staged import from job")
		writeHTMXError(w, "No staged review data found for this job. Re-run the import analysis.")
		return
	}

	target := "/dashboard/settings/migrations/review/" + staged.ID
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
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
		StationIDs:    r.Form["station_ids"],
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

type stationFilterOption struct {
	ID    string
	Label string
}

func extractStationFilters(staged *models.StagedImport) []stationFilterOption {
	if staged == nil {
		return nil
	}
	unique := map[int]struct{}{}
	labels := map[int]string{}
	addFrom := func(sourceID string) {
		parts := strings.SplitN(sourceID, "::", 2)
		if len(parts) != 2 {
			return
		}
		id, err := strconv.Atoi(parts[0])
		if err != nil {
			return
		}
		unique[id] = struct{}{}
	}
	setLabel := func(sourceID, label string) {
		parts := strings.SplitN(sourceID, "::", 2)
		if len(parts) != 2 {
			return
		}
		id, err := strconv.Atoi(parts[0])
		if err != nil {
			return
		}
		label = strings.TrimSpace(label)
		if label == "" {
			return
		}
		if _, ok := labels[id]; !ok {
			labels[id] = label
		}
	}
	setLabelByID := func(id, label string) {
		parsed, err := strconv.Atoi(strings.TrimSpace(id))
		if err != nil {
			return
		}
		label = strings.TrimSpace(label)
		if label == "" {
			return
		}
		labels[parsed] = label
	}
	labelFromDescription := func(desc string) string {
		desc = strings.TrimSpace(desc)
		if desc == "" {
			return ""
		}
		if strings.HasPrefix(desc, "Station: ") {
			return strings.TrimSpace(strings.TrimPrefix(desc, "Station: "))
		}
		const prefix = "Imported from station "
		if strings.HasPrefix(desc, prefix) {
			rest := strings.TrimPrefix(desc, prefix)
			if i := strings.Index(rest, " playlist schedule"); i > 0 {
				return strings.TrimSpace(rest[:i])
			}
			return strings.TrimSpace(rest)
		}
		return ""
	}

	for _, m := range staged.StagedMedia {
		addFrom(m.SourceID)
	}
	for _, p := range staged.StagedPlaylists {
		addFrom(p.SourceID)
	}
	for _, sb := range staged.StagedSmartBlocks {
		addFrom(sb.SourceID)
		setLabel(sb.SourceID, labelFromDescription(sb.Description))
	}
	for _, sh := range staged.StagedShows {
		addFrom(sh.SourceID)
		setLabel(sh.SourceID, labelFromDescription(sh.Description))
	}
	for _, ws := range staged.StagedWebstreams {
		addFrom(ws.SourceID)
		setLabel(ws.SourceID, labelFromDescription(ws.Description))
	}
	for _, w := range staged.Warnings {
		if w.Code != "source_station_label" {
			continue
		}
		if w.ItemType != "station" {
			continue
		}
		setLabelByID(w.ItemID, w.Message)
	}

	ids := make([]int, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	out := make([]stationFilterOption, 0, len(ids))
	for _, id := range ids {
		label := labels[id]
		if strings.TrimSpace(label) == "" {
			label = fmt.Sprintf("Station %d", id)
		}
		out = append(out, stationFilterOption{
			ID:    fmt.Sprintf("%d", id),
			Label: label,
		})
	}
	return out
}

func selectedStationMap(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		m[id] = true
	}
	return m
}

type stagedAnomalyCard struct {
	Key      string
	Label    string
	Count    int
	Examples []string
}

func buildStagedAnomalyCards(staged *models.StagedImport) []stagedAnomalyCard {
	if staged == nil {
		return nil
	}

	addExamples := func(items []string) []string {
		if len(items) > 3 {
			return items[:3]
		}
		return items
	}

	durationExamples := []string{}
	missingExamples := []string{}
	skippedExamples := []string{}
	for _, w := range staged.Warnings {
		code := strings.ToLower(w.Code)
		msg := strings.TrimSpace(w.Message)
		if msg == "" {
			continue
		}
		switch {
		case strings.Contains(code, "duration") || strings.Contains(strings.ToLower(msg), "duration"):
			durationExamples = append(durationExamples, msg)
		case strings.Contains(code, "missing") || strings.Contains(code, "orphan") || strings.Contains(code, "not_found") ||
			strings.Contains(strings.ToLower(msg), "missing") || strings.Contains(strings.ToLower(msg), "not found"):
			missingExamples = append(missingExamples, msg)
		case strings.Contains(code, "skip") || strings.Contains(strings.ToLower(msg), "skip"):
			skippedExamples = append(skippedExamples, msg)
		}
	}

	duplicateExamples := []string{}
	for _, m := range staged.StagedMedia {
		if !m.IsDuplicate {
			continue
		}
		duplicateExamples = append(duplicateExamples, m.Title)
		if len(duplicateExamples) >= 3 {
			break
		}
	}

	return []stagedAnomalyCard{
		{
			Key:      "duration",
			Label:    "Duration",
			Count:    len(durationExamples),
			Examples: addExamples(durationExamples),
		},
		{
			Key:      "duplicate_resolution",
			Label:    "Duplicate Resolution",
			Count:    staged.DuplicateCount(),
			Examples: addExamples(duplicateExamples),
		},
		{
			Key:      "missing_links",
			Label:    "Missing Links",
			Count:    len(missingExamples),
			Examples: addExamples(missingExamples),
		},
		{
			Key:      "skipped_entities",
			Label:    "Skipped Entities",
			Count:    len(skippedExamples),
			Examples: addExamples(skippedExamples),
		},
	}
}
