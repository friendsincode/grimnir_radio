/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/scheduling"
)

// scheduleRuleCreateRequest is the request body for creating a schedule rule.
type scheduleRuleCreateRequest struct {
	StationID string              `json:"station_id"`
	Name      string              `json:"name"`
	RuleType  models.RuleType     `json:"rule_type"`
	Config    map[string]any      `json:"config"`
	Severity  models.RuleSeverity `json:"severity"`
}

// scheduleRuleUpdateRequest is the request body for updating a schedule rule.
type scheduleRuleUpdateRequest struct {
	Name     *string              `json:"name"`
	Config   map[string]any       `json:"config"`
	Severity *models.RuleSeverity `json:"severity"`
	Active   *bool                `json:"active"`
}

// AddScheduleRuleRoutes registers schedule rule routes.
func (a *API) AddScheduleRuleRoutes(r chi.Router) {
	r.Route("/schedule-rules", func(r chi.Router) {
		r.Get("/", a.handleScheduleRulesList)
		r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/", a.handleScheduleRulesCreate)
		r.Route("/{ruleID}", func(r chi.Router) {
			r.Get("/", a.handleScheduleRulesGet)
			r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Put("/", a.handleScheduleRulesUpdate)
			r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Delete("/", a.handleScheduleRulesDelete)
		})
	})

	r.Get("/schedule/validate", a.handleScheduleValidate)
}

