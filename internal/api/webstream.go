package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/go-chi/chi/v5"
)

// Webstream API request/response types

type createWebstreamRequest struct {
	StationID             string                 `json:"station_id"`
	Name                  string                 `json:"name"`
	Description           string                 `json:"description"`
	URLs                  []string               `json:"urls"`
	HealthCheckEnabled    bool                   `json:"health_check_enabled"`
	HealthCheckIntervalSec int                   `json:"health_check_interval_sec"`
	HealthCheckTimeoutSec  int                   `json:"health_check_timeout_sec"`
	HealthCheckMethod     string                 `json:"health_check_method"`
	FailoverEnabled       bool                   `json:"failover_enabled"`
	FailoverGraceMs       int                    `json:"failover_grace_ms"`
	AutoRecoverEnabled    bool                   `json:"auto_recover_enabled"`
	PreflightCheck        bool                   `json:"preflight_check"`
	BufferSizeMS          int                    `json:"buffer_size_ms"`
	ReconnectDelayMS      int                    `json:"reconnect_delay_ms"`
	MaxReconnectAttempts  int                    `json:"max_reconnect_attempts"`
	PassthroughMetadata   bool                   `json:"passthrough_metadata"`
	OverrideMetadata      bool                   `json:"override_metadata"`
	CustomMetadata        map[string]any         `json:"custom_metadata"`
}

type updateWebstreamRequest struct {
	Name                  *string                 `json:"name,omitempty"`
	Description           *string                 `json:"description,omitempty"`
	URLs                  []string                `json:"urls,omitempty"`
	HealthCheckEnabled    *bool                   `json:"health_check_enabled,omitempty"`
	HealthCheckIntervalSec *int                   `json:"health_check_interval_sec,omitempty"`
	HealthCheckTimeoutSec  *int                   `json:"health_check_timeout_sec,omitempty"`
	HealthCheckMethod     *string                 `json:"health_check_method,omitempty"`
	FailoverEnabled       *bool                   `json:"failover_enabled,omitempty"`
	FailoverGraceMs       *int                    `json:"failover_grace_ms,omitempty"`
	AutoRecoverEnabled    *bool                   `json:"auto_recover_enabled,omitempty"`
	PreflightCheck        *bool                   `json:"preflight_check,omitempty"`
	BufferSizeMS          *int                    `json:"buffer_size_ms,omitempty"`
	ReconnectDelayMS      *int                    `json:"reconnect_delay_ms,omitempty"`
	MaxReconnectAttempts  *int                    `json:"max_reconnect_attempts,omitempty"`
	PassthroughMetadata   *bool                   `json:"passthrough_metadata,omitempty"`
	OverrideMetadata      *bool                   `json:"override_metadata,omitempty"`
	CustomMetadata        map[string]any          `json:"custom_metadata,omitempty"`
}

