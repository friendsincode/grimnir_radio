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
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/webdj"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

// WebDJAPI handles WebDJ console endpoints.
type WebDJAPI struct {
	db          *gorm.DB
	webdjSvc    *webdj.Service
	waveformSvc *webdj.WaveformService
}

// NewWebDJAPI creates a new WebDJ API handler.
func NewWebDJAPI(db *gorm.DB, webdjSvc *webdj.Service, waveformSvc *webdj.WaveformService) *WebDJAPI {
	return &WebDJAPI{
		db:          db,
		webdjSvc:    webdjSvc,
		waveformSvc: waveformSvc,
	}
}

// WebDJ request/response types

type webdjStartSessionRequest struct {
	StationID string `json:"station_id"`
}

type webdjSessionResponse struct {
	ID              string            `json:"id"`
	StationID       string            `json:"station_id"`
	UserID          string            `json:"user_id"`
	DeckAState      models.DeckState  `json:"deck_a_state"`
	DeckBState      models.DeckState  `json:"deck_b_state"`
	MixerState      models.MixerState `json:"mixer_state"`
	CrossfaderCurve string            `json:"crossfader_curve"`
	Active          bool              `json:"active"`
	CreatedAt       string            `json:"created_at"`
}

type webdjLoadTrackRequest struct {
	MediaID string `json:"media_id"`
}

type webdjSeekRequest struct {
	PositionMS int64 `json:"position_ms"`
}

type webdjSetCueRequest struct {
	CueID      int   `json:"cue_id"`
	PositionMS int64 `json:"position_ms"`
}

type webdjSetVolumeRequest struct {
	Volume float64 `json:"volume"`
}

type webdjSetEQRequest struct {
	High float64 `json:"high"`
	Mid  float64 `json:"mid"`
	Low  float64 `json:"low"`
}

type webdjSetPitchRequest struct {
	Pitch float64 `json:"pitch"`
}

type webdjSetCrossfaderRequest struct {
	Position float64 `json:"position"`
}

type webdjSetMasterVolumeRequest struct {
	Volume float64 `json:"volume"`
}

type webdjWaveformResponse struct {
	MediaID       string    `json:"media_id"`
	SamplesPerSec int       `json:"samples_per_sec"`
	DurationMS    int64     `json:"duration_ms"`
	PeakLeft      []float32 `json:"peak_left"`
	PeakRight     []float32 `json:"peak_right"`
}

// Handlers

