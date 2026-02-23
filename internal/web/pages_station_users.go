/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// StationUserList renders the station users management page
func (h *Handler) StationUserList(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	// Check if user can manage station users
	if !h.canManageStationUsers(user, station) {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	// Get all station users with their user info
	type StationUserWithUser struct {
		models.StationUser
		User *models.User
	}

	var stationUsers []models.StationUser
	h.db.Where("station_id = ?", station.ID).Find(&stationUsers)

	var usersWithInfo []StationUserWithUser
	for _, su := range stationUsers {
		var u models.User
		h.db.First(&u, "id = ?", su.UserID)
		usersWithInfo = append(usersWithInfo, StationUserWithUser{
			StationUser: su,
			User:        &u,
		})
	}

	h.Render(w, r, "pages/dashboard/station/users", PageData{
		Title:    "Station Users - " + station.Name,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Station":      station,
			"StationUsers": usersWithInfo,
			"Roles":        []models.StationRole{models.StationRoleOwner, models.StationRoleAdmin, models.StationRoleManager, models.StationRoleDJ, models.StationRoleViewer},
		},
	})
}

// StationUserInvite renders the invite user form
func (h *Handler) StationUserInvite(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	if !h.canManageStationUsers(user, station) {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	// Get all users not currently in this station
	var existingUserIDs []string
	h.db.Model(&models.StationUser{}).Where("station_id = ?", station.ID).Pluck("user_id", &existingUserIDs)

	var availableUsers []models.User
	if len(existingUserIDs) > 0 {
		h.db.Where("id NOT IN ?", existingUserIDs).Order("email ASC").Find(&availableUsers)
	} else {
		h.db.Order("email ASC").Find(&availableUsers)
	}

	h.Render(w, r, "pages/dashboard/station/user-invite", PageData{
		Title:    "Invite User - " + station.Name,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Station":        station,
			"AvailableUsers": availableUsers,
			"Roles":          []models.StationRole{models.StationRoleAdmin, models.StationRoleManager, models.StationRoleDJ, models.StationRoleViewer},
		},
	})
}

