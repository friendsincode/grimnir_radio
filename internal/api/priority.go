/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"net/http"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
	"github.com/go-chi/chi/v5"
)

// Priority API request/response types

type insertEmergencyRequest struct {
	StationID string         `json:"station_id"`
	MediaID   string         `json:"media_id"`
	MountID   string         `json:"mount_id"`
	Metadata  map[string]any `json:"metadata"`
}

type startOverrideRequest struct {
	StationID  string         `json:"station_id"`
	MountID    string         `json:"mount_id"`
	SourceType string         `json:"source_type"`
	SourceID   string         `json:"source_id"`
	Metadata   map[string]any `json:"metadata"`
}

type releaseRequest struct {
	StationID string `json:"station_id"`
	SourceID  string `json:"source_id"`
}

// handlePriorityEmergency inserts emergency broadcast content.
func (a *API) handlePriorityEmergency(w http.ResponseWriter, r *http.Request) {
	var req insertEmergencyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.StationID == "" || req.MediaID == "" {
		writeError(w, http.StatusBadRequest, "station_id_and_media_id_required")
		return
	}

	priorityReq := priority.InsertEmergencyRequest{
		StationID: req.StationID,
		MediaID:   req.MediaID,
		MountID:   req.MountID,
		Metadata:  req.Metadata,
	}

	result, err := a.prioritySvc.InsertEmergency(r.Context(), priorityReq)
	if err != nil {
		a.logger.Error().Err(err).Msg("emergency insertion failed")
		writeError(w, http.StatusInternalServerError, "emergency_insertion_failed")
		return
	}

	// Publish audit event with user context (the priority service event doesn't include user info)
	a.publishAuditEvent(r, events.EventPriorityEmergency, events.Payload{
		"station_id":    req.StationID,
		"resource_type": "priority_source",
		"resource_id":   result.NewSource.ID,
		"media_id":      req.MediaID,
		"mount_id":      req.MountID,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "emergency_activated",
		"preempted":       result.Preempted,
		"transition_type": string(result.TransitionType),
		"source_id":       result.NewSource.ID,
	})
}

// handlePriorityOverride starts a manual DJ override.
func (a *API) handlePriorityOverride(w http.ResponseWriter, r *http.Request) {
	var req startOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.StationID == "" || req.SourceID == "" {
		writeError(w, http.StatusBadRequest, "station_id_and_source_id_required")
		return
	}

	// Validate source type
	var sourceType models.SourceType
	switch req.SourceType {
	case "live":
		sourceType = models.SourceTypeLive
	case "media":
		sourceType = models.SourceTypeMedia
	default:
		writeError(w, http.StatusBadRequest, "invalid_source_type")
		return
	}

	priorityReq := priority.StartOverrideRequest{
		StationID:  req.StationID,
		MountID:    req.MountID,
		SourceType: sourceType,
		SourceID:   req.SourceID,
		Metadata:   req.Metadata,
	}

	result, err := a.prioritySvc.StartOverride(r.Context(), priorityReq)
	if err != nil {
		a.logger.Error().Err(err).Msg("override start failed")
		writeError(w, http.StatusInternalServerError, "override_start_failed")
		return
	}

	if result.TransitionType == priority.TransitionNone {
		writeJSON(w, http.StatusConflict, map[string]any{
			"status":  "no_preemption",
			"message": "Cannot preempt current priority source",
		})
		return
	}

	// Publish audit event with user context
	a.publishAuditEvent(r, events.EventPriorityOverride, events.Payload{
		"station_id":    req.StationID,
		"resource_type": "priority_source",
		"resource_id":   result.NewSource.ID,
		"source_type":   req.SourceType,
		"source_id":     req.SourceID,
		"mount_id":      req.MountID,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "override_activated",
		"preempted":       result.Preempted,
		"transition_type": string(result.TransitionType),
		"requires_fade":   result.RequiresFade,
		"source_id":       result.NewSource.ID,
	})
}

// handlePriorityRelease releases a priority source.
func (a *API) handlePriorityRelease(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")
	if sourceID == "" {
		writeError(w, http.StatusBadRequest, "source_id_required")
		return
	}

	var req releaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.StationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	result, err := a.prioritySvc.Release(r.Context(), req.StationID, sourceID)
	if err != nil {
		a.logger.Error().Err(err).Msg("release failed")
		writeError(w, http.StatusInternalServerError, "release_failed")
		return
	}

	// Publish audit event with user context
	a.publishAuditEvent(r, events.EventPriorityReleased, events.Payload{
		"station_id":    req.StationID,
		"resource_type": "priority_source",
		"resource_id":   sourceID,
	})

	response := map[string]any{
		"status":          "released",
		"transition_type": string(result.TransitionType),
	}

	if result.NewSource != nil {
		response["next_source_id"] = result.NewSource.ID
		response["next_priority"] = int(result.NewSource.Priority)
	}

	writeJSON(w, http.StatusOK, response)
}

// handlePriorityCurrent returns the current active priority source.
func (a *API) handlePriorityCurrent(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	source, err := a.prioritySvc.GetCurrent(r.Context(), stationID)
	if err != nil {
		a.logger.Error().Err(err).Msg("get current priority failed")
		writeError(w, http.StatusInternalServerError, "get_current_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"source_id":     source.ID,
		"station_id":    source.StationID,
		"mount_id":      source.MountID,
		"priority":      int(source.Priority),
		"priority_name": source.Priority.String(),
		"source_type":   string(source.SourceType),
		"active":        source.Active,
		"activated_at":  source.ActivatedAt,
		"metadata":      source.Metadata,
	})
}

// handlePriorityActive returns all active priority sources for a station.
func (a *API) handlePriorityActive(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	sources, err := a.prioritySvc.GetActive(r.Context(), stationID)
	if err != nil {
		a.logger.Error().Err(err).Msg("get active priorities failed")
		writeError(w, http.StatusInternalServerError, "get_active_failed")
		return
	}

	result := make([]map[string]any, len(sources))
	for i, source := range sources {
		result[i] = map[string]any{
			"source_id":     source.ID,
			"station_id":    source.StationID,
			"mount_id":      source.MountID,
			"priority":      int(source.Priority),
			"priority_name": source.Priority.String(),
			"source_type":   string(source.SourceType),
			"active":        source.Active,
			"activated_at":  source.ActivatedAt,
			"metadata":      source.Metadata,
		}
	}

	writeJSON(w, http.StatusOK, result)
}

// AddPriorityRoutes adds priority management routes to the router.
func (a *API) AddPriorityRoutes(r chi.Router) {
	r.Route("/priority", func(r chi.Router) {
		// Emergency broadcasts (admin only)
		r.With(a.requireRoles(models.RoleAdmin)).Post("/emergency", a.handlePriorityEmergency)

		// Live overrides (admin, manager, DJ)
		r.With(a.requireRoles(models.RoleAdmin, models.RoleManager, models.RoleDJ)).Post("/override", a.handlePriorityOverride)

		// Release priority source (admin, manager)
		r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Delete("/{sourceID}", a.handlePriorityRelease)

		// Query current priority (all authenticated users)
		r.Get("/current", a.handlePriorityCurrent)
		r.Get("/active", a.handlePriorityActive)
	})
}
