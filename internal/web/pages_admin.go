/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"encoding/json"
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

		// If demoting an admin to a non-admin role, check if they're the last admin
		if targetUser.PlatformRole == models.PlatformRoleAdmin && newRole != models.PlatformRoleAdmin {
			if errMsg := h.ensureAtLeastOneAdmin([]string{targetUser.ID}); errMsg != "" {
				http.Error(w, errMsg, http.StatusBadRequest)
				return
			}
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

	// Check if deleting this user would leave no admins
	var targetUser models.User
	if err := h.db.First(&targetUser, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if targetUser.PlatformRole == models.PlatformRoleAdmin {
		if errMsg := h.ensureAtLeastOneAdmin([]string{id}); errMsg != "" {
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
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

// BulkRequest is the JSON structure for bulk action requests
type BulkRequest struct {
	Action string   `json:"action"`
	IDs    []string `json:"ids"`
	Value  string   `json:"value,omitempty"`
}

// ensureAtLeastOneAdmin checks if demoting or deleting users would leave the platform without admins.
// Returns an error message if the operation would leave no admins, or empty string if safe to proceed.
func (h *Handler) ensureAtLeastOneAdmin(excludeIDs []string) string {
	// Count remaining admins not in the exclude list
	var adminCount int64
	h.db.Model(&models.User{}).
		Where("platform_role = ?", models.PlatformRoleAdmin).
		Where("id NOT IN ?", excludeIDs).
		Count(&adminCount)

	if adminCount == 0 {
		return "Cannot perform this action - it would leave the platform without any administrators"
	}
	return ""
}

// promoteFirstUserToAdmin ensures there's at least one platform admin.
// If no admin exists, promotes the first created user.
func (h *Handler) promoteFirstUserToAdmin() {
	var adminCount int64
	h.db.Model(&models.User{}).Where("platform_role = ?", models.PlatformRoleAdmin).Count(&adminCount)

	if adminCount == 0 {
		// Find the first user by creation time
		var firstUser models.User
		if err := h.db.Order("created_at ASC").First(&firstUser).Error; err == nil {
			firstUser.PlatformRole = models.PlatformRoleAdmin
			h.db.Save(&firstUser)
			h.logger.Warn().
				Str("user_id", firstUser.ID).
				Str("email", firstUser.Email).
				Msg("promoted first user to platform admin - no other admins exist")
		}
	}
}

// AdminStationsBulk handles bulk actions on stations
func (h *Handler) AdminStationsBulk(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	var req BulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if len(req.IDs) == 0 {
		http.Error(w, "No items selected", http.StatusBadRequest)
		return
	}

	var affected int64
	var err error

	switch req.Action {
	case "activate":
		result := h.db.Model(&models.Station{}).Where("id IN ?", req.IDs).Update("active", true)
		affected, err = result.RowsAffected, result.Error
	case "deactivate":
		result := h.db.Model(&models.Station{}).Where("id IN ?", req.IDs).Update("active", false)
		affected, err = result.RowsAffected, result.Error
	case "make_public":
		result := h.db.Model(&models.Station{}).Where("id IN ?", req.IDs).Update("public", true)
		affected, err = result.RowsAffected, result.Error
	case "make_private":
		result := h.db.Model(&models.Station{}).Where("id IN ?", req.IDs).Update("public", false)
		affected, err = result.RowsAffected, result.Error
	case "approve":
		result := h.db.Model(&models.Station{}).Where("id IN ?", req.IDs).Update("approved", true)
		affected, err = result.RowsAffected, result.Error
	case "unapprove":
		result := h.db.Model(&models.Station{}).Where("id IN ?", req.IDs).Update("approved", false)
		affected, err = result.RowsAffected, result.Error
	case "delete":
		result := h.db.Where("id IN ?", req.IDs).Delete(&models.Station{})
		affected, err = result.RowsAffected, result.Error
	default:
		http.Error(w, "Unknown action", http.StatusBadRequest)
		return
	}

	if err != nil {
		h.logger.Error().Err(err).Str("action", req.Action).Msg("bulk station action failed")
		http.Error(w, "Operation failed", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("action", req.Action).
		Int64("affected", affected).
		Str("admin_id", user.ID).
		Msg("bulk station action completed")

	w.WriteHeader(http.StatusOK)
}

// AdminUsersBulk handles bulk actions on users
func (h *Handler) AdminUsersBulk(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	var req BulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if len(req.IDs) == 0 {
		http.Error(w, "No items selected", http.StatusBadRequest)
		return
	}

	// Filter out current user from bulk operations
	filteredIDs := make([]string, 0, len(req.IDs))
	for _, id := range req.IDs {
		if id != user.ID {
			filteredIDs = append(filteredIDs, id)
		}
	}
	if len(filteredIDs) == 0 {
		http.Error(w, "Cannot perform bulk action on yourself", http.StatusBadRequest)
		return
	}

	var affected int64
	var err error

	switch req.Action {
	case "set_role_user":
		// Check if demoting admins would leave no admins
		if errMsg := h.ensureAtLeastOneAdmin(filteredIDs); errMsg != "" {
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
		result := h.db.Model(&models.User{}).Where("id IN ?", filteredIDs).Update("platform_role", models.PlatformRoleUser)
		affected, err = result.RowsAffected, result.Error
	case "set_role_mod":
		// Check if demoting admins would leave no admins
		if errMsg := h.ensureAtLeastOneAdmin(filteredIDs); errMsg != "" {
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
		result := h.db.Model(&models.User{}).Where("id IN ?", filteredIDs).Update("platform_role", models.PlatformRoleMod)
		affected, err = result.RowsAffected, result.Error
	case "set_role_admin":
		result := h.db.Model(&models.User{}).Where("id IN ?", filteredIDs).Update("platform_role", models.PlatformRoleAdmin)
		affected, err = result.RowsAffected, result.Error
	case "delete":
		// Check if deleting admins would leave no admins
		if errMsg := h.ensureAtLeastOneAdmin(filteredIDs); errMsg != "" {
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
		result := h.db.Where("id IN ?", filteredIDs).Delete(&models.User{})
		affected, err = result.RowsAffected, result.Error
	default:
		http.Error(w, "Unknown action", http.StatusBadRequest)
		return
	}

	if err != nil {
		h.logger.Error().Err(err).Str("action", req.Action).Msg("bulk user action failed")
		http.Error(w, "Operation failed", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("action", req.Action).
		Int64("affected", affected).
		Str("admin_id", user.ID).
		Msg("bulk user action completed")

	w.WriteHeader(http.StatusOK)
}
