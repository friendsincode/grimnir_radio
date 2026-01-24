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

// UserList renders the users management page
func (h *Handler) UserList(w http.ResponseWriter, r *http.Request) {
	currentUser := h.GetUser(r)

	var users []models.User
	query := h.db.Order("email ASC")

	// Managers can only see DJs, admins can see everyone
	if currentUser.Role == models.RoleManager {
		query = query.Where("role = ?", models.RoleDJ)
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

	// Determine available roles based on current user
	var availableRoles []models.RoleName
	if currentUser.Role == models.RoleAdmin {
		availableRoles = []models.RoleName{models.RoleAdmin, models.RoleManager, models.RoleDJ}
	} else {
		availableRoles = []models.RoleName{models.RoleDJ}
	}

	h.Render(w, r, "pages/dashboard/users/form", PageData{
		Title:    "New User",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"User":           models.User{},
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
	role := models.RoleName(r.FormValue("role"))

	if email == "" || password == "" {
		http.Error(w, "Email and password required", http.StatusBadRequest)
		return
	}

	// Validate role permissions
	if currentUser.Role == models.RoleManager && role != models.RoleDJ {
		http.Error(w, "Managers can only create DJ accounts", http.StatusForbidden)
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
		ID:       uuid.New().String(),
		Email:    email,
		Password: string(hashedPassword),
		Role:     role,
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

	h.Render(w, r, "pages/dashboard/users/detail", PageData{
		Title:    user.Email,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"User":     user,
			"Sessions": sessions,
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

	// Check permissions
	if currentUser.Role == models.RoleManager && user.Role != models.RoleDJ {
		http.Error(w, "Not authorized to edit this user", http.StatusForbidden)
		return
	}

	var availableRoles []models.RoleName
	if currentUser.Role == models.RoleAdmin {
		availableRoles = []models.RoleName{models.RoleAdmin, models.RoleManager, models.RoleDJ}
	} else {
		availableRoles = []models.RoleName{models.RoleDJ}
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
	if currentUser.Role == models.RoleManager && user.Role != models.RoleDJ {
		http.Error(w, "Not authorized", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	user.Email = r.FormValue("email")

	// Only admins can change roles (except for DJs)
	if newRole := models.RoleName(r.FormValue("role")); newRole != "" {
		if currentUser.Role == models.RoleAdmin {
			user.Role = newRole
		} else if newRole == models.RoleDJ {
			user.Role = newRole
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

	// Check permissions
	if currentUser.Role == models.RoleManager && user.Role != models.RoleDJ {
		http.Error(w, "Not authorized", http.StatusForbidden)
		return
	}

	// Only admins can delete other admins
	if user.Role == models.RoleAdmin && currentUser.Role != models.RoleAdmin {
		http.Error(w, "Not authorized", http.StatusForbidden)
		return
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
