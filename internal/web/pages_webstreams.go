/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// WebstreamList renders the webstreams page
func (h *Handler) WebstreamList(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	var webstreams []models.Webstream
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&webstreams)

	h.Render(w, r, "pages/dashboard/webstreams/list", PageData{
		Title:    "Webstreams",
		Stations: h.LoadStations(r),
		Data:     webstreams,
	})
}

// WebstreamNew renders the new webstream form
func (h *Handler) WebstreamNew(w http.ResponseWriter, r *http.Request) {
	h.Render(w, r, "pages/dashboard/webstreams/form", PageData{
		Title:    "New Webstream",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Webstream": models.Webstream{
				HealthCheckEnabled:   true,
				FailoverEnabled:      true,
				AutoRecoverEnabled:   true,
				BufferSizeMS:         5000,
				ReconnectDelayMS:     1000,
				MaxReconnectAttempts: 5,
			},
			"IsNew": true,
		},
	})
}

// WebstreamCreate handles webstream creation
func (h *Handler) WebstreamCreate(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	// Parse URLs (comma or newline separated)
	urlsRaw := r.FormValue("urls")
	urls := strings.Split(strings.ReplaceAll(urlsRaw, "\n", ","), ",")
	var cleanURLs []string
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u != "" {
			cleanURLs = append(cleanURLs, u)
		}
	}

	bufferSize, _ := strconv.Atoi(r.FormValue("buffer_size_ms"))
	reconnectDelay, _ := strconv.Atoi(r.FormValue("reconnect_delay_ms"))
	maxReconnect, _ := strconv.Atoi(r.FormValue("max_reconnect_attempts"))

	webstream := models.Webstream{
		ID:                   uuid.New().String(),
		StationID:            station.ID,
		Name:                 r.FormValue("name"),
		Description:          r.FormValue("description"),
		URLs:                 cleanURLs,
		HealthCheckEnabled:   r.FormValue("health_check_enabled") == "on",
		FailoverEnabled:      r.FormValue("failover_enabled") == "on",
		AutoRecoverEnabled:   r.FormValue("auto_recover_enabled") == "on",
		BufferSizeMS:         bufferSize,
		ReconnectDelayMS:     reconnectDelay,
		MaxReconnectAttempts: maxReconnect,
		Active:               r.FormValue("active") == "on",
	}

	if webstream.Name == "" || len(cleanURLs) == 0 {
		http.Error(w, "Name and at least one URL required", http.StatusBadRequest)
		return
	}

	webstream.CurrentURL = cleanURLs[0]
	webstream.CurrentIndex = 0

	if err := h.db.Create(&webstream).Error; err != nil {
		http.Error(w, "Failed to create webstream", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/webstreams/"+webstream.ID)
		return
	}

	http.Redirect(w, r, "/dashboard/webstreams/"+webstream.ID, http.StatusSeeOther)
}

