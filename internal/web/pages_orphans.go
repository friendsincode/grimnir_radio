/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/go-chi/chi/v5"
)

// parsePageSize reads the per_page query param and clamps to allowed values.
// Returns 0 for "all" (no pagination).
func parsePageSize(r *http.Request) int {
	raw := r.URL.Query().Get("per_page")
	if raw == "all" {
		return 0
	}
	n, _ := strconv.Atoi(raw)
	switch n {
	case 50, 100:
		return n
	default:
		return 25
	}
}

// OrphansPage renders the orphan media management page.
func (h *Handler) OrphansPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get pagination params
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize := parsePageSize(r)

	// Get orphans
	var orphans []models.OrphanMedia
	var total int64
	var err error

	if h.mediaService != nil {
		orphans, total, err = h.mediaService.GetOrphans(ctx, page, pageSize)
		if err != nil {
			h.logger.Error().Err(err).Msg("failed to get orphans")
		}
	}

	// Get stats
	var count, totalSize int64
	if h.mediaService != nil {
		count, totalSize, _ = h.mediaService.GetOrphanStats(ctx)
	}

	// Get stations for adopt dropdown
	stations := h.LoadStations(r)

	totalPages := 0
	if pageSize > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}

	h.Render(w, r, "pages/dashboard/settings/orphans", PageData{
		Title:    "Orphan Media Manager",
		Stations: stations,
		Data: map[string]any{
			"Orphans":     orphans,
			"Total":       total,
			"Count":       count,
			"TotalSize":   totalSize,
			"SizeHuman":   formatBytesHuman(totalSize),
			"Page":        page,
			"PageSize":    pageSize,
			"TotalPages":  totalPages,
			"HasPrev":     page > 1,
			"HasNext":     page < totalPages,
			"PrevPage":    page - 1,
			"NextPage":    page + 1,
			"StationList": stations,
		},
	})
}

