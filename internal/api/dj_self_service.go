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
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// availabilityRequest is the request body for availability operations.
type availabilityRequest struct {
	StationID    *string `json:"station_id"`
	DayOfWeek    *int    `json:"day_of_week"`
	SpecificDate *string `json:"specific_date"` // YYYY-MM-DD
	StartTime    string  `json:"start_time"`    // HH:MM
	EndTime      string  `json:"end_time"`      // HH:MM
	Available    bool    `json:"available"`
	Note         string  `json:"note"`
}

// scheduleRequestCreate is the request body for creating a schedule request.
type scheduleRequestCreate struct {
	StationID        string         `json:"station_id"`
	RequestType      string         `json:"request_type"`
	TargetInstanceID *string        `json:"target_instance_id"`
	SwapWithUserID   *string        `json:"swap_with_user_id"`
	ProposedData     map[string]any `json:"proposed_data"`
}

// AddDJSelfServiceRoutes registers DJ self-service routes.
func (a *API) AddDJSelfServiceRoutes(r chi.Router) {
	// DJ availability (own)
	r.Route("/dj/availability", func(r chi.Router) {
		r.Get("/", a.handleGetMyAvailability)
		r.Post("/", a.handleCreateAvailability)
		r.Put("/{id}", a.handleUpdateAvailability)
		r.Delete("/{id}", a.handleDeleteAvailability)
	})

	// View other DJ's availability (manager+)
	r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).
		Get("/users/{userID}/availability", a.handleGetUserAvailability)

	// Schedule requests
	r.Route("/schedule-requests", func(r chi.Router) {
		r.Get("/", a.handleListScheduleRequests)
		r.Post("/", a.handleCreateScheduleRequest)
		r.Route("/{requestID}", func(r chi.Router) {
			r.Get("/", a.handleGetScheduleRequest)
			r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).
				Put("/approve", a.handleApproveScheduleRequest)
			r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).
				Put("/reject", a.handleRejectScheduleRequest)
			r.Delete("/", a.handleCancelScheduleRequest) // Requester can cancel own
		})
	})

	// Schedule locks (admin only)
	r.Route("/schedule-locks", func(r chi.Router) {
		r.Get("/", a.handleGetScheduleLock)
		r.With(a.requireRoles(models.RoleAdmin)).Put("/", a.handleUpdateScheduleLock)
	})
}

// ===== Availability Handlers =====

