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

	"github.com/friendsincode/grimnir_radio/internal/analytics"
)

// ScheduleAnalyticsAPI handles schedule analytics endpoints.
type ScheduleAnalyticsAPI struct {
	*API
	analyticsSvc *analytics.ScheduleAnalyticsService
}

// NewScheduleAnalyticsAPI creates a new schedule analytics API handler.
func NewScheduleAnalyticsAPI(api *API, svc *analytics.ScheduleAnalyticsService) *ScheduleAnalyticsAPI {
	return &ScheduleAnalyticsAPI{
		API:          api,
		analyticsSvc: svc,
	}
}

// RegisterRoutes registers schedule analytics routes.
func (a *ScheduleAnalyticsAPI) RegisterRoutes(r chi.Router) {
	r.Route("/schedule-analytics", func(r chi.Router) {
		r.Get("/shows", a.handleShowPerformance)
		r.Get("/time-slots", a.handleTimeSlotPerformance)
		r.Get("/best-slots", a.handleBestTimeSlots)
		r.Get("/suggestions", a.handleSchedulingSuggestions)
		// Maintenance endpoint to backfill daily rollups (admin/manager group already enforced at router mount).
		r.Post("/aggregate-daily", a.handleAggregateDaily)
	})
}

func (a *ScheduleAnalyticsAPI) handleAggregateDaily(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	// Default: yesterday only (UTC)
	end := time.Now().UTC().AddDate(0, 0, -1)
	start := end

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

	// Safety cap: 366 days per request.
	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	endDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	if endDay.Before(startDay) {
		startDay, endDay = endDay, startDay
	}
	if endDay.Sub(startDay) > 366*24*time.Hour {
		endDay = startDay.Add(366 * 24 * time.Hour)
	}

	if err := a.analyticsSvc.BackfillDaily(r.Context(), stationID, startDay, endDay); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to aggregate daily")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"start":  startDay.Format("2006-01-02"),
		"end":    endDay.Format("2006-01-02"),
	})
}

// handleShowPerformance returns performance metrics for shows.
func (a *ScheduleAnalyticsAPI) handleShowPerformance(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	// Parse date range (default to last 30 days)
	end := time.Now()
	start := end.AddDate(0, 0, -30)

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

	performance, err := a.analyticsSvc.GetShowPerformance(r.Context(), stationID, start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get show performance")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"start":       start.Format("2006-01-02"),
		"end":         end.Format("2006-01-02"),
		"performance": performance,
	})
}

// handleTimeSlotPerformance returns performance metrics by time slot.
func (a *ScheduleAnalyticsAPI) handleTimeSlotPerformance(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	end := time.Now()
	start := end.AddDate(0, 0, -30)

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

	slots, err := a.analyticsSvc.GetTimeSlotPerformance(r.Context(), stationID, start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get time slot performance")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"start":      start.Format("2006-01-02"),
		"end":        end.Format("2006-01-02"),
		"time_slots": slots,
	})
}

// handleBestTimeSlots returns the best performing time slots.
func (a *ScheduleAnalyticsAPI) handleBestTimeSlots(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := parseInt(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	slots, err := a.analyticsSvc.GetBestTimeSlots(r.Context(), stationID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get best time slots")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"best_slots": slots,
	})
}

// handleSchedulingSuggestions returns data-driven scheduling suggestions.
func (a *ScheduleAnalyticsAPI) handleSchedulingSuggestions(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	suggestions, err := a.analyticsSvc.GetSchedulingSuggestions(r.Context(), stationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get suggestions")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"suggestions": suggestions,
	})
}

func parseInt(s string) (int, error) {
	var v int
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}
