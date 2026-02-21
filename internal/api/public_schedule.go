/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// PublicShow represents a show for public API responses.
type PublicShow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	HostName    string `json:"host_name,omitempty"`
	ArtworkURL  string `json:"artwork_url,omitempty"`
	Color       string `json:"color,omitempty"`
}

// PublicShowInstance represents a scheduled show instance for public API.
type PublicShowInstance struct {
	ID        string     `json:"id"`
	Show      PublicShow `json:"show"`
	StartsAt  time.Time  `json:"starts_at"`
	EndsAt    time.Time  `json:"ends_at"`
	IsCurrent bool       `json:"is_current,omitempty"`
	IsNext    bool       `json:"is_next,omitempty"`
}

// AddPublicScheduleRoutes adds public schedule routes (no auth required).
func (a *API) AddPublicScheduleRoutes(r chi.Router) {
	r.Route("/public/schedule", func(r chi.Router) {
		r.Get("/", a.handlePublicSchedule)
		r.Get("/now", a.handlePublicNowPlaying)
		r.Get("/ical", a.handlePublicScheduleICal)
		r.Get("/rss", a.handlePublicScheduleRSS)
	})
}

// handlePublicSchedule returns the public schedule as JSON.
func (a *API) handlePublicSchedule(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	// Validate station exists and is public
	var station models.Station
	if err := a.db.First(&station, "id = ?", stationID).Error; err != nil {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}

	if !station.Public {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}

	// Parse date range (default to next 7 days)
	start := time.Now()
	end := start.AddDate(0, 0, 7)

	if startStr := r.URL.Query().Get("start"); startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = t
		} else if t, err := time.Parse("2006-01-02", startStr); err == nil {
			start = t
		}
	}

	if endStr := r.URL.Query().Get("end"); endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = t
		} else if t, err := time.Parse("2006-01-02", endStr); err == nil {
			end = t.AddDate(0, 0, 1) // End of day
		}
	}

	// Limit range to 30 days
	if end.Sub(start) > 30*24*time.Hour {
		end = start.AddDate(0, 0, 30)
	}

	// Get show instances in range
	var instances []models.ShowInstance
	a.db.Where("station_id = ? AND starts_at >= ? AND starts_at < ?", stationID, start, end).
		Where("status = ?", models.ShowInstanceScheduled).
		Preload("Show").
		Preload("Host").
		Order("starts_at ASC").
		Find(&instances)

	// Convert to public format
	publicInstances := make([]PublicShowInstance, 0, len(instances))
	for _, inst := range instances {
		if inst.Show == nil {
			continue
		}

		pi := PublicShowInstance{
			ID:       inst.ID,
			StartsAt: inst.StartsAt,
			EndsAt:   inst.EndsAt,
			Show: PublicShow{
				ID:          inst.Show.ID,
				Name:        inst.Show.Name,
				Description: inst.Show.Description,
				Color:       inst.Show.Color,
			},
		}

		if inst.Host != nil {
			pi.Show.HostName = inst.Host.Email // Could add display name field later
		} else if inst.Show.Host != nil {
			pi.Show.HostName = inst.Show.Host.Email
		}

		pi.Show.ArtworkURL = publicArtworkURL(inst.Show.ArtworkPath)

		publicInstances = append(publicInstances, pi)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"station": map[string]any{
			"id":   station.ID,
			"name": station.Name,
		},
		"start":    start.Format(time.RFC3339),
		"end":      end.Format(time.RFC3339),
		"schedule": publicInstances,
	})
}