// handleGetMyAvailability returns the current user's availability.
func (a *API) handleGetMyAvailability(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	stationID := r.URL.Query().Get("station_id")

	query := a.db.WithContext(r.Context()).Where("user_id = ?", claims.UserID)
	if stationID != "" {
		query = query.Where("station_id = ? OR station_id IS NULL", stationID)
	}

	var availability []models.DJAvailability
	if err := query.Order("day_of_week ASC, start_time ASC").Find(&availability).Error; err != nil {
		a.logger.Error().Err(err).Msg("get availability failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"availability": availability})
}

// handleGetUserAvailability returns a specific user's availability (manager+).
func (a *API) handleGetUserAvailability(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id_required")
		return
	}

	stationID := r.URL.Query().Get("station_id")

	query := a.db.WithContext(r.Context()).Where("user_id = ?", userID)
	if stationID != "" {
		query = query.Where("station_id = ? OR station_id IS NULL", stationID)
	}

	var availability []models.DJAvailability
	if err := query.Order("day_of_week ASC, start_time ASC").Find(&availability).Error; err != nil {
		a.logger.Error().Err(err).Msg("get user availability failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"availability": availability})
}

// handleCreateAvailability creates a new availability entry.
func (a *API) handleCreateAvailability(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req availabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.StartTime == "" || req.EndTime == "" {
		writeError(w, http.StatusBadRequest, "start_time_and_end_time_required")
		return
	}

	// Must have either day_of_week or specific_date
	if req.DayOfWeek == nil && req.SpecificDate == nil {
		writeError(w, http.StatusBadRequest, "day_of_week_or_specific_date_required")
		return
	}

	avail := models.DJAvailability{
		ID:        uuid.NewString(),
		UserID:    claims.UserID,
		StationID: req.StationID,
		DayOfWeek: req.DayOfWeek,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
		Available: req.Available,
		Note:      req.Note,
	}

	if req.SpecificDate != nil {
		parsed, err := time.Parse("2006-01-02", *req.SpecificDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_specific_date")
			return
		}
		avail.SpecificDate = &parsed
	}

	if err := a.db.WithContext(r.Context()).Create(&avail).Error; err != nil {
		a.logger.Error().Err(err).Msg("create availability failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusCreated, avail)
}

// handleUpdateAvailability updates an availability entry.
func (a *API) handleUpdateAvailability(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	availID := chi.URLParam(r, "id")
	if availID == "" {
		writeError(w, http.StatusBadRequest, "id_required")
		return
	}

	var avail models.DJAvailability
	result := a.db.WithContext(r.Context()).First(&avail, "id = ?", availID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Only owner can update
	if avail.UserID != claims.UserID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req availabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	updates := make(map[string]any)
	if req.StartTime != "" {
		updates["start_time"] = req.StartTime
	}
	if req.EndTime != "" {
		updates["end_time"] = req.EndTime
	}
	if req.DayOfWeek != nil {
		updates["day_of_week"] = *req.DayOfWeek
	}
	if req.SpecificDate != nil {
		parsed, err := time.Parse("2006-01-02", *req.SpecificDate)
		if err == nil {
			updates["specific_date"] = parsed
		}
	}
	updates["available"] = req.Available
	if req.Note != "" {
		updates["note"] = req.Note
	}

	if err := a.db.WithContext(r.Context()).Model(&avail).Updates(updates).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	a.db.WithContext(r.Context()).First(&avail, "id = ?", availID)
	writeJSON(w, http.StatusOK, avail)
}

// handleDeleteAvailability deletes an availability entry.
func (a *API) handleDeleteAvailability(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	availID := chi.URLParam(r, "id")
	if availID == "" {
		writeError(w, http.StatusBadRequest, "id_required")
		return
	}

	var avail models.DJAvailability
	result := a.db.WithContext(r.Context()).First(&avail, "id = ?", availID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Only owner can delete
	if avail.UserID != claims.UserID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := a.db.WithContext(r.Context()).Delete(&avail).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ===== Schedule Request Handlers =====

// handleListScheduleRequests returns schedule requests.
func (a *API) handleListScheduleRequests(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	status := r.URL.Query().Get("status")

	query := a.db.WithContext(r.Context()).
		Where("station_id = ?", stationID).
		Preload("Requester").
		Preload("TargetInstance").
		Preload("TargetInstance.Show").
		Preload("SwapWithUser").
		Preload("Reviewer")

	// Non-managers only see their own requests
	isManager := false
	for _, role := range claims.Roles {
		if role == string(models.RoleAdmin) || role == string(models.RoleManager) {
			isManager = true
			break
		}
	}
	if !isManager {
		query = query.Where("requester_id = ?", claims.UserID)
	}

	if status != "" {
		query = query.Where("status = ?", status)
	}

	var requests []models.ScheduleRequest
	if err := query.Order("created_at DESC").Find(&requests).Error; err != nil {
		a.logger.Error().Err(err).Msg("list schedule requests failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"requests": requests})
}

// handleCreateScheduleRequest creates a new schedule request.
func (a *API) handleCreateScheduleRequest(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req scheduleRequestCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.StationID == "" || req.RequestType == "" {
		writeError(w, http.StatusBadRequest, "station_id_and_request_type_required")
		return
	}

	// Validate request type
	validTypes := map[models.RequestType]bool{
		models.RequestTypeNewShow:    true,
		models.RequestTypeSwap:       true,
		models.RequestTypeCancel:     true,
		models.RequestTypeTimeOff:    true,
		models.RequestTypeReschedule: true,
	}
	if !validTypes[models.RequestType(req.RequestType)] {
		writeError(w, http.StatusBadRequest, "invalid_request_type")
		return
	}

	// Check schedule lock
	var lock models.ScheduleLock
	if err := a.db.WithContext(r.Context()).Where("station_id = ?", req.StationID).First(&lock).Error; err == nil {
		// Lock exists, check if target date is locked
		if req.TargetInstanceID != nil {
			var instance models.ShowInstance
			if a.db.WithContext(r.Context()).First(&instance, "id = ?", *req.TargetInstanceID).Error == nil {
				if lock.IsLocked(instance.StartsAt) {
					writeError(w, http.StatusForbidden, "schedule_locked")
					return
				}
			}
		}
	}

	schedReq := models.ScheduleRequest{
		ID:               uuid.NewString(),
		StationID:        req.StationID,
		RequestType:      models.RequestType(req.RequestType),
		RequesterID:      claims.UserID,
		TargetInstanceID: req.TargetInstanceID,
		SwapWithUserID:   req.SwapWithUserID,
		ProposedData:     req.ProposedData,
		Status:           models.RequestStatusPending,
	}

	if err := a.db.WithContext(r.Context()).Create(&schedReq).Error; err != nil {
		a.logger.Error().Err(err).Msg("create schedule request failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	a.logger.Info().
		Str("request_id", schedReq.ID).
		Str("requester_id", claims.UserID).
		Str("request_type", string(schedReq.RequestType)).
		Msg("schedule request created")

	// Reload with relationships
	a.db.WithContext(r.Context()).
		Preload("Requester").
		Preload("TargetInstance").
		Preload("TargetInstance.Show").
		First(&schedReq, "id = ?", schedReq.ID)

	writeJSON(w, http.StatusCreated, schedReq)
}

// handleGetScheduleRequest returns a single schedule request.
func (a *API) handleGetScheduleRequest(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	requestID := chi.URLParam(r, "requestID")
	if requestID == "" {
		writeError(w, http.StatusBadRequest, "request_id_required")
		return
	}

	var schedReq models.ScheduleRequest
	result := a.db.WithContext(r.Context()).
		Preload("Requester").
		Preload("TargetInstance").
		Preload("TargetInstance.Show").
		Preload("SwapWithUser").
		Preload("Reviewer").
		First(&schedReq, "id = ?", requestID)

	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Check permission
	isManager := false
	for _, role := range claims.Roles {
		if role == string(models.RoleAdmin) || role == string(models.RoleManager) {
			isManager = true
			break
		}
	}
	if !isManager && schedReq.RequesterID != claims.UserID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	writeJSON(w, http.StatusOK, schedReq)
}

// handleApproveScheduleRequest approves a schedule request.
func (a *API) handleApproveScheduleRequest(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	requestID := chi.URLParam(r, "requestID")
	if requestID == "" {
		writeError(w, http.StatusBadRequest, "request_id_required")
		return
	}

	var schedReq models.ScheduleRequest
	result := a.db.WithContext(r.Context()).First(&schedReq, "id = ?", requestID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	if schedReq.Status != models.RequestStatusPending {
		writeError(w, http.StatusBadRequest, "request_not_pending")
		return
	}

	var body struct {
		Note string `json:"note"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	now := time.Now()
	updates := map[string]any{
		"status":      models.RequestStatusApproved,
		"reviewed_by": claims.UserID,
		"reviewed_at": now,
		"review_note": body.Note,
	}

	if err := a.db.WithContext(r.Context()).Model(&schedReq).Updates(updates).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	// Apply the request based on type
	a.applyScheduleRequest(r.Context(), &schedReq)

	a.logger.Info().
		Str("request_id", requestID).
		Str("reviewer_id", claims.UserID).
		Msg("schedule request approved")

	a.db.WithContext(r.Context()).
		Preload("Requester").
		Preload("Reviewer").
		First(&schedReq, "id = ?", requestID)

	writeJSON(w, http.StatusOK, schedReq)
}

// handleRejectScheduleRequest rejects a schedule request.
func (a *API) handleRejectScheduleRequest(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	requestID := chi.URLParam(r, "requestID")
	if requestID == "" {
		writeError(w, http.StatusBadRequest, "request_id_required")
		return
	}

	var schedReq models.ScheduleRequest
	result := a.db.WithContext(r.Context()).First(&schedReq, "id = ?", requestID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	if schedReq.Status != models.RequestStatusPending {
		writeError(w, http.StatusBadRequest, "request_not_pending")
		return
	}

	var body struct {
		Note string `json:"note"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	now := time.Now()
	updates := map[string]any{
		"status":      models.RequestStatusRejected,
		"reviewed_by": claims.UserID,
		"reviewed_at": now,
		"review_note": body.Note,
	}

	if err := a.db.WithContext(r.Context()).Model(&schedReq).Updates(updates).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	a.logger.Info().
		Str("request_id", requestID).
		Str("reviewer_id", claims.UserID).
		Msg("schedule request rejected")

	a.db.WithContext(r.Context()).
		Preload("Requester").
		Preload("Reviewer").
		First(&schedReq, "id = ?", requestID)

	writeJSON(w, http.StatusOK, schedReq)
}

// handleCancelScheduleRequest allows requester to cancel their own request.
func (a *API) handleCancelScheduleRequest(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	requestID := chi.URLParam(r, "requestID")
	if requestID == "" {
		writeError(w, http.StatusBadRequest, "request_id_required")
		return
	}

	var schedReq models.ScheduleRequest
	result := a.db.WithContext(r.Context()).First(&schedReq, "id = ?", requestID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Only requester can cancel
	if schedReq.RequesterID != claims.UserID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	if schedReq.Status != models.RequestStatusPending {
		writeError(w, http.StatusBadRequest, "request_not_pending")
		return
	}

	if err := a.db.WithContext(r.Context()).Model(&schedReq).
		Update("status", models.RequestStatusCancelled).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// applyScheduleRequest applies an approved request to the schedule.
func (a *API) applyScheduleRequest(ctx context.Context, req *models.ScheduleRequest) {
	switch req.RequestType {
	case models.RequestTypeCancel:
		if req.TargetInstanceID != nil {
			a.db.WithContext(ctx).Model(&models.ShowInstance{}).
				Where("id = ?", *req.TargetInstanceID).
				Update("status", models.ShowInstanceCancelled)
		}

	case models.RequestTypeReschedule:
		if req.TargetInstanceID != nil && req.ProposedData != nil {
			updates := make(map[string]any)
			if startsAt, ok := req.ProposedData["starts_at"].(string); ok {
				if t, err := time.Parse(time.RFC3339, startsAt); err == nil {
					updates["starts_at"] = t
				}
			}
			if endsAt, ok := req.ProposedData["ends_at"].(string); ok {
				if t, err := time.Parse(time.RFC3339, endsAt); err == nil {
					updates["ends_at"] = t
				}
			}
			if len(updates) > 0 {
				updates["exception_type"] = models.ShowExceptionRescheduled
				a.db.WithContext(ctx).Model(&models.ShowInstance{}).
					Where("id = ?", *req.TargetInstanceID).
					Updates(updates)
			}
		}

	case models.RequestTypeSwap:
		if req.TargetInstanceID != nil && req.SwapWithUserID != nil {
			a.db.WithContext(ctx).Model(&models.ShowInstance{}).
				Where("id = ?", *req.TargetInstanceID).
				Updates(map[string]any{
					"host_user_id":   *req.SwapWithUserID,
					"exception_type": models.ShowExceptionSubstitute,
				})
		}
	}
}

// ===== Schedule Lock Handlers =====

// handleGetScheduleLock returns the schedule lock settings.
func (a *API) handleGetScheduleLock(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	var lock models.ScheduleLock
	result := a.db.WithContext(r.Context()).Where("station_id = ?", stationID).First(&lock)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		// Return defaults
		writeJSON(w, http.StatusOK, models.ScheduleLock{
			StationID:      stationID,
			LockBeforeDays: 7,
			MinBypassRole:  models.RoleManager,
		})
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, lock)
}

// handleUpdateScheduleLock updates the schedule lock settings.
func (a *API) handleUpdateScheduleLock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StationID      string   `json:"station_id"`
		LockBeforeDays int      `json:"lock_before_days"`
		MinBypassRole  string   `json:"min_bypass_role"`
		LockedDates    []string `json:"locked_dates"` // YYYY-MM-DD
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.StationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	// Parse locked dates
	var lockedDates []time.Time
	for _, d := range req.LockedDates {
		if parsed, err := time.Parse("2006-01-02", d); err == nil {
			lockedDates = append(lockedDates, parsed)
		}
	}

	var lock models.ScheduleLock
	result := a.db.WithContext(r.Context()).Where("station_id = ?", req.StationID).First(&lock)

	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		// Create new
		lock = models.ScheduleLock{
			ID:             uuid.NewString(),
			StationID:      req.StationID,
			LockBeforeDays: req.LockBeforeDays,
			MinBypassRole:  models.RoleName(req.MinBypassRole),
			LockedDates:    lockedDates,
		}
		if lock.LockBeforeDays == 0 {
			lock.LockBeforeDays = 7
		}
		if lock.MinBypassRole == "" {
			lock.MinBypassRole = models.RoleManager
		}
		if err := a.db.WithContext(r.Context()).Create(&lock).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "db_error")
			return
		}
	} else if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	} else {
		// Update existing
		updates := map[string]any{
			"lock_before_days": req.LockBeforeDays,
			"locked_dates":     lockedDates,
		}
		if req.MinBypassRole != "" {
			updates["min_bypass_role"] = req.MinBypassRole
		}
		if err := a.db.WithContext(r.Context()).Model(&lock).Updates(updates).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "update_failed")
			return
		}
		a.db.WithContext(r.Context()).First(&lock, "id = ?", lock.ID)
	}

	writeJSON(w, http.StatusOK, lock)
}
