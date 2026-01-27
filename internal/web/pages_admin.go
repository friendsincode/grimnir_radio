/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// AdminStationsList renders the platform admin stations management page
func (h *Handler) AdminStationsList(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	var stations []models.Station
	h.db.Order("name ASC").Find(&stations)

	// Load owner info for each station
	type StationWithOwner struct {
		models.Station
		Owner *models.User
	}
	var stationsWithOwners []StationWithOwner
	for _, s := range stations {
		swo := StationWithOwner{Station: s}
		if s.OwnerID != "" {
			var owner models.User
			if err := h.db.First(&owner, "id = ?", s.OwnerID).Error; err == nil {
				swo.Owner = &owner
			}
		}
		stationsWithOwners = append(stationsWithOwners, swo)
	}

	h.Render(w, r, "pages/dashboard/admin/stations", PageData{
		Title:    "All Stations - Admin",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"AllStations": stationsWithOwners,
		},
	})
}

// AdminStationToggleActive toggles a station's active status
func (h *Handler) AdminStationToggleActive(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	id := chi.URLParam(r, "id")
	var station models.Station
	if err := h.db.First(&station, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	station.Active = !station.Active
	if err := h.db.Save(&station).Error; err != nil {
		http.Error(w, "Failed to update station", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("station_id", station.ID).
		Bool("active", station.Active).
		Str("admin_id", user.ID).
		Msg("station active status changed")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		return
	}
	http.Redirect(w, r, "/dashboard/admin/stations", http.StatusSeeOther)
}

// AdminStationTogglePublic toggles a station's public visibility
func (h *Handler) AdminStationTogglePublic(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	id := chi.URLParam(r, "id")
	var station models.Station
	if err := h.db.First(&station, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	station.Public = !station.Public
	if err := h.db.Save(&station).Error; err != nil {
		http.Error(w, "Failed to update station", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("station_id", station.ID).
		Bool("public", station.Public).
		Str("admin_id", user.ID).
		Msg("station public status changed")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		return
	}
	http.Redirect(w, r, "/dashboard/admin/stations", http.StatusSeeOther)
}

// AdminStationToggleApproved toggles a station's approval status
func (h *Handler) AdminStationToggleApproved(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	id := chi.URLParam(r, "id")
	var station models.Station
	if err := h.db.First(&station, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	station.Approved = !station.Approved
	if err := h.db.Save(&station).Error; err != nil {
		http.Error(w, "Failed to update station", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("station_id", station.ID).
		Bool("approved", station.Approved).
		Str("admin_id", user.ID).
		Msg("station approval status changed")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		return
	}
	http.Redirect(w, r, "/dashboard/admin/stations", http.StatusSeeOther)
}

// AdminUsersList renders the platform admin users management page
func (h *Handler) AdminUsersList(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	var users []models.User
	h.db.Order("email ASC").Find(&users)

	h.Render(w, r, "pages/dashboard/admin/users", PageData{
		Title:    "All Users - Admin",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"AllUsers": users,
		},
	})
}

// AdminUserEdit renders the user edit form for platform admins
func (h *Handler) AdminUserEdit(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	id := chi.URLParam(r, "id")
	var targetUser models.User
	if err := h.db.First(&targetUser, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	h.Render(w, r, "pages/dashboard/admin/user-edit", PageData{
		Title:    "Edit User - Admin",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"TargetUser":    targetUser,
			"PlatformRoles": []models.PlatformRole{models.PlatformRoleUser, models.PlatformRoleMod, models.PlatformRoleAdmin},
		},
	})
}

// AdminUserUpdate handles user updates from platform admin
func (h *Handler) AdminUserUpdate(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	id := chi.URLParam(r, "id")
	var targetUser models.User
	if err := h.db.First(&targetUser, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	// Update platform role
	newRole := models.PlatformRole(r.FormValue("platform_role"))
	if newRole != "" {
		// Validate role
		validRoles := map[models.PlatformRole]bool{
			models.PlatformRoleUser:  true,
			models.PlatformRoleMod:   true,
			models.PlatformRoleAdmin: true,
		}
		if !validRoles[newRole] {
			http.Error(w, "Invalid platform role", http.StatusBadRequest)
			return
		}
		targetUser.PlatformRole = newRole
	}

	// Update email if provided
	email := r.FormValue("email")
	if email != "" && email != targetUser.Email {
		// Check if email is already in use
		var existing models.User
		if err := h.db.Where("email = ? AND id != ?", email, id).First(&existing).Error; err == nil {
			http.Error(w, "Email already in use", http.StatusBadRequest)
			return
		}
		targetUser.Email = email
	}

	if err := h.db.Save(&targetUser).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to update user")
		http.Error(w, "Failed to update user", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("target_user_id", targetUser.ID).
		Str("platform_role", string(targetUser.PlatformRole)).
		Str("admin_id", user.ID).
		Msg("user updated by admin")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/admin/users")
		return
	}
	http.Redirect(w, r, "/dashboard/admin/users", http.StatusSeeOther)
}

// AdminUserDelete handles user deletion from platform admin
func (h *Handler) AdminUserDelete(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	id := chi.URLParam(r, "id")

	// Don't allow deleting yourself
	if id == user.ID {
		http.Error(w, "Cannot delete yourself", http.StatusBadRequest)
		return
	}

	if err := h.db.Delete(&models.User{}, "id = ?", id).Error; err != nil {
		http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("deleted_user_id", id).
		Str("admin_id", user.ID).
		Msg("user deleted by admin")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		return
	}
	http.Redirect(w, r, "/dashboard/admin/users", http.StatusSeeOther)
}
