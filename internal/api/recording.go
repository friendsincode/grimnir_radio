/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/recording"
	"github.com/go-chi/chi/v5"
)

// RecordingAPI handles recording-related HTTP endpoints.
type RecordingAPI struct {
	api *API
	svc *recording.Service
}

// NewRecordingAPI creates a new recording API handler.
func NewRecordingAPI(api *API, svc *recording.Service) *RecordingAPI {
	return &RecordingAPI{api: api, svc: svc}
}

// RegisterRoutes mounts recording routes onto the router.
func (ra *RecordingAPI) RegisterRoutes(r chi.Router) {
	r.Route("/recordings", func(r chi.Router) {
		// Start/stop recording (DJ+)
		r.With(ra.api.requireRoles(models.RoleAdmin, models.RoleManager, models.RoleDJ)).
			Post("/start", ra.handleStartRecording)
		r.With(ra.api.requireRoles(models.RoleAdmin, models.RoleManager, models.RoleDJ)).
			Post("/{recordingID}/stop", ra.handleStopRecording)

		// List recordings (DJ+)
		r.With(ra.api.requireRoles(models.RoleAdmin, models.RoleManager, models.RoleDJ)).
			Get("/", ra.handleListRecordings)

		// Get single recording (DJ+)
		r.With(ra.api.requireRoles(models.RoleAdmin, models.RoleManager, models.RoleDJ)).
			Get("/{recordingID}", ra.handleGetRecording)

		// Update recording metadata (DJ+)
		r.With(ra.api.requireRoles(models.RoleAdmin, models.RoleManager, models.RoleDJ)).
			Patch("/{recordingID}", ra.handleUpdateRecording)

		// Delete recording (manager+)
		r.With(ra.api.requireRoles(models.RoleAdmin, models.RoleManager)).
			Delete("/{recordingID}", ra.handleDeleteRecording)

		// Chapter management
		r.With(ra.api.requireRoles(models.RoleAdmin, models.RoleManager, models.RoleDJ)).
			Post("/{recordingID}/chapters", ra.handleAddChapter)
		r.With(ra.api.requireRoles(models.RoleAdmin, models.RoleManager, models.RoleDJ)).
			Get("/{recordingID}/chapters", ra.handleListChapters)

		// Quota info (DJ+)
		r.With(ra.api.requireRoles(models.RoleAdmin, models.RoleManager, models.RoleDJ)).
			Get("/quota", ra.handleGetQuota)
	})
}

