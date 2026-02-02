/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/webhooks"
)

// WebhookAPI handles webhook management endpoints.
type WebhookAPI struct {
	*API
	webhookSvc *webhooks.Service
}

// NewWebhookAPI creates a new webhook API handler.
func NewWebhookAPI(api *API, webhookSvc *webhooks.Service) *WebhookAPI {
	return &WebhookAPI{
		API:        api,
		webhookSvc: webhookSvc,
	}
}

// RegisterRoutes registers webhook API routes.
func (w *WebhookAPI) RegisterRoutes(r chi.Router) {
	r.Route("/webhooks", func(r chi.Router) {
		r.Get("/", w.handleList)
		r.Post("/", w.handleCreate)
		r.Get("/{id}", w.handleGet)
		r.Put("/{id}", w.handleUpdate)
		r.Delete("/{id}", w.handleDelete)
		r.Post("/{id}/test", w.handleTest)
		r.Get("/{id}/logs", w.handleLogs)
		// Track start webhook (admin only)
		r.With(w.requireRoles(models.RoleAdmin)).Post("/track-start", w.handleWebhookTrackStart)
	})
}

// handleList returns all webhooks for the station.
func (w *WebhookAPI) handleList(rw http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(rw, http.StatusBadRequest, "station_id required")
		return
	}

	// Check station access
	if !w.hasStationAccess(r, stationID, "manager") {
		writeError(rw, http.StatusForbidden, "access denied")
		return
	}

	var webhooksList []models.WebhookTarget
	if err := w.db.Where("station_id = ?", stationID).Order("created_at DESC").Find(&webhooksList).Error; err != nil {
		writeError(rw, http.StatusInternalServerError, "failed to fetch webhooks")
		return
	}

	writeJSON(rw, http.StatusOK, map[string]any{
		"webhooks": webhooksList,
	})
}