// handlePublicNowPlaying returns the current and next show.
func (a *API) handlePublicNowPlaying(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	// Validate station
	var station models.Station
	if err := a.db.First(&station, "id = ?", stationID).Error; err != nil {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}

	if !station.Public {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}

	now := time.Now()

	// Find current show (overlaps with now)
	var currentInstance models.ShowInstance
	a.db.Where("station_id = ? AND starts_at <= ? AND ends_at > ?", stationID, now, now).
		Where("status = ?", models.ShowInstanceScheduled).
		Preload("Show").
		Preload("Show.Host").
		Preload("Host").
		First(&currentInstance)

	// Find next show
	var nextInstance models.ShowInstance
	searchStart := now
	if currentInstance.ID != "" {
		searchStart = currentInstance.EndsAt
	}
	a.db.Where("station_id = ? AND starts_at >= ?", stationID, searchStart).
		Where("status = ?", models.ShowInstanceScheduled).
		Preload("Show").
		Preload("Show.Host").
		Preload("Host").
		Order("starts_at ASC").
		First(&nextInstance)

	result := map[string]any{
		"station": map[string]any{
			"id":   station.ID,
			"name": station.Name,
		},
		"timestamp": now.Format(time.RFC3339),
	}

	if currentInstance.ID != "" && currentInstance.Show != nil {
		current := instanceToPublic(&currentInstance)
		current.IsCurrent = true
		result["current"] = current
	}

	if nextInstance.ID != "" && nextInstance.Show != nil {
		next := instanceToPublic(&nextInstance)
		next.IsNext = true
		result["next"] = next
	}

	writeJSON(w, http.StatusOK, result)
}

// handlePublicScheduleICal returns the schedule as an iCal feed.
func (a *API) handlePublicScheduleICal(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	// Validate station
	var station models.Station
	if err := a.db.First(&station, "id = ?", stationID).Error; err != nil {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}

	if !station.Public {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}

	// Get shows for next 30 days
	now := time.Now()
	end := now.AddDate(0, 0, 30)

	var instances []models.ShowInstance
	a.db.Where("station_id = ? AND starts_at >= ? AND starts_at < ?", stationID, now, end).
		Where("status = ?", models.ShowInstanceScheduled).
		Preload("Show").
		Preload("Host").
		Order("starts_at ASC").
		Find(&instances)

	// Build iCal
	var ical strings.Builder
	ical.WriteString("BEGIN:VCALENDAR\r\n")
	ical.WriteString("VERSION:2.0\r\n")
	ical.WriteString("PRODID:-//Grimnir Radio//Schedule//EN\r\n")
	ical.WriteString(fmt.Sprintf("X-WR-CALNAME:%s Schedule\r\n", escapeICalText(station.Name)))
	ical.WriteString("CALSCALE:GREGORIAN\r\n")
	ical.WriteString("METHOD:PUBLISH\r\n")

	for _, inst := range instances {
		if inst.Show == nil {
			continue
		}

		ical.WriteString("BEGIN:VEVENT\r\n")
		ical.WriteString(fmt.Sprintf("UID:%s@grimnir\r\n", inst.ID))
		ical.WriteString(fmt.Sprintf("DTSTAMP:%s\r\n", formatICalTime(time.Now())))
		ical.WriteString(fmt.Sprintf("DTSTART:%s\r\n", formatICalTime(inst.StartsAt)))
		ical.WriteString(fmt.Sprintf("DTEND:%s\r\n", formatICalTime(inst.EndsAt)))
		ical.WriteString(fmt.Sprintf("SUMMARY:%s\r\n", escapeICalText(inst.Show.Name)))

		if inst.Show.Description != "" {
			ical.WriteString(fmt.Sprintf("DESCRIPTION:%s\r\n", escapeICalText(inst.Show.Description)))
		}

		hostName := ""
		if inst.Host != nil {
			hostName = inst.Host.Email
		} else if inst.Show.Host != nil {
			hostName = inst.Show.Host.Email
		}
		if hostName != "" {
			ical.WriteString(fmt.Sprintf("ORGANIZER:mailto:%s\r\n", hostName))
		}

		ical.WriteString("END:VEVENT\r\n")
	}

	ical.WriteString("END:VCALENDAR\r\n")

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-schedule.ics\"", slugify(station.Name)))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(ical.String()))
}