func (ra *RecordingAPI) handleStartRecording(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		StationID string `json:"station_id"`
		MountID   string `json:"mount_id"`
		Title     string `json:"title"`
		Format    string `json:"format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.StationID == "" || req.MountID == "" {
		writeError(w, http.StatusBadRequest, "station_and_mount_required")
		return
	}
	if !ra.api.requireStationAccess(w, r, req.StationID) {
		return
	}

	rec, err := ra.svc.StartRecording(r.Context(), recording.StartRequest{
		StationID: req.StationID,
		MountID:   req.MountID,
		UserID:    claims.UserID,
		Title:     req.Title,
		Format:    req.Format,
	})
	if err != nil {
		ra.api.logger.Error().Err(err).Msg("start recording failed")
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ra.api.bus.Publish(events.EventRecordingStarted, events.Payload{
		"recording_id": rec.ID,
		"station_id":   rec.StationID,
		"mount_id":     rec.MountID,
		"user_id":      rec.UserID,
		"format":       rec.Format,
	})

	writeJSON(w, http.StatusCreated, rec)
}

func (ra *RecordingAPI) handleStopRecording(w http.ResponseWriter, r *http.Request) {
	recordingID := chi.URLParam(r, "recordingID")
	if recordingID == "" {
		writeError(w, http.StatusBadRequest, "recording_id_required")
		return
	}

	rec, err := ra.svc.StopRecording(r.Context(), recordingID)
	if err != nil {
		ra.api.logger.Error().Err(err).Str("recording_id", recordingID).Msg("stop recording failed")
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ra.api.bus.Publish(events.EventRecordingStopped, events.Payload{
		"recording_id": rec.ID,
		"station_id":   rec.StationID,
		"size_bytes":   rec.SizeBytes,
		"duration_ms":  rec.DurationMs,
	})

	writeJSON(w, http.StatusOK, rec)
}

func (ra *RecordingAPI) handleListRecordings(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}
	if !ra.api.requireStationAccess(w, r, stationID) {
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	recordings, total, err := ra.svc.ListRecordings(r.Context(), stationID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"recordings": recordings,
		"total":      total,
		"limit":      limit,
		"offset":     offset,
	})
}

func (ra *RecordingAPI) handleGetRecording(w http.ResponseWriter, r *http.Request) {
	recordingID := chi.URLParam(r, "recordingID")
	if recordingID == "" {
		writeError(w, http.StatusBadRequest, "recording_id_required")
		return
	}

	rec, err := ra.svc.GetRecording(r.Context(), recordingID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if !ra.api.requireStationAccess(w, r, rec.StationID) {
		return
	}

	writeJSON(w, http.StatusOK, rec)
}

func (ra *RecordingAPI) handleUpdateRecording(w http.ResponseWriter, r *http.Request) {
	recordingID := chi.URLParam(r, "recordingID")
	if recordingID == "" {
		writeError(w, http.StatusBadRequest, "recording_id_required")
		return
	}

	var req struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Visibility  *string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	// Fetch the recording to check station access
	rec, err := ra.svc.GetRecording(r.Context(), recordingID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if !ra.api.requireStationAccess(w, r, rec.StationID) {
		return
	}

	updates := map[string]any{}
	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Visibility != nil {
		updates["visibility"] = *req.Visibility
	}

	if len(updates) == 0 {
		writeJSON(w, http.StatusOK, rec)
		return
	}

	if err := ra.svc.UpdateRecording(r.Context(), recordingID, updates); err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	// Reload
	rec, _ = ra.svc.GetRecording(r.Context(), recordingID)
	writeJSON(w, http.StatusOK, rec)
}

func (ra *RecordingAPI) handleDeleteRecording(w http.ResponseWriter, r *http.Request) {
	recordingID := chi.URLParam(r, "recordingID")
	if recordingID == "" {
		writeError(w, http.StatusBadRequest, "recording_id_required")
		return
	}

	// Fetch the recording to check station access
	rec, err := ra.svc.GetRecording(r.Context(), recordingID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if !ra.api.requireStationAccess(w, r, rec.StationID) {
		return
	}

	if err := ra.svc.DeleteRecording(r.Context(), recordingID); err != nil {
		ra.api.logger.Error().Err(err).Str("recording_id", recordingID).Msg("delete recording failed")
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (ra *RecordingAPI) handleAddChapter(w http.ResponseWriter, r *http.Request) {
	recordingID := chi.URLParam(r, "recordingID")
	if recordingID == "" {
		writeError(w, http.StatusBadRequest, "recording_id_required")
		return
	}

	var req struct {
		Title  string `json:"title"`
		Artist string `json:"artist"`
		Album  string `json:"album"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := ra.svc.AddChapter(r.Context(), recordingID, req.Title, req.Artist, req.Album); err != nil {
		ra.api.logger.Error().Err(err).Str("recording_id", recordingID).Msg("add chapter failed")
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ra.api.bus.Publish(events.EventRecordingChapter, events.Payload{
		"recording_id": recordingID,
		"title":        req.Title,
		"artist":       req.Artist,
	})

	writeJSON(w, http.StatusCreated, map[string]string{"status": "chapter_added"})
}

func (ra *RecordingAPI) handleListChapters(w http.ResponseWriter, r *http.Request) {
	recordingID := chi.URLParam(r, "recordingID")
	if recordingID == "" {
		writeError(w, http.StatusBadRequest, "recording_id_required")
		return
	}

	rec, err := ra.svc.GetRecording(r.Context(), recordingID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"chapters": rec.Chapters,
	})
}

func (ra *RecordingAPI) handleGetQuota(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}
	if !ra.api.requireStationAccess(w, r, stationID) {
		return
	}

	quota, err := ra.svc.GetQuotaUsage(r.Context(), stationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "quota_lookup_failed")
		return
	}

	writeJSON(w, http.StatusOK, quota)
}
