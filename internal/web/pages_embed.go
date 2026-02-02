/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// EmbedSchedule renders an embeddable schedule widget.
func (h *Handler) EmbedSchedule(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station")
	if stationID == "" {
		http.Error(w, "station parameter required", http.StatusBadRequest)
		return
	}

	// Validate station
	var station models.Station
	if err := h.db.First(&station, "id = ?", stationID).Error; err != nil {
		http.Error(w, "Station not found", http.StatusNotFound)
		return
	}

	if !station.Public {
		http.Error(w, "Station not found", http.StatusNotFound)
		return
	}

	// Parse customization options
	theme := r.URL.Query().Get("theme")
	if theme == "" {
		theme = "light"
	}
	if theme != "light" && theme != "dark" {
		theme = "light"
	}

	days := 7 // Default to 7 days
	if daysStr := r.URL.Query().Get("days"); daysStr != "" {
		fmt.Sscanf(daysStr, "%d", &days)
		if days < 1 {
			days = 1
		}
		if days > 30 {
			days = 30
		}
	}

	compact := r.URL.Query().Get("compact") == "true"
	showHost := r.URL.Query().Get("show_host") != "false"

	h.Render(w, r, "pages/embed/schedule", PageData{
		Title: station.Name + " Schedule",
		Data: map[string]any{
			"Station":   station,
			"Theme":     theme,
			"Days":      days,
			"Compact":   compact,
			"ShowHost":  showHost,
			"StationID": stationID,
		},
	})
}

// EmbedNowPlaying renders an embeddable now-playing widget.
func (h *Handler) EmbedNowPlaying(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station")
	if stationID == "" {
		http.Error(w, "station parameter required", http.StatusBadRequest)
		return
	}

	// Validate station
	var station models.Station
	if err := h.db.First(&station, "id = ?", stationID).Error; err != nil {
		http.Error(w, "Station not found", http.StatusNotFound)
		return
	}

	if !station.Public {
		http.Error(w, "Station not found", http.StatusNotFound)
		return
	}

	// Parse customization options
	theme := r.URL.Query().Get("theme")
	if theme == "" {
		theme = "light"
	}
	showNext := r.URL.Query().Get("show_next") != "false"

	h.Render(w, r, "pages/embed/now-playing", PageData{
		Title: station.Name + " - Now Playing",
		Data: map[string]any{
			"Station":   station,
			"Theme":     theme,
			"ShowNext":  showNext,
			"StationID": stationID,
		},
	})
}

// EmbedScheduleJS serves a JavaScript snippet for embedding schedules.
func (h *Handler) EmbedScheduleJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")

	// Get the host from the request for building URLs
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)

	js := fmt.Sprintf(`
(function() {
    'use strict';

    var GrimnirSchedule = {
        baseUrl: '%s',

        init: function() {
            var containers = document.querySelectorAll('[data-grimnir-schedule]');
            containers.forEach(function(container) {
                GrimnirSchedule.render(container);
            });
        },

        render: function(container) {
            var stationId = container.getAttribute('data-grimnir-schedule') || container.getAttribute('data-station');
            if (!stationId) {
                console.error('Grimnir Schedule: Missing station ID');
                return;
            }

            var options = {
                station: stationId,
                theme: container.getAttribute('data-theme') || 'light',
                days: container.getAttribute('data-days') || '7',
                compact: container.getAttribute('data-compact') === 'true',
                showHost: container.getAttribute('data-show-host') !== 'false',
                width: container.getAttribute('data-width') || '100%%',
                height: container.getAttribute('data-height') || '400px'
            };

            var iframe = document.createElement('iframe');
            iframe.src = GrimnirSchedule.baseUrl + '/embed/schedule?' +
                'station=' + encodeURIComponent(options.station) +
                '&theme=' + encodeURIComponent(options.theme) +
                '&days=' + encodeURIComponent(options.days) +
                (options.compact ? '&compact=true' : '') +
                (options.showHost ? '' : '&show_host=false');

            iframe.style.width = options.width;
            iframe.style.height = options.height;
            iframe.style.border = 'none';
            iframe.style.overflow = 'hidden';
            iframe.setAttribute('frameborder', '0');
            iframe.setAttribute('scrolling', 'auto');
            iframe.setAttribute('allowtransparency', 'true');

            container.innerHTML = '';
            container.appendChild(iframe);
        },

        nowPlaying: function(container) {
            var stationId = container.getAttribute('data-grimnir-now-playing') || container.getAttribute('data-station');
            if (!stationId) {
                console.error('Grimnir Now Playing: Missing station ID');
                return;
            }

            var options = {
                station: stationId,
                theme: container.getAttribute('data-theme') || 'light',
                showNext: container.getAttribute('data-show-next') !== 'false',
                width: container.getAttribute('data-width') || '300px',
                height: container.getAttribute('data-height') || '150px'
            };

            var iframe = document.createElement('iframe');
            iframe.src = GrimnirSchedule.baseUrl + '/embed/now-playing?' +
                'station=' + encodeURIComponent(options.station) +
                '&theme=' + encodeURIComponent(options.theme) +
                (options.showNext ? '' : '&show_next=false');

            iframe.style.width = options.width;
            iframe.style.height = options.height;
            iframe.style.border = 'none';
            iframe.style.overflow = 'hidden';
            iframe.setAttribute('frameborder', '0');
            iframe.setAttribute('scrolling', 'no');
            iframe.setAttribute('allowtransparency', 'true');

            container.innerHTML = '';
            container.appendChild(iframe);
        }
    };

    // Auto-initialize on DOM ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', function() {
            GrimnirSchedule.init();

            // Initialize now-playing widgets
            var nowPlayingContainers = document.querySelectorAll('[data-grimnir-now-playing]');
            nowPlayingContainers.forEach(function(container) {
                GrimnirSchedule.nowPlaying(container);
            });
        });
    } else {
        GrimnirSchedule.init();

        var nowPlayingContainers = document.querySelectorAll('[data-grimnir-now-playing]');
        nowPlayingContainers.forEach(function(container) {
            GrimnirSchedule.nowPlaying(container);
        });
    }

    // Expose globally
    window.GrimnirSchedule = GrimnirSchedule;
})();
`, baseURL)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(js))
}

