/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/teambition/rrule-go"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// showCreateRequest is the request body for creating a show.
type showCreateRequest struct {
	StationID              string         `json:"station_id"`
	Name                   string         `json:"name"`
	Description            string         `json:"description"`
	ArtworkPath            string         `json:"artwork_path"`
	HostUserID             *string        `json:"host_user_id"`
	DefaultDurationMinutes int            `json:"default_duration_minutes"`
	Color                  string         `json:"color"`
	RRule                  string         `json:"rrule"`
	DTStart                string         `json:"dtstart"` // RFC3339
	DTEnd                  *string        `json:"dtend"`   // RFC3339, optional
	Timezone               string         `json:"timezone"`
	Metadata               map[string]any `json:"metadata"`
}

// showUpdateRequest is the request body for updating a show.
type showUpdateRequest struct {
	Name                   *string        `json:"name"`
	Description            *string        `json:"description"`
	ArtworkPath            *string        `json:"artwork_path"`
	HostUserID             *string        `json:"host_user_id"`
	DefaultDurationMinutes *int           `json:"default_duration_minutes"`
	Color                  *string        `json:"color"`
	RRule                  *string        `json:"rrule"`
	DTStart                *string        `json:"dtstart"`
	DTEnd                  *string        `json:"dtend"`
	Timezone               *string        `json:"timezone"`
	Active                 *bool          `json:"active"`
	Metadata               map[string]any `json:"metadata"`
}

// showMaterializeRequest is the request to generate instances for a date range.
type showMaterializeRequest struct {
	Start string `json:"start"` // RFC3339
	End   string `json:"end"`   // RFC3339
}

// instanceUpdateRequest is the request for modifying a single show instance.
type instanceUpdateRequest struct {
	StartsAt      *string                    `json:"starts_at"` // RFC3339
	EndsAt        *string                    `json:"ends_at"`   // RFC3339
	HostUserID    *string                    `json:"host_user_id"`
	ExceptionType *models.ShowExceptionType  `json:"exception_type"`
	ExceptionNote *string                    `json:"exception_note"`
	Status        *models.ShowInstanceStatus `json:"status"`
	Metadata      map[string]any             `json:"metadata"`
}

// AddShowRoutes registers show-related routes.
func (a *API) AddShowRoutes(r chi.Router) {
	r.Route("/shows", func(r chi.Router) {
		r.Get("/", a.handleShowsList)
		r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/", a.handleShowsCreate)
		r.Route("/{showID}", func(r chi.Router) {
			r.Get("/", a.handleShowsGet)
			r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Put("/", a.handleShowsUpdate)
			r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Delete("/", a.handleShowsDelete)
			r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/materialize", a.handleShowsMaterialize)
		})
	})

	r.Route("/show-instances", func(r chi.Router) {
		r.Get("/", a.handleInstancesList)
		r.Route("/{instanceID}", func(r chi.Router) {
			r.Get("/", a.handleInstancesGet)
			r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Put("/", a.handleInstancesUpdate)
			r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Delete("/", a.handleInstancesDelete)
		})
	})
}

