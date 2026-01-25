/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	ws "nhooyr.io/websocket"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// StreamWebSocket proxies audio streams over WebSocket for reliable browser playback.
// URL format: /ws/stream/{station}/{mount}
func (h *Handler) StreamWebSocket(w http.ResponseWriter, r *http.Request) {
	stationSlug := chi.URLParam(r, "station")
	mountName := chi.URLParam(r, "mount")

	if stationSlug == "" || mountName == "" {
		http.Error(w, "Invalid stream path", http.StatusBadRequest)
		return
	}

	// Look up station by name (case-insensitive, spaces converted to dashes)
	var station models.Station
	normalizedSlug := strings.ToLower(strings.ReplaceAll(stationSlug, "-", " "))
	if err := h.db.Where("LOWER(name) = ? AND active = ?", normalizedSlug, true).First(&station).Error; err != nil {
		if err := h.db.Where("LOWER(REPLACE(name, ' ', '-')) = ? AND active = ?", strings.ToLower(stationSlug), true).First(&station).Error; err != nil {
			http.Error(w, "Station not found", http.StatusNotFound)
			return
		}
	}

	// Look up mount for this station
	var mount models.Mount
	if err := h.db.Where("station_id = ? AND name = ?", station.ID, mountName).First(&mount).Error; err != nil {
		baseName := strings.TrimSuffix(mountName, ".mp3")
		baseName = strings.TrimSuffix(baseName, ".ogg")
		baseName = strings.TrimSuffix(baseName, ".opus")
		baseName = strings.TrimSuffix(baseName, ".aac")
		baseName = strings.TrimSuffix(baseName, ".flac")

		if err := h.db.Where("station_id = ? AND name = ?", station.ID, baseName).First(&mount).Error; err != nil {
			http.Error(w, "Mount not found", http.StatusNotFound)
			return
		}
	}

	// Build the Icecast URL
	icecastPath := mount.URL
	if icecastPath == "" || strings.HasPrefix(icecastPath, "http") {
		icecastPath = "/" + mount.Name
	}
	if !strings.HasPrefix(icecastPath, "/") {
		icecastPath = "/" + icecastPath
	}
	targetURL := strings.TrimSuffix(h.icecastURL, "/") + icecastPath

	h.logger.Debug().
		Str("station", station.Name).
		Str("mount", mount.Name).
		Str("target", targetURL).
		Msg("proxying stream via websocket")

	// Accept WebSocket connection
	conn, err := ws.Accept(w, r, &ws.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("websocket accept failed")
		return
	}
	defer conn.Close(ws.StatusInternalError, "server error")

	// Connect to Icecast
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	proxyReq, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		h.logger.Error().Err(err).Str("url", targetURL).Msg("failed to create proxy request")
		conn.Close(ws.StatusInternalError, "stream unavailable")
		return
	}

	proxyReq.Header.Set("User-Agent", "GrimnirRadio/1.0")

	client := &http.Client{
		Timeout: 0, // No timeout for streaming
	}
	resp, err := client.Do(proxyReq)
	if err != nil {
		h.logger.Error().Err(err).Str("url", targetURL).Msg("failed to connect to icecast")
		conn.Close(ws.StatusInternalError, "stream unavailable")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		h.logger.Warn().Int("status", resp.StatusCode).Str("url", targetURL).Msg("icecast returned non-200")
		conn.Close(ws.StatusInternalError, "stream unavailable")
		return
	}

	// Send content type as first message
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "audio/mpeg"
	}
	conn.Write(ctx, ws.MessageText, []byte(`{"type":"init","content_type":"`+contentType+`"}`))

	// Stream audio data over WebSocket
	buf := make([]byte, 16*1024) // 16KB chunks
	for {
		select {
		case <-ctx.Done():
			conn.Close(ws.StatusNormalClosure, "client disconnected")
			return
		default:
		}

		n, err := resp.Body.Read(buf)
		if n > 0 {
			if writeErr := conn.Write(ctx, ws.MessageBinary, buf[:n]); writeErr != nil {
				h.logger.Debug().Err(writeErr).Msg("websocket write failed, client disconnected")
				return
			}
		}
		if err != nil {
			h.logger.Debug().Err(err).Msg("icecast stream ended")
			conn.Close(ws.StatusNormalClosure, "stream ended")
			return
		}
	}
}
