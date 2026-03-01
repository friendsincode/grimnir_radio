/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// LiveDashboard renders the live DJ control panel
func (h *Handler) LiveDashboard(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	// Get mounts for this station
	var mounts []models.Mount
	h.db.Where("station_id = ?", station.ID).Find(&mounts)

	// Get active live sessions
	var sessions []models.LiveSession
	h.db.Where("station_id = ? AND active = ?", station.ID, true).Find(&sessions)

	// Check if current user has an active session
	user := h.GetUser(r)
	var userSession *models.LiveSession
	for _, s := range sessions {
		if s.UserID == user.ID {
			userSession = &s
			break
		}
	}

	h.Render(w, r, "pages/dashboard/live/dashboard", PageData{
		Title:    "Live DJ",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Mounts":            mounts,
			"Sessions":          sessions,
			"UserSession":       userSession,
			"HarborEnabled":     h.harborEnabled,
			"HarborHost":        h.harborHost,
			"HarborPort":        h.harborPort,
			"HarborMountPrefix": h.harborMountPrefix,
			"HarborSSL":         h.harborSSL,
		},
	})
}

// LiveSessions returns active sessions as HTML partial
func (h *Handler) LiveSessions(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	var sessions []models.LiveSession
	h.db.Where("station_id = ? AND active = ?", station.ID, true).Find(&sessions)

	h.RenderPartial(w, r, "partials/live-sessions", sessions)
}

// LiveGenerateToken generates a new live session token
func (h *Handler) LiveGenerateToken(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	user := h.GetUser(r)

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	mountID := r.FormValue("mount_id")
	if mountID == "" {
		http.Error(w, "Mount ID required", http.StatusBadRequest)
		return
	}

	// Verify mount exists and belongs to station
	var mount models.Mount
	if err := h.db.First(&mount, "id = ? AND station_id = ?", mountID, station.ID).Error; err != nil {
		http.Error(w, "Mount not found", http.StatusNotFound)
		return
	}

	// Generate token via live service
	var token string
	if h.liveSvc != nil {
		var err error
		token, err = h.liveSvc.GenerateToken(r.Context(), station.ID, mountID, user.ID, user.Email)
		if err != nil {
			h.logger.Error().Err(err).Str("station_id", station.ID).Str("user_id", user.ID).Msg("failed to generate live token")
			if r.Header.Get("HX-Request") == "true" {
				w.Write([]byte(`<div class="alert alert-danger">Failed to generate token</div>`))
				return
			}
			http.Error(w, "Failed to generate token", http.StatusInternalServerError)
			return
		}
		h.logger.Info().Str("station_id", station.ID).Str("user_id", user.ID).Msg("live token generated")
	} else {
		h.logger.Error().Msg("live service not available for token generation")
		if r.Header.Get("HX-Request") == "true" {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`<div class="alert alert-danger">Live token service unavailable</div>`))
			return
		}
		http.Error(w, "Live token service unavailable", http.StatusServiceUnavailable)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.RenderPartial(w, r, "partials/live-token", map[string]any{
			"Token":             token,
			"MountID":           mountID,
			"MountName":         mount.Name,
			"HarborEnabled":     h.harborEnabled,
			"HarborHost":        h.harborHost,
			"HarborPort":        h.harborPort,
			"HarborMountPrefix": h.harborMountPrefix,
			"HarborSSL":         h.harborSSL,
		})
		return

	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"token":    token,
		"mount_id": mountID,
	})
}

// LiveConnect handles live session connection
func (h *Handler) LiveConnect(w http.ResponseWriter, r *http.Request) {
	// This endpoint is typically called by the media engine
	// For UI, we just show connection instructions

	http.Error(w, "Use streaming software to connect", http.StatusBadRequest)
}

// LiveDisconnect terminates a live session
func (h *Handler) LiveDisconnect(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	user := h.GetUser(r)

	// Find session
	var session models.LiveSession
	if err := h.db.First(&session, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Check permission - users can disconnect their own sessions, managers+ can disconnect any
	if session.UserID != user.ID && !roleAtLeast(user, "manager") {
		http.Error(w, "Not authorized", http.StatusForbidden)
		return
	}

	// Use live service if available (handles DB update, priority release, events)
	if h.liveSvc != nil {
		if err := h.liveSvc.DisconnectSession(r.Context(), id); err != nil {
			h.logger.Error().Err(err).Str("session_id", id).Msg("failed to disconnect session")
			if r.Header.Get("HX-Request") == "true" {
				w.Write([]byte(`<div class="alert alert-danger">Failed to disconnect session</div>`))
				return
			}
			http.Error(w, "Failed to disconnect", http.StatusInternalServerError)
			return
		}
		h.logger.Info().Str("session_id", id).Msg("live session disconnected")
	} else {
		// Fallback: direct DB update if service not available
		session.Active = false
		if err := h.db.Save(&session).Error; err != nil {
			http.Error(w, "Failed to disconnect", http.StatusInternalServerError)
			return
		}
		h.logger.Warn().Msg("live service not available, disconnect event not emitted")
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		return
	}

	http.Redirect(w, r, "/dashboard/live", http.StatusSeeOther)
}

// LiveHandover initiates a handover to automation
func (h *Handler) LiveHandover(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	user := h.GetUser(r)

	// Find user's active session
	var session models.LiveSession
	if err := h.db.First(&session, "station_id = ? AND user_id = ? AND active = ?",
		station.ID, user.ID, true).Error; err != nil {
		http.Error(w, "No active session found", http.StatusNotFound)
		return
	}

	// Use live service if available
	if h.liveSvc != nil {
		if err := h.liveSvc.InitiateHandover(r.Context(), session.ID, station.ID, session.MountID, user.ID); err != nil {
			h.logger.Error().Err(err).Str("session_id", session.ID).Msg("failed to initiate handover")
			if r.Header.Get("HX-Request") == "true" {
				w.Write([]byte(`<div class="alert alert-danger">Failed to initiate handover</div>`))
				return
			}
			http.Error(w, "Failed to initiate handover", http.StatusInternalServerError)
			return
		}
		h.logger.Info().Str("session_id", session.ID).Msg("live handover initiated")
	} else {
		h.logger.Warn().Msg("live service not available, handover not initiated")
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(`<div class="alert alert-info">Handover initiated - automation will take over after current track</div>`))
		return
	}

	http.Redirect(w, r, "/dashboard/live", http.StatusSeeOther)
}

// LiveReleaseHandover cancels a pending handover
func (h *Handler) LiveReleaseHandover(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	user := h.GetUser(r)

	// Find user's active session
	var session models.LiveSession
	if err := h.db.First(&session, "station_id = ? AND user_id = ? AND active = ?",
		station.ID, user.ID, true).Error; err != nil {
		http.Error(w, "No active session found", http.StatusNotFound)
		return
	}

	// Use live service if available
	if h.liveSvc != nil {
		if err := h.liveSvc.CancelHandover(r.Context(), session.ID); err != nil {
			h.logger.Error().Err(err).Str("session_id", session.ID).Msg("failed to cancel handover")
			if r.Header.Get("HX-Request") == "true" {
				w.Write([]byte(`<div class="alert alert-danger">Failed to cancel handover</div>`))
				return
			}
			http.Error(w, "Failed to cancel handover", http.StatusInternalServerError)
			return
		}
		h.logger.Info().Str("session_id", session.ID).Msg("live handover cancelled")
	} else {
		h.logger.Warn().Msg("live service not available, handover not cancelled")
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(`<div class="alert alert-success">Handover cancelled</div>`))
		return
	}

	http.Redirect(w, r, "/dashboard/live", http.StatusSeeOther)
}
