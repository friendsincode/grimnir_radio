/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/friendsincode/grimnir_radio/internal/audit"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// auditLogResponse is the JSON response for an audit log entry.
type auditLogResponse struct {
	ID           string         `json:"id"`
	Timestamp    time.Time      `json:"timestamp"`
	UserID       *string        `json:"user_id,omitempty"`
	UserEmail    string         `json:"user_email,omitempty"`
	StationID    *string        `json:"station_id,omitempty"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type,omitempty"`
	ResourceID   string         `json:"resource_id,omitempty"`
	Details      map[string]any `json:"details,omitempty"`
	IPAddress    string         `json:"ip_address,omitempty"`
	UserAgent    string         `json:"user_agent,omitempty"`
}

// handleAuditList returns a paginated list of audit logs (platform admin only).
func (a *API) handleAuditList(w http.ResponseWriter, r *http.Request) {
	filters := parseAuditFilters(r)

	logs, total, err := a.auditSvc.Query(r.Context(), filters)
	if err != nil {
		a.logger.Error().Err(err).Msg("failed to query audit logs")
		writeError(w, http.StatusInternalServerError, "query_failed")
		return
	}

	response := make([]auditLogResponse, len(logs))
	for i, log := range logs {
		response[i] = toAuditLogResponse(log)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"audit_logs": response,
		"total":      total,
		"limit":      filters.Limit,
		"offset":     filters.Offset,
	})
}

// handleStationAuditList returns audit logs for a specific station.
func (a *API) handleStationAuditList(w http.ResponseWriter, r *http.Request) {
	stationID := chi.URLParam(r, "stationID")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	filters := parseAuditFilters(r)
	filters.StationID = &stationID

	logs, total, err := a.auditSvc.Query(r.Context(), filters)
	if err != nil {
		a.logger.Error().Err(err).Msg("failed to query station audit logs")
		writeError(w, http.StatusInternalServerError, "query_failed")
		return
	}

	response := make([]auditLogResponse, len(logs))
	for i, log := range logs {
		response[i] = toAuditLogResponse(log)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"audit_logs": response,
		"total":      total,
		"limit":      filters.Limit,
		"offset":     filters.Offset,
		"station_id": stationID,
	})
}

// parseAuditFilters extracts query filters from the request.
func parseAuditFilters(r *http.Request) audit.QueryFilters {
	filters := audit.QueryFilters{
		Limit:  100,
		Offset: 0,
	}

	if userID := r.URL.Query().Get("user_id"); userID != "" {
		filters.UserID = &userID
	}

	if stationID := r.URL.Query().Get("station_id"); stationID != "" {
		filters.StationID = &stationID
	}

	if action := r.URL.Query().Get("action"); action != "" {
		a := models.AuditAction(action)
		filters.Action = &a
	}

	if startTime := r.URL.Query().Get("start_time"); startTime != "" {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			filters.StartTime = &t
		}
	}

	if endTime := r.URL.Query().Get("end_time"); endTime != "" {
		if t, err := time.Parse(time.RFC3339, endTime); err == nil {
			filters.EndTime = &t
		}
	}

	if limit := r.URL.Query().Get("limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil && n > 0 && n <= 1000 {
			filters.Limit = n
		}
	}

	if offset := r.URL.Query().Get("offset"); offset != "" {
		if n, err := strconv.Atoi(offset); err == nil && n >= 0 {
			filters.Offset = n
		}
	}

	return filters
}

// toAuditLogResponse converts an AuditLog model to a response struct.
func toAuditLogResponse(log models.AuditLog) auditLogResponse {
	return auditLogResponse{
		ID:           log.ID,
		Timestamp:    log.Timestamp,
		UserID:       log.UserID,
		UserEmail:    log.UserEmail,
		StationID:    log.StationID,
		Action:       string(log.Action),
		ResourceType: log.ResourceType,
		ResourceID:   log.ResourceID,
		Details:      log.Details,
		IPAddress:    log.IPAddress,
		UserAgent:    log.UserAgent,
	}
}
