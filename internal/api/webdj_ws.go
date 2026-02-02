/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/telemetry"
	"github.com/friendsincode/grimnir_radio/internal/webdj"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	ws "nhooyr.io/websocket"
)

// WebDJWebSocket handles WebSocket connections for WebDJ console.
type WebDJWebSocket struct {
	webdjSvc *webdj.Service
	logger   zerolog.Logger
}

// NewWebDJWebSocket creates a new WebDJ WebSocket handler.
func NewWebDJWebSocket(webdjSvc *webdj.Service, logger zerolog.Logger) *WebDJWebSocket {
	return &WebDJWebSocket{
		webdjSvc: webdjSvc,
		logger:   logger.With().Str("component", "webdj_ws").Logger(),
	}
}

// WebSocket message types
type wsMessage struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	Deck      string          `json:"deck,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data,omitempty"`
}

type wsCommand struct {
	Action string          `json:"action"`
	Deck   string          `json:"deck,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// HandleWebSocket handles WebSocket connections for a WebDJ session.
func (h *WebDJWebSocket) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "session_id required", http.StatusBadRequest)
		return
	}

	// Verify session exists
	session, err := h.webdjSvc.GetSession(r.Context(), sessionID)
	if err != nil {
		if err == webdj.ErrSessionNotFound {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if !session.Active {
		http.Error(w, "session not active", http.StatusBadRequest)
		return
	}

	// Accept WebSocket connection
	conn, err := ws.Accept(w, r, &ws.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("websocket accept failed")
		return
	}
	defer conn.Close(ws.StatusInternalError, "server error")

	// Track WebSocket connection
	telemetry.APIWebSocketConnections.Inc()
	defer telemetry.APIWebSocketConnections.Dec()

	h.logger.Debug().
		Str("session_id", sessionID).
		Msg("webdj websocket connected")

	// Subscribe to state updates
	updateCh, unsubscribe, err := h.webdjSvc.Subscribe(sessionID)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to subscribe to updates")
		conn.Close(ws.StatusInternalError, "subscribe failed")
		return
	}
	defer unsubscribe()

	ctx := r.Context()

	// Send initial state
	if err := h.sendInitialState(ctx, conn, session); err != nil {
		h.logger.Error().Err(err).Msg("failed to send initial state")
		conn.Close(ws.StatusInternalError, "send failed")
		return
	}

	// Create channels for coordination
	done := make(chan struct{})
	commandCh := make(chan wsCommand, 16)

	// Read commands from client
	go func() {
		defer close(done)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				if ws.CloseStatus(err) == ws.StatusNormalClosure {
					return
				}
				h.logger.Debug().Err(err).Msg("websocket read error")
				return
			}

			var cmd wsCommand
			if err := json.Unmarshal(data, &cmd); err != nil {
				h.logger.Warn().Err(err).Msg("invalid websocket message")
				continue
			}

			select {
			case commandCh <- cmd:
			default:
				h.logger.Warn().Msg("command channel full, dropping message")
			}
		}
	}()

	// Main loop
	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			conn.Close(ws.StatusNormalClosure, "context cancelled")
			return

		case <-done:
			conn.Close(ws.StatusNormalClosure, "client disconnected")
			return

		case <-pingTicker.C:
			if err := h.sendPing(ctx, conn); err != nil {
				h.logger.Error().Err(err).Msg("ping failed")
				conn.Close(ws.StatusInternalError, "ping failed")
				return
			}

		case update := <-updateCh:
			if update == nil {
				// Channel closed
				conn.Close(ws.StatusNormalClosure, "session ended")
				return
			}
			if err := h.sendUpdate(ctx, conn, update); err != nil {
				h.logger.Error().Err(err).Msg("send update failed")
				conn.Close(ws.StatusInternalError, "send failed")
				return
			}

		case cmd := <-commandCh:
			if err := h.handleCommand(ctx, sessionID, cmd); err != nil {
				h.logger.Warn().Err(err).Str("action", cmd.Action).Msg("command failed")
				// Send error to client
				h.sendError(ctx, conn, cmd.Action, err.Error())
			}
		}
	}
}

func (h *WebDJWebSocket) sendInitialState(ctx context.Context, conn *ws.Conn, session *models.WebDJSession) error {
	msg := wsMessage{
		Type:      "initial_state",
		SessionID: session.ID,
		Timestamp: time.Now(),
	}

	stateData := map[string]interface{}{
		"deck_a":          session.DeckAState,
		"deck_b":          session.DeckBState,
		"mixer":           session.MixerState,
		"crossfader_curve": session.CrossfaderCurve,
	}

	data, err := json.Marshal(stateData)
	if err != nil {
		return err
	}
	msg.Data = data

	bytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return conn.Write(ctx, ws.MessageText, bytes)
}

func (h *WebDJWebSocket) sendPing(ctx context.Context, conn *ws.Conn) error {
	msg := wsMessage{
		Type:      "ping",
		Timestamp: time.Now(),
	}
	bytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.Write(ctx, ws.MessageText, bytes)
}

func (h *WebDJWebSocket) sendUpdate(ctx context.Context, conn *ws.Conn, update *webdj.StateUpdate) error {
	msg := wsMessage{
		Type:      update.Type,
		SessionID: update.SessionID,
		Deck:      update.Deck,
		Timestamp: update.Timestamp,
	}

	data, err := json.Marshal(update.Data)
	if err != nil {
		return err
	}
	msg.Data = data

	bytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return conn.Write(ctx, ws.MessageText, bytes)
}

func (h *WebDJWebSocket) sendError(ctx context.Context, conn *ws.Conn, action, errMsg string) {
	msg := wsMessage{
		Type:      "error",
		Timestamp: time.Now(),
	}

	data, _ := json.Marshal(map[string]string{
		"action":  action,
		"message": errMsg,
	})
	msg.Data = data

	bytes, _ := json.Marshal(msg)
	conn.Write(ctx, ws.MessageText, bytes)
}

func (h *WebDJWebSocket) handleCommand(ctx context.Context, sessionID string, cmd wsCommand) error {
	deck, _ := parseDeck(cmd.Deck) // May be empty for mixer commands

	switch cmd.Action {
	case "play":
		return h.webdjSvc.Play(ctx, sessionID, deck)

	case "pause":
		return h.webdjSvc.Pause(ctx, sessionID, deck)

	case "seek":
		var data struct {
			PositionMS int64 `json:"position_ms"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err != nil {
			return err
		}
		return h.webdjSvc.Seek(ctx, sessionID, deck, data.PositionMS)

	case "load":
		var data struct {
			MediaID string `json:"media_id"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err != nil {
			return err
		}
		_, err := h.webdjSvc.LoadTrack(ctx, webdj.LoadTrackRequest{
			SessionID: sessionID,
			Deck:      deck,
			MediaID:   data.MediaID,
		})
		return err

	case "eject":
		return h.webdjSvc.EjectTrack(ctx, sessionID, deck)

	case "volume":
		var data struct {
			Volume float64 `json:"volume"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err != nil {
			return err
		}
		return h.webdjSvc.SetVolume(ctx, sessionID, deck, data.Volume)

	case "eq":
		var data struct {
			High float64 `json:"high"`
			Mid  float64 `json:"mid"`
			Low  float64 `json:"low"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err != nil {
			return err
		}
		return h.webdjSvc.SetEQ(ctx, sessionID, deck, data.High, data.Mid, data.Low)

	case "pitch":
		var data struct {
			Pitch float64 `json:"pitch"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err != nil {
			return err
		}
		return h.webdjSvc.SetPitch(ctx, sessionID, deck, data.Pitch)

	case "cue_set":
		var data struct {
			CueID      int   `json:"cue_id"`
			PositionMS int64 `json:"position_ms"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err != nil {
			return err
		}
		return h.webdjSvc.SetCue(ctx, sessionID, deck, data.CueID, data.PositionMS)

	case "cue_delete":
		var data struct {
			CueID int `json:"cue_id"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err != nil {
			return err
		}
		return h.webdjSvc.DeleteCue(ctx, sessionID, deck, data.CueID)

	case "crossfader":
		var data struct {
			Position float64 `json:"position"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err != nil {
			return err
		}
		return h.webdjSvc.SetCrossfader(ctx, sessionID, data.Position)

	case "master_volume":
		var data struct {
			Volume float64 `json:"volume"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err != nil {
			return err
		}
		return h.webdjSvc.SetMasterVolume(ctx, sessionID, data.Volume)

	case "pong":
		// Client ping response, ignore
		return nil

	default:
		h.logger.Warn().Str("action", cmd.Action).Msg("unknown command action")
		return nil
	}
}