// handleShowsList returns all shows, optionally filtered by station.
func (a *API) handleShowsList(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	query := a.db.WithContext(r.Context()).Preload("Host")

	if stationID != "" {
		if !a.requireStationAccess(w, r, stationID) {
			return
		}
		query = query.Where("station_id = ?", stationID)
	}

	activeOnly := r.URL.Query().Get("active")
	if activeOnly == "true" {
		query = query.Where("active = ?", true)
	}

	var shows []models.Show
	if err := query.Order("name ASC").Find(&shows).Error; err != nil {
		a.logger.Error().Err(err).Msg("list shows failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"shows": shows})
}

// handleShowsCreate creates a new show with optional RRULE recurrence.
func (a *API) handleShowsCreate(w http.ResponseWriter, r *http.Request) {
	var req showCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.StationID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "station_id_and_name_required")
		return
	}
	if !a.requireStationAccess(w, r, req.StationID) {
		return
	}

	// Parse dtstart
	dtstart, err := time.Parse(time.RFC3339, req.DTStart)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_dtstart")
		return
	}

	// Validate RRULE if provided
	if req.RRule != "" {
		if _, err := rrule.StrToRRule(req.RRule); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_rrule")
			return
		}
	}

	// Parse dtend if provided
	var dtend *time.Time
	if req.DTEnd != nil && *req.DTEnd != "" {
		parsed, err := time.Parse(time.RFC3339, *req.DTEnd)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_dtend")
			return
		}
		dtend = &parsed
	}

	// Default values
	if req.DefaultDurationMinutes <= 0 {
		req.DefaultDurationMinutes = 60
	}
	if req.Timezone == "" {
		req.Timezone = "UTC"
	}

	show := models.Show{
		ID:                     uuid.NewString(),
		StationID:              req.StationID,
		Name:                   req.Name,
		Description:            req.Description,
		ArtworkPath:            req.ArtworkPath,
		HostUserID:             req.HostUserID,
		DefaultDurationMinutes: req.DefaultDurationMinutes,
		Color:                  req.Color,
		RRule:                  req.RRule,
		DTStart:                dtstart,
		DTEnd:                  dtend,
		Timezone:               req.Timezone,
		Active:                 true,
		Metadata:               req.Metadata,
	}

	if err := a.db.WithContext(r.Context()).Create(&show).Error; err != nil {
		a.logger.Error().Err(err).Msg("create show failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	a.logger.Info().
		Str("show_id", show.ID).
		Str("station_id", show.StationID).
		Str("name", show.Name).
		Msg("show created")

	writeJSON(w, http.StatusCreated, show)
}

// handleShowsGet returns a single show by ID.
func (a *API) handleShowsGet(w http.ResponseWriter, r *http.Request) {
	showID := chi.URLParam(r, "showID")
	if showID == "" {
		writeError(w, http.StatusBadRequest, "show_id_required")
		return
	}

	var show models.Show
	result := a.db.WithContext(r.Context()).Preload("Host").First(&show, "id = ?", showID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		a.logger.Error().Err(result.Error).Msg("get show failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	if !a.requireStationAccess(w, r, show.StationID) {
		return
	}

	writeJSON(w, http.StatusOK, show)
}

// handleShowsUpdate updates a show.
func (a *API) handleShowsUpdate(w http.ResponseWriter, r *http.Request) {
	showID := chi.URLParam(r, "showID")
	if showID == "" {
		writeError(w, http.StatusBadRequest, "show_id_required")
		return
	}

	var show models.Show
	result := a.db.WithContext(r.Context()).First(&show, "id = ?", showID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	if !a.requireStationAccess(w, r, show.StationID) {
		return
	}

	var req showUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	updates := make(map[string]any)

	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.ArtworkPath != nil {
		updates["artwork_path"] = *req.ArtworkPath
	}
	if req.HostUserID != nil {
		updates["host_user_id"] = *req.HostUserID
	}
	if req.DefaultDurationMinutes != nil {
		updates["default_duration_minutes"] = *req.DefaultDurationMinutes
	}
	if req.Color != nil {
		updates["color"] = *req.Color
	}
	if req.RRule != nil {
		// Validate RRULE
		if *req.RRule != "" {
			if _, err := rrule.StrToRRule(*req.RRule); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_rrule")
				return
			}
		}
		updates["rrule"] = *req.RRule
	}
	if req.DTStart != nil {
		dtstart, err := time.Parse(time.RFC3339, *req.DTStart)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_dtstart")
			return
		}
		updates["dtstart"] = dtstart
	}
	if req.DTEnd != nil {
		if *req.DTEnd == "" {
			updates["dtend"] = nil
		} else {
			dtend, err := time.Parse(time.RFC3339, *req.DTEnd)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_dtend")
				return
			}
			updates["dtend"] = dtend
		}
	}
	if req.Timezone != nil {
		updates["timezone"] = *req.Timezone
	}
	if req.Active != nil {
		updates["active"] = *req.Active
	}
	if req.Metadata != nil {
		updates["metadata"] = req.Metadata
	}

	if len(updates) == 0 {
		writeJSON(w, http.StatusOK, show)
		return
	}

	if err := a.db.WithContext(r.Context()).Model(&show).Updates(updates).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	// Reload show with updated values
	a.db.WithContext(r.Context()).Preload("Host").First(&show, "id = ?", showID)
	writeJSON(w, http.StatusOK, show)
}

