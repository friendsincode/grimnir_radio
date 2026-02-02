/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/notifications"
)

// NotificationAPI handles notification-related API endpoints.
type NotificationAPI struct {
	svc *notifications.Service
}

// NewNotificationAPI creates a new notification API handler.
func NewNotificationAPI(svc *notifications.Service) *NotificationAPI {
	return &NotificationAPI{svc: svc}
}

// RegisterRoutes adds notification routes to the router.
func (n *NotificationAPI) RegisterRoutes(r chi.Router) {
	r.Route("/notifications", func(r chi.Router) {
		// User notifications
		r.Get("/", n.handleList)
		r.Get("/unread-count", n.handleUnreadCount)
		r.Post("/mark-all-read", n.handleMarkAllRead)
		r.Post("/{id}/read", n.handleMarkRead)

		// Preferences
		r.Get("/preferences", n.handleGetPreferences)
		r.Put("/preferences/{id}", n.handleUpdatePreference)
	})
}

// handleList returns the user's notifications.
func (n *NotificationAPI) handleList(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse query params
	unreadOnly := r.URL.Query().Get("unread_only") == "true"
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	if limit <= 0 || limit > 100 {
		limit = 50
	}

	notifications, total, err := n.svc.GetUserNotifications(r.Context(), claims.UserID, unreadOnly, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": notifications,
		"total":         total,
		"limit":         limit,
		"offset":        offset,
	})
}

// handleUnreadCount returns the count of unread notifications.
func (n *NotificationAPI) handleUnreadCount(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	count, err := n.svc.GetUnreadCount(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"unread_count": count,
	})
}

// handleMarkRead marks a single notification as read.
func (n *NotificationAPI) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	notificationID := chi.URLParam(r, "id")
	if notificationID == "" {
		writeError(w, http.StatusBadRequest, "id_required")
		return
	}

	if err := n.svc.MarkAsRead(r.Context(), notificationID, claims.UserID); err != nil {
		if err.Error() == "notification not found" {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleMarkAllRead marks all notifications as read.
func (n *NotificationAPI) handleMarkAllRead(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := n.svc.MarkAllAsRead(r.Context(), claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleGetPreferences returns the user's notification preferences.
func (n *NotificationAPI) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	prefs, err := n.svc.GetUserPreferences(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// If no preferences, create defaults
	if len(prefs) == 0 {
		if err := n.svc.CreateDefaultPreferences(r.Context(), claims.UserID); err != nil {
			writeError(w, http.StatusInternalServerError, "db_error")
			return
		}
		prefs, _ = n.svc.GetUserPreferences(r.Context(), claims.UserID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"preferences": prefs,
	})
}

// handleUpdatePreference updates a notification preference.
func (n *NotificationAPI) handleUpdatePreference(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	prefID := chi.URLParam(r, "id")
	if prefID == "" {
		writeError(w, http.StatusBadRequest, "id_required")
		return
	}

	var req struct {
		Enabled bool           `json:"enabled"`
		Config  map[string]any `json:"config,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := n.svc.UpdatePreference(r.Context(), prefID, claims.UserID, req.Enabled, req.Config); err != nil {
		if err.Error() == "preference not found" {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
