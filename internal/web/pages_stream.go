/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// StreamProxy proxies audio streams from Icecast to provide clean URLs.
// URL format: /stream/{station}/{mount}
// Example: /stream/rockradio/live.mp3 -> proxies to icecast:8000/rockradio/live.mp3
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

	// Build the Icecast URL
	// Mount.URL should be the full path like /rockradio/live.mp3
	// or we construct it from station name + mount name
	icecastPath := mount.URL
	if icecastPath == "" {
		// Construct path: /{station-slug}/{mount-name}
		stationPath := strings.ToLower(strings.ReplaceAll(station.Name, " ", "-"))
		ext := ".mp3" // default
		switch strings.ToLower(mount.Format) {
		case "opus":
			ext = ".opus"
		case "ogg", "vorbis":
			ext = ".ogg"
		case "aac":
			ext = ".aac"
		case "flac":
			ext = ".flac"
		}
		icecastPath = "/" + stationPath + "/" + mount.Name + ext
	}

	// Ensure path starts with /
	if !strings.HasPrefix(icecastPath, "/") {
		icecastPath = "/" + icecastPath
	}

	targetURL := strings.TrimSuffix(h.icecastURL, "/") + icecastPath

	h.logger.Debug().
		Str("station", station.Name).
		Str("mount", mount.Name).
		Str("target", targetURL).
		Msg("proxying stream")

	// Create proxy request
	proxyReq, err := http.NewRequestWithContext(r.Context(), "GET", targetURL, nil)
	if err != nil {
		h.logger.Error().Err(err).Str("url", targetURL).Msg("failed to create proxy request")
		http.Error(w, "Stream unavailable", http.StatusBadGateway)
		return
	}

	// Copy relevant headers
	if ua := r.Header.Get("User-Agent"); ua != "" {
		proxyReq.Header.Set("User-Agent", ua)
	}
	if icyMeta := r.Header.Get("Icy-MetaData"); icyMeta != "" {
		proxyReq.Header.Set("Icy-MetaData", icyMeta)
	}

	// Make the request to Icecast
	client := &http.Client{}
	resp, err := client.Do(proxyReq)
	if err != nil {
		h.logger.Error().Err(err).Str("url", targetURL).Msg("failed to connect to icecast")
		http.Error(w, "Stream unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		h.logger.Warn().
			Int("status", resp.StatusCode).
			Str("url", targetURL).
			Msg("icecast returned non-200")
		http.Error(w, "Stream unavailable", resp.StatusCode)
		return
	}

	// Copy response headers
	for _, header := range []string{
		"Content-Type",
		"Icy-Br",
		"Icy-Genre",
		"Icy-Name",
		"Icy-Description",
		"Icy-Url",
		"Icy-Pub",
		"Icy-MetaInt",
	} {
		if val := resp.Header.Get(header); val != "" {
			w.Header().Set(header, val)
		}
	}

	// Set CORS headers for web players
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Range, Icy-MetaData")

	// Stream the response
	w.WriteHeader(http.StatusOK)

	// Use a buffer for efficient copying
	buf := make([]byte, 32*1024)
	io.CopyBuffer(w, resp.Body, buf)
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

	stationPath := strings.ToLower(strings.ReplaceAll(station.Name, " ", "-"))
	streams := make([]streamInfo, 0, len(mounts))

	for _, m := range mounts {
		ext := ".mp3"
		switch strings.ToLower(m.Format) {
		case "opus":
			ext = ".opus"
		case "ogg", "vorbis":
			ext = ".ogg"
		case "aac":
			ext = ".aac"
		case "flac":
			ext = ".flac"
		}

		streams = append(streams, streamInfo{
			Name:       m.Name,
			Format:     m.Format,
			Bitrate:    m.Bitrate,
			URL:        "/stream/" + stationPath + "/" + m.Name + ext,
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
