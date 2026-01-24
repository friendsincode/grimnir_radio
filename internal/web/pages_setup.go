/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// NeedsSetup returns true if no users exist in the database.
func (h *Handler) NeedsSetup() bool {
	var count int64
	h.db.Model(&models.User{}).Count(&count)
	return count == 0
}

// RequireSetup middleware redirects to setup if no users exist.
func (h *Handler) RequireSetup(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip if already on setup page
		if r.URL.Path == "/setup" || r.URL.Path == "/static/" ||
		   len(r.URL.Path) > 8 && r.URL.Path[:8] == "/static/" {
			next.ServeHTTP(w, r)
			return
		}

		if h.NeedsSetup() {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// SetupPage renders the initial setup wizard
func (h *Handler) SetupPage(w http.ResponseWriter, r *http.Request) {
	// If setup is complete, redirect to login
	if !h.NeedsSetup() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	h.Render(w, r, "pages/setup", PageData{
		Title: "Setup",
		Data:  map[string]any{},
	})
}

// SetupSubmit handles the setup form submission
func (h *Handler) SetupSubmit(w http.ResponseWriter, r *http.Request) {
	// If setup is complete, redirect to login
	if !h.NeedsSetup() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderSetupError(w, r, "Invalid form data")
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")
	stationName := r.FormValue("station_name")
	timezone := r.FormValue("timezone")

	// Validation
	if email == "" {
		h.renderSetupError(w, r, "Email is required")
		return
	}
	if password == "" {
		h.renderSetupError(w, r, "Password is required")
		return
	}
	if len(password) < 8 {
		h.renderSetupError(w, r, "Password must be at least 8 characters")
		return
	}
	if password != confirmPassword {
		h.renderSetupError(w, r, "Passwords do not match")
		return
	}
	if stationName == "" {
		h.renderSetupError(w, r, "Station name is required")
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to hash password")
		h.renderSetupError(w, r, "Failed to create account")
		return
	}

	// Create admin user
	admin := models.User{
		ID:       uuid.New().String(),
		Email:    email,
		Password: string(hashedPassword),
		Role:     models.RoleAdmin,
	}

	if err := h.db.Create(&admin).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to create admin user")
		h.renderSetupError(w, r, "Failed to create account")
		return
	}

	// Create station
	if timezone == "" {
		timezone = "UTC"
	}

	station := models.Station{
		ID:          uuid.New().String(),
		Name:        stationName,
		Description: "Created during initial setup",
		Timezone:    timezone,
		Active:      true,
	}

	if err := h.db.Create(&station).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to create station")
		// User was created, so we can continue
	}

	h.logger.Info().
		Str("email", email).
		Str("station", stationName).
		Msg("initial setup completed")

	// Redirect to login
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) renderSetupError(w http.ResponseWriter, r *http.Request, message string) {
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<div class="alert alert-danger" role="alert">` + message + `</div>`))
		return
	}

	h.Render(w, r, "pages/setup", PageData{
		Title: "Setup",
		Flash: &FlashMessage{Type: "error", Message: message},
		Data: map[string]any{
			"Email":       r.FormValue("email"),
			"StationName": r.FormValue("station_name"),
			"Timezone":    r.FormValue("timezone"),
		},
	})
}