// OrphansScan triggers a scan for orphaned media files.
func (h *Handler) OrphansScan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.mediaService == nil {
		writeHTMXError(w, "Media service not available")
		return
	}

	result, err := h.mediaService.ScanForOrphans(ctx)
	if err != nil {
		h.logger.Error().Err(err).Msg("orphan scan failed")
		writeHTMXError(w, fmt.Sprintf("Scan failed: %v", err))
		return
	}

	h.logger.Info().
		Int("new_orphans", result.NewOrphans).
		Int("total_files", result.TotalFiles).
		Dur("duration", result.Duration).
		Msg("orphan scan completed")

	html := fmt.Sprintf(`<div class="alert alert-success">
		<i class="bi bi-check-circle me-2"></i>
		<strong>Scan complete!</strong>
		<ul class="mb-0 mt-2">
			<li>Files scanned: %d</li>
			<li>New orphans found: %d</li>
			<li>Already tracked: %d</li>
			<li>Errors: %d</li>
			<li>Duration: %s</li>
		</ul>
	</div>`, result.TotalFiles, result.NewOrphans, result.AlreadyKnown, result.Errors, result.Duration)

	w.Header().Set("HX-Trigger", "orphans-updated")
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// OrphansAdopt adopts a single orphan to a station.
func (h *Handler) OrphansAdopt(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orphanID := chi.URLParam(r, "id")

	if err := r.ParseForm(); err != nil {
		writeHTMXError(w, "Invalid form data")
		return
	}

	stationID := r.FormValue("station_id")
	if stationID == "" {
		writeHTMXError(w, "Station ID is required")
		return
	}

	if h.mediaService == nil {
		writeHTMXError(w, "Media service not available")
		return
	}

	mediaItem, err := h.mediaService.AdoptOrphan(ctx, orphanID, stationID)
	if err != nil {
		h.logger.Error().Err(err).Str("orphan_id", orphanID).Msg("failed to adopt orphan")
		writeHTMXError(w, fmt.Sprintf("Failed to adopt: %v", err))
		return
	}

	h.logger.Info().
		Str("orphan_id", orphanID).
		Str("media_id", mediaItem.ID).
		Str("station_id", stationID).
		Msg("orphan adopted")

	html := fmt.Sprintf(`<div class="alert alert-success">
		<i class="bi bi-check-circle me-2"></i>
		Adopted as media item: <strong>%s</strong>
	</div>`, mediaItem.Title)

	w.Header().Set("HX-Trigger", "orphan-updated")
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// OrphansDelete deletes a single orphan.
func (h *Handler) OrphansDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orphanID := chi.URLParam(r, "id")

	deleteFile := r.URL.Query().Get("delete_file") == "true"

	if h.mediaService == nil {
		writeHTMXError(w, "Media service not available")
		return
	}

	if err := h.mediaService.DeleteOrphan(ctx, orphanID, deleteFile); err != nil {
		h.logger.Error().Err(err).Str("orphan_id", orphanID).Msg("failed to delete orphan")
		writeHTMXError(w, fmt.Sprintf("Failed to delete: %v", err))
		return
	}

	h.logger.Info().
		Str("orphan_id", orphanID).
		Bool("file_deleted", deleteFile).
		Msg("orphan deleted")

	action := "Record removed"
	if deleteFile {
		action = "File and record deleted"
	}

	html := fmt.Sprintf(`<div class="alert alert-success">
		<i class="bi bi-check-circle me-2"></i>
		%s
	</div>`, action)

	w.Header().Set("HX-Trigger", "orphan-updated")
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// OrphansBulkAdopt adopts multiple orphans to a station.
func (h *Handler) OrphansBulkAdopt(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		writeHTMXError(w, "Invalid form data")
		return
	}

	stationID := r.FormValue("station_id")
	if stationID == "" {
		writeHTMXError(w, "Station ID is required")
		return
	}

	if h.mediaService == nil {
		writeHTMXError(w, "Media service not available")
		return
	}

	// Support select_all=true to operate on all orphans
	orphanIDs := r.Form["orphan_ids"]
	if r.FormValue("select_all") == "true" {
		allIDs, err := h.mediaService.GetAllOrphanIDs(ctx)
		if err != nil {
			h.logger.Error().Err(err).Msg("failed to get all orphan IDs")
			writeHTMXError(w, "Failed to retrieve orphan list")
			return
		}
		orphanIDs = allIDs
	}

	if len(orphanIDs) == 0 {
		writeHTMXError(w, "No orphans selected")
		return
	}

	adopted, err := h.mediaService.BulkAdoptOrphans(ctx, orphanIDs, stationID)
	if err != nil {
		h.logger.Error().Err(err).Msg("bulk adopt failed")
		writeHTMXError(w, fmt.Sprintf("Bulk adopt failed: %v", err))
		return
	}

	h.logger.Info().
		Int("adopted", adopted).
		Int("requested", len(orphanIDs)).
		Str("station_id", stationID).
		Msg("bulk adopt completed")

	html := fmt.Sprintf(`<div class="alert alert-success">
		<i class="bi bi-check-circle me-2"></i>
		Adopted %d of %d orphans
	</div>`, adopted, len(orphanIDs))

	w.Header().Set("HX-Trigger", "orphans-updated")
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// OrphansBulkDelete deletes multiple orphans.
func (h *Handler) OrphansBulkDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		writeHTMXError(w, "Invalid form data")
		return
	}

	deleteFiles := r.FormValue("delete_files") == "true"

	if h.mediaService == nil {
		writeHTMXError(w, "Media service not available")
		return
	}

	// Support select_all=true to operate on all orphans
	orphanIDs := r.Form["orphan_ids"]
	if r.FormValue("select_all") == "true" {
		allIDs, err := h.mediaService.GetAllOrphanIDs(ctx)
		if err != nil {
			h.logger.Error().Err(err).Msg("failed to get all orphan IDs")
			writeHTMXError(w, "Failed to retrieve orphan list")
			return
		}
		orphanIDs = allIDs
	}

	if len(orphanIDs) == 0 {
		writeHTMXError(w, "No orphans selected")
		return
	}

	deleted, err := h.mediaService.BulkDeleteOrphans(ctx, orphanIDs, deleteFiles)
	if err != nil {
		h.logger.Error().Err(err).Msg("bulk delete failed")
		writeHTMXError(w, fmt.Sprintf("Bulk delete failed: %v", err))
		return
	}

	h.logger.Info().
		Int("deleted", deleted).
		Int("requested", len(orphanIDs)).
		Bool("files_deleted", deleteFiles).
		Msg("bulk delete completed")

	action := "records"
	if deleteFiles {
		action = "files and records"
	}

	html := fmt.Sprintf(`<div class="alert alert-success">
		<i class="bi bi-check-circle me-2"></i>
		Deleted %d of %d %s
	</div>`, deleted, len(orphanIDs), action)

	w.Header().Set("HX-Trigger", "orphans-updated")
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// formatBytesHuman converts bytes to human-readable format.
func formatBytesHuman(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), []string{"KB", "MB", "GB", "TB"}[exp])
}
