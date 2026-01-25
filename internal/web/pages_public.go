/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Landing renders the public landing page
func (h *Handler) Landing(w http.ResponseWriter, r *http.Request) {
	// Get the first active station and its mount for the player
	var station models.Station
	h.db.Where("active = ?", true).First(&station)

	var mount models.Mount
	if station.ID != "" {
		h.db.Where("station_id = ?", station.ID).First(&mount)
	}

	// Build stream URLs using the Go broadcast server
	streamURL := ""
	streamURLLQ := ""
	if station.ID != "" && mount.ID != "" {
		// Use the built-in broadcast server at /live/{mount}
		streamURL = "/live/" + mount.Name
		streamURLLQ = "/live/" + mount.Name + "-lq"
	}

	h.Render(w, r, "pages/public/landing", PageData{
		Title: "Welcome",
		Data: map[string]any{
			"StationID":   station.ID,
			"StationName": station.Name,
			"MountName":   mount.Name,
			"StreamURL":   streamURL,
			"StreamURLLQ": streamURLLQ,
		},
	})
}

// Listen renders the public listening page
func (h *Handler) Listen(w http.ResponseWriter, r *http.Request) {
	// Get active stations and their mounts
	var stations []models.Station
	h.db.Where("active = ?", true).Find(&stations)

	type mountWithURL struct {
		models.Mount
		URL   string
		LQURL string // Low-quality stream URL
	}

	type stationWithMounts struct {
		Station models.Station
		Mounts  []mountWithURL
	}

	var data []stationWithMounts
	for _, s := range stations {
		var mounts []models.Mount
		h.db.Where("station_id = ?", s.ID).Find(&mounts)

		var mountsWithURLs []mountWithURL
		for _, m := range mounts {
			// Use the built-in broadcast server at /live/{mount}
			streamURL := "/live/" + m.Name
			lqStreamURL := "/live/" + m.Name + "-lq"
			mountsWithURLs = append(mountsWithURLs, mountWithURL{
				Mount: m,
				URL:   streamURL,
				LQURL: lqStreamURL,
			})
		}

		data = append(data, stationWithMounts{Station: s, Mounts: mountsWithURLs})
	}

	h.Render(w, r, "pages/public/listen", PageData{
		Title: "Listen Live",
		Data:  data,
	})
}

// Archive renders the public archive browser
func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	// Pagination
	page := 1
	perPage := 24

	var media []models.MediaItem
	var total int64

	query := h.db.Model(&models.MediaItem{}).Where("1=1") // Publicly accessible media

	// Search filter
	if q := r.URL.Query().Get("q"); q != "" {
		query = query.Where("title ILIKE ? OR artist ILIKE ? OR album ILIKE ?",
			"%"+q+"%", "%"+q+"%", "%"+q+"%")
	}

	query.Count(&total)
	query.Order("created_at DESC").
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&media)

	h.Render(w, r, "pages/public/archive", PageData{
		Title: "Archive",
		Data: map[string]any{
			"Media":   media,
			"Total":   total,
			"Page":    page,
			"PerPage": perPage,
			"Query":   r.URL.Query().Get("q"),
		},
	})
}

// ArchiveDetail renders a single media item page
func (h *Handler) ArchiveDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var media models.MediaItem
	if err := h.db.First(&media, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	h.Render(w, r, "pages/public/archive-detail", PageData{
		Title: media.Title,
		Data:  media,
	})
}

