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
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// templateCreateRequest is the request body for creating a schedule template.
type templateCreateRequest struct {
	StationID   string `json:"station_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// If not provided, captures current week's schedule
	StartDate string `json:"start_date,omitempty"` // YYYY-MM-DD, defaults to current week start
	EndDate   string `json:"end_date,omitempty"`   // YYYY-MM-DD, defaults to current week end
}

// templateApplyRequest is the request body for applying a template.
type templateApplyRequest struct {
	TargetDate    string `json:"target_date"`    // YYYY-MM-DD - start of target week
	ClearExisting bool   `json:"clear_existing"` // Remove existing entries in target range
}

// AddScheduleTemplateRoutes registers schedule template routes.
func (a *API) AddScheduleTemplateRoutes(r chi.Router) {
	r.Route("/schedule-templates", func(r chi.Router) {
		r.Get("/", a.handleTemplatesList)
		r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/", a.handleTemplatesCreate)
		r.Route("/{templateID}", func(r chi.Router) {
			r.Get("/", a.handleTemplatesGet)
			r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Put("/", a.handleTemplatesUpdate)
			r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Delete("/", a.handleTemplatesDelete)
			r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/apply", a.handleTemplatesApply)
		})
	})
}

// handleTemplatesList returns all schedule templates for a station.
func (a *API) handleTemplatesList(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	var templates []models.ScheduleTemplate
	query := a.db.WithContext(r.Context()).
		Where("station_id = ?", stationID).
		Preload("CreatedBy").
		Order("created_at DESC")

	if err := query.Find(&templates).Error; err != nil {
		a.logger.Error().Err(err).Msg("list schedule templates failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"templates": templates})
}

// handleTemplatesCreate creates a new schedule template from current schedule.
func (a *API) handleTemplatesCreate(w http.ResponseWriter, r *http.Request) {
	var req templateCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.StationID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "station_id_and_name_required")
		return
	}

	// Determine date range for template capture
	now := time.Now()
	var startDate, endDate time.Time

	if req.StartDate != "" {
		parsed, err := time.Parse("2006-01-02", req.StartDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_start_date")
			return
		}
		startDate = parsed
	} else {
		// Default to start of current week (Sunday)
		startDate = now.AddDate(0, 0, -int(now.Weekday()))
		startDate = time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, now.Location())
	}

	if req.EndDate != "" {
		parsed, err := time.Parse("2006-01-02", req.EndDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_end_date")
			return
		}
		endDate = parsed.Add(24*time.Hour - time.Second) // End of day
	} else {
		// Default to end of current week (Saturday 23:59:59)
		endDate = startDate.AddDate(0, 0, 7).Add(-time.Second)
	}

	// Capture schedule entries in range
	var entries []models.ScheduleEntry
	if err := a.db.WithContext(r.Context()).
		Where("station_id = ? AND starts_at >= ? AND starts_at < ?", req.StationID, startDate, endDate).
		Order("starts_at ASC").
		Find(&entries).Error; err != nil {
		a.logger.Error().Err(err).Msg("fetch entries for template failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Capture show instances in range
	var instances []models.ShowInstance
	if err := a.db.WithContext(r.Context()).
		Preload("Show").
		Where("station_id = ? AND starts_at >= ? AND starts_at < ?", req.StationID, startDate, endDate).
		Order("starts_at ASC").
		Find(&instances).Error; err != nil {
		a.logger.Error().Err(err).Msg("fetch show instances for template failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Convert to template entries (relative to week start)
	templateEntries := make([]models.TemplateEntry, 0, len(entries)+len(instances))

	for _, entry := range entries {
		te := models.TemplateEntry{
			DayOfWeek:       int(entry.StartsAt.Weekday()),
			StartTime:       entry.StartsAt.Format("15:04"),
			DurationMinutes: int(entry.EndsAt.Sub(entry.StartsAt).Minutes()),
			SourceType:      entry.SourceType,
			SourceID:        entry.SourceID,
			Title:           entry.SourceType, // Will be enriched on apply
			Metadata:        entry.Metadata,
		}
		templateEntries = append(templateEntries, te)
	}

	for _, inst := range instances {
		showName := ""
		if inst.Show != nil {
			showName = inst.Show.Name
		}
		te := models.TemplateEntry{
			DayOfWeek:       int(inst.StartsAt.Weekday()),
			StartTime:       inst.StartsAt.Format("15:04"),
			DurationMinutes: int(inst.EndsAt.Sub(inst.StartsAt).Minutes()),
			SourceType:      "show",
			ShowID:          inst.ShowID,
			ShowName:        showName,
			Title:           showName,
		}
		templateEntries = append(templateEntries, te)
	}

	// Get user ID if authenticated
	var createdByID *string
	if claims, ok := auth.ClaimsFromContext(r.Context()); ok && claims.UserID != "" {
		createdByID = &claims.UserID
	}

	template := models.ScheduleTemplate{
		ID:          uuid.NewString(),
		StationID:   req.StationID,
		Name:        req.Name,
		Description: req.Description,
		TemplateData: map[string]any{
			"entries":    templateEntries,
			"start_date": startDate.Format("2006-01-02"),
			"end_date":   endDate.Format("2006-01-02"),
		},
		CreatedByID: createdByID,
	}

	if err := a.db.WithContext(r.Context()).Create(&template).Error; err != nil {
		a.logger.Error().Err(err).Msg("create schedule template failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	a.logger.Info().
		Str("template_id", template.ID).
		Str("station_id", template.StationID).
		Int("entry_count", len(templateEntries)).
		Msg("schedule template created")

	writeJSON(w, http.StatusCreated, template)
}

// handleTemplatesGet returns a single schedule template.
func (a *API) handleTemplatesGet(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")
	if templateID == "" {
		writeError(w, http.StatusBadRequest, "template_id_required")
		return
	}

	var template models.ScheduleTemplate
	result := a.db.WithContext(r.Context()).
		Preload("CreatedBy").
		First(&template, "id = ?", templateID)

	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, template)
}

// handleTemplatesUpdate updates a schedule template.
func (a *API) handleTemplatesUpdate(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")
	if templateID == "" {
		writeError(w, http.StatusBadRequest, "template_id_required")
		return
	}

	var template models.ScheduleTemplate
	result := a.db.WithContext(r.Context()).First(&template, "id = ?", templateID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
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

	if len(updates) == 0 {
		writeJSON(w, http.StatusOK, template)
		return
	}

	if err := a.db.WithContext(r.Context()).Model(&template).Updates(updates).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	a.db.WithContext(r.Context()).First(&template, "id = ?", templateID)
	writeJSON(w, http.StatusOK, template)
}

// handleTemplatesDelete deletes a schedule template.
func (a *API) handleTemplatesDelete(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")
	if templateID == "" {
		writeError(w, http.StatusBadRequest, "template_id_required")
		return
	}

	var template models.ScheduleTemplate
	result := a.db.WithContext(r.Context()).First(&template, "id = ?", templateID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	if err := a.db.WithContext(r.Context()).Delete(&template).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed")
		return
	}

	a.logger.Info().Str("template_id", templateID).Msg("schedule template deleted")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleTemplatesApply applies a template to a target date range.
func (a *API) handleTemplatesApply(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")
	if templateID == "" {
		writeError(w, http.StatusBadRequest, "template_id_required")
		return
	}

	var req templateApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.TargetDate == "" {
		writeError(w, http.StatusBadRequest, "target_date_required")
		return
	}

	targetStart, err := time.Parse("2006-01-02", req.TargetDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_target_date")
		return
	}

	// Get template
	var template models.ScheduleTemplate
	result := a.db.WithContext(r.Context()).First(&template, "id = ?", templateID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "template_not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Parse template entries
	entriesRaw, ok := template.TemplateData["entries"]
	if !ok {
		writeError(w, http.StatusInternalServerError, "invalid_template_data")
		return
	}

	// Convert entries from map to struct
	entriesJSON, err := json.Marshal(entriesRaw)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid_template_data")
		return
	}

	var entries []models.TemplateEntry
	if err := json.Unmarshal(entriesJSON, &entries); err != nil {
		writeError(w, http.StatusInternalServerError, "invalid_template_data")
		return
	}

	targetEnd := targetStart.AddDate(0, 0, 7)

	// Create version snapshot before applying
	if err := a.createScheduleVersion(r.Context(), template.StationID, "apply_template", "Applied template: "+template.Name); err != nil {
		a.logger.Warn().Err(err).Msg("failed to create version before template apply")
	}

	// Optionally clear existing entries
	if req.ClearExisting {
		if err := a.db.WithContext(r.Context()).
			Where("station_id = ? AND starts_at >= ? AND starts_at < ?", template.StationID, targetStart, targetEnd).
			Delete(&models.ScheduleEntry{}).Error; err != nil {
			a.logger.Error().Err(err).Msg("clear existing entries failed")
			writeError(w, http.StatusInternalServerError, "clear_failed")
			return
		}
	}

	// Apply template entries
	var created int
	for _, te := range entries {
		// Calculate actual date/time
		entryDate := targetStart.AddDate(0, 0, te.DayOfWeek)
		startTime, err := time.Parse("15:04", te.StartTime)
		if err != nil {
			continue
		}
		startsAt := time.Date(entryDate.Year(), entryDate.Month(), entryDate.Day(),
			startTime.Hour(), startTime.Minute(), 0, 0, targetStart.Location())
		endsAt := startsAt.Add(time.Duration(te.DurationMinutes) * time.Minute)

		if te.SourceType == "show" && te.ShowID != "" {
			// Create show instance
			instance := models.ShowInstance{
				ID:        uuid.NewString(),
				ShowID:    te.ShowID,
				StationID: template.StationID,
				StartsAt:  startsAt,
				EndsAt:    endsAt,
				Status:    models.ShowInstanceScheduled,
			}
			if err := a.db.WithContext(r.Context()).Create(&instance).Error; err != nil {
				a.logger.Warn().Err(err).Str("show_id", te.ShowID).Msg("create show instance from template failed")
				continue
			}
		} else if te.SourceType != "" && te.SourceID != "" {
			// Create schedule entry
			entry := models.ScheduleEntry{
				ID:         uuid.NewString(),
				StationID:  template.StationID,
				StartsAt:   startsAt,
				EndsAt:     endsAt,
				SourceType: te.SourceType,
				SourceID:   te.SourceID,
				Metadata:   te.Metadata,
			}
			if err := a.db.WithContext(r.Context()).Create(&entry).Error; err != nil {
				a.logger.Warn().Err(err).Msg("create entry from template failed")
				continue
			}
		}
		created++
	}

	a.logger.Info().
		Str("template_id", templateID).
		Str("station_id", template.StationID).
		Str("target_date", req.TargetDate).
		Int("created", created).
		Msg("template applied")

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "applied",
		"created": created,
	})
}
