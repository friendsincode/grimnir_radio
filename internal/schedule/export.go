/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package schedule

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ExportService handles schedule import/export.
type ExportService struct {
	db     *gorm.DB
	logger zerolog.Logger
}

// NewExportService creates a new export service.
func NewExportService(db *gorm.DB, logger zerolog.Logger) *ExportService {
	return &ExportService{
		db:     db,
		logger: logger.With().Str("component", "schedule_export").Logger(),
	}
}

// ExportICalResult contains the iCal export data.
type ExportICalResult struct {
	Data        []byte
	Filename    string
	ContentType string
}

// ExportToICal exports schedule to iCal format.
func (s *ExportService) ExportToICal(ctx context.Context, stationID string, start, end time.Time) (*ExportICalResult, error) {
	// Get station
	var station models.Station
	if err := s.db.First(&station, "id = ?", stationID).Error; err != nil {
		return nil, fmt.Errorf("station not found: %w", err)
	}

	// Get show instances
	var instances []models.ShowInstance
	if err := s.db.Where("station_id = ? AND starts_at >= ? AND starts_at < ?", stationID, start, end).
		Where("status = ?", models.ShowInstanceScheduled).
		Preload("Show").
		Preload("Host").
		Order("starts_at ASC").
		Find(&instances).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch instances: %w", err)
	}

	// Build iCal
	var buf bytes.Buffer
	buf.WriteString("BEGIN:VCALENDAR\r\n")
	buf.WriteString("VERSION:2.0\r\n")
	buf.WriteString("PRODID:-//Grimnir Radio//Schedule Export//EN\r\n")
	buf.WriteString(fmt.Sprintf("X-WR-CALNAME:%s Schedule\r\n", escapeICalText(station.Name)))
	buf.WriteString("CALSCALE:GREGORIAN\r\n")
	buf.WriteString("METHOD:PUBLISH\r\n")

	for _, inst := range instances {
		if inst.Show == nil {
			continue
		}

		buf.WriteString("BEGIN:VEVENT\r\n")
		buf.WriteString(fmt.Sprintf("UID:%s@grimnir\r\n", inst.ID))
		buf.WriteString(fmt.Sprintf("DTSTAMP:%s\r\n", formatICalTime(time.Now())))
		buf.WriteString(fmt.Sprintf("DTSTART:%s\r\n", formatICalTime(inst.StartsAt)))
		buf.WriteString(fmt.Sprintf("DTEND:%s\r\n", formatICalTime(inst.EndsAt)))
		buf.WriteString(fmt.Sprintf("SUMMARY:%s\r\n", escapeICalText(inst.Show.Name)))

		if inst.Show.Description != "" {
			buf.WriteString(fmt.Sprintf("DESCRIPTION:%s\r\n", escapeICalText(inst.Show.Description)))
		}

		if inst.Show.Color != "" {
			buf.WriteString(fmt.Sprintf("X-APPLE-CALENDAR-COLOR:%s\r\n", inst.Show.Color))
		}

		hostEmail := ""
		if inst.Host != nil {
			hostEmail = inst.Host.Email
		} else if inst.Show.Host != nil {
			hostEmail = inst.Show.Host.Email
		}
		if hostEmail != "" {
			buf.WriteString(fmt.Sprintf("ORGANIZER:mailto:%s\r\n", hostEmail))
		}

		buf.WriteString("END:VEVENT\r\n")
	}

	buf.WriteString("END:VCALENDAR\r\n")

	filename := fmt.Sprintf("%s-schedule-%s-to-%s.ics",
		slugify(station.Name),
		start.Format("2006-01-02"),
		end.Format("2006-01-02"))

	return &ExportICalResult{
		Data:        buf.Bytes(),
		Filename:    filename,
		ContentType: "text/calendar; charset=utf-8",
	}, nil
}

// ImportICalResult contains the result of an iCal import.
type ImportICalResult struct {
	Imported int
	Skipped  int
	Errors   []string
}