// WebstreamDetail renders the webstream detail page
func (h *Handler) WebstreamDetail(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	id := chi.URLParam(r, "id")

	var webstream models.Webstream
	if err := h.db.First(&webstream, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	h.Render(w, r, "pages/dashboard/webstreams/detail", PageData{
		Title:    webstream.Name,
		Stations: h.LoadStations(r),
		Data:     webstream,
	})
}

// WebstreamEdit renders the webstream edit form
func (h *Handler) WebstreamEdit(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	id := chi.URLParam(r, "id")

	var webstream models.Webstream
	if err := h.db.First(&webstream, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	h.Render(w, r, "pages/dashboard/webstreams/form", PageData{
		Title:    "Edit: " + webstream.Name,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Webstream": webstream,
			"IsNew":     false,
		},
	})
}

// WebstreamUpdate handles webstream updates
func (h *Handler) WebstreamUpdate(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	var webstream models.Webstream
	if err := h.db.First(&webstream, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	// Parse URLs
	urlsRaw := r.FormValue("urls")
	urls := strings.Split(strings.ReplaceAll(urlsRaw, "\n", ","), ",")
	var cleanURLs []string
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u != "" {
			cleanURLs = append(cleanURLs, u)
		}
	}

	bufferSize, _ := strconv.Atoi(r.FormValue("buffer_size_ms"))
	reconnectDelay, _ := strconv.Atoi(r.FormValue("reconnect_delay_ms"))
	maxReconnect, _ := strconv.Atoi(r.FormValue("max_reconnect_attempts"))

	webstream.Name = r.FormValue("name")
	webstream.Description = r.FormValue("description")
	webstream.URLs = cleanURLs
	webstream.HealthCheckEnabled = r.FormValue("health_check_enabled") == "on"
	webstream.FailoverEnabled = r.FormValue("failover_enabled") == "on"
	webstream.AutoRecoverEnabled = r.FormValue("auto_recover_enabled") == "on"
	webstream.BufferSizeMS = bufferSize
	webstream.ReconnectDelayMS = reconnectDelay
	webstream.MaxReconnectAttempts = maxReconnect
	webstream.Active = r.FormValue("active") == "on"

	if err := h.db.Save(&webstream).Error; err != nil {
		http.Error(w, "Failed to update webstream", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/webstreams/"+id)
		return
	}

	http.Redirect(w, r, "/dashboard/webstreams/"+id, http.StatusSeeOther)
}

// WebstreamDelete handles webstream deletion
func (h *Handler) WebstreamDelete(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	// Verify webstream belongs to station
	var webstream models.Webstream
	if err := h.db.First(&webstream, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := h.db.Delete(&models.Webstream{}, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.Error(w, "Failed to delete webstream", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/webstreams")
		return
	}

	http.Redirect(w, r, "/dashboard/webstreams", http.StatusSeeOther)
}

// WebstreamFailover triggers a manual failover
func (h *Handler) WebstreamFailover(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	// Verify webstream belongs to station
	var webstream models.Webstream
	if err := h.db.First(&webstream, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Use webstream service if available (handles DB update, events, logging)
	if h.webstreamSvc != nil {
		if err := h.webstreamSvc.TriggerFailover(r.Context(), id); err != nil {
			h.logger.Error().Err(err).Str("webstream_id", id).Msg("failed to trigger failover")
			if r.Header.Get("HX-Request") == "true" {
				w.Write([]byte(`<div class="alert alert-danger">Failed to trigger failover: ` + err.Error() + `</div>`))
				return
			}
			http.Error(w, "Failed to trigger failover", http.StatusInternalServerError)
			return
		}
	} else {
		// Fallback: direct DB update if service not available
		if len(webstream.URLs) > 1 {
			webstream.CurrentIndex = (webstream.CurrentIndex + 1) % len(webstream.URLs)
			webstream.CurrentURL = webstream.URLs[webstream.CurrentIndex]
			if err := h.db.Save(&webstream).Error; err != nil {
				http.Error(w, "Failed to trigger failover", http.StatusInternalServerError)
				return
			}
		}
		h.logger.Warn().Msg("webstream service not available, failover event not emitted")
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(`<div class="alert alert-success">Failover triggered</div>`))
		return
	}

	http.Redirect(w, r, "/dashboard/webstreams/"+id, http.StatusSeeOther)
}

// WebstreamReset resets to primary URL
func (h *Handler) WebstreamReset(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	// Verify webstream belongs to station
	var webstream models.Webstream
	if err := h.db.First(&webstream, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Use webstream service if available (handles DB update, events, logging)
	if h.webstreamSvc != nil {
		if err := h.webstreamSvc.ResetToPrimary(r.Context(), id); err != nil {
			h.logger.Error().Err(err).Str("webstream_id", id).Msg("failed to reset to primary")
			if r.Header.Get("HX-Request") == "true" {
				w.Write([]byte(`<div class="alert alert-danger">Failed to reset: ` + err.Error() + `</div>`))
				return
			}
			http.Error(w, "Failed to reset", http.StatusInternalServerError)
			return
		}
	} else {
		// Fallback: direct DB update if service not available
		if len(webstream.URLs) > 0 {
			webstream.CurrentIndex = 0
			webstream.CurrentURL = webstream.URLs[0]
			if err := h.db.Save(&webstream).Error; err != nil {
				http.Error(w, "Failed to reset", http.StatusInternalServerError)
				return
			}
		}
		h.logger.Warn().Msg("webstream service not available, reset event not emitted")
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(`<div class="alert alert-success">Reset to primary URL</div>`))
		return
	}

	http.Redirect(w, r, "/dashboard/webstreams/"+id, http.StatusSeeOther)
}
