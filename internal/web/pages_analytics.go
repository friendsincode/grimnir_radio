/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// AnalyticsDashboard renders the main analytics page
func (h *Handler) AnalyticsDashboard(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	// Get recent play history
	var recentPlays []models.PlayHistory
	h.db.Where("station_id = ?", station.ID).
		Order("started_at DESC").
		Limit(20).
		Find(&recentPlays)

	// Get top artists (last 7 days)
	type artistCount struct {
		Artist string
		Count  int64
	}
	var topArtists []artistCount
	h.db.Model(&models.PlayHistory{}).
		Select("artist, COUNT(*) as count").
		Where("station_id = ? AND started_at >= ?", station.ID, time.Now().AddDate(0, 0, -7)).
		Group("artist").
		Order("count DESC").
		Limit(10).
		Scan(&topArtists)

	// Get play counts per hour (last 24 hours)
	type hourlyCount struct {
		Hour  int
		Count int64
	}
	var hourlyPlays []hourlyCount
	// Note: This query is PostgreSQL-specific
	h.db.Raw(`
		SELECT EXTRACT(HOUR FROM started_at) as hour, COUNT(*) as count
		FROM play_histories
		WHERE station_id = ? AND started_at >= ?
		GROUP BY hour
		ORDER BY hour
	`, station.ID, time.Now().Add(-24*time.Hour)).Scan(&hourlyPlays)

	h.Render(w, r, "pages/dashboard/analytics/dashboard", PageData{
		Title:    "Analytics",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"RecentPlays": recentPlays,
			"TopArtists":  topArtists,
			"HourlyPlays": hourlyPlays,
		},
	})
}

// AnalyticsNowPlaying returns the current playing item
func (h *Handler) AnalyticsNowPlaying(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		w.Write([]byte(`<span class="text-body-secondary">No station</span>`))
		return
	}

	// Get most recent play history entry that might still be playing
	var recent models.PlayHistory
	if err := h.db.Where("station_id = ? AND ended_at IS NULL OR ended_at > ?",
		station.ID, time.Now()).
		Order("started_at DESC").
		First(&recent).Error; err != nil {
		w.Write([]byte(`<i class="bi bi-music-note"></i> <span class="text-body-secondary">Nothing playing</span>`))
		return
	}

	h.RenderPartial(w, r, "partials/now-playing", recent)
}

// AnalyticsHistory renders the play history page
func (h *Handler) AnalyticsHistory(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	// Pagination
	page := 1
	perPage := 50

	var history []models.PlayHistory
	var total int64

	query := h.db.Model(&models.PlayHistory{}).Where("station_id = ?", station.ID)

	// Date filter
	if from := r.URL.Query().Get("from"); from != "" {
		if t, err := time.Parse("2006-01-02", from); err == nil {
			query = query.Where("started_at >= ?", t)
		}
	}
	if to := r.URL.Query().Get("to"); to != "" {
		if t, err := time.Parse("2006-01-02", to); err == nil {
			query = query.Where("started_at <= ?", t.Add(24*time.Hour))
		}
	}

	query.Count(&total)
	query.Order("started_at DESC").
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&history)

	h.Render(w, r, "pages/dashboard/analytics/history", PageData{
		Title:    "Play History",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"History": history,
			"Total":   total,
			"Page":    page,
			"PerPage": perPage,
		},
	})
}

// AnalyticsSpins renders the spin reports page
func (h *Handler) AnalyticsSpins(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	// Default to last 7 days
	fromDate := time.Now().AddDate(0, 0, -7)
	toDate := time.Now()

	if from := r.URL.Query().Get("from"); from != "" {
		if t, err := time.Parse("2006-01-02", from); err == nil {
			fromDate = t
		}
	}
	if to := r.URL.Query().Get("to"); to != "" {
		if t, err := time.Parse("2006-01-02", to); err == nil {
			toDate = t.Add(24 * time.Hour)
		}
	}

	// Top tracks
	type trackSpin struct {
		Artist string
		Title  string
		Count  int64
	}
	var topTracks []trackSpin
	h.db.Model(&models.PlayHistory{}).
		Select("artist, title, COUNT(*) as count").
		Where("station_id = ? AND started_at >= ? AND started_at <= ?", station.ID, fromDate, toDate).
		Group("artist, title").
		Order("count DESC").
		Limit(50).
		Scan(&topTracks)

	// Top artists
	type artistSpin struct {
		Artist string
		Count  int64
	}
	var topArtists []artistSpin
	h.db.Model(&models.PlayHistory{}).
		Select("artist, COUNT(*) as count").
		Where("station_id = ? AND started_at >= ? AND started_at <= ?", station.ID, fromDate, toDate).
		Group("artist").
		Order("count DESC").
		Limit(20).
		Scan(&topArtists)

	h.Render(w, r, "pages/dashboard/analytics/spins", PageData{
		Title:    "Spin Reports",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"TopTracks":  topTracks,
			"TopArtists": topArtists,
			"FromDate":   fromDate.Format("2006-01-02"),
			"ToDate":     toDate.Format("2006-01-02"),
		},
	})
}

// AnalyticsListeners renders listener statistics (placeholder)
func (h *Handler) AnalyticsListeners(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	// TODO: Integrate with Icecast/streaming server stats

	h.Render(w, r, "pages/dashboard/analytics/listeners", PageData{
		Title:    "Listener Stats",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Message": "Listener statistics coming soon",
		},
	})
}

// Playout control handlers

// PlayoutSkip skips the current track
func (h *Handler) PlayoutSkip(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	// TODO: Call playout manager to skip

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(`<div class="alert alert-success">Skip command sent</div>`))
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// PlayoutStop stops playout
func (h *Handler) PlayoutStop(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	// TODO: Call playout manager to stop

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(`<div class="alert alert-warning">Playout stopped</div>`))
		return
	}

	w.WriteHeader(http.StatusOK)
}

// PlayoutReload reloads the playout pipeline
func (h *Handler) PlayoutReload(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	// TODO: Call playout manager to reload

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(`<div class="alert alert-info">Playout reload initiated</div>`))
		return
	}

	w.WriteHeader(http.StatusOK)
}
