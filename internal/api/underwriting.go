/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/underwriting"
)

// UnderwritingAPI handles underwriting endpoints.
type UnderwritingAPI struct {
	*API
	underwritingSvc *underwriting.Service
}

// NewUnderwritingAPI creates a new underwriting API handler.
func NewUnderwritingAPI(api *API, svc *underwriting.Service) *UnderwritingAPI {
	return &UnderwritingAPI{
		API:             api,
		underwritingSvc: svc,
	}
}

// RegisterRoutes registers underwriting routes.
func (u *UnderwritingAPI) RegisterRoutes(r chi.Router) {
	// Sponsors
	r.Route("/sponsors", func(r chi.Router) {
		r.Get("/", u.handleListSponsors)
		r.Post("/", u.handleCreateSponsor)
		r.Get("/{id}", u.handleGetSponsor)
		r.Put("/{id}", u.handleUpdateSponsor)
		r.Delete("/{id}", u.handleDeleteSponsor)
	})

	// Obligations
	r.Route("/obligations", func(r chi.Router) {
		r.Get("/", u.handleListObligations)
		r.Post("/", u.handleCreateObligation)
		r.Get("/{id}", u.handleGetObligation)
		r.Put("/{id}", u.handleUpdateObligation)
		r.Delete("/{id}", u.handleDeleteObligation)
	})

	// Spots
	r.Route("/spots", func(r chi.Router) {
		r.Get("/", u.handleListSpots)
		r.Post("/", u.handleScheduleSpot)
		r.Post("/{id}/aired", u.handleMarkAired)
		r.Post("/{id}/missed", u.handleMarkMissed)
		r.Post("/schedule-weekly", u.handleScheduleWeekly)
	})

	// Reports
	r.Route("/fulfillment", func(r chi.Router) {
		r.Get("/", u.handleFulfillmentReports)
		r.Get("/{obligationID}", u.handleFulfillmentReport)
	})
}

// handleListSponsors lists sponsors for a station.
func (u *UnderwritingAPI) handleListSponsors(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	activeOnly := r.URL.Query().Get("active") != "false"

	sponsors, err := u.underwritingSvc.ListSponsors(r.Context(), stationID, activeOnly)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sponsors")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sponsors": sponsors,
	})
}

// handleCreateSponsor creates a new sponsor.
func (u *UnderwritingAPI) handleCreateSponsor(w http.ResponseWriter, r *http.Request) {
	var sponsor models.Sponsor
	if err := json.NewDecoder(r.Body).Decode(&sponsor); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if sponsor.StationID == "" || sponsor.Name == "" {
		writeError(w, http.StatusBadRequest, "station_id and name required")
		return
	}

	if err := u.underwritingSvc.CreateSponsor(r.Context(), &sponsor); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create sponsor")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"sponsor": sponsor,
	})
}

// handleGetSponsor retrieves a sponsor by ID.
func (u *UnderwritingAPI) handleGetSponsor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	sponsor, err := u.underwritingSvc.GetSponsor(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "sponsor not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sponsor": sponsor,
	})
}

// handleUpdateSponsor updates a sponsor.
func (u *UnderwritingAPI) handleUpdateSponsor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := u.underwritingSvc.UpdateSponsor(r.Context(), id, req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update sponsor")
		return
	}

	sponsor, _ := u.underwritingSvc.GetSponsor(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{
		"sponsor": sponsor,
	})
}

// handleDeleteSponsor deletes a sponsor.
func (u *UnderwritingAPI) handleDeleteSponsor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := u.underwritingSvc.DeleteSponsor(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sponsor")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "sponsor deleted",
	})
}

// handleListObligations lists obligations.
func (u *UnderwritingAPI) handleListObligations(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	sponsorID := r.URL.Query().Get("sponsor_id")
	activeOnly := r.URL.Query().Get("active") != "false"

	obligations, err := u.underwritingSvc.ListObligations(r.Context(), stationID, sponsorID, activeOnly)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list obligations")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"obligations": obligations,
	})
}

// handleCreateObligation creates a new obligation.
func (u *UnderwritingAPI) handleCreateObligation(w http.ResponseWriter, r *http.Request) {
	var obl models.UnderwritingObligation
	if err := json.NewDecoder(r.Body).Decode(&obl); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if obl.SponsorID == "" || obl.StationID == "" || obl.SpotsPerWeek == 0 {
		writeError(w, http.StatusBadRequest, "sponsor_id, station_id, and spots_per_week required")
		return
	}

	if err := u.underwritingSvc.CreateObligation(r.Context(), &obl); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create obligation")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"obligation": obl,
	})
}

// handleGetObligation retrieves an obligation by ID.
func (u *UnderwritingAPI) handleGetObligation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	obl, err := u.underwritingSvc.GetObligation(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "obligation not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"obligation": obl,
	})
}