// ArchiveStream serves the audio file for a public archive item
func (h *Handler) ArchiveStream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var media models.MediaItem
	if err := h.db.Select("id", "path", "title", "artist").First(&media, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if media.Path == "" {
		http.Error(w, "No media file available", http.StatusNotFound)
		return
	}

	fullPath := h.mediaRoot + "/" + media.Path

	// Set content type based on file extension
	ext := media.Path[len(media.Path)-3:]
	contentTypes := map[string]string{
		"mp3": "audio/mpeg",
		"lac": "audio/flac",
		"wav": "audio/wav",
		"ogg": "audio/ogg",
		"m4a": "audio/mp4",
	}
	if ct, ok := contentTypes[ext]; ok {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Accept-Ranges", "bytes")

	http.ServeFile(w, r, fullPath)
}

// ArchiveArtwork serves the album art for a public archive item
func (h *Handler) ArchiveArtwork(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var media models.MediaItem
	if err := h.db.Select("id", "artwork", "artwork_mime").First(&media, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if len(media.Artwork) == 0 {
		http.Error(w, "No artwork available", http.StatusNotFound)
		return
	}

	contentType := media.ArtworkMime
	if contentType == "" {
		contentType = "image/jpeg"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(media.Artwork)
}

// PublicSchedule renders the public schedule view
func (h *Handler) PublicSchedule(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")

	var stations []models.Station
	h.db.Where("active = ?", true).Find(&stations)

	var entries []models.ScheduleEntry
	query := h.db.Where("starts_at >= ? AND starts_at <= ?",
		time.Now(), time.Now().Add(48*time.Hour))

	if stationID != "" {
		query = query.Where("station_id = ?", stationID)
	}

	query.Order("starts_at ASC").Limit(100).Find(&entries)

	h.Render(w, r, "pages/public/schedule", PageData{
		Title: "Schedule",
		Data: map[string]any{
			"Stations":  stations,
			"Entries":   entries,
			"StationID": stationID,
		},
	})
}

// StationInfo renders a station info page
func (h *Handler) StationInfo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var station models.Station
	if err := h.db.First(&station, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	var mounts []models.Mount
	h.db.Where("station_id = ?", station.ID).Find(&mounts)

	h.Render(w, r, "pages/public/station", PageData{
		Title: station.Name,
		Data: map[string]any{
			"Station": station,
			"Mounts":  mounts,
		},
	})
}

// LoginPage renders the login form
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	// Redirect if already logged in
	if h.GetUser(r) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	h.Render(w, r, "pages/public/login", PageData{
		Title: "Login",
		Data: map[string]any{
			"Redirect": r.URL.Query().Get("redirect"),
		},
	})
}

// LoginSubmit handles the login form submission
func (h *Handler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderLoginError(w, r, "Invalid form data")
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")
	redirect := r.FormValue("redirect")

	if email == "" || password == "" {
		h.renderLoginError(w, r, "Email and password are required")
		return
	}

	// Find user
	var user models.User
	if err := h.db.First(&user, "email = ?", email).Error; err != nil {
		h.renderLoginError(w, r, "Invalid email or password")
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		h.renderLoginError(w, r, "Invalid email or password")
		return
	}

	// Generate JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"roles":   []string{string(user.Role)},
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
		"iat":     time.Now().Unix(),
		"sub":     user.ID,
	})

	tokenStr, err := token.SignedString(h.jwtSecret)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to sign JWT")
		h.renderLoginError(w, r, "Authentication failed")
		return
	}

	// Set cookie (24 hours)
	h.SetAuthToken(w, tokenStr, 86400)

	// Handle HTMX request
	if r.Header.Get("HX-Request") == "true" {
		if redirect != "" && redirect != "/login" {
			w.Header().Set("HX-Redirect", redirect)
		} else {
			w.Header().Set("HX-Redirect", "/dashboard")
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	// Standard redirect
	if redirect != "" && redirect != "/login" {
		http.Redirect(w, r, redirect, http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	}
}

func (h *Handler) renderLoginError(w http.ResponseWriter, r *http.Request, message string) {
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`<div class="alert alert-danger" role="alert">` + message + `</div>`))
		return
	}

	h.Render(w, r, "pages/public/login", PageData{
		Title: "Login",
		Flash: &FlashMessage{Type: "error", Message: message},
		Data: map[string]any{
			"Email":    r.FormValue("email"),
			"Redirect": r.FormValue("redirect"),
		},
	})
}

// Logout clears the auth cookie and redirects to login
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	h.ClearAuthToken(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
