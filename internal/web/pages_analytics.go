/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

type listenerSeriesPoint struct {
	Timestamp string `json:"timestamp"`
	Listeners int    `json:"listeners"`
}

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

	// Use Session clones to avoid Count mutating query state
	query.Session(&gorm.Session{}).Count(&total)
	query.Session(&gorm.Session{}).Order("started_at DESC").
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

// AnalyticsListeners renders listener statistics.
func (h *Handler) AnalyticsListeners(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	now := time.Now()
	windowStart := now.Add(-24 * time.Hour)

	loc := time.UTC
	if station.Timezone != "" {
		if loaded, err := time.LoadLocation(station.Timezone); err == nil {
			loc = loaded
		}
	}
	todayStartLocal := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, loc)
	todayStartUTC := todayStartLocal.UTC()

	currentListeners := 0
	if h.director != nil {
		if listeners, err := h.director.ListenerCount(r.Context(), station.ID); err == nil {
			currentListeners = listeners
		} else {
			h.logger.Warn().Err(err).Str("station_id", station.ID).Msg("failed to load listener counts")
		}
	}

	peakToday := currentListeners
	var peakTodayDB sql.NullInt64
	if err := h.db.Model(&models.ListenerSample{}).
		Select("MAX(listeners)").
		Where("station_id = ? AND captured_at >= ?", station.ID, todayStartUTC).
		Scan(&peakTodayDB).Error; err == nil && peakTodayDB.Valid && int(peakTodayDB.Int64) > peakToday {
		peakToday = int(peakTodayDB.Int64)
	}

	avg24h := float64(currentListeners)
	var avg24hDB sql.NullFloat64
	if err := h.db.Model(&models.ListenerSample{}).
		Select("AVG(listeners)").
		Where("station_id = ? AND captured_at >= ?", station.ID, windowStart.UTC()).
		Scan(&avg24hDB).Error; err == nil && avg24hDB.Valid {
		avg24h = avg24hDB.Float64
	}

	series, err := h.buildListenerSeries(r.Context(), station.ID, windowStart, now, 5*time.Minute)
	if err != nil {
		h.logger.Warn().Err(err).Str("station_id", station.ID).Msg("failed to build listener time series")
		series = []listenerSeriesPoint{}
	}

	h.Render(w, r, "pages/dashboard/analytics/listeners", PageData{
		Title:    "Listener Stats",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"CurrentListeners": currentListeners,
			"PeakToday":        peakToday,
			"Avg24h":           avg24h,
			"SeriesPoints":     series,
		},
	})
}

// AnalyticsListenersTimeSeries returns JSON listener time-series data for charts.
func (h *Handler) AnalyticsListenersTimeSeries(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	now := time.Now()
	windowStart := now.Add(-24 * time.Hour)
	series, err := h.buildListenerSeries(r.Context(), station.ID, windowStart, now, 5*time.Minute)
	if err != nil {
		http.Error(w, "Failed to load listener time series", http.StatusInternalServerError)
		return
	}

	currentListeners := 0
	if h.director != nil {
		if listeners, err := h.director.ListenerCount(r.Context(), station.ID); err == nil {
			currentListeners = listeners
		}
	}

	json.NewEncoder(w).Encode(map[string]any{
		"station_id": station.ID,
		"from":       windowStart.UTC(),
		"to":         now.UTC(),
		"current":    currentListeners,
		"points":     series,
	})
}

func (h *Handler) buildListenerSeries(ctx context.Context, stationID string, from, to time.Time, bucketSize time.Duration) ([]listenerSeriesPoint, error) {
	if bucketSize <= 0 {
		bucketSize = 5 * time.Minute
	}

	var samples []models.ListenerSample
	if err := h.db.WithContext(ctx).
		Where("station_id = ? AND captured_at >= ? AND captured_at <= ?", stationID, from.UTC(), to.UTC()).
		Order("captured_at ASC").
		Find(&samples).Error; err != nil {
		return nil, err
	}

	bucketSums := map[int64]int{}
	bucketCounts := map[int64]int{}
	for _, sample := range samples {
		ts := sample.CapturedAt.UTC().Truncate(bucketSize).Unix()
		bucketSums[ts] += sample.Listeners
		bucketCounts[ts]++
	}

	points := make([]listenerSeriesPoint, 0, int(to.Sub(from)/bucketSize)+1)
	for t := from.UTC().Truncate(bucketSize); !t.After(to.UTC()); t = t.Add(bucketSize) {
		ts := t.Unix()
		listeners := 0
		if count := bucketCounts[ts]; count > 0 {
			listeners = bucketSums[ts] / count
		}
		points = append(points, listenerSeriesPoint{
			Timestamp: t.Format(time.RFC3339),
			Listeners: listeners,
		})
	}

	return points, nil
}

// Playout control handlers

// PlayoutSkip skips the current track
func (h *Handler) PlayoutSkip(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	if h.director == nil {
		http.Error(w, "Playout system unavailable", http.StatusServiceUnavailable)
		return
	}
	skipped, err := h.director.SkipStation(r.Context(), station.ID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to skip track: %v", err), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(fmt.Sprintf(`<div class="alert alert-success">Skipped on %d active mount(s)</div>`, skipped)))
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "mounts": skipped})
}

// PlayoutStop stops playout
func (h *Handler) PlayoutStop(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	if h.director == nil {
		http.Error(w, "Playout system unavailable", http.StatusServiceUnavailable)
		return
	}
	stopped, err := h.director.StopStation(r.Context(), station.ID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to stop playout: %v", err), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(fmt.Sprintf(`<div class="alert alert-warning">Playout stopped on %d mount(s)</div>`, stopped)))
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "mounts": stopped})
}

// PlayoutReload reloads the playout pipeline
func (h *Handler) PlayoutReload(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	if h.director == nil {
		http.Error(w, "Playout system unavailable", http.StatusServiceUnavailable)
		return
	}
	reloaded, err := h.director.ReloadStation(r.Context(), station.ID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to reload playout: %v", err), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Write([]byte(fmt.Sprintf(`<div class="alert alert-info">Playout reload initiated on %d mount(s)</div>`, reloaded)))
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "mounts": reloaded})
}