// ImportFromICal imports schedule from iCal data.
func (s *ExportService) ImportFromICal(ctx context.Context, stationID string, data io.Reader) (*ImportICalResult, error) {
	result := &ImportICalResult{}

	// Read all data
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(data); err != nil {
		return nil, fmt.Errorf("failed to read iCal data: %w", err)
	}

	content := buf.String()

	// Parse events (simple parser)
	events := parseICalEvents(content)

	for _, event := range events {
		// Check for required fields
		if event.Summary == "" || event.Start.IsZero() || event.End.IsZero() {
			result.Skipped++
			continue
		}

		// Find or create show
		var show models.Show
		if err := s.db.Where("station_id = ? AND name = ?", stationID, event.Summary).First(&show).Error; err != nil {
			// Create new show
			show = models.Show{
				ID:                     uuid.NewString(),
				StationID:              stationID,
				Name:                   event.Summary,
				Description:            event.Description,
				DefaultDurationMinutes: int(event.End.Sub(event.Start).Minutes()),
				Active:                 true,
			}
			if err := s.db.Create(&show).Error; err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("failed to create show %s: %v", event.Summary, err))
				continue
			}
		}

		// Check for conflicts
		var conflictCount int64
		s.db.Model(&models.ShowInstance{}).
			Where("station_id = ?", stationID).
			Where("starts_at < ? AND ends_at > ?", event.End, event.Start).
			Where("status = ?", models.ShowInstanceScheduled).
			Count(&conflictCount)

		if conflictCount > 0 {
			result.Skipped++
			result.Errors = append(result.Errors, fmt.Sprintf("conflict for %s at %s", event.Summary, event.Start.Format(time.RFC3339)))
			continue
		}

		// Create instance
		instance := &models.ShowInstance{
			ID:            uuid.NewString(),
			StationID:     stationID,
			ShowID:        show.ID,
			StartsAt:      event.Start,
			EndsAt:        event.End,
			Status:        models.ShowInstanceScheduled,
			ExceptionNote: "Imported from iCal",
		}

		if err := s.db.Create(instance).Error; err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to create instance for %s: %v", event.Summary, err))
			continue
		}

		result.Imported++
	}

	s.logger.Info().
		Str("station", stationID).
		Int("imported", result.Imported).
		Int("skipped", result.Skipped).
		Msg("iCal import completed")

	return result, nil
}

// ICalEvent represents a parsed iCal event.
type ICalEvent struct {
	UID         string
	Summary     string
	Description string
	Start       time.Time
	End         time.Time
}

// parseICalEvents parses events from iCal content (simple implementation).
func parseICalEvents(content string) []ICalEvent {
	var events []ICalEvent
	var currentEvent *ICalEvent

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimSuffix(line, "\r")

		if line == "BEGIN:VEVENT" {
			currentEvent = &ICalEvent{}
		} else if line == "END:VEVENT" && currentEvent != nil {
			events = append(events, *currentEvent)
			currentEvent = nil
		} else if currentEvent != nil {
			if strings.HasPrefix(line, "UID:") {
				currentEvent.UID = strings.TrimPrefix(line, "UID:")
			} else if strings.HasPrefix(line, "SUMMARY:") {
				currentEvent.Summary = unescapeICalText(strings.TrimPrefix(line, "SUMMARY:"))
			} else if strings.HasPrefix(line, "DESCRIPTION:") {
				currentEvent.Description = unescapeICalText(strings.TrimPrefix(line, "DESCRIPTION:"))
			} else if strings.HasPrefix(line, "DTSTART:") {
				currentEvent.Start = parseICalTime(strings.TrimPrefix(line, "DTSTART:"))
			} else if strings.HasPrefix(line, "DTEND:") {
				currentEvent.End = parseICalTime(strings.TrimPrefix(line, "DTEND:"))
			}
		}
	}

	return events
}

// parseICalTime parses an iCal time string.
func parseICalTime(s string) time.Time {
	// Remove TZID if present
	if idx := strings.Index(s, ":"); idx > 0 {
		s = s[idx+1:]
	}

	// Try various formats
	formats := []string{
		"20060102T150405Z",
		"20060102T150405",
		"20060102",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t
		}
	}

	return time.Time{}
}