// handleCreate creates a new webhook.
func (w *WebhookAPI) handleCreate(rw http.ResponseWriter, r *http.Request) {
	var req struct {
		StationID string `json:"station_id"`
		URL       string `json:"url"`
		Events    string `json:"events"` // comma-separated: show_start,show_end
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(rw, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.StationID == "" {
		writeError(rw, http.StatusBadRequest, "station_id required")
		return
	}

	if req.URL == "" {
		writeError(rw, http.StatusBadRequest, "url required")
		return
	}

	// Check station access
	if !w.hasStationAccess(r, req.StationID, "manager") {
		writeError(rw, http.StatusForbidden, "access denied")
		return
	}

	webhook := models.NewWebhookTarget(req.StationID, req.URL, req.Events)

	if err := w.db.Create(webhook).Error; err != nil {
		writeError(rw, http.StatusInternalServerError, "failed to create webhook")
		return
	}

	writeJSON(rw, http.StatusCreated, map[string]any{
		"webhook": webhook,
		"secret":  webhook.Secret, // Return secret only on create
	})
}

// handleGet returns a specific webhook.
func (w *WebhookAPI) handleGet(rw http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var webhook models.WebhookTarget
	if err := w.db.First(&webhook, "id = ?", id).Error; err != nil {
		writeError(rw, http.StatusNotFound, "webhook not found")
		return
	}

	// Check station access
	if !w.hasStationAccess(r, webhook.StationID, "manager") {
		writeError(rw, http.StatusForbidden, "access denied")
		return
	}

	writeJSON(rw, http.StatusOK, map[string]any{
		"webhook": webhook,
	})
}

// handleUpdate updates a webhook.
func (w *WebhookAPI) handleUpdate(rw http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var webhook models.WebhookTarget
	if err := w.db.First(&webhook, "id = ?", id).Error; err != nil {
		writeError(rw, http.StatusNotFound, "webhook not found")
		return
	}

	// Check station access
	if !w.hasStationAccess(r, webhook.StationID, "manager") {
		writeError(rw, http.StatusForbidden, "access denied")
		return
	}

	var req struct {
		URL    *string `json:"url,omitempty"`
		Events *string `json:"events,omitempty"`
		Active *bool   `json:"active,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(rw, http.StatusBadRequest, "invalid request body")
		return
	}

	updates := make(map[string]any)
	if req.URL != nil {
		updates["url"] = *req.URL
	}
	if req.Events != nil {
		updates["events"] = *req.Events
	}
	if req.Active != nil {
		updates["active"] = *req.Active
	}

	if len(updates) > 0 {
		if err := w.db.Model(&webhook).Updates(updates).Error; err != nil {
			writeError(rw, http.StatusInternalServerError, "failed to update webhook")
			return
		}
	}

	// Reload webhook
	w.db.First(&webhook, "id = ?", id)

	writeJSON(rw, http.StatusOK, map[string]any{
		"webhook": webhook,
	})
}

// handleDelete deletes a webhook.
func (w *WebhookAPI) handleDelete(rw http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var webhook models.WebhookTarget
	if err := w.db.First(&webhook, "id = ?", id).Error; err != nil {
		writeError(rw, http.StatusNotFound, "webhook not found")
		return
	}

	// Check station access
	if !w.hasStationAccess(r, webhook.StationID, "manager") {
		writeError(rw, http.StatusForbidden, "access denied")
		return
	}

	// Delete associated logs
	w.db.Where("target_id = ?", id).Delete(&models.WebhookLog{})

	// Delete webhook
	if err := w.db.Delete(&webhook).Error; err != nil {
		writeError(rw, http.StatusInternalServerError, "failed to delete webhook")
		return
	}

	writeJSON(rw, http.StatusOK, map[string]any{
		"message": "webhook deleted",
	})
}

// handleTest sends a test webhook.
func (w *WebhookAPI) handleTest(rw http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var webhook models.WebhookTarget
	if err := w.db.First(&webhook, "id = ?", id).Error; err != nil {
		writeError(rw, http.StatusNotFound, "webhook not found")
		return
	}

	// Check station access
	if !w.hasStationAccess(r, webhook.StationID, "manager") {
		writeError(rw, http.StatusForbidden, "access denied")
		return
	}

	if err := w.webhookSvc.TestWebhook(&webhook); err != nil {
		writeJSON(rw, http.StatusOK, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(rw, http.StatusOK, map[string]any{
		"success": true,
		"message": "test webhook sent successfully",
	})
}

// handleLogs returns webhook delivery logs.
func (w *WebhookAPI) handleLogs(rw http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var webhook models.WebhookTarget
	if err := w.db.First(&webhook, "id = ?", id).Error; err != nil {
		writeError(rw, http.StatusNotFound, "webhook not found")
		return
	}

	// Check station access
	if !w.hasStationAccess(r, webhook.StationID, "manager") {
		writeError(rw, http.StatusForbidden, "access denied")
		return
	}

	var logs []models.WebhookLog
	if err := w.db.Where("target_id = ?", id).Order("created_at DESC").Limit(100).Find(&logs).Error; err != nil {
		writeError(rw, http.StatusInternalServerError, "failed to fetch logs")
		return
	}

	writeJSON(rw, http.StatusOK, map[string]any{
		"logs": logs,
	})
}

// hasStationAccess checks if the user has the required role for the station.
func (w *WebhookAPI) hasStationAccess(r *http.Request, stationID, minRole string) bool {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		return false
	}

	// Platform admin has access to all stations
	for _, role := range claims.Roles {
		if role == string(models.PlatformRoleAdmin) {
			return true
		}
	}

	// Check station membership
	var stationUser models.StationUser
	if err := w.db.Where("user_id = ? AND station_id = ?", claims.UserID, stationID).First(&stationUser).Error; err != nil {
		return false
	}

	// Check role hierarchy
	roleHierarchy := map[string]int{
		"dj":      1,
		"manager": 2,
		"admin":   3,
	}

	userRoleLevel := roleHierarchy[string(stationUser.Role)]
	requiredLevel := roleHierarchy[minRole]

	return userRoleLevel >= requiredLevel
}
