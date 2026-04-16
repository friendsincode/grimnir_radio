/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package telemetry

import (
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

var startTime = time.Now()

// safeLabel sanitizes a string for use as a Prometheus label value.
// Prometheus panics on invalid UTF-8, so we sanitize first, then truncate
// by rune count (not bytes) to avoid splitting multi-byte sequences.
func safeLabel(s string, maxRunes int) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	runes := []rune(s)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes-3]) + "..."
	}
	return s
}

// UpdateStationMetrics queries the DB and updates station-level Prometheus gauges.
// A deferred recover prevents any bad label value from crashing the server.
func UpdateStationMetrics(db *gorm.DB) {
	defer func() {
		if r := recover(); r != nil {
			// A corrupted or unexpected label value caused a panic.
			// Log nothing here — the caller's ticker loop will retry next cycle.
			_ = r
		}
	}()
	UptimeSeconds.Set(time.Since(startTime).Seconds())

	// Total stations
	var stationCount int64
	db.Table("stations").Count(&stationCount)
	StationsTotal.Set(float64(stationCount))

	// Active stations — stations that have a mount_playout_state row (i.e. currently playing)
	var activeCount int64
	db.Table("mount_playout_states").
		Select("COUNT(DISTINCT station_id)").
		Scan(&activeCount)
	StationsActive.Set(float64(activeCount))

	// Per-station media counts and total duration
	type stationMedia struct {
		StationID     string
		ItemCount     int64
		DurationHours float64
	}
	var stationMediaStats []stationMedia
	db.Table("media_items").
		Select("station_id, COUNT(*) as item_count, COALESCE(SUM(duration), 0) / 3600000.0 as duration_hours").
		Where("analysis_state <> 'failed' AND duration > 0").
		Group("station_id").
		Scan(&stationMediaStats)

	for _, s := range stationMediaStats {
		MediaItemsTotal.WithLabelValues(s.StationID).Set(float64(s.ItemCount))
		MediaLibraryDurationHours.WithLabelValues(s.StationID).Set(s.DurationHours)
	}

	// Now playing per station
	type nowPlaying struct {
		StationID string
		Title     string
		Artist    string
	}
	var npRows []nowPlaying
	db.Raw(`SELECT DISTINCT ON (station_id) station_id, title, artist
		FROM play_histories ORDER BY station_id, started_at DESC`).
		Scan(&npRows)

	// Reset now-playing metric to avoid stale label combos building up
	NowPlayingInfo.Reset()
	for _, np := range npRows {
		NowPlayingInfo.WithLabelValues(np.StationID, safeLabel(np.Title, 60), safeLabel(np.Artist, 40)).Set(1)
	}

	// Plays in last 24h per station
	cutoff := time.Now().Add(-24 * time.Hour)
	type stationPlayCount struct {
		StationID string
		PlayCount int64
	}
	var playCounts []stationPlayCount
	db.Table("play_histories").
		Select("station_id, COUNT(*) as play_count").
		Where("started_at >= ?", cutoff).
		Group("station_id").
		Scan(&playCounts)

	for _, pc := range playCounts {
		PlayHistoryTotal.WithLabelValues(pc.StationID).Set(float64(pc.PlayCount))
	}

	// Interrupted plays in last 24h (tracks cut early, recorded with was_interrupted=true in metadata).
	type stationInterrupted struct {
		StationID      string
		InterruptCount int64
	}
	var interruptedCounts []stationInterrupted
	db.Table("play_histories").
		Select("station_id, COUNT(*) as interrupt_count").
		Where("started_at >= ? AND metadata->>'was_interrupted' = ?", cutoff, "true").
		Group("station_id").
		Scan(&interruptedCounts)

	InterruptedPlays24hTotal.Reset()
	for _, ic := range interruptedCounts {
		InterruptedPlays24hTotal.WithLabelValues(ic.StationID).Set(float64(ic.InterruptCount))
	}
}

// UpdateListenerMetrics resets the listener gauge and sets per-station values.
func UpdateListenerMetrics(counts map[string]int) {
	ListenersCurrentTotal.Reset()
	for stationID, count := range counts {
		ListenersCurrentTotal.WithLabelValues(stationID).Set(float64(count))
	}
}
