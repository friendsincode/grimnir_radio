/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

const listenerRangeInputLayout = "2006-01-02T15:04"

type listenerSeriesPoint struct {
	Timestamp string `json:"timestamp"`
	Listeners int    `json:"listeners"`
}

type analyticsHistoryRow struct {
	StartedAt time.Time
	Title     string
	Artist    string
	Duration  time.Duration
	Source    string
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
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 {
			page = p
		}
	}
	perPage := 100

	var historyEntries []models.PlayHistory
	var total int64
	fromValue := strings.TrimSpace(r.URL.Query().Get("from"))
	toValue := strings.TrimSpace(r.URL.Query().Get("to"))

	query := h.db.Model(&models.PlayHistory{}).Where("station_id = ?", station.ID)

	// Date filter
	if from := fromValue; from != "" {
		if t, err := time.Parse("2006-01-02", from); err == nil {
			query = query.Where("started_at >= ?", t)
		}
	}
	if to := toValue; to != "" {
		if t, err := time.Parse("2006-01-02", to); err == nil {
			query = query.Where("started_at <= ?", t.Add(24*time.Hour))
		}
	}

	// Use Session clones to avoid Count mutating query state
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		h.logger.Error().Err(err).Str("station_id", station.ID).Msg("failed to count play history")
		http.Error(w, "Failed to load play history", http.StatusInternalServerError)
		return
	}
	if err := query.Session(&gorm.Session{}).Order("started_at DESC").
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&historyEntries).Error; err != nil {
		h.logger.Error().Err(err).Str("station_id", station.ID).Msg("failed to fetch play history")
		http.Error(w, "Failed to load play history", http.StatusInternalServerError)
		return
	}

	history := make([]analyticsHistoryRow, 0, len(historyEntries))
	for _, entry := range historyEntries {
		duration := time.Duration(0)
		if !entry.EndedAt.IsZero() && entry.EndedAt.After(entry.StartedAt) {
			duration = entry.EndedAt.Sub(entry.StartedAt)
		}

		source := "automation"
		if entry.Metadata != nil {
			if st, ok := entry.Metadata["source_type"].(string); ok && strings.TrimSpace(st) != "" {
				source = strings.ToLower(strings.TrimSpace(st))
			} else if typ, ok := entry.Metadata["type"].(string); ok && strings.TrimSpace(typ) != "" {
				source = strings.ToLower(strings.TrimSpace(typ))
			}
		}
		if source == "" {
			source = "automation"
		}
		switch source {
		case "live", "live_dj":
			source = "live"
		case "playlist", "media", "smart_block", "clock_template", "webstream":
			// keep known source tags
		default:
			source = "automation"
		}

		history = append(history, analyticsHistoryRow{
			StartedAt: entry.StartedAt,
			Title:     entry.Title,
			Artist:    entry.Artist,
			Duration:  duration,
			Source:    source,
		})
	}

	h.Render(w, r, "pages/dashboard/analytics/history", PageData{
		Title:    "Play History",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"History":   history,
			"Total":     total,
			"Page":      page,
			"PerPage":   perPage,
			"FromValue": fromValue,
			"ToValue":   toValue,
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

	from, to, fromValue, toValue := parseListenerRange(r, station)

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
		Where("station_id = ? AND captured_at >= ? AND captured_at <= ?", station.ID, from.UTC(), to.UTC()).
		Scan(&peakTodayDB).Error; err == nil && peakTodayDB.Valid && int(peakTodayDB.Int64) > peakToday {
		peakToday = int(peakTodayDB.Int64)
	}

	avg24h := float64(currentListeners)
	var avg24hDB sql.NullFloat64
	if err := h.db.Model(&models.ListenerSample{}).
		Select("AVG(listeners)").
		Where("station_id = ? AND captured_at >= ? AND captured_at <= ?", station.ID, from.UTC(), to.UTC()).
		Scan(&avg24hDB).Error; err == nil && avg24hDB.Valid {
		avg24h = avg24hDB.Float64
	}

	series, err := h.buildListenerSeries(r.Context(), station.ID, from, to, 5*time.Minute)
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
			"FromValue":        fromValue,
			"ToValue":          toValue,
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

	from, to, _, _ := parseListenerRange(r, station)
	series, err := h.buildListenerSeries(r.Context(), station.ID, from, to, 5*time.Minute)
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
		"from":       from.UTC(),
		"to":         to.UTC(),
		"current":    currentListeners,
		"points":     series,
	})
}