func (a *WebDJAPI) handleStartSession(w http.ResponseWriter, r *http.Request) {
	var req webdjStartSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.StationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	// Get user from context
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get user email for display name
	var user models.User
	if err := a.db.Select("email").First(&user, "id = ?", claims.UserID).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "user_lookup_failed")
		return
	}

	session, err := a.webdjSvc.StartSession(r.Context(), webdj.StartSessionRequest{
		StationID: req.StationID,
		UserID:    claims.UserID,
		Username:  user.Email,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_start_failed")
		return
	}

	writeJSON(w, http.StatusOK, webdjSessionResponse{
		ID:              session.ID,
		StationID:       session.StationID,
		UserID:          session.UserID,
		DeckAState:      session.DeckAState,
		DeckBState:      session.DeckBState,
		MixerState:      session.MixerState,
		CrossfaderCurve: session.CrossfaderCurve,
		Active:          session.Active,
		CreatedAt:       session.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

func (a *WebDJAPI) handleEndSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id_required")
		return
	}

	if err := a.webdjSvc.EndSession(r.Context(), sessionID); err != nil {
		if err == webdj.ErrSessionNotFound {
			writeError(w, http.StatusNotFound, "session_not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "session_end_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ended"})
}

func (a *WebDJAPI) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id_required")
		return
	}

	session, err := a.webdjSvc.GetSession(r.Context(), sessionID)
	if err != nil {
		if err == webdj.ErrSessionNotFound {
			writeError(w, http.StatusNotFound, "session_not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get_session_failed")
		return
	}

	writeJSON(w, http.StatusOK, webdjSessionResponse{
		ID:              session.ID,
		StationID:       session.StationID,
		UserID:          session.UserID,
		DeckAState:      session.DeckAState,
		DeckBState:      session.DeckBState,
		MixerState:      session.MixerState,
		CrossfaderCurve: session.CrossfaderCurve,
		Active:          session.Active,
		CreatedAt:       session.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

func (a *WebDJAPI) handleListSessions(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")

	sessions, err := a.webdjSvc.GetActiveSessions(r.Context(), stationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_sessions_failed")
		return
	}

	response := make([]webdjSessionResponse, len(sessions))
	for i, session := range sessions {
		response[i] = webdjSessionResponse{
			ID:              session.ID,
			StationID:       session.StationID,
			UserID:          session.UserID,
			DeckAState:      session.DeckAState,
			DeckBState:      session.DeckBState,
			MixerState:      session.MixerState,
			CrossfaderCurve: session.CrossfaderCurve,
			Active:          session.Active,
			CreatedAt:       session.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// Deck control handlers

func (a *WebDJAPI) handleLoadTrack(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	deckStr := chi.URLParam(r, "deck")

	deck, err := parseDeck(deckStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_deck")
		return
	}

	var req webdjLoadTrackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.MediaID == "" {
		writeError(w, http.StatusBadRequest, "media_id_required")
		return
	}

	deckState, err := a.webdjSvc.LoadTrack(r.Context(), webdj.LoadTrackRequest{
		SessionID: sessionID,
		Deck:      deck,
		MediaID:   req.MediaID,
	})
	if err != nil {
		handleWebDJError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, deckState)
}

func (a *WebDJAPI) handlePlay(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	deckStr := chi.URLParam(r, "deck")

	deck, err := parseDeck(deckStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_deck")
		return
	}

	if err := a.webdjSvc.Play(r.Context(), sessionID, deck); err != nil {
		handleWebDJError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "playing"})
}

func (a *WebDJAPI) handlePause(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	deckStr := chi.URLParam(r, "deck")

	deck, err := parseDeck(deckStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_deck")
		return
	}

	if err := a.webdjSvc.Pause(r.Context(), sessionID, deck); err != nil {
		handleWebDJError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (a *WebDJAPI) handleSeek(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	deckStr := chi.URLParam(r, "deck")

	deck, err := parseDeck(deckStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_deck")
		return
	}

	var req webdjSeekRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := a.webdjSvc.Seek(r.Context(), sessionID, deck, req.PositionMS); err != nil {
		handleWebDJError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "seeked",
		"position_ms": req.PositionMS,
	})
}

func (a *WebDJAPI) handleSetCue(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	deckStr := chi.URLParam(r, "deck")

	deck, err := parseDeck(deckStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_deck")
		return
	}

	var req webdjSetCueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.CueID < 1 || req.CueID > 8 {
		writeError(w, http.StatusBadRequest, "cue_id_must_be_1_to_8")
		return
	}

	if err := a.webdjSvc.SetCue(r.Context(), sessionID, deck, req.CueID, req.PositionMS); err != nil {
		handleWebDJError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "cue_set",
		"cue_id":      req.CueID,
		"position_ms": req.PositionMS,
	})
}

func (a *WebDJAPI) handleDeleteCue(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	deckStr := chi.URLParam(r, "deck")
	cueIDStr := chi.URLParam(r, "cue_id")

	deck, err := parseDeck(deckStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_deck")
		return
	}

	cueID, err := strconv.Atoi(cueIDStr)
	if err != nil || cueID < 1 || cueID > 8 {
		writeError(w, http.StatusBadRequest, "invalid_cue_id")
		return
	}

	if err := a.webdjSvc.DeleteCue(r.Context(), sessionID, deck, cueID); err != nil {
		handleWebDJError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cue_deleted"})
}

func (a *WebDJAPI) handleEject(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	deckStr := chi.URLParam(r, "deck")

	deck, err := parseDeck(deckStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_deck")
		return
	}

	if err := a.webdjSvc.EjectTrack(r.Context(), sessionID, deck); err != nil {
		handleWebDJError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ejected"})
}

func (a *WebDJAPI) handleSetVolume(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	deckStr := chi.URLParam(r, "deck")

	deck, err := parseDeck(deckStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_deck")
		return
	}

	var req webdjSetVolumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := a.webdjSvc.SetVolume(r.Context(), sessionID, deck, req.Volume); err != nil {
		handleWebDJError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "volume_set",
		"volume": req.Volume,
	})
}

func (a *WebDJAPI) handleSetEQ(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	deckStr := chi.URLParam(r, "deck")

	deck, err := parseDeck(deckStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_deck")
		return
	}

	var req webdjSetEQRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := a.webdjSvc.SetEQ(r.Context(), sessionID, deck, req.High, req.Mid, req.Low); err != nil {
		handleWebDJError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "eq_set",
		"high":   req.High,
		"mid":    req.Mid,
		"low":    req.Low,
	})
}

func (a *WebDJAPI) handleSetPitch(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	deckStr := chi.URLParam(r, "deck")

	deck, err := parseDeck(deckStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_deck")
		return
	}

	var req webdjSetPitchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := a.webdjSvc.SetPitch(r.Context(), sessionID, deck, req.Pitch); err != nil {
		handleWebDJError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "pitch_set",
		"pitch":  req.Pitch,
	})
}

// Mixer control handlers

func (a *WebDJAPI) handleSetCrossfader(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")

	var req webdjSetCrossfaderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := a.webdjSvc.SetCrossfader(r.Context(), sessionID, req.Position); err != nil {
		handleWebDJError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "crossfader_set",
		"position": req.Position,
	})
}

func (a *WebDJAPI) handleSetMasterVolume(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")

	var req webdjSetMasterVolumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := a.webdjSvc.SetMasterVolume(r.Context(), sessionID, req.Volume); err != nil {
		handleWebDJError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "master_volume_set",
		"volume": req.Volume,
	})
}

// Library handlers

func (a *WebDJAPI) handleGetWaveform(w http.ResponseWriter, r *http.Request) {
	mediaID := chi.URLParam(r, "id")
	if mediaID == "" {
		writeError(w, http.StatusBadRequest, "media_id_required")
		return
	}

	data, err := a.waveformSvc.GetWaveform(r.Context(), mediaID)
	if err != nil {
		if err == webdj.ErrMediaNotFound {
			writeError(w, http.StatusNotFound, "media_not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "waveform_generation_failed")
		return
	}

	writeJSON(w, http.StatusOK, webdjWaveformResponse{
		MediaID:       data.MediaID,
		SamplesPerSec: data.SamplesPerSec,
		DurationMS:    data.DurationMS,
		PeakLeft:      data.PeakLeft,
		PeakRight:     data.PeakRight,
	})
}

// Helper functions

func parseDeck(s string) (models.DeckID, error) {
	switch s {
	case "a", "A":
		return models.DeckA, nil
	case "b", "B":
		return models.DeckB, nil
	default:
		return "", webdj.ErrInvalidDeck
	}
}

func handleWebDJError(w http.ResponseWriter, err error) {
	switch err {
	case webdj.ErrSessionNotFound:
		writeError(w, http.StatusNotFound, "session_not_found")
	case webdj.ErrSessionNotActive:
		writeError(w, http.StatusBadRequest, "session_not_active")
	case webdj.ErrInvalidDeck:
		writeError(w, http.StatusBadRequest, "invalid_deck")
	case webdj.ErrNoTrackLoaded:
		writeError(w, http.StatusBadRequest, "no_track_loaded")
	case webdj.ErrMediaNotFound:
		writeError(w, http.StatusNotFound, "media_not_found")
	case webdj.ErrUnauthorized:
		writeError(w, http.StatusForbidden, "unauthorized")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error")
	}
}
