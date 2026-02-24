/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// StreamProxy proxies audio streams from Icecast to provide clean URLs.
// URL format: /stream/{station}/{mount}
// Example: /stream/rockradio/live.mp3 -> proxies to icecast:8000/live.mp3
func (h *Handler) StreamProxy(w http.ResponseWriter, r *http.Request) {
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
		// Also try exact slug match
		if err := h.db.Where("LOWER(REPLACE(name, ' ', '-')) = ? AND active = ?", strings.ToLower(stationSlug), true).First(&station).Error; err != nil {
			http.Error(w, "Station not found", http.StatusNotFound)
			return
		}
	}

	// Look up mount for this station
	var mount models.Mount
	if err := h.db.Where("station_id = ? AND name = ?", station.ID, mountName).First(&mount).Error; err != nil {
		// Try without extension
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

	// Build the Icecast path
	icecastPath := mount.URL
	if icecastPath == "" || strings.HasPrefix(icecastPath, "http") {
		icecastPath = "/" + mount.Name
	}
	if !strings.HasPrefix(icecastPath, "/") {
		icecastPath = "/" + icecastPath
	}

	h.logger.Debug().
		Str("station", station.Name).
		Str("mount", mount.Name).
		Str("icecast_path", icecastPath).
		Msg("proxying stream")

	// Parse icecast URL
	target, err := url.Parse(h.icecastURL)
	if err != nil {
		h.logger.Error().Err(err).Msg("invalid icecast URL")
		http.Error(w, "Stream unavailable", http.StatusBadGateway)
		return
	}

	// Create reverse proxy
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = icecastPath
			req.Host = target.Host

			// Keep original headers that Icecast needs
			if icyMeta := r.Header.Get("Icy-MetaData"); icyMeta != "" {
				req.Header.Set("Icy-MetaData", icyMeta)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			// Add CORS headers
			resp.Header.Set("Access-Control-Allow-Origin", "*")
			resp.Header.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			resp.Header.Set("Access-Control-Allow-Headers", "Range, Icy-MetaData")
			// Disable buffering
			resp.Header.Set("X-Accel-Buffering", "no")
			resp.Header.Set("Cache-Control", "no-cache, no-store")
			return nil
		},
		FlushInterval: -1, // Flush immediately for streaming
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			h.logger.Error().Err(err).Msg("stream proxy error")
			http.Error(w, "Stream unavailable", http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(w, r)
}

// StreamInfo returns JSON info about available streams for a station.
func (h *Handler) StreamInfo(w http.ResponseWriter, r *http.Request) {
	stationSlug := chi.URLParam(r, "station")

	var station models.Station
	normalizedSlug := strings.ToLower(strings.ReplaceAll(stationSlug, "-", " "))
	if err := h.db.Where("LOWER(name) = ? AND active = ?", normalizedSlug, true).First(&station).Error; err != nil {
		if err := h.db.Where("LOWER(REPLACE(name, ' ', '-')) = ? AND active = ?", strings.ToLower(stationSlug), true).First(&station).Error; err != nil {
			http.Error(w, "Station not found", http.StatusNotFound)
			return
		}
	}

	var mounts []models.Mount
	h.db.Where("station_id = ?", station.ID).Find(&mounts)

	type streamInfo struct {
		Name       string `json:"name"`
		Format     string `json:"format"`
		Bitrate    int    `json:"bitrate"`
		URL        string `json:"url"`
		SampleRate int    `json:"sample_rate,omitempty"`
		Channels   int    `json:"channels,omitempty"`
	}

	streams := make([]streamInfo, 0, len(mounts))

	for _, m := range mounts {
		streams = append(streams, streamInfo{
			Name:       m.Name,
			Format:     m.Format,
			Bitrate:    m.Bitrate,
			URL:        "/live/" + m.Name,
			SampleRate: m.SampleRate,
			Channels:   m.Channels,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"station":"` + station.Name + `","streams":[`))

	for i, s := range streams {
		if i > 0 {
			w.Write([]byte(","))
		}
		// Simple JSON encoding
		w.Write([]byte(`{"name":"` + s.Name + `","format":"` + s.Format + `","bitrate":` +
			itoa(s.Bitrate) + `,"url":"` + s.URL + `"}`))
	}

	w.Write([]byte("]}"))
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}

	var buf [20]byte
	pos := len(buf)
	negative := i < 0
	if negative {
		i = -i
	}

	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}

	if negative {
		pos--
		buf[pos] = '-'
	}

	return string(buf[pos:])
}