// handleUpdateObligation updates an obligation.
func (u *UnderwritingAPI) handleUpdateObligation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := u.underwritingSvc.UpdateObligation(r.Context(), id, req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update obligation")
		return
	}

	obl, _ := u.underwritingSvc.GetObligation(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{
		"obligation": obl,
	})
}

// handleDeleteObligation deletes an obligation.
func (u *UnderwritingAPI) handleDeleteObligation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := u.underwritingSvc.DeleteObligation(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete obligation")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "obligation deleted",
	})
}

// handleListSpots lists spots for an obligation.
func (u *UnderwritingAPI) handleListSpots(w http.ResponseWriter, r *http.Request) {
	obligationID := r.URL.Query().Get("obligation_id")
	if obligationID == "" {
		writeError(w, http.StatusBadRequest, "obligation_id required")
		return
	}

	var spots []models.UnderwritingSpot
	query := u.db.Where("obligation_id = ?", obligationID).Order("scheduled_at DESC").Limit(100)

	if status := r.URL.Query().Get("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Find(&spots).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list spots")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"spots": spots,
	})
}

// handleScheduleSpot schedules a single spot.
func (u *UnderwritingAPI) handleScheduleSpot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ObligationID string `json:"obligation_id"`
		ScheduledAt  string `json:"scheduled_at"`
		InstanceID   string `json:"instance_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ObligationID == "" || req.ScheduledAt == "" {
		writeError(w, http.StatusBadRequest, "obligation_id and scheduled_at required")
		return
	}

	scheduledAt, err := time.Parse(time.RFC3339, req.ScheduledAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid scheduled_at format")
		return
	}

	var instanceID *string
	if req.InstanceID != "" {
		instanceID = &req.InstanceID
	}

	spot, err := u.underwritingSvc.ScheduleSpot(r.Context(), req.ObligationID, scheduledAt, instanceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to schedule spot")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"spot": spot,
	})
}

// handleMarkAired marks a spot as aired.
func (u *UnderwritingAPI) handleMarkAired(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := u.underwritingSvc.MarkSpotAired(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark spot aired")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "spot marked as aired",
	})
}

// handleMarkMissed marks a spot as missed.
func (u *UnderwritingAPI) handleMarkMissed(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := u.underwritingSvc.MarkSpotMissed(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark spot missed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "spot marked as missed",
	})
}

// handleScheduleWeekly schedules spots for a week.
func (u *UnderwritingAPI) handleScheduleWeekly(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StationID string `json:"station_id"`
		WeekStart string `json:"week_start"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.StationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	weekStart := time.Now()
	if req.WeekStart != "" {
		if t, err := time.Parse("2006-01-02", req.WeekStart); err == nil {
			weekStart = t
		}
	}

	spots, err := u.underwritingSvc.ScheduleWeeklySpots(r.Context(), req.StationID, weekStart)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to schedule weekly spots")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"spots_scheduled": len(spots),
		"spots":           spots,
	})
}

// handleFulfillmentReports returns fulfillment reports for all obligations.
func (u *UnderwritingAPI) handleFulfillmentReports(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	// Default to current month
	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	periodEnd := periodStart.AddDate(0, 1, 0)

	if start := r.URL.Query().Get("start"); start != "" {
		if t, err := time.Parse("2006-01-02", start); err == nil {
			periodStart = t
		}
	}
	if end := r.URL.Query().Get("end"); end != "" {
		if t, err := time.Parse("2006-01-02", end); err == nil {
			periodEnd = t
		}
	}

	reports, err := u.underwritingSvc.GetAllFulfillmentReports(r.Context(), stationID, periodStart, periodEnd)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get fulfillment reports")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"period_start": periodStart.Format("2006-01-02"),
		"period_end":   periodEnd.Format("2006-01-02"),
		"reports":      reports,
	})
}

// handleFulfillmentReport returns a single fulfillment report.
func (u *UnderwritingAPI) handleFulfillmentReport(w http.ResponseWriter, r *http.Request) {
	obligationID := chi.URLParam(r, "obligationID")

	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	periodEnd := periodStart.AddDate(0, 1, 0)

	if start := r.URL.Query().Get("start"); start != "" {
		if t, err := time.Parse("2006-01-02", start); err == nil {
			periodStart = t
		}
	}
	if end := r.URL.Query().Get("end"); end != "" {
		if t, err := time.Parse("2006-01-02", end); err == nil {
			periodEnd = t
		}
	}

	report, err := u.underwritingSvc.GetFulfillmentReport(r.Context(), obligationID, periodStart, periodEnd)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get fulfillment report")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"report": report,
	})
}
