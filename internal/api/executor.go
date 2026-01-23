/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package api

import (
	"net/http"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/go-chi/chi/v5"
)

// Executor API handlers

// handleExecutorStates returns all executor states.
func (a *API) handleExecutorStates(w http.ResponseWriter, r *http.Request) {
	states, err := a.executorStateMgr.ListStates(r.Context())
	if err != nil {
		a.logger.Error().Err(err).Msg("list executor states failed")
		writeError(w, http.StatusInternalServerError, "list_states_failed")
		return
	}

	result := make([]map[string]any, len(states))
	for i, state := range states {
		result[i] = serializeExecutorState(state)
	}

	writeJSON(w, http.StatusOK, result)
}

// handleExecutorState returns the executor state for a specific station.
func (a *API) handleExecutorState(w http.ResponseWriter, r *http.Request) {
	stationID := chi.URLParam(r, "stationID")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	state, err := a.executorStateMgr.GetState(r.Context(), stationID)
	if err != nil {
		a.logger.Error().Err(err).Str("station_id", stationID).Msg("get executor state failed")
		writeError(w, http.StatusInternalServerError, "get_state_failed")
		return
	}

	writeJSON(w, http.StatusOK, serializeExecutorState(state))
}

// handleExecutorTelemetry returns real-time telemetry for a station.
func (a *API) handleExecutorTelemetry(w http.ResponseWriter, r *http.Request) {
	stationID := chi.URLParam(r, "stationID")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	state, err := a.executorStateMgr.GetState(r.Context(), stationID)
	if err != nil {
		a.logger.Error().Err(err).Str("station_id", stationID).Msg("get telemetry failed")
		writeError(w, http.StatusInternalServerError, "get_telemetry_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"station_id":      state.StationID,
		"state":           string(state.State),
		"audio_level_l":   state.AudioLevelL,
		"audio_level_r":   state.AudioLevelR,
		"loudness_lufs":   state.LoudnessLUFS,
		"buffer_depth_ms": state.BufferDepthMS,
		"underrun_count":  state.UnderrunCount,
		"last_heartbeat":  state.LastHeartbeat,
		"healthy":         state.IsHealthy(),
		"playing":         state.IsPlaying(),
	})
}

// handleExecutorHealth returns health status for all executors.
func (a *API) handleExecutorHealth(w http.ResponseWriter, r *http.Request) {
	states, err := a.executorStateMgr.ListStates(r.Context())
	if err != nil {
		a.logger.Error().Err(err).Msg("list executor health failed")
		writeError(w, http.StatusInternalServerError, "list_health_failed")
		return
	}

	result := make([]map[string]any, len(states))
	for i, state := range states {
		result[i] = map[string]any{
			"station_id":     state.StationID,
			"state":          string(state.State),
			"healthy":        state.IsHealthy(),
			"playing":        state.IsPlaying(),
			"last_heartbeat": state.LastHeartbeat,
			"underrun_count": state.UnderrunCount,
		}
	}

	writeJSON(w, http.StatusOK, result)
}

// serializeExecutorState converts an executor state to a JSON-friendly map.
func serializeExecutorState(state *models.ExecutorState) map[string]any {
	return map[string]any{
		"id":                state.ID,
		"station_id":        state.StationID,
		"mount_id":          state.MountID,
		"state":             string(state.State),
		"current_priority":  int(state.CurrentPriority),
		"current_source_id": state.CurrentSourceID,
		"next_source_id":    state.NextSourceID,
		"audio_level_l":     state.AudioLevelL,
		"audio_level_r":     state.AudioLevelR,
		"loudness_lufs":     state.LoudnessLUFS,
		"buffer_depth_ms":   state.BufferDepthMS,
		"underrun_count":    state.UnderrunCount,
		"last_heartbeat":    state.LastHeartbeat,
		"healthy":           state.IsHealthy(),
		"playing":           state.IsPlaying(),
		"metadata":          state.Metadata,
		"created_at":        state.CreatedAt,
		"updated_at":        state.UpdatedAt,
	}
}

// AddExecutorRoutes adds executor state routes to the router.
func (a *API) AddExecutorRoutes(r chi.Router) {
	r.Route("/executor", func(r chi.Router) {
		// List all executor states (admin, manager)
		r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Get("/states", a.handleExecutorStates)

		// Get specific executor state
		r.Get("/states/{stationID}", a.handleExecutorState)

		// Get real-time telemetry for a station
		r.Get("/telemetry/{stationID}", a.handleExecutorTelemetry)

		// Health check for all executors
		r.Get("/health", a.handleExecutorHealth)
	})
}