// AnalyticsListenersExportCSV exports hourly listener stats for the selected range.
func (h *Handler) AnalyticsListenersExportCSV(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	from, to, _, _ := parseListenerRange(r, station)
	loc := stationLocation(station)

	var samples []models.ListenerSample
	if err := h.db.WithContext(r.Context()).
		Where("station_id = ? AND captured_at >= ? AND captured_at <= ?", station.ID, from.UTC(), to.UTC()).
		Order("captured_at ASC").
		Find(&samples).Error; err != nil {
		http.Error(w, "Failed to load listener samples", http.StatusInternalServerError)
		return
	}

	type hourlyBucket struct {
		sum   int
		count int
		peak  int
	}
	buckets := make(map[int64]*hourlyBucket)
	for _, sample := range samples {
		hourStart := sample.CapturedAt.In(loc).Truncate(time.Hour)
		key := hourStart.Unix()
		b := buckets[key]
		if b == nil {
			b = &hourlyBucket{}
			buckets[key] = b
		}
		b.sum += sample.Listeners
		b.count++
		if sample.Listeners > b.peak {
			b.peak = sample.Listeners
		}
	}

	keys := make([]int64, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	filename := fmt.Sprintf("listener-hourly-%s-to-%s.csv", from.Format("20060102-1504"), to.Format("20060102-1504"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"hour_start_local", "hour_start_utc", "avg_listeners", "peak_listeners", "sample_count"})
	for _, key := range keys {
		b := buckets[key]
		if b == nil || b.count == 0 {
			continue
		}
		localHour := time.Unix(key, 0).In(loc)
		utcHour := localHour.UTC()
		avg := float64(b.sum) / float64(b.count)
		_ = cw.Write([]string{
			localHour.Format("2006-01-02 15:04"),
			utcHour.Format(time.RFC3339),
			fmt.Sprintf("%.2f", avg),
			strconv.Itoa(b.peak),
			strconv.Itoa(b.count),
		})
	}
	cw.Flush()
}

func parseListenerRange(r *http.Request, station *models.Station) (time.Time, time.Time, string, string) {
	loc := stationLocation(station)

	now := time.Now().In(loc).Truncate(time.Minute)
	from := now.Add(-24 * time.Hour)
	to := now

	rawFrom := strings.TrimSpace(r.URL.Query().Get("from"))
	rawTo := strings.TrimSpace(r.URL.Query().Get("to"))
	if rawFrom != "" {
		if parsedFrom, err := time.ParseInLocation(listenerRangeInputLayout, rawFrom, loc); err == nil {
			from = parsedFrom
		} else if parsedFromRFC, err := time.Parse(time.RFC3339, rawFrom); err == nil {
			from = parsedFromRFC.In(loc)
		}
	}
	if rawTo != "" {
		if parsedTo, err := time.ParseInLocation(listenerRangeInputLayout, rawTo, loc); err == nil {
			to = parsedTo
		} else if parsedToRFC, err := time.Parse(time.RFC3339, rawTo); err == nil {
			to = parsedToRFC.In(loc)
		}
	}

	if !to.After(from) {
		to = from.Add(24 * time.Hour)
	}

	// Cap large ranges to keep charts responsive.
	if to.Sub(from) > 31*24*time.Hour {
		from = to.Add(-31 * 24 * time.Hour)
	}

	return from.UTC(), to.UTC(), from.Format(listenerRangeInputLayout), to.Format(listenerRangeInputLayout)
}

func stationLocation(station *models.Station) *time.Location {
	if station != nil && station.Timezone != "" {
		if loaded, err := time.LoadLocation(station.Timezone); err == nil {
			return loaded
		}
	}
	return time.UTC
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