// PublicShowInstance represents a show instance for embed data.
type PublicShowInstance struct {
	ID        string    `json:"id"`
	ShowName  string    `json:"show_name"`
	ShowDesc  string    `json:"show_description,omitempty"`
	HostName  string    `json:"host_name,omitempty"`
	Color     string    `json:"color,omitempty"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	IsCurrent bool      `json:"is_current,omitempty"`
}

// GetScheduleData returns schedule data as JSON for the embed widget.
func (h *Handler) GetScheduleData(stationID string, days int) ([]PublicShowInstance, error) {
	now := time.Now()
	end := now.AddDate(0, 0, days)

	var instances []models.ShowInstance
	if err := h.db.Where("station_id = ? AND starts_at >= ? AND starts_at < ?", stationID, now, end).
		Where("status = ?", models.ShowInstanceScheduled).
		Preload("Show").
		Preload("Show.Host").
		Preload("Host").
		Order("starts_at ASC").
		Find(&instances).Error; err != nil {
		return nil, err
	}

	result := make([]PublicShowInstance, 0, len(instances))
	for _, inst := range instances {
		if inst.Show == nil {
			continue
		}

		pi := PublicShowInstance{
			ID:       inst.ID,
			ShowName: inst.Show.Name,
			ShowDesc: inst.Show.Description,
			Color:    inst.Show.Color,
			StartsAt: inst.StartsAt,
			EndsAt:   inst.EndsAt,
		}

		if inst.Host != nil {
			pi.HostName = inst.Host.Email
		} else if inst.Show.Host != nil {
			pi.HostName = inst.Show.Host.Email
		}

		// Check if current
		if now.After(inst.StartsAt) && now.Before(inst.EndsAt) {
			pi.IsCurrent = true
		}

		result = append(result, pi)
	}

	return result, nil
}

// GetNowPlayingData returns now playing data for the embed widget.
func (h *Handler) GetNowPlayingData(stationID string) (current, next *PublicShowInstance, err error) {
	now := time.Now()

	// Find current show
	var currentInstance models.ShowInstance
	if err := h.db.Where("station_id = ? AND starts_at <= ? AND ends_at > ?", stationID, now, now).
		Where("status = ?", models.ShowInstanceScheduled).
		Preload("Show").
		Preload("Show.Host").
		Preload("Host").
		First(&currentInstance).Error; err == nil && currentInstance.Show != nil {
		current = &PublicShowInstance{
			ID:        currentInstance.ID,
			ShowName:  currentInstance.Show.Name,
			ShowDesc:  currentInstance.Show.Description,
			Color:     currentInstance.Show.Color,
			StartsAt:  currentInstance.StartsAt,
			EndsAt:    currentInstance.EndsAt,
			IsCurrent: true,
		}
		if currentInstance.Host != nil {
			current.HostName = currentInstance.Host.Email
		} else if currentInstance.Show.Host != nil {
			current.HostName = currentInstance.Show.Host.Email
		}
	}

	// Find next show
	searchStart := now
	if current != nil {
		searchStart = current.EndsAt
	}

	var nextInstance models.ShowInstance
	if err := h.db.Where("station_id = ? AND starts_at >= ?", stationID, searchStart).
		Where("status = ?", models.ShowInstanceScheduled).
		Preload("Show").
		Preload("Show.Host").
		Preload("Host").
		Order("starts_at ASC").
		First(&nextInstance).Error; err == nil && nextInstance.Show != nil {
		next = &PublicShowInstance{
			ID:       nextInstance.ID,
			ShowName: nextInstance.Show.Name,
			ShowDesc: nextInstance.Show.Description,
			Color:    nextInstance.Show.Color,
			StartsAt: nextInstance.StartsAt,
			EndsAt:   nextInstance.EndsAt,
		}
		if nextInstance.Host != nil {
			next.HostName = nextInstance.Host.Email
		} else if nextInstance.Show.Host != nil {
			next.HostName = nextInstance.Show.Host.Email
		}
	}

	return current, next, nil
}

// EmbedScheduleDataJSON returns schedule data as JSON for AJAX requests.
func (h *Handler) EmbedScheduleDataJSON(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station")
	if stationID == "" {
		writeJSONError(w, http.StatusBadRequest, "station required")
		return
	}

	// Validate station
	var station models.Station
	if err := h.db.First(&station, "id = ?", stationID).Error; err != nil || !station.Public {
		writeJSONError(w, http.StatusNotFound, "station not found")
		return
	}

	days := 7
	if daysStr := r.URL.Query().Get("days"); daysStr != "" {
		fmt.Sscanf(daysStr, "%d", &days)
		if days < 1 || days > 30 {
			days = 7
		}
	}

	schedule, err := h.GetScheduleData(stationID, days)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "error loading schedule")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]any{
		"station":  station.Name,
		"schedule": schedule,
	})
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
