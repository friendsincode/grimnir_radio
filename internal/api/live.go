package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/live"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/go-chi/chi/v5"
)

// Live API request/response types

type liveGenerateTokenRequest struct {
	StationID string `json:"station_id"`
	MountID   string `json:"mount_id"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	Priority  int    `json:"priority"` // 1 = override, 2 = scheduled
	ExpiresIn int    `json:"expires_in_seconds,omitempty"` // Optional, default 3600
}

type liveGenerateTokenResponse struct {
	Token     string    `json:"token"`
	SessionID string    `json:"session_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type liveConnectRequest struct {
	StationID  string `json:"station_id"`
	MountID    string `json:"mount_id"`
	Token      string `json:"token"`
	SourceIP   string `json:"source_ip,omitempty"`
	SourcePort int    `json:"source_port,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"`
}

type liveSessionResponse struct {
	ID             string         `json:"id"`
	StationID      string         `json:"station_id"`
	MountID        string         `json:"mount_id"`
	UserID         string         `json:"user_id"`
	Username       string         `json:"username"`
	Priority       int            `json:"priority"`
	Active         bool           `json:"active"`
	ConnectedAt    time.Time      `json:"connected_at"`
	DisconnectedAt *time.Time     `json:"disconnected_at,omitempty"`
	Duration       float64        `json:"duration_seconds"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// Live API handlers

func (a *API) handleLiveGenerateToken(w http.ResponseWriter, r *http.Request) {
	var req liveGenerateTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	// Validate required fields
	if req.StationID == "" || req.MountID == "" || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "station_mount_user_required")
		return
	}

	// Validate priority
	priority := models.PriorityLevel(req.Priority)
	if priority != models.PriorityLiveOverride && priority != models.PriorityLiveScheduled {
		writeError(w, http.StatusBadRequest, "invalid_priority")
		return
	}

	// Default expiration
	expiresIn := time.Duration(req.ExpiresIn) * time.Second
	if expiresIn == 0 {
		expiresIn = 1 * time.Hour // Default 1 hour
	}

	// Generate token
	token, err := a.live.GenerateToken(r.Context(), live.GenerateTokenRequest{
		StationID: req.StationID,
		MountID:   req.MountID,
		UserID:    req.UserID,
		Username:  req.Username,
		Priority:  priority,
		ExpiresIn: expiresIn,
	})

	if err != nil {
		a.logger.Error().Err(err).Msg("failed to generate live token")
		writeError(w, http.StatusInternalServerError, "token_generation_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"expires_in": int(expiresIn.Seconds()),
	})
}

func (a *API) handleLiveAuthorize(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StationID string `json:"station_id"`
		MountID   string `json:"mount_id"`
		Token     string `json:"token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.StationID == "" || req.MountID == "" || req.Token == "" {
		writeError(w, http.StatusBadRequest, "station_mount_token_required")
		return
	}

	authorized, err := a.live.AuthorizeSource(r.Context(), req.StationID, req.MountID, req.Token)
	if err != nil {
		if err == live.ErrInvalidToken || err == live.ErrTokenAlreadyUsed {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		a.logger.Error().Err(err).Msg("authorization check failed")
		writeError(w, http.StatusInternalServerError, "authorization_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"authorized": authorized})
}

func (a *API) handleLiveConnect(w http.ResponseWriter, r *http.Request) {
	var req liveConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.StationID == "" || req.MountID == "" || req.Token == "" {
		writeError(w, http.StatusBadRequest, "station_mount_token_required")
		return
	}

	// If source IP not provided, try to get from request
	if req.SourceIP == "" {
		req.SourceIP = r.RemoteAddr
	}

	session, err := a.live.HandleConnect(r.Context(), live.ConnectRequest{
		StationID:  req.StationID,
		MountID:    req.MountID,
		Token:      req.Token,
		SourceIP:   req.SourceIP,
		SourcePort: req.SourcePort,
		UserAgent:  req.UserAgent,
	})

	if err != nil {
		if err == live.ErrSessionNotFound {
			writeError(w, http.StatusNotFound, "session_not_found")
			return
		}
		a.logger.Error().Err(err).Msg("live connect failed")
		writeError(w, http.StatusInternalServerError, "connect_failed")
		return
	}

	writeJSON(w, http.StatusOK, liveSessionResponse{
		ID:          session.ID,
		StationID:   session.StationID,
		MountID:     session.MountID,
		UserID:      session.UserID,
		Username:    session.Username,
		Priority:    int(session.Priority),
		Active:      session.Active,
		ConnectedAt: session.ConnectedAt,
		Duration:    session.Duration().Seconds(),
		Metadata:    session.Metadata,
	})
}

func (a *API) handleLiveDisconnect(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id_required")
		return
	}

	if err := a.live.HandleDisconnect(r.Context(), sessionID); err != nil {
		if err == live.ErrSessionNotFound {
			writeError(w, http.StatusNotFound, "session_not_found")
			return
		}
		a.logger.Error().Err(err).Msg("live disconnect failed")
		writeError(w, http.StatusInternalServerError, "disconnect_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

func (a *API) handleListLiveSessions(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")

	sessions, err := a.live.GetActiveSessions(r.Context(), stationID)
	if err != nil {
		a.logger.Error().Err(err).Msg("failed to list live sessions")
		writeError(w, http.StatusInternalServerError, "list_failed")
		return
	}

	// Convert to response format
	response := make([]liveSessionResponse, len(sessions))
	for i, session := range sessions {
		response[i] = liveSessionResponse{
			ID:             session.ID,
			StationID:      session.StationID,
			MountID:        session.MountID,
			UserID:         session.UserID,
			Username:       session.Username,
			Priority:       int(session.Priority),
			Active:         session.Active,
			ConnectedAt:    session.ConnectedAt,
			DisconnectedAt: session.DisconnectedAt,
			Duration:       session.Duration().Seconds(),
			Metadata:       session.Metadata,
		}
	}

	writeJSON(w, http.StatusOK, response)
}

func (a *API) handleGetLiveSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id_required")
		return
	}

	session, err := a.live.GetSession(r.Context(), sessionID)
	if err != nil {
		if err == live.ErrSessionNotFound {
			writeError(w, http.StatusNotFound, "session_not_found")
			return
		}
		a.logger.Error().Err(err).Msg("failed to get live session")
		writeError(w, http.StatusInternalServerError, "get_failed")
		return
	}

	writeJSON(w, http.StatusOK, liveSessionResponse{
		ID:             session.ID,
		StationID:      session.StationID,
		MountID:        session.MountID,
		UserID:         session.UserID,
		Username:       session.Username,
		Priority:       int(session.Priority),
		Active:         session.Active,
		ConnectedAt:    session.ConnectedAt,
		DisconnectedAt: session.DisconnectedAt,
		Duration:       session.Duration().Seconds(),
		Metadata:       session.Metadata,
	})
}
