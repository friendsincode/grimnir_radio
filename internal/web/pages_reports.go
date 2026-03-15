/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// SmartBlockIssue describes a fill problem with a single smart block entry.
type SmartBlockIssue struct {
	EntryID     string
	BlockID     string
	BlockName   string
	StartsAt    time.Time
	EndsAt      time.Time
	FillPct     float64
	Underfilled bool
	Pending     bool // true when entry is beyond materialization lookahead
	Error       string
}

// DayHealth holds the computed health for a single station day.
type DayHealth struct {
	Date             time.Time
	ScheduledHours   float64
	GapHours         float64
	CoveragePct      float64
	SmartBlockIssues []SmartBlockIssue
	Health           string // "green" | "yellow" | "red"
	PlayedCount      int    // actual plays from history (past days only)
	PlannedCount     int    // number of scheduled entries
}

// ScheduleHealthReport renders the 7-day schedule health report.
func (h *Handler) ScheduleHealthReport(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)
	weekFrom := today
	weekTo := today.Add(7 * 24 * time.Hour)
	lookahead := h.schedulerLookaheadDuration()

	// ── 1. Load all schedule entries for the whole 7-day window in one query ──
	var allEntries []models.ScheduleEntry
	h.db.WithContext(r.Context()).
		Where("station_id = ? AND starts_at >= ? AND starts_at < ?", station.ID, weekFrom, weekTo).
		Find(&allEntries)

	// Group entries by day index.
	entriesByDay := make([][]models.ScheduleEntry, 7)
	for i := range entriesByDay {
		entriesByDay[i] = []models.ScheduleEntry{}
	}
	for _, e := range allEntries {
		d := int(e.StartsAt.Sub(weekFrom).Hours() / 24)
		if d >= 0 && d < 7 {
			entriesByDay[d] = append(entriesByDay[d], e)
		}
	}

	// ── 2. Batch-load smart block names for all referenced blocks ──
	sbIDSet := make(map[string]struct{})
	for _, e := range allEntries {
		if e.SourceType == "smart_block" {
			sbIDSet[e.SourceID] = struct{}{}
		}
	}
	sbNames := make(map[string]string)
	if len(sbIDSet) > 0 {
		ids := make([]string, 0, len(sbIDSet))
		for id := range sbIDSet {
			ids = append(ids, id)
		}
		var blocks []models.SmartBlock
		h.db.WithContext(r.Context()).Select("id, name").Where("id IN ?", ids).Find(&blocks)
		for _, b := range blocks {
			sbNames[b.ID] = b.Name
		}
	}

	// ── 3. Batch-load play history counts per day (past days only) ──
	type dayCount struct {
		Day   time.Time
		Count int64
	}
	playedByDay := make(map[int]int64)
	if weekFrom.Before(now) {
		pastEnd := now
		if pastEnd.After(weekTo) {
			pastEnd = weekTo
		}
		rows, err := h.db.WithContext(r.Context()).
			Model(&models.PlayHistory{}).
			Select("DATE_TRUNC('day', started_at) as day, COUNT(*) as count").
			Where("station_id = ? AND started_at >= ? AND started_at < ?", station.ID, weekFrom, pastEnd).
			Group("DATE_TRUNC('day', started_at)").
			Rows()
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var dc dayCount
				if rows.Scan(&dc.Day, &dc.Count) == nil {
					d := int(dc.Day.UTC().Sub(weekFrom).Hours() / 24)
					if d >= 0 && d < 7 {
						playedByDay[d] = dc.Count
					}
				}
			}
		}
	}

	// ── 4. Compute each day in parallel ──
	days := make([]DayHealth, 7)
	var wg sync.WaitGroup

	for d := 0; d < 7; d++ {
		wg.Add(1)
		go func(d int) {
			defer wg.Done()

			dayStart := weekFrom.Add(time.Duration(d) * 24 * time.Hour)
			entries := entriesByDay[d]

			// Coverage calculation.
			var scheduledSecs float64
			for _, e := range entries {
				if dur := e.EndsAt.Sub(e.StartsAt); dur > 0 {
					scheduledSecs += dur.Seconds()
				}
			}
			scheduledHours := scheduledSecs / 3600.0
			gapHours := 24.0 - scheduledHours
			if gapHours < 0 {
				gapHours = 0
			}
			coveragePct := scheduledHours / 24.0 * 100.0
			if coveragePct > 100 {
				coveragePct = 100
			}

			// Smart block fill check — uses materialized entries, not engine.Generate().
			var issues []SmartBlockIssue
			for _, e := range entries {
				if e.SourceType != "smart_block" {
					continue
				}
				targetDur := e.EndsAt.Sub(e.StartsAt)
				if targetDur <= 0 {
					continue
				}

				issue := SmartBlockIssue{
					EntryID:   e.ID,
					BlockID:   e.SourceID,
					BlockName: sbNames[e.SourceID],
					StartsAt:  e.StartsAt,
					EndsAt:    e.EndsAt,
				}
				if issue.BlockName == "" {
					issue.BlockName = e.SourceID
				}

				// If this slot is beyond the lookahead window it hasn't been
				// materialized yet — flag it as pending rather than an error.
				if e.StartsAt.After(now.Add(lookahead)) {
					issue.Pending = true
					issues = append(issues, issue)
					continue
				}

				// Fast check: query materialized media entries for this slot.
				type fillResult struct {
					TotalMS int64
					Cnt     int64
				}
				var fill fillResult
				h.db.WithContext(r.Context()).
					Model(&models.ScheduleEntry{}).
					Select("COALESCE(SUM(EXTRACT(EPOCH FROM (ends_at - starts_at)) * 1000), 0) AS total_ms, COUNT(*) AS cnt").
					Where("station_id = ? AND source_type = 'media' AND metadata->>'smart_block_id' = ? AND starts_at >= ? AND starts_at < ?",
						station.ID, e.SourceID, e.StartsAt, e.EndsAt).
					Scan(&fill)

				targetMS := targetDur.Milliseconds()
				if fill.Cnt == 0 {
					issue.Underfilled = true
					issue.Error = h.diagnoseMissingMaterialization(r, station.ID, e.SourceID, sbNames[e.SourceID])
				} else if targetMS > 0 {
				} else if targetMS > 0 {
					issue.FillPct = float64(fill.TotalMS) / float64(targetMS) * 100.0
					if issue.FillPct > 100 {
						issue.FillPct = 100
					}
					issue.Underfilled = issue.FillPct < 95
				}

				if issue.Underfilled || issue.Error != "" {
					issues = append(issues, issue)
				}
			}

			// Overall health.
			health := "green"
			for _, iss := range issues {
				if iss.Pending {
					continue
				}
				if iss.Error != "" || iss.FillPct < 70 {
					health = "red"
					break
				}
				if health != "red" {
					health = "yellow"
				}
			}
			if health == "green" && coveragePct < 70 {
				health = "red"
			} else if health == "green" && coveragePct < 95 {
				health = "yellow"
			}

			days[d] = DayHealth{
				Date:             dayStart,
				ScheduledHours:   scheduledHours,
				GapHours:         gapHours,
				CoveragePct:      coveragePct,
				SmartBlockIssues: issues,
				Health:           health,
				PlayedCount:      int(playedByDay[d]),
				PlannedCount:     len(entries),
			}
		}(d)
	}

	wg.Wait()

	// Summary counts.
	greenCount, yellowCount, redCount := 0, 0, 0
	for _, day := range days {
		switch day.Health {
		case "green":
			greenCount++
		case "yellow":
			yellowCount++
		case "red":
			redCount++
		}
	}

	h.Render(w, r, "pages/dashboard/schedule/report", PageData{
		Title:    "Schedule Health Report",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Station":         station,
			"Days":            days,
			"WeekFrom":        weekFrom,
			"WeekTo":          weekTo,
			"GreenCount":      greenCount,
			"YellowCount":     yellowCount,
			"RedCount":        redCount,
			"GeneratedAt":     now,
			"LookaheadHours":  int(lookahead.Hours()),
			"LookaheadCutoff": now.Add(lookahead),
		},
	})
}