// handleShowsDelete deletes a show and all its future instances.
func (a *API) handleShowsDelete(w http.ResponseWriter, r *http.Request) {
	showID := chi.URLParam(r, "showID")
	if showID == "" {
		writeError(w, http.StatusBadRequest, "show_id_required")
		return
	}

	var show models.Show
	result := a.db.WithContext(r.Context()).First(&show, "id = ?", showID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	if !a.requireStationAccess(w, r, show.StationID) {
		return
	}

	tx := a.db.WithContext(r.Context()).Begin()

	// Delete future instances (keep past ones for history)
	if err := tx.Where("show_id = ? AND starts_at > ?", showID, time.Now()).Delete(&models.ShowInstance{}).Error; err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "delete_instances_failed")
		return
	}

	// Delete the show
	if err := tx.Delete(&show).Error; err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "delete_failed")
		return
	}

	if err := tx.Commit().Error; err != nil {
		writeError(w, http.StatusInternalServerError, "commit_failed")
		return
	}

	a.logger.Info().Str("show_id", showID).Msg("show deleted")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleShowsMaterialize generates show instances for a date range using RRULE.
func (a *API) handleShowsMaterialize(w http.ResponseWriter, r *http.Request) {
	showID := chi.URLParam(r, "showID")
	if showID == "" {
		writeError(w, http.StatusBadRequest, "show_id_required")
		return
	}

	var show models.Show
	result := a.db.WithContext(r.Context()).First(&show, "id = ?", showID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	if !a.requireStationAccess(w, r, show.StationID) {
		return
	}

	var req showMaterializeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	// Parse date range
	start, err := time.Parse(time.RFC3339, req.Start)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_start")
		return
	}
	end, err := time.Parse(time.RFC3339, req.End)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_end")
		return
	}

	// Limit materialization window to 1 year
	if end.Sub(start) > 365*24*time.Hour {
		writeError(w, http.StatusBadRequest, "range_too_large")
		return
	}

	instances, err := a.materializeShow(r.Context(), &show, start, end)
	if err != nil {
		a.logger.Error().Err(err).Str("show_id", showID).Msg("materialize failed")
		writeError(w, http.StatusInternalServerError, "materialize_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"instances": instances,
		"count":     len(instances),
	})
}

// materializeShow generates instances for a show within the given date range.
func (a *API) materializeShow(ctx context.Context, show *models.Show, start, end time.Time) ([]models.ShowInstance, error) {
	duration := time.Duration(show.DefaultDurationMinutes) * time.Minute

	var occurrences []time.Time

	if show.RRule == "" {
		// No recurrence - single instance at DTStart
		if show.DTStart.After(start) && show.DTStart.Before(end) {
			occurrences = []time.Time{show.DTStart}
		}
	} else {
		// Parse and evaluate RRULE
		rr, err := rrule.StrToRRule(show.RRule)
		if err != nil {
			return nil, err
		}

		// Set DTStart for the rule
		rr.DTStart(show.DTStart)

		// Get occurrences in range
		occurrences = rr.Between(start, end, true)
	}

	// Filter out occurrences after show's end date
	if show.DTEnd != nil {
		filtered := make([]time.Time, 0, len(occurrences))
		for _, occ := range occurrences {
			if occ.Before(*show.DTEnd) || occ.Equal(*show.DTEnd) {
				filtered = append(filtered, occ)
			}
		}
		occurrences = filtered
	}

	var created []models.ShowInstance
	for _, startsAt := range occurrences {
		// Check if instance already exists
		var existing models.ShowInstance
		err := a.db.WithContext(ctx).Where("show_id = ? AND starts_at = ?", show.ID, startsAt).First(&existing).Error
		if err == nil {
			// Instance already exists
			created = append(created, existing)
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}

		// Create new instance
		instance := models.ShowInstance{
			ID:         uuid.NewString(),
			ShowID:     show.ID,
			StationID:  show.StationID,
			StartsAt:   startsAt,
			EndsAt:     startsAt.Add(duration),
			HostUserID: show.HostUserID,
			Status:     models.ShowInstanceScheduled,
		}

		if err := a.db.WithContext(ctx).Create(&instance).Error; err != nil {
			return nil, err
		}
		created = append(created, instance)
	}

	return created, nil
}

