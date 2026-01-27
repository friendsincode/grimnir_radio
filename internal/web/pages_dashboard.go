/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// DashboardHome renders the main dashboard overview
func (h *Handler) DashboardHome(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	// If no station selected, redirect to selection
	if station == nil {
		var stations []models.Station
		h.db.Where("active = ?", true).Find(&stations)

		if len(stations) == 1 {
			// Auto-select single station
			h.SetStation(w, stations[0].ID)
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	// Gather dashboard data
	var data DashboardData

	// Upcoming schedule
	h.db.Where("station_id = ? AND starts_at >= ? AND starts_at <= ?",
		station.ID, time.Now(), time.Now().Add(6*time.Hour)).
		Order("starts_at ASC").
		Limit(10).
		Find(&data.UpcomingEntries)

	// Recent media uploads
	h.db.Where("station_id = ?", station.ID).
		Order("created_at DESC").
		Limit(5).
		Find(&data.RecentMedia)

	// Active live sessions
	h.db.Where("station_id = ? AND active = ?", station.ID, true).
		Find(&data.LiveSessions)

	// Media stats
	h.db.Model(&models.MediaItem{}).Where("station_id = ?", station.ID).Count(&data.MediaCount)

	// Playlist count
	h.db.Model(&models.Playlist{}).Where("station_id = ?", station.ID).Count(&data.PlaylistCount)

	// Smart block count
	h.db.Model(&models.SmartBlock{}).Where("station_id = ?", station.ID).Count(&data.SmartBlockCount)

	h.Render(w, r, "pages/dashboard/home", PageData{
		Title:    "Dashboard",
		User:     user,
		Station:  station,
		Stations: h.LoadStations(r),
		Data:     data,
	})
}

// DashboardData holds data for the dashboard overview
type DashboardData struct {
	UpcomingEntries []models.ScheduleEntry
	RecentMedia     []models.MediaItem
	LiveSessions    []models.LiveSession
	MediaCount      int64
	PlaylistCount   int64
	SmartBlockCount int64
	NowPlaying      *NowPlayingInfo
}

// NowPlayingInfo holds current playback info
type NowPlayingInfo struct {
	Title    string
	Artist   string
	Album    string
	Duration time.Duration
	Elapsed  time.Duration
	MountID  string
}

// StationSelect renders the station selection page
func (h *Handler) StationSelect(w http.ResponseWriter, r *http.Request) {
	// Use LoadStations which filters by user access
	stations := h.LoadStations(r)

	h.Render(w, r, "pages/dashboard/station-select", PageData{
		Title:    "Select Station",
		Stations: stations,
	})
}

// StationSelectSubmit handles station selection
func (h *Handler) StationSelectSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	stationID := r.FormValue("station_id")
	if stationID == "" {
		http.Error(w, "Station required", http.StatusBadRequest)
		return
	}

	// Verify station exists
	var station models.Station
	if err := h.db.First(&station, "id = ?", stationID).Error; err != nil {
		http.Error(w, "Station not found", http.StatusNotFound)
		return
	}

	// Verify user has access to this station
	user := h.GetUser(r)
	if user == nil {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	if !h.HasStationAccess(user, stationID) {
		http.Error(w, "You don't have access to this station", http.StatusForbidden)
		return
	}

	h.SetStation(w, stationID)

	// Handle HTMX
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// ProfilePage renders the user's profile page
func (h *Handler) ProfilePage(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	h.Render(w, r, "pages/dashboard/profile", PageData{
		Title:    "Profile",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"User": user,
		},
	})
}

// ProfileUpdate handles profile updates
func (h *Handler) ProfileUpdate(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderProfileError(w, r, "Invalid form data")
		return
	}

	// Update allowed fields
	email := r.FormValue("email")
	if email == "" {
		h.renderProfileError(w, r, "Email is required")
		return
	}

	// Check if email changed and is unique
	if email != user.Email {
		var existing models.User
		if err := h.db.Where("email = ? AND id != ?", email, user.ID).First(&existing).Error; err == nil {
			h.renderProfileError(w, r, "Email already in use")
			return
		}
	}

	user.Email = email

	if err := h.db.Save(user).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to update user")
		h.renderProfileError(w, r, "Failed to update profile")
		return
	}

	// Handle HTMX
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Trigger", "profileUpdated")
		w.Write([]byte(`<div class="alert alert-success">Profile updated successfully</div>`))
		return
	}

	http.Redirect(w, r, "/dashboard/profile", http.StatusSeeOther)
}

// ProfileUpdatePassword handles password changes
func (h *Handler) ProfileUpdatePassword(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderPasswordError(w, r, "Invalid form data")
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	// Verify current password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(currentPassword)); err != nil {
		h.renderPasswordError(w, r, "Current password is incorrect")
		return
	}

	// Validate new password
	if len(newPassword) < 8 {
		h.renderPasswordError(w, r, "New password must be at least 8 characters")
		return
	}

	if newPassword != confirmPassword {
		h.renderPasswordError(w, r, "New passwords do not match")
		return
	}

	// Hash and save new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to hash password")
		h.renderPasswordError(w, r, "Failed to update password")
		return
	}

	user.Password = string(hashedPassword)

	if err := h.db.Save(user).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to update user password")
		h.renderPasswordError(w, r, "Failed to update password")
		return
	}

	// Handle HTMX
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Trigger", "passwordUpdated")
		w.Write([]byte(`<div class="alert alert-success">Password updated successfully</div>`))
		return
	}

	http.Redirect(w, r, "/dashboard/profile", http.StatusSeeOther)
}

func (h *Handler) renderProfileError(w http.ResponseWriter, r *http.Request, message string) {
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<div class="alert alert-danger">` + message + `</div>`))
		return
	}

	user := h.GetUser(r)
	h.Render(w, r, "pages/dashboard/profile", PageData{
		Title:    "Profile",
		Flash:    &FlashMessage{Type: "error", Message: message},
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"User": user,
		},
	})
}

func (h *Handler) renderPasswordError(w http.ResponseWriter, r *http.Request, message string) {
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<div class="alert alert-danger">` + message + `</div>`))
		return
	}

	user := h.GetUser(r)
	h.Render(w, r, "pages/dashboard/profile", PageData{
		Title:    "Profile",
		Flash:    &FlashMessage{Type: "error", Message: message},
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"User":          user,
			"PasswordError": true,
		},
	})
}