// handlePublicScheduleRSS returns the schedule as an RSS feed.
func (a *API) handlePublicScheduleRSS(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	// Validate station
	var station models.Station
	if err := a.db.First(&station, "id = ?", stationID).Error; err != nil {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}

	if !station.Public {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}

	// Get shows for next 7 days
	now := time.Now()
	end := now.AddDate(0, 0, 7)

	var instances []models.ShowInstance
	a.db.Where("station_id = ? AND starts_at >= ? AND starts_at < ?", stationID, now, end).
		Where("status = ?", models.ShowInstanceScheduled).
		Preload("Show").
		Preload("Host").
		Order("starts_at ASC").
		Find(&instances)

	// Build RSS
	type RSSItem struct {
		XMLName     xml.Name `xml:"item"`
		Title       string   `xml:"title"`
		Description string   `xml:"description"`
		PubDate     string   `xml:"pubDate"`
		GUID        string   `xml:"guid"`
	}

	type RSSChannel struct {
		XMLName     xml.Name  `xml:"channel"`
		Title       string    `xml:"title"`
		Link        string    `xml:"link"`
		Description string    `xml:"description"`
		LastBuild   string    `xml:"lastBuildDate"`
		Items       []RSSItem `xml:"item"`
	}

	type RSS struct {
		XMLName xml.Name   `xml:"rss"`
		Version string     `xml:"version,attr"`
		Channel RSSChannel `xml:"channel"`
	}

	items := make([]RSSItem, 0, len(instances))
	for _, inst := range instances {
		if inst.Show == nil {
			continue
		}

		hostName := ""
		if inst.Host != nil {
			hostName = inst.Host.Email
		} else if inst.Show.Host != nil {
			hostName = inst.Show.Host.Email
		}

		desc := inst.Show.Description
		if desc == "" {
			desc = fmt.Sprintf("%s - %s", inst.StartsAt.Format("Mon, Jan 2 3:04 PM"), inst.EndsAt.Format("3:04 PM"))
		}
		if hostName != "" {
			desc = fmt.Sprintf("Host: %s\n%s", hostName, desc)
		}

		items = append(items, RSSItem{
			Title:       fmt.Sprintf("%s - %s", inst.Show.Name, inst.StartsAt.Format("Mon, Jan 2 3:04 PM")),
			Description: desc,
			PubDate:     inst.StartsAt.Format(time.RFC1123Z),
			GUID:        inst.ID,
		})
	}

	rss := RSS{
		Version: "2.0",
		Channel: RSSChannel{
			Title:       fmt.Sprintf("%s Schedule", station.Name),
			Link:        fmt.Sprintf("/schedule?station=%s", station.ID),
			Description: fmt.Sprintf("Upcoming shows on %s", station.Name),
			LastBuild:   time.Now().Format(time.RFC1123Z),
			Items:       items,
		},
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(rss)
}

// instanceToPublic converts a ShowInstance to PublicShowInstance.
func instanceToPublic(inst *models.ShowInstance) PublicShowInstance {
	pi := PublicShowInstance{
		ID:       inst.ID,
		StartsAt: inst.StartsAt,
		EndsAt:   inst.EndsAt,
		Show: PublicShow{
			ID:          inst.Show.ID,
			Name:        inst.Show.Name,
			Description: inst.Show.Description,
			Color:       inst.Show.Color,
		},
	}

	if inst.Host != nil {
		pi.Show.HostName = inst.Host.Email
	} else if inst.Show != nil && inst.Show.Host != nil {
		pi.Show.HostName = inst.Show.Host.Email
	}

	if inst.Show != nil {
		pi.Show.ArtworkURL = publicArtworkURL(inst.Show.ArtworkPath)
	}

	return pi
}

// formatICalTime formats a time for iCal (YYYYMMDDTHHMMSSZ).
func formatICalTime(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

// escapeICalText escapes text for iCal format.
func escapeICalText(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

// slugify converts a string to a URL-safe slug.
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

func publicArtworkURL(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	// Allow absolute URLs and root-relative paths only.
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "/") {
		return path
	}
	return ""
}

// ShowTransitionWebhook represents a webhook payload for show transitions.
type ShowTransitionWebhook struct {
	Event     string              `json:"event"` // "show_start", "show_end"
	Timestamp time.Time           `json:"timestamp"`
	StationID string              `json:"station_id"`
	Show      *PublicShowInstance `json:"show,omitempty"`
	NextShow  *PublicShowInstance `json:"next_show,omitempty"`
}