// ExportToPDF exports schedule to a printable HTML format (can be converted to PDF).
func (s *ExportService) ExportToPDF(ctx context.Context, stationID string, start, end time.Time) ([]byte, error) {
	// Get station
	var station models.Station
	if err := s.db.First(&station, "id = ?", stationID).Error; err != nil {
		return nil, fmt.Errorf("station not found: %w", err)
	}

	// Get show instances grouped by day
	var instances []models.ShowInstance
	if err := s.db.Where("station_id = ? AND starts_at >= ? AND starts_at < ?", stationID, start, end).
		Where("status = ?", models.ShowInstanceScheduled).
		Preload("Show").
		Preload("Host").
		Order("starts_at ASC").
		Find(&instances).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch instances: %w", err)
	}

	// Group by day
	dayInstances := make(map[string][]models.ShowInstance)
	for _, inst := range instances {
		day := inst.StartsAt.Format("2006-01-02")
		dayInstances[day] = append(dayInstances[day], inst)
	}

	// Generate printable HTML
	var buf bytes.Buffer
	buf.WriteString(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>` + station.Name + ` Schedule</title>
    <style>
        @page { margin: 1cm; }
        body { font-family: Arial, sans-serif; font-size: 11pt; line-height: 1.4; }
        h1 { font-size: 18pt; margin-bottom: 5mm; border-bottom: 2px solid #333; padding-bottom: 3mm; }
        h2 { font-size: 14pt; margin-top: 5mm; margin-bottom: 3mm; color: #444; }
        .day { page-break-inside: avoid; margin-bottom: 5mm; }
        table { width: 100%; border-collapse: collapse; }
        th, td { padding: 2mm 3mm; text-align: left; border-bottom: 1px solid #ddd; }
        th { background: #f5f5f5; font-weight: bold; }
        .time { width: 25%; white-space: nowrap; }
        .show { width: 40%; }
        .host { width: 35%; color: #666; }
        .color-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 5px; }
        .footer { margin-top: 10mm; font-size: 9pt; color: #666; text-align: center; }
    </style>
</head>
<body>
    <h1>` + station.Name + ` Schedule</h1>
    <p>` + start.Format("January 2, 2006") + ` - ` + end.Format("January 2, 2006") + `</p>
`)

	// Sort days
	days := make([]string, 0, len(dayInstances))
	for day := range dayInstances {
		days = append(days, day)
	}
	sortStrings(days)

	for _, day := range days {
		insts := dayInstances[day]
		dayTime, _ := time.Parse("2006-01-02", day)

		buf.WriteString(`    <div class="day">
        <h2>` + dayTime.Format("Monday, January 2") + `</h2>
        <table>
            <tr><th class="time">Time</th><th class="show">Show</th><th class="host">Host</th></tr>
`)

		for _, inst := range insts {
			showName := "Unknown"
			showColor := "#ccc"
			hostName := ""

			if inst.Show != nil {
				showName = inst.Show.Name
				if inst.Show.Color != "" {
					showColor = inst.Show.Color
				}
			}

			if inst.Host != nil {
				hostName = inst.Host.Email
			} else if inst.Show != nil && inst.Show.Host != nil {
				hostName = inst.Show.Host.Email
			}

			buf.WriteString(fmt.Sprintf(`            <tr>
                <td class="time">%s - %s</td>
                <td class="show"><span class="color-dot" style="background:%s"></span>%s</td>
                <td class="host">%s</td>
            </tr>
`,
				inst.StartsAt.Format("3:04 PM"),
				inst.EndsAt.Format("3:04 PM"),
				showColor,
				showName,
				hostName))
		}

		buf.WriteString(`        </table>
    </div>
`)
	}

	buf.WriteString(`    <div class="footer">
        Generated by Grimnir Radio on ` + time.Now().Format("January 2, 2006 at 3:04 PM") + `
    </div>
</body>
</html>`)

	return buf.Bytes(), nil
}

// Helper functions

func formatICalTime(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

func escapeICalText(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

func unescapeICalText(s string) string {
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\,", ",")
	s = strings.ReplaceAll(s, "\\;", ";")
	s = strings.ReplaceAll(s, "\\\\", "\\")
	return s
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func sortStrings(s []string) {
	for i := 0; i < len(s)-1; i++ {
		for j := 0; j < len(s)-i-1; j++ {
			if s[j] > s[j+1] {
				s[j], s[j+1] = s[j+1], s[j]
			}
		}
	}
}
