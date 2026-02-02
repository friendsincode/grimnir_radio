/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/friendsincode/grimnir_radio/internal/schedule"
)

// ScheduleExportAPI handles schedule import/export endpoints.
type ScheduleExportAPI struct {
	*API
	exportSvc *schedule.ExportService
}

// NewScheduleExportAPI creates a new schedule export API handler.
func NewScheduleExportAPI(api *API, svc *schedule.ExportService) *ScheduleExportAPI {
	return &ScheduleExportAPI{
		API:       api,
		exportSvc: svc,
	}
}

// RegisterRoutes registers schedule export routes.
func (e *ScheduleExportAPI) RegisterRoutes(r chi.Router) {
	r.Route("/schedule/export", func(r chi.Router) {
		r.Get("/ical", e.handleExportICal)
		r.Get("/pdf", e.handleExportPDF)
		r.Post("/import/ical", e.handleImportICal)
	})
}

// handleExportICal exports schedule to iCal format.
func (e *ScheduleExportAPI) handleExportICal(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	// Default to next 30 days
	start := time.Now()
	end := start.AddDate(0, 0, 30)

	if startStr := r.URL.Query().Get("start"); startStr != "" {
		if t, err := time.Parse("2006-01-02", startStr); err == nil {
			start = t
		}
	}
	if endStr := r.URL.Query().Get("end"); endStr != "" {
		if t, err := time.Parse("2006-01-02", endStr); err == nil {
			end = t
		}
	}

	result, err := e.exportSvc.ExportToICal(r.Context(), stationID, start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export schedule")
		return
	}

	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", result.Filename))
	w.WriteHeader(http.StatusOK)
	w.Write(result.Data)
}

// handleExportPDF exports schedule to printable HTML/PDF format.
func (e *ScheduleExportAPI) handleExportPDF(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	// Default to next 7 days
	start := time.Now()
	end := start.AddDate(0, 0, 7)

	if startStr := r.URL.Query().Get("start"); startStr != "" {
		if t, err := time.Parse("2006-01-02", startStr); err == nil {
			start = t
		}
	}
	if endStr := r.URL.Query().Get("end"); endStr != "" {
		if t, err := time.Parse("2006-01-02", endStr); err == nil {
			end = t
		}
	}

	html, err := e.exportSvc.ExportToPDF(r.Context(), stationID, start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export schedule")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(html)
}

// handleImportICal imports schedule from iCal file.
func (e *ScheduleExportAPI) handleImportICal(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10 MB max
		writeError(w, http.StatusBadRequest, "failed to parse form")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file required")
		return
	}
	defer file.Close()

	result, err := e.exportSvc.ImportFromICal(r.Context(), stationID, file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to import schedule")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"imported": result.Imported,
		"skipped":  result.Skipped,
		"errors":   result.Errors,
	})
}
