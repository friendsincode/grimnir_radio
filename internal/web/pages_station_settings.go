/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

const maxStationDescriptionChars = 5000

// StationSettings renders the station settings page
func (h *Handler) StationSettings(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	// Check if user can manage station settings
	if !h.canManageStationSettings(user, station) {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	// Get station mounts
	var mounts []models.Mount
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&mounts)

	h.Render(w, r, "pages/dashboard/station/settings", PageData{
		Title:    "Station Settings - " + station.Name,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Station": station,
			"Mounts":  mounts,
		},
	})
}

// StationSettingsUpdate handles station settings updates
func (h *Handler) StationSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	if !h.canManageStationSettings(user, station) {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	if err := parseURLFormSemicolonTolerant(r); err != nil {
		if r.Header.Get("HX-Request") == "true" {
			w.Write([]byte(`<div class="alert alert-danger">Invalid form data. If your text includes special characters, try again after saving.</div>`))
			return
		}
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	station.Name = r.FormValue("name")
	station.Description = r.FormValue("description")
	station.Timezone = r.FormValue("timezone")
	station.DefaultShowInArchive = r.FormValue("default_show_in_archive") == "on"
	station.DefaultAllowDownload = r.FormValue("default_allow_download") == "on"

	// Schedule boundary policy
	switch r.FormValue("schedule_boundary_mode") {
	case "soft":
		station.ScheduleBoundaryMode = "soft"
	default:
		station.ScheduleBoundaryMode = "hard"
	}
	if raw := r.FormValue("schedule_soft_overrun_minutes"); raw != "" {
		mins, err := strconv.Atoi(raw)
		if err == nil && mins >= 0 {
			// Keep it bounded to avoid accidental huge values. A week is plenty.
			if mins > 7*24*60 {
				mins = 7 * 24 * 60
			}
			station.ScheduleSoftOverrunSeconds = mins * 60
		}
	}

	// Crossfade defaults
	station.CrossfadeEnabled = r.FormValue("crossfade_enabled") == "on"
	if raw := r.FormValue("crossfade_duration_ms"); raw != "" {
		ms, err := strconv.Atoi(raw)
		if err == nil && ms >= 0 {
			// Keep it bounded to avoid silly values.
			if ms > 30000 {
				ms = 30000
			}
			station.CrossfadeDurationMs = ms
		}
	}

	if station.Name == "" {
		if r.Header.Get("HX-Request") == "true" {
			w.Write([]byte(`<div class="alert alert-danger">Name is required</div>`))
			return
		}
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	if err := validateStationDescription(station.Description); err != nil {
		if r.Header.Get("HX-Request") == "true" {
			w.Write([]byte(fmt.Sprintf(`<div class="alert alert-danger">%s</div>`, err.Error())))
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.db.Save(station).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to update station settings")
		if r.Header.Get("HX-Request") == "true" {
			w.Write([]byte(`<div class="alert alert-danger">Failed to save station settings. Check description length/content and try again.</div>`))
			return
		}
		http.Error(w, "Failed to update settings", http.StatusInternalServerError)
		return
	}

	// Ensure public/listen pages are refreshed with updated station metadata.
	h.publishCacheEvent(events.EventStationUpdated, station.ID)

	h.logger.Info().
		Str("station_id", station.ID).
		Str("updated_by", user.ID).
		Msg("station settings updated")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Trigger", "settingsUpdated")
		w.Write([]byte(`<div class="alert alert-success">Settings saved successfully</div>`))
		return
	}
	http.Redirect(w, r, "/dashboard/station/settings", http.StatusSeeOther)
}

func validateStationDescription(desc string) error {
	if !utf8.ValidString(desc) {
		return fmt.Errorf("description contains invalid text encoding")
	}
	if strings.ContainsRune(desc, '\x00') {
		return fmt.Errorf("description contains unsupported control characters")
	}
	if utf8.RuneCountInString(desc) > maxStationDescriptionChars {
		return fmt.Errorf("description is too long (max %d characters)", maxStationDescriptionChars)
	}
	return nil
}

// parseURLFormSemicolonTolerant parses URL-encoded form bodies while tolerating raw semicolons
// in values (e.g. station descriptions). Go's standard parser treats raw ';' as invalid.
func parseURLFormSemicolonTolerant(r *http.Request) error {
	if r == nil {
		return fmt.Errorf("nil request")
	}

	ctype := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(ctype, "application/x-www-form-urlencoded") &&
		(r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			return err
		}
		_ = r.Body.Close()

		// Escape raw semicolons before ParseQuery so semicolons in values are preserved.
		fixedBody := strings.ReplaceAll(string(bodyBytes), ";", "%3B")
		postVals, err := url.ParseQuery(fixedBody)
		if err != nil {
			// Restore body for any downstream consumer before returning.
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			return err
		}

		queryVals, err := url.ParseQuery(strings.ReplaceAll(r.URL.RawQuery, ";", "%3B"))
		if err != nil {
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			return err
		}

		r.PostForm = postVals
		r.Form = make(url.Values, len(queryVals)+len(postVals))
		for k, v := range queryVals {
			r.Form[k] = append([]string(nil), v...)
		}
		for k, v := range postVals {
			r.Form[k] = append([]string(nil), v...)
		}

		// Keep request body readable after parsing.
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		return nil
	}

	return r.ParseForm()
}

// canManageStationSettings checks if user can manage station settings
func (h *Handler) canManageStationSettings(user *models.User, station *models.Station) bool {
	if user == nil || station == nil {
		return false
	}

	// Platform admins can manage all stations
	if user.IsPlatformAdmin() {
		return true
	}

	// Check station role
	stationUser := h.GetStationRole(user, station.ID)
	if stationUser == nil {
		return false
	}

	// Owner and admin can manage settings
	return stationUser.Role == models.StationRoleOwner || stationUser.Role == models.StationRoleAdmin
}

// StationStopPlayout handles emergency stop of all playout for the station
func (h *Handler) StationStopPlayout(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	if !h.canManageStationSettings(user, station) {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	if h.director == nil {
		h.logger.Error().Msg("playout director not available")
		if r.Header.Get("HX-Request") == "true" {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`<div class="alert alert-danger">Playout system not available</div>`))
			return
		}
		http.Error(w, "Playout system not available", http.StatusInternalServerError)
		return
	}

	stopped, err := h.director.StopStation(r.Context(), station.ID)
	if err != nil {
		h.logger.Error().Err(err).Str("station_id", station.ID).Msg("failed to stop station playout")
		if r.Header.Get("HX-Request") == "true" {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(fmt.Sprintf(`<div class="alert alert-danger">Failed to stop playout: %v</div>`, err)))
			return
		}
		http.Error(w, fmt.Sprintf("Failed to stop playout: %v", err), http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("station_id", station.ID).
		Str("user_id", user.ID).
		Int("mounts_stopped", stopped).
		Msg("station playout stopped by user")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Trigger", "playoutStopped")
		w.Write([]byte(fmt.Sprintf(`<div class="alert alert-success"><i class="bi bi-check-circle me-2"></i>Playout stopped on %d mount(s)</div>`, stopped)))
		return
	}

	http.Redirect(w, r, "/dashboard/station/settings", http.StatusSeeOther)
}