// diagnoseMissingMaterialization returns a human-readable explanation for why
// a smart block slot has no materialized media entries.
func (h *Handler) diagnoseMissingMaterialization(r *http.Request, stationID, blockID, blockName string) string {
	// 1. Does the block still exist?
	if blockName == "" || blockName == blockID {
		// sbNames lookup failed — block may be deleted
		var count int64
		h.db.WithContext(r.Context()).Model(&models.SmartBlock{}).Where("id = ?", blockID).Count(&count)
		if count == 0 {
			return fmt.Sprintf("Smart block no longer exists (ID: %s) — remove this schedule entry", blockID)
		}
	}

	// 2. How many media items are linked to this block (via its playlists)?
	var totalTracks int64
	h.db.WithContext(r.Context()).Raw(`
		SELECT COUNT(DISTINCT mi.id)
		FROM media_items mi
		JOIN playlist_items pi ON pi.media_id = mi.id
		JOIN smart_block_playlists sbp ON sbp.playlist_id = pi.playlist_id
		WHERE sbp.smart_block_id = ? AND mi.analysis_state != 'failed' AND mi.duration > 0
	`, blockID).Scan(&totalTracks)

	if totalTracks == 0 {
		// Check if there are any playlists at all
		var playlistCount int64
		h.db.WithContext(r.Context()).Raw(
			`SELECT COUNT(*) FROM smart_block_playlists WHERE smart_block_id = ?`, blockID,
		).Scan(&playlistCount)
		if playlistCount == 0 {
			return fmt.Sprintf("Block \"%s\" has no playlists assigned — add a playlist with tracks", blockName)
		}
		return fmt.Sprintf("Block \"%s\" has %d playlist(s) but 0 eligible tracks — check that tracks are analyzed and not marked failed", blockName, playlistCount)
	}

	// 3. Tracks exist but still nothing materialized — scheduler hasn't run yet for this slot.
	return fmt.Sprintf("Block \"%s\" has %d eligible track(s) but scheduler has not materialized this slot yet — click Re-run Scheduler", blockName, totalTracks)
}

// ScheduleRefreshReport triggers the scheduler for the current station then
// redirects back to the report page.
func (h *Handler) ScheduleRefreshReport(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}
	if h.scheduler != nil {
		_ = h.scheduler.RefreshStation(r.Context(), station.ID)
	}
	http.Redirect(w, r, "/dashboard/schedule/report", http.StatusSeeOther)
}
