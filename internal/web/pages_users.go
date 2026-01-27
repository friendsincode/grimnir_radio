/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// UserList renders the platform users management page (platform admins only)
func (h *Handler) UserList(w http.ResponseWriter, r *http.Request) {
	currentUser := h.GetUser(r)

	var users []models.User
	query := h.db.Order("email ASC")

	// Platform mods can only see regular users, platform admins can see everyone
	if currentUser.PlatformRole == models.PlatformRoleMod {
		query = query.Where("platform_role = ?", models.PlatformRoleUser)
	}

	query.Find(&users)

	h.Render(w, r, "pages/dashboard/users/list", PageData{
		Title:    "Users",
		Stations: h.LoadStations(r),
		Data:     users,
	})
}

// UserNew renders the new user form
func (h *Handler) UserNew(w http.ResponseWriter, r *http.Request) {
	currentUser := h.GetUser(r)

	// Determine available platform roles based on current user
	var availableRoles []string
	if currentUser.IsPlatformAdmin() {
		availableRoles = []string{"platform_admin", "platform_mod", "user"}
	} else {
		availableRoles = []string{"user"}
	}

	h.Render(w, r, "pages/dashboard/users/form", PageData{
		Title:    "New User",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"User":           models.User{PlatformRole: models.PlatformRoleUser},
			"IsNew":          true,
			"AvailableRoles": availableRoles,
		},
	})
}

// UserCreate handles new user creation
func (h *Handler) UserCreate(w http.ResponseWriter, r *http.Request) {
	currentUser := h.GetUser(r)

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")
	role := models.PlatformRole(r.FormValue("platform_role"))

	if email == "" || password == "" {
		http.Error(w, "Email and password required", http.StatusBadRequest)
		return
	}

	// Validate role permissions - only platform admins can create elevated roles
	if !currentUser.IsPlatformAdmin() && role != models.PlatformRoleUser {
		http.Error(w, "Only platform admins can create elevated accounts", http.StatusForbidden)
		return
	}

	// Check if email already exists
	var existing models.User
	if err := h.db.First(&existing, "email = ?", email).Error; err == nil {
		http.Error(w, "Email already in use", http.StatusBadRequest)
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	user := models.User{
		ID:           uuid.New().String(),
		Email:        email,
		Password:     string(hashedPassword),
		PlatformRole: role,
	}

	if err := h.db.Create(&user).Error; err != nil {
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/users")
		return
	}

	http.Redirect(w, r, "/dashboard/users", http.StatusSeeOther)
}

// UserDetail renders the user detail page
func (h *Handler) UserDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var user models.User
	if err := h.db.First(&user, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Get user's live session history
	var sessions []models.LiveSession
	h.db.Where("user_id = ?", id).Order("created_at DESC").Limit(10).Find(&sessions)

	// Get user's station associations
	var stationUsers []models.StationUser
	h.db.Where("user_id = ?", id).Preload("Station").Find(&stationUsers)

	h.Render(w, r, "pages/dashboard/users/detail", PageData{
		Title:    user.Email,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"User":         user,
			"Sessions":     sessions,
			"StationUsers": stationUsers,
		},
	})
}

// UserEdit renders the user edit form
func (h *Handler) UserEdit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	currentUser := h.GetUser(r)

	var user models.User
	if err := h.db.First(&user, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Check permissions - platform mods can only edit regular users
	if currentUser.PlatformRole == models.PlatformRoleMod && user.PlatformRole != models.PlatformRoleUser {
		http.Error(w, "Not authorized to edit this user", http.StatusForbidden)
		return
	}

	// Available roles based on current user's permissions
	var availableRoles []string
	if currentUser.IsPlatformAdmin() {
		availableRoles = []string{"platform_admin", "platform_mod", "user"}
	} else {
		availableRoles = []string{"user"}
	}

	h.Render(w, r, "pages/dashboard/users/form", PageData{
		Title:    "Edit: " + user.Email,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"User":           user,
			"IsNew":          false,
			"AvailableRoles": availableRoles,
		},
	})
}

// UserUpdate handles user updates
func (h *Handler) UserUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	currentUser := h.GetUser(r)

	var user models.User
	if err := h.db.First(&user, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Check permissions
	if currentUser.PlatformRole == models.PlatformRoleMod && user.PlatformRole != models.PlatformRoleUser {
		http.Error(w, "Not authorized", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	user.Email = r.FormValue("email")

	// Only platform admins can change roles
	if newRole := models.PlatformRole(r.FormValue("platform_role")); newRole != "" {
		if currentUser.IsPlatformAdmin() {
			user.PlatformRole = newRole
		}
	}

	// Update password if provided
	if newPassword := r.FormValue("password"); newPassword != "" {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "Failed to hash password", http.StatusInternalServerError)
			return
		}
		user.Password = string(hashedPassword)
	}

	if err := h.db.Save(&user).Error; err != nil {
		http.Error(w, "Failed to update user", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/users")
		return
	}

	http.Redirect(w, r, "/dashboard/users", http.StatusSeeOther)
}

// UserDelete handles user deletion
func (h *Handler) UserDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	currentUser := h.GetUser(r)

	var user models.User
	if err := h.db.First(&user, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Can't delete yourself
	if user.ID == currentUser.ID {
		http.Error(w, "Cannot delete your own account", http.StatusBadRequest)
		return
	}

	// Check permissions - mods can only delete regular users
	if currentUser.PlatformRole == models.PlatformRoleMod && user.PlatformRole != models.PlatformRoleUser {
		http.Error(w, "Not authorized", http.StatusForbidden)
		return
	}

	// Only platform admins can delete other platform admins
	if user.PlatformRole == models.PlatformRoleAdmin && !currentUser.IsPlatformAdmin() {
		http.Error(w, "Not authorized", http.StatusForbidden)
		return
	}

	// Delete user's station associations first
	if err := h.db.Where("user_id = ?", user.ID).Delete(&models.StationUser{}).Error; err != nil {
		h.logger.Error().Err(err).Str("user_id", user.ID).Msg("failed to delete station associations")
	}

	if err := h.db.Delete(&user).Error; err != nil {
		http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/users")
		return
	}

	http.Redirect(w, r, "/dashboard/users", http.StatusSeeOther)
}