// StationUserAdd handles adding a user to the station
func (h *Handler) StationUserAdd(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	if !h.canManageStationUsers(user, station) {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := r.FormValue("user_id")
	role := models.StationRole(r.FormValue("role"))

	// Validate user exists
	var targetUser models.User
	if err := h.db.First(&targetUser, "id = ?", userID).Error; err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Check if already a member
	var existing models.StationUser
	if err := h.db.Where("station_id = ? AND user_id = ?", station.ID, userID).First(&existing).Error; err == nil {
		http.Error(w, "User is already a member of this station", http.StatusBadRequest)
		return
	}

	// Validate role
	validRoles := map[models.StationRole]bool{
		models.StationRoleAdmin:   true,
		models.StationRoleManager: true,
		models.StationRoleDJ:      true,
		models.StationRoleViewer:  true,
	}
	if !validRoles[role] {
		http.Error(w, "Invalid role", http.StatusBadRequest)
		return
	}

	// Create station user association
	stationUser := models.StationUser{
		ID:        uuid.New().String(),
		UserID:    userID,
		StationID: station.ID,
		Role:      role,
	}

	if err := h.db.Create(&stationUser).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to add user to station")
		http.Error(w, "Failed to add user", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("station_id", station.ID).
		Str("user_id", userID).
		Str("role", string(role)).
		Str("added_by", user.ID).
		Msg("user added to station")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/station/users")
		return
	}
	http.Redirect(w, r, "/dashboard/station/users", http.StatusSeeOther)
}

// StationUserEdit renders the edit user role form
func (h *Handler) StationUserEdit(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)
	suID := chi.URLParam(r, "id")

	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	if !h.canManageStationUsers(user, station) {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	var stationUser models.StationUser
	if err := h.db.First(&stationUser, "id = ?", suID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Verify this belongs to the current station
	if stationUser.StationID != station.ID {
		http.Error(w, "User does not belong to this station", http.StatusForbidden)
		return
	}

	// Get the user details
	var targetUser models.User
	h.db.First(&targetUser, "id = ?", stationUser.UserID)

	h.Render(w, r, "pages/dashboard/station/user-edit", PageData{
		Title:    "Edit User Role - " + station.Name,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Station":     station,
			"StationUser": stationUser,
			"TargetUser":  targetUser,
			"Roles":       []models.StationRole{models.StationRoleOwner, models.StationRoleAdmin, models.StationRoleManager, models.StationRoleDJ, models.StationRoleViewer},
		},
	})
}

// StationUserUpdate handles updating a user's station role
func (h *Handler) StationUserUpdate(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)
	suID := chi.URLParam(r, "id")

	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	if !h.canManageStationUsers(user, station) {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	var stationUser models.StationUser
	if err := h.db.First(&stationUser, "id = ?", suID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if stationUser.StationID != station.ID {
		http.Error(w, "User does not belong to this station", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	newRole := models.StationRole(r.FormValue("role"))

	// Validate role
	validRoles := map[models.StationRole]bool{
		models.StationRoleOwner:   true,
		models.StationRoleAdmin:   true,
		models.StationRoleManager: true,
		models.StationRoleDJ:      true,
		models.StationRoleViewer:  true,
	}
	if !validRoles[newRole] {
		http.Error(w, "Invalid role", http.StatusBadRequest)
		return
	}

	// Only owner or platform admin can assign owner role
	if newRole == models.StationRoleOwner {
		currentRole := h.GetStationRole(user, station.ID)
		if !user.IsPlatformAdmin() && (currentRole == nil || currentRole.Role != models.StationRoleOwner) {
			http.Error(w, "Only station owner or platform admin can transfer ownership", http.StatusForbidden)
			return
		}
	}

	// Do not allow leaving a station without an owner.
	if stationUser.Role == models.StationRoleOwner && newRole != models.StationRoleOwner {
		http.Error(w, "Cannot demote the current owner directly. Transfer ownership to another user first.", http.StatusBadRequest)
		return
	}

	if newRole == models.StationRoleOwner {
		tx := h.db.Begin()
		if tx.Error != nil {
			http.Error(w, "Failed to transfer ownership", http.StatusInternalServerError)
			return
		}

		// Demote any existing owner(s) in this station, except the selected target.
		if err := tx.Model(&models.StationUser{}).
			Where("station_id = ? AND role = ? AND id <> ?", station.ID, models.StationRoleOwner, stationUser.ID).
			Update("role", models.StationRoleAdmin).Error; err != nil {
			tx.Rollback()
			h.logger.Error().Err(err).Msg("failed to demote previous station owner")
			http.Error(w, "Failed to transfer ownership", http.StatusInternalServerError)
			return
		}

		// Set selected user as owner.
		if err := tx.Model(&models.StationUser{}).
			Where("id = ?", stationUser.ID).
			Update("role", models.StationRoleOwner).Error; err != nil {
			tx.Rollback()
			h.logger.Error().Err(err).Msg("failed to promote new station owner")
			http.Error(w, "Failed to transfer ownership", http.StatusInternalServerError)
			return
		}

		// Keep station owner_id in sync.
		if err := tx.Model(&models.Station{}).
			Where("id = ?", station.ID).
			Update("owner_id", stationUser.UserID).Error; err != nil {
			tx.Rollback()
			h.logger.Error().Err(err).Msg("failed to update station owner_id")
			http.Error(w, "Failed to transfer ownership", http.StatusInternalServerError)
			return
		}

		if err := tx.Commit().Error; err != nil {
			tx.Rollback()
			h.logger.Error().Err(err).Msg("failed to commit station ownership transfer")
			http.Error(w, "Failed to transfer ownership", http.StatusInternalServerError)
			return
		}
	} else {
		stationUser.Role = newRole
		if err := h.db.Save(&stationUser).Error; err != nil {
			h.logger.Error().Err(err).Msg("failed to update station user role")
			http.Error(w, "Failed to update role", http.StatusInternalServerError)
			return
		}
	}

	h.logger.Info().
		Str("station_id", station.ID).
		Str("user_id", stationUser.UserID).
		Str("new_role", string(newRole)).
		Str("updated_by", user.ID).
		Msg("station user role updated")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/station/users")
		return
	}
	http.Redirect(w, r, "/dashboard/station/users", http.StatusSeeOther)
}

// StationUserRemove handles removing a user from the station
func (h *Handler) StationUserRemove(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)
	suID := chi.URLParam(r, "id")

	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	if !h.canManageStationUsers(user, station) {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	var stationUser models.StationUser
	if err := h.db.First(&stationUser, "id = ?", suID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if stationUser.StationID != station.ID {
		http.Error(w, "User does not belong to this station", http.StatusForbidden)
		return
	}

	// Can't remove the owner
	if stationUser.Role == models.StationRoleOwner {
		http.Error(w, "Cannot remove the station owner. Transfer ownership first.", http.StatusBadRequest)
		return
	}

	// Can't remove yourself
	if stationUser.UserID == user.ID {
		http.Error(w, "Cannot remove yourself from the station", http.StatusBadRequest)
		return
	}

	if err := h.db.Delete(&stationUser).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to remove user from station")
		http.Error(w, "Failed to remove user", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("station_id", station.ID).
		Str("user_id", stationUser.UserID).
		Str("removed_by", user.ID).
		Msg("user removed from station")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		return
	}
	http.Redirect(w, r, "/dashboard/station/users", http.StatusSeeOther)
}

// canManageStationUsers checks if user can manage station users
func (h *Handler) canManageStationUsers(user *models.User, station *models.Station) bool {
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

	// Owner and admin can manage users
	return stationUser.Role == models.StationRoleOwner || stationUser.Role == models.StationRoleAdmin
}
