/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
)

// SmartBlockIssue describes a fill problem with a single smart block entry.
type SmartBlockIssue struct {
	EntryID     string
	BlockName   string
	StartsAt    time.Time
	EndsAt      time.Time
	FillPct     float64
	Underfilled bool
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

// ScheduleHealthReportData is the top-level data passed to the report template.
type ScheduleHealthReportData struct {
	Station  *models.Station
	Days     []DayHealth
	WeekFrom time.Time
	WeekTo   time.Time
}

// ScheduleHealthReport renders the 7-day schedule health report.
func (h *Handler) ScheduleHealthReport(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	weekFrom := today
	weekTo := today.Add(7 * 24 * time.Hour)

	days := make([]DayHealth, 0, 7)
	engine := smartblock.New(h.db, h.logger)

	for d := 0; d < 7; d++ {
		dayStart := weekFrom.Add(time.Duration(d) * 24 * time.Hour)
		dayEnd := dayStart.Add(24 * time.Hour)

		var entries []models.ScheduleEntry
		h.db.Where("station_id = ? AND starts_at >= ? AND starts_at < ?", station.ID, dayStart, dayEnd).
			Find(&entries)

		var scheduledSecs float64
		for _, e := range entries {
			dur := e.EndsAt.Sub(e.StartsAt)
			if dur > 0 {
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

		// Analyze smart block entries for fill issues.
		var issues []SmartBlockIssue
		for _, e := range entries {
			if e.SourceType != "smart_block" {
				continue
			}

			targetDur := e.EndsAt.Sub(e.StartsAt)
			if targetDur <= 0 {
				continue
			}

			var blockName string
			var sb models.SmartBlock
			if h.db.First(&sb, "id = ?", e.SourceID).Error == nil {
				blockName = sb.Name
			}

			result, genErr := engine.Generate(r.Context(), smartblock.GenerateRequest{
				SmartBlockID: e.SourceID,
				Seed:         e.StartsAt.Unix(),
				Duration:     targetDur.Milliseconds(),
				StationID:    station.ID,
				MountID:      e.MountID,
			})

			issue := SmartBlockIssue{
				EntryID:   e.ID,
				BlockName: blockName,
				StartsAt:  e.StartsAt,
				EndsAt:    e.EndsAt,
			}

			if genErr != nil {
				issue.Error = genErr.Error()
				issue.Underfilled = true
				issue.FillPct = 0
			} else {
				targetMS := targetDur.Milliseconds()
				if targetMS > 0 {
					issue.FillPct = float64(result.TotalMS) / float64(targetMS) * 100.0
				}
				issue.Underfilled = result.Exhausted
			}

			if issue.Underfilled || issue.Error != "" || issue.FillPct < 95 {
				issues = append(issues, issue)
			}
		}

		// Actual play history count for past days.
		var playedCount int64
		if dayEnd.Before(time.Now().UTC()) {
			h.db.Model(&models.PlayHistory{}).
				Where("station_id = ? AND started_at >= ? AND started_at < ?", station.ID, dayStart, dayEnd).
				Count(&playedCount)
		}

		// Determine overall health.
		health := "green"
		hasSmartBlockError := len(issues) > 0
		for _, iss := range issues {
			if iss.Error != "" || iss.FillPct < 70 {
				hasSmartBlockError = true
				break
			}
		}
		if coveragePct < 70 || (hasSmartBlockError && len(issues) > 0) {
			health = "red"
		} else if coveragePct < 95 || len(issues) > 0 {
			health = "yellow"
		}

		days = append(days, DayHealth{
			Date:             dayStart,
			ScheduledHours:   scheduledHours,
			GapHours:         gapHours,
			CoveragePct:      coveragePct,
			SmartBlockIssues: issues,
			Health:           health,
			PlayedCount:      int(playedCount),
			PlannedCount:     len(entries),
		})
	}

	// Compute summary counts for the template.
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
			"Station":     station,
			"Days":        days,
			"WeekFrom":    weekFrom,
			"WeekTo":      weekTo,
			"GreenCount":  greenCount,
			"YellowCount": yellowCount,
			"RedCount":    redCount,
		},
	})
}