type webstreamResponse struct {
	ID                   string         `json:"id"`
	StationID            string         `json:"station_id"`
	Name                 string         `json:"name"`
	Description          string         `json:"description"`
	URLs                 []string       `json:"urls"`
	HealthCheckEnabled   bool           `json:"health_check_enabled"`
	HealthCheckInterval  int            `json:"health_check_interval_sec"`
	HealthCheckTimeout   int            `json:"health_check_timeout_sec"`
	HealthCheckMethod    string         `json:"health_check_method"`
	FailoverEnabled      bool           `json:"failover_enabled"`
	FailoverGraceMs      int            `json:"failover_grace_ms"`
	AutoRecoverEnabled   bool           `json:"auto_recover_enabled"`
	PreflightCheck       bool           `json:"preflight_check"`
	BufferSizeMS         int            `json:"buffer_size_ms"`
	ReconnectDelayMS     int            `json:"reconnect_delay_ms"`
	MaxReconnectAttempts int            `json:"max_reconnect_attempts"`
	PassthroughMetadata  bool           `json:"passthrough_metadata"`
	OverrideMetadata     bool           `json:"override_metadata"`
	CustomMetadata       map[string]any `json:"custom_metadata,omitempty"`
	Active               bool           `json:"active"`
	CurrentURL           string         `json:"current_url"`
	CurrentIndex         int            `json:"current_index"`
	HealthStatus         string         `json:"health_status"`
	LastHealthCheck      *time.Time     `json:"last_health_check,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

// Webstream API handlers

func (a *API) handleCreateWebstream(w http.ResponseWriter, r *http.Request) {
	var req createWebstreamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	// Validate required fields
	if req.StationID == "" || req.Name == "" || len(req.URLs) == 0 {
		writeError(w, http.StatusBadRequest, "station_name_urls_required")
		return
	}

	// Build webstream model
	ws := &models.Webstream{
		StationID:             req.StationID,
		Name:                  req.Name,
		Description:           req.Description,
		URLs:                  req.URLs,
		HealthCheckEnabled:    req.HealthCheckEnabled,
		HealthCheckInterval:   time.Duration(req.HealthCheckIntervalSec) * time.Second,
		HealthCheckTimeout:    time.Duration(req.HealthCheckTimeoutSec) * time.Second,
		HealthCheckMethod:     req.HealthCheckMethod,
		FailoverEnabled:       req.FailoverEnabled,
		FailoverGraceMs:       req.FailoverGraceMs,
		AutoRecoverEnabled:    req.AutoRecoverEnabled,
		PreflightCheck:        req.PreflightCheck,
		BufferSizeMS:          req.BufferSizeMS,
		ReconnectDelayMS:      req.ReconnectDelayMS,
		MaxReconnectAttempts:  req.MaxReconnectAttempts,
		PassthroughMetadata:   req.PassthroughMetadata,
		OverrideMetadata:      req.OverrideMetadata,
		CustomMetadata:        req.CustomMetadata,
	}

	if err := a.webstreamSvc.CreateWebstream(r.Context(), ws); err != nil {
		a.logger.Error().Err(err).Msg("failed to create webstream")
		writeError(w, http.StatusInternalServerError, "create_failed")
		return
	}

	writeJSON(w, http.StatusCreated, toWebstreamResponse(ws))
}

func (a *API) handleGetWebstream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id_required")
		return
	}

	ws, err := a.webstreamSvc.GetWebstream(r.Context(), id)
	if err != nil {
		a.logger.Error().Err(err).Msg("failed to get webstream")
		writeError(w, http.StatusNotFound, "not_found")
		return
	}

	writeJSON(w, http.StatusOK, toWebstreamResponse(ws))
}

func (a *API) handleListWebstreams(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")

	webstreams, err := a.webstreamSvc.ListWebstreams(r.Context(), stationID)
	if err != nil {
		a.logger.Error().Err(err).Msg("failed to list webstreams")
		writeError(w, http.StatusInternalServerError, "list_failed")
		return
	}

	response := make([]webstreamResponse, len(webstreams))
	for i, ws := range webstreams {
		response[i] = toWebstreamResponse(&ws)
	}

	writeJSON(w, http.StatusOK, response)
}

func (a *API) handleUpdateWebstream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id_required")
		return
	}

	var req updateWebstreamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	// Build updates map
	updates := make(map[string]any)
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.URLs != nil {
		updates["urls"] = req.URLs
	}
	if req.HealthCheckEnabled != nil {
		updates["health_check_enabled"] = *req.HealthCheckEnabled
	}
	if req.HealthCheckIntervalSec != nil {
		updates["health_check_interval"] = time.Duration(*req.HealthCheckIntervalSec) * time.Second
	}
	if req.HealthCheckTimeoutSec != nil {
		updates["health_check_timeout"] = time.Duration(*req.HealthCheckTimeoutSec) * time.Second
	}
	if req.HealthCheckMethod != nil {
		updates["health_check_method"] = *req.HealthCheckMethod
	}
	if req.FailoverEnabled != nil {
		updates["failover_enabled"] = *req.FailoverEnabled
	}
	if req.FailoverGraceMs != nil {
		updates["failover_grace_ms"] = *req.FailoverGraceMs
	}
	if req.AutoRecoverEnabled != nil {
		updates["auto_recover_enabled"] = *req.AutoRecoverEnabled
	}
	if req.PreflightCheck != nil {
		updates["preflight_check"] = *req.PreflightCheck
	}
	if req.BufferSizeMS != nil {
		updates["buffer_size_ms"] = *req.BufferSizeMS
	}
	if req.ReconnectDelayMS != nil {
		updates["reconnect_delay_ms"] = *req.ReconnectDelayMS
	}
	if req.MaxReconnectAttempts != nil {
		updates["max_reconnect_attempts"] = *req.MaxReconnectAttempts
	}
	if req.PassthroughMetadata != nil {
		updates["passthrough_metadata"] = *req.PassthroughMetadata
	}
	if req.OverrideMetadata != nil {
		updates["override_metadata"] = *req.OverrideMetadata
	}
	if req.CustomMetadata != nil {
		updates["custom_metadata"] = req.CustomMetadata
	}

	if err := a.webstreamSvc.UpdateWebstream(r.Context(), id, updates); err != nil {
		a.logger.Error().Err(err).Msg("failed to update webstream")
		writeError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	// Get updated webstream
	ws, err := a.webstreamSvc.GetWebstream(r.Context(), id)
	if err != nil {
		a.logger.Error().Err(err).Msg("failed to get updated webstream")
		writeError(w, http.StatusInternalServerError, "get_failed")
		return
	}

	writeJSON(w, http.StatusOK, toWebstreamResponse(ws))
}

func (a *API) handleDeleteWebstream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id_required")
		return
	}

	if err := a.webstreamSvc.DeleteWebstream(r.Context(), id); err != nil {
		a.logger.Error().Err(err).Msg("failed to delete webstream")
		writeError(w, http.StatusInternalServerError, "delete_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *API) handleTriggerWebstreamFailover(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id_required")
		return
	}

	if err := a.webstreamSvc.TriggerFailover(r.Context(), id); err != nil {
		a.logger.Error().Err(err).Msg("failed to trigger failover")
		writeError(w, http.StatusInternalServerError, "failover_failed")
		return
	}

	// Get updated webstream
	ws, err := a.webstreamSvc.GetWebstream(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "failover_triggered"})
		return
	}

	writeJSON(w, http.StatusOK, toWebstreamResponse(ws))
}

func (a *API) handleResetWebstreamToPrimary(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id_required")
		return
	}

	if err := a.webstreamSvc.ResetToPrimary(r.Context(), id); err != nil {
		a.logger.Error().Err(err).Msg("failed to reset to primary")
		writeError(w, http.StatusInternalServerError, "reset_failed")
		return
	}

	// Get updated webstream
	ws, err := a.webstreamSvc.GetWebstream(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "reset_to_primary"})
		return
	}

	writeJSON(w, http.StatusOK, toWebstreamResponse(ws))
}

// Helper functions

func toWebstreamResponse(ws *models.Webstream) webstreamResponse {
	return webstreamResponse{
		ID:                   ws.ID,
		StationID:            ws.StationID,
		Name:                 ws.Name,
		Description:          ws.Description,
		URLs:                 ws.URLs,
		HealthCheckEnabled:   ws.HealthCheckEnabled,
		HealthCheckInterval:  int(ws.HealthCheckInterval.Seconds()),
		HealthCheckTimeout:   int(ws.HealthCheckTimeout.Seconds()),
		HealthCheckMethod:    ws.HealthCheckMethod,
		FailoverEnabled:      ws.FailoverEnabled,
		FailoverGraceMs:      ws.FailoverGraceMs,
		AutoRecoverEnabled:   ws.AutoRecoverEnabled,
		PreflightCheck:       ws.PreflightCheck,
		BufferSizeMS:         ws.BufferSizeMS,
		ReconnectDelayMS:     ws.ReconnectDelayMS,
		MaxReconnectAttempts: ws.MaxReconnectAttempts,
		PassthroughMetadata:  ws.PassthroughMetadata,
		OverrideMetadata:     ws.OverrideMetadata,
		CustomMetadata:       ws.CustomMetadata,
		Active:               ws.Active,
		CurrentURL:           ws.CurrentURL,
		CurrentIndex:         ws.CurrentIndex,
		HealthStatus:         ws.HealthStatus,
		LastHealthCheck:      ws.LastHealthCheck,
		CreatedAt:            ws.CreatedAt,
		UpdatedAt:            ws.UpdatedAt,
	}
}