// handleInstancesList returns show instances with optional filters.
func (a *API) handleInstancesList(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	showID := r.URL.Query().Get("show_id")
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	query := a.db.WithContext(r.Context()).Preload("Show").Preload("Host")

	if stationID != "" {
		if !a.requireStationAccess(w, r, stationID) {
			return
		}
		query = query.Where("station_id = ?", stationID)
	}
	if showID != "" {
		query = query.Where("show_id = ?", showID)
	}

	// Default to next 7 days if no range specified
	start := time.Now()
	end := start.Add(7 * 24 * time.Hour)

	if startStr != "" {
		if parsed, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = parsed
		}
	}
	if endStr != "" {
		if parsed, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = parsed
		}
	}

	query = query.Where("starts_at >= ? AND starts_at <= ?", start, end)

	status := r.URL.Query().Get("status")
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var instances []models.ShowInstance
	if err := query.Order("starts_at ASC").Find(&instances).Error; err != nil {
		a.logger.Error().Err(err).Msg("list instances failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"instances": instances})
}

// handleInstancesGet returns a single show instance.
func (a *API) handleInstancesGet(w http.ResponseWriter, r *http.Request) {
	instanceID := chi.URLParam(r, "instanceID")
	if instanceID == "" {
		writeError(w, http.StatusBadRequest, "instance_id_required")
		return
	}

	var instance models.ShowInstance
	result := a.db.WithContext(r.Context()).Preload("Show").Preload("Host").First(&instance, "id = ?", instanceID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	if !a.requireStationAccess(w, r, instance.StationID) {
		return
	}

	writeJSON(w, http.StatusOK, instance)
}

// handleInstancesUpdate modifies a single instance (exception handling).
func (a *API) handleInstancesUpdate(w http.ResponseWriter, r *http.Request) {
	instanceID := chi.URLParam(r, "instanceID")
	if instanceID == "" {
		writeError(w, http.StatusBadRequest, "instance_id_required")
		return
	}

	var instance models.ShowInstance
	result := a.db.WithContext(r.Context()).First(&instance, "id = ?", instanceID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	if !a.requireStationAccess(w, r, instance.StationID) {
		return
	}

	var req instanceUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	updates := make(map[string]any)

	if req.StartsAt != nil {
		startsAt, err := time.Parse(time.RFC3339, *req.StartsAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_starts_at")
			return
		}
		updates["starts_at"] = startsAt

		// If rescheduling, mark as exception
		if req.ExceptionType == nil {
			exType := models.ShowExceptionRescheduled
			updates["exception_type"] = exType
		}
	}
	if req.EndsAt != nil {
		endsAt, err := time.Parse(time.RFC3339, *req.EndsAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_ends_at")
			return
		}
		updates["ends_at"] = endsAt
	}
	if req.HostUserID != nil {
		updates["host_user_id"] = *req.HostUserID
		// Mark as substitute if host changed
		if req.ExceptionType == nil && *req.HostUserID != "" {
			exType := models.ShowExceptionSubstitute
			updates["exception_type"] = exType
		}
	}
	if req.ExceptionType != nil {
		updates["exception_type"] = *req.ExceptionType
	}
	if req.ExceptionNote != nil {
		updates["exception_note"] = *req.ExceptionNote
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.Metadata != nil {
		updates["metadata"] = req.Metadata
	}

	if len(updates) == 0 {
		writeJSON(w, http.StatusOK, instance)
		return
	}

	if err := a.db.WithContext(r.Context()).Model(&instance).Updates(updates).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	// Reload with relationships
	a.db.WithContext(r.Context()).Preload("Show").Preload("Host").First(&instance, "id = ?", instanceID)
	writeJSON(w, http.StatusOK, instance)
}

// handleInstancesDelete cancels a single instance.
func (a *API) handleInstancesDelete(w http.ResponseWriter, r *http.Request) {
	instanceID := chi.URLParam(r, "instanceID")
	if instanceID == "" {
		writeError(w, http.StatusBadRequest, "instance_id_required")
		return
	}

	var instance models.ShowInstance
	result := a.db.WithContext(r.Context()).First(&instance, "id = ?", instanceID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	if !a.requireStationAccess(w, r, instance.StationID) {
		return
	}

	// Mark as cancelled rather than deleting (preserve history)
	updates := map[string]any{
		"status":         models.ShowInstanceCancelled,
		"exception_type": models.ShowExceptionCancelled,
	}

	if err := a.db.WithContext(r.Context()).Model(&instance).Updates(updates).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "cancel_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}