// handleScheduleRulesList returns all schedule rules for a station.
func (a *API) handleScheduleRulesList(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	query := a.db.WithContext(r.Context()).Where("station_id = ?", stationID)

	activeOnly := r.URL.Query().Get("active")
	if activeOnly == "true" {
		query = query.Where("active = ?", true)
	}

	var rules []models.ScheduleRule
	if err := query.Order("name ASC").Find(&rules).Error; err != nil {
		a.logger.Error().Err(err).Msg("list schedule rules failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

// handleScheduleRulesCreate creates a new schedule rule.
func (a *API) handleScheduleRulesCreate(w http.ResponseWriter, r *http.Request) {
	var req scheduleRuleCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.StationID == "" || req.Name == "" || req.RuleType == "" {
		writeError(w, http.StatusBadRequest, "station_id_name_and_type_required")
		return
	}

	// Validate rule type
	validTypes := map[models.RuleType]bool{
		models.RuleTypeGap:             true,
		models.RuleTypeDJDoubleBooking: true,
		models.RuleTypeMinDuration:     true,
		models.RuleTypeMaxDuration:     true,
		models.RuleTypeMaxConsecutive:  true,
		models.RuleTypeStationID:       true,
		models.RuleTypeContentRestrict: true,
		models.RuleTypeRequiredBreak:   true,
	}
	if !validTypes[req.RuleType] {
		writeError(w, http.StatusBadRequest, "invalid_rule_type")
		return
	}

	if req.Severity == "" {
		req.Severity = models.RuleSeverityWarning
	}

	if req.Config == nil {
		req.Config = make(map[string]any)
	}

	rule := models.ScheduleRule{
		ID:        uuid.NewString(),
		StationID: req.StationID,
		Name:      req.Name,
		RuleType:  req.RuleType,
		Config:    req.Config,
		Severity:  req.Severity,
		Active:    true,
	}

	if err := a.db.WithContext(r.Context()).Create(&rule).Error; err != nil {
		a.logger.Error().Err(err).Msg("create schedule rule failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	a.logger.Info().
		Str("rule_id", rule.ID).
		Str("station_id", rule.StationID).
		Str("rule_type", string(rule.RuleType)).
		Msg("schedule rule created")

	writeJSON(w, http.StatusCreated, rule)
}

// handleScheduleRulesGet returns a single schedule rule.
func (a *API) handleScheduleRulesGet(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleID")
	if ruleID == "" {
		writeError(w, http.StatusBadRequest, "rule_id_required")
		return
	}

	var rule models.ScheduleRule
	result := a.db.WithContext(r.Context()).First(&rule, "id = ?", ruleID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, rule)
}

// handleScheduleRulesUpdate updates a schedule rule.
func (a *API) handleScheduleRulesUpdate(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleID")
	if ruleID == "" {
		writeError(w, http.StatusBadRequest, "rule_id_required")
		return
	}

	var rule models.ScheduleRule
	result := a.db.WithContext(r.Context()).First(&rule, "id = ?", ruleID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	var req scheduleRuleUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	updates := make(map[string]any)

	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Config != nil {
		updates["config"] = req.Config
	}
	if req.Severity != nil {
		updates["severity"] = *req.Severity
	}
	if req.Active != nil {
		updates["active"] = *req.Active
	}

	if len(updates) == 0 {
		writeJSON(w, http.StatusOK, rule)
		return
	}

	if err := a.db.WithContext(r.Context()).Model(&rule).Updates(updates).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	a.db.WithContext(r.Context()).First(&rule, "id = ?", ruleID)
	writeJSON(w, http.StatusOK, rule)
}

// handleScheduleRulesDelete deletes a schedule rule.
func (a *API) handleScheduleRulesDelete(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleID")
	if ruleID == "" {
		writeError(w, http.StatusBadRequest, "rule_id_required")
		return
	}

	var rule models.ScheduleRule
	result := a.db.WithContext(r.Context()).First(&rule, "id = ?", ruleID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	if err := a.db.WithContext(r.Context()).Delete(&rule).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed")
		return
	}

	a.logger.Info().Str("rule_id", ruleID).Msg("schedule rule deleted")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleScheduleValidate validates the schedule for a station.
func (a *API) handleScheduleValidate(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	// Parse date range
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	start := time.Now()
	end := start.Add(7 * 24 * time.Hour) // Default: next 7 days

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

	// Limit validation range to 90 days
	if end.Sub(start) > 90*24*time.Hour {
		end = start.Add(90 * 24 * time.Hour)
	}

	validator := scheduling.NewValidator(a.db, zerolog.Nop())
	result, err := validator.Validate(stationID, start, end)
	if err != nil {
		a.logger.Error().
			Err(err).
			Str("station_id", stationID).
			Time("range_start", start).
			Time("range_end", end).
			Msg("schedule validation failed")
		writeError(w, http.StatusInternalServerError, "validation_failed")
		return
	}
	a.logValidationSummary(stationID, start, end, result)

	writeJSON(w, http.StatusOK, result)
}

func (a *API) logValidationSummary(stationID string, start, end time.Time, result *models.ValidationResult) {
	if result == nil {
		return
	}

	overlapViolations := make([]models.ValidationViolation, 0)
	collect := func(items []models.ValidationViolation) {
		for _, item := range items {
			if item.RuleType == models.RuleTypeOverlap {
				overlapViolations = append(overlapViolations, item)
			}
		}
	}
	collect(result.Errors)
	collect(result.Warnings)
	collect(result.Info)

	logger := a.logger.With().
		Str("station_id", stationID).
		Time("range_start", start).
		Time("range_end", end).
		Int("overlap_count", len(overlapViolations)).
		Bool("valid", result.Valid).
		Logger()

	if len(overlapViolations) == 0 {
		logger.Info().Msg("schedule validation completed with no overlaps")
		return
	}

	logger.Warn().Msg("schedule validation detected overlaps")
	for i, v := range overlapViolations {
		entry := logger.Warn().
			Int("overlap_index", i+1).
			Time("starts_at", v.StartsAt).
			Time("ends_at", v.EndsAt).
			Strs("affected_ids", v.AffectedIDs)
		if minutes, ok := v.Details["overlap_minutes"]; ok {
			entry = entry.Interface("overlap_minutes", minutes)
		}
		entry.Msg(v.Message)
	}
}
