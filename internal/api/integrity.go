/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/integrity"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

type integrityFindingResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Severity   string         `json:"severity"`
	Summary    string         `json:"summary"`
	StationID  *string        `json:"station_id,omitempty"`
	ResourceID string         `json:"resource_id,omitempty"`
	Repairable bool           `json:"repairable"`
	Details    map[string]any `json:"details,omitempty"`
}

type integrityRepairRequest struct {
	Type       string `json:"type"`
	StationID  string `json:"station_id,omitempty"`
	ResourceID string `json:"resource_id,omitempty"`
}

func (a *API) handleIntegrityReport(w http.ResponseWriter, r *http.Request) {
	if a.integritySvc == nil {
		writeError(w, http.StatusServiceUnavailable, "integrity_service_unavailable")
		return
	}

	report, err := a.integritySvc.Scan(r.Context())
	if err != nil {
		a.logger.Error().Err(err).Msg("failed to run integrity scan")
		writeError(w, http.StatusInternalServerError, "scan_failed")
		return
	}

	findings := make([]integrityFindingResponse, len(report.Findings))
	for i, finding := range report.Findings {
		findings[i] = integrityFindingResponse{
			ID:         finding.ID,
			Type:       string(finding.Type),
			Severity:   finding.Severity,
			Summary:    finding.Summary,
			StationID:  finding.StationID,
			ResourceID: finding.ResourceID,
			Repairable: finding.Repairable,
			Details:    finding.Details,
		}
	}

	byType := make(map[string]int, len(report.ByType))
	for k, v := range report.ByType {
		byType[string(k)] = v
	}

	a.logIntegrityScanAudit(r, report.Total, byType)

	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": report.GeneratedAt,
		"total":        report.Total,
		"by_type":      byType,
		"findings":     findings,
	})
}

func (a *API) handleIntegrityRepair(w http.ResponseWriter, r *http.Request) {
	if a.integritySvc == nil {
		writeError(w, http.StatusServiceUnavailable, "integrity_service_unavailable")
		return
	}

	var req integrityRepairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.Type == "" || req.ResourceID == "" {
		writeError(w, http.StatusBadRequest, "type_and_resource_id_required")
		return
	}

	result, err := a.integritySvc.Repair(r.Context(), integrity.RepairInput{
		Type:       integrity.FindingType(req.Type),
		StationID:  req.StationID,
		ResourceID: req.ResourceID,
	})
	if err != nil {
		a.logger.Error().
			Err(err).
			Str("type", req.Type).
			Str("station_id", req.StationID).
			Str("resource_id", req.ResourceID).
			Msg("integrity repair failed")
		writeError(w, http.StatusInternalServerError, "repair_failed")
		return
	}

	a.logIntegrityRepairAudit(r, req, result)

	writeJSON(w, http.StatusOK, map[string]any{
		"changed": result.Changed,
		"message": result.Message,
		"details": result.Details,
	})
}

func (a *API) logIntegrityRepairAudit(r *http.Request, req integrityRepairRequest, result integrity.RepairResult) {
	if a.auditSvc == nil {
		return
	}

	var (
		userID    *string
		userEmail string
	)
	if claims, ok := auth.ClaimsFromContext(r.Context()); ok && claims != nil && claims.UserID != "" {
		userID = &claims.UserID
		var user models.User
		if err := a.db.Select("email").First(&user, "id = ?", claims.UserID).Error; err == nil {
			userEmail = user.Email
		}
	}

	entry := &models.AuditLog{
		Timestamp:    time.Now(),
		UserID:       userID,
		UserEmail:    userEmail,
		Action:       models.AuditActionIntegrityRepair,
		ResourceType: "integrity_finding",
		ResourceID:   req.ResourceID,
		Details: map[string]any{
			"type":      req.Type,
			"stationID": req.StationID,
			"changed":   result.Changed,
			"message":   result.Message,
			"details":   result.Details,
		},
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
	}
	if req.StationID != "" {
		entry.StationID = &req.StationID
	}

	if err := a.auditSvc.Log(r.Context(), entry); err != nil {
		a.logger.Error().Err(err).Msg("failed to write integrity repair audit log")
	}
}

func (a *API) logIntegrityScanAudit(r *http.Request, total int, byType map[string]int) {
	if a.auditSvc == nil {
		return
	}

	var (
		userID    *string
		userEmail string
	)
	if claims, ok := auth.ClaimsFromContext(r.Context()); ok && claims != nil && claims.UserID != "" {
		userID = &claims.UserID
		var user models.User
		if err := a.db.Select("email").First(&user, "id = ?", claims.UserID).Error; err == nil {
			userEmail = user.Email
		}
	}

	entry := &models.AuditLog{
		Timestamp:    time.Now(),
		UserID:       userID,
		UserEmail:    userEmail,
		Action:       models.AuditActionIntegrityScan,
		ResourceType: "integrity_report",
		Details: map[string]any{
			"total":   total,
			"by_type": byType,
		},
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
	}
	if err := a.auditSvc.Log(r.Context(), entry); err != nil {
		a.logger.Error().Err(err).Msg("failed to write integrity scan audit log")
	}
}
