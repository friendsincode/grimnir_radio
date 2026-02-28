/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// diskUsageInfo holds filesystem usage stats for the media storage volume.
type diskUsageInfo struct {
	Total   string
	Used    string
	Free    string
	UsedPct int
	Path    string
}

func getDiskUsage(path string) *diskUsageInfo {
	if path == "" {
		return nil
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - free
	pct := 0
	if total > 0 {
		pct = int(float64(used) / float64(total) * 100)
	}
	return &diskUsageInfo{
		Total:   formatBytesUint64(total),
		Used:    formatBytesUint64(used),
		Free:    formatBytesUint64(free),
		UsedPct: pct,
		Path:    path,
	}
}

func formatBytesUint64(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %s", float64(b)/float64(div), []string{"KB", "MB", "GB", "TB", "PB"}[exp])
}

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

// AdminStationDelete deletes a station from the platform admin "All Stations" page.
func (h *Handler) AdminStationDelete(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	id := chi.URLParam(r, "id")

	// Verify station exists
	var station models.Station
	if err := h.db.First(&station, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Delete in transaction to ensure consistency
	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("station_id = ?", id).Delete(&models.ScheduleEntry{}).Error; err != nil {
			return err
		}

		var clockHourIDs []string
		tx.Model(&models.ClockHour{}).Where("station_id = ?", id).Pluck("id", &clockHourIDs)
		if len(clockHourIDs) > 0 {
			if err := tx.Where("clock_hour_id IN ?", clockHourIDs).Delete(&models.ClockSlot{}).Error; err != nil {
				return err
			}
		}

		if err := tx.Where("station_id = ?", id).Delete(&models.ClockHour{}).Error; err != nil {
			return err
		}
		if err := tx.Where("station_id = ?", id).Delete(&models.Clock{}).Error; err != nil {
			return err
		}

		var playlistIDs []string
		tx.Model(&models.Playlist{}).Where("station_id = ?", id).Pluck("id", &playlistIDs)
		if len(playlistIDs) > 0 {
			if err := tx.Where("playlist_id IN ?", playlistIDs).Delete(&models.PlaylistItem{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("station_id = ?", id).Delete(&models.Playlist{}).Error; err != nil {
			return err
		}

		if err := tx.Where("station_id = ?", id).Delete(&models.SmartBlock{}).Error; err != nil {
			return err
		}
		if err := tx.Where("station_id = ?", id).Delete(&models.MediaItem{}).Error; err != nil {
			return err
		}
		if err := tx.Where("station_id = ?", id).Delete(&models.Webstream{}).Error; err != nil {
			return err
		}
		if err := tx.Where("station_id = ?", id).Delete(&models.Mount{}).Error; err != nil {
			return err
		}
		if err := tx.Where("station_id = ?", id).Delete(&models.StationUser{}).Error; err != nil {
			return err
		}
		if err := tx.Where("station_id = ?", id).Delete(&models.PlayHistory{}).Error; err != nil {
			return err
		}

		return tx.Delete(&station).Error
	})
	if err != nil {
		h.logger.Error().Err(err).Str("station_id", id).Msg("failed to delete station from admin")
		http.Error(w, "Failed to delete station", http.StatusInternalServerError)
		return
	}

	h.publishCacheEvent(events.EventStationDeleted, id)
	h.logger.Info().Str("station_id", id).Str("station_name", station.Name).Str("admin_id", user.ID).Msg("station deleted from admin")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusOK)
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

// =============================================================================
// PLATFORM MEDIA LIBRARY
// =============================================================================

// MediaWithStation embeds media item with its station info
type MediaWithStation struct {
	models.MediaItem
	Station *models.Station
}

type DuplicateMediaItem struct {
	models.MediaItem
	StationName string
	StationUser string
	FileExists  bool
}

type DuplicateHashGroup struct {
	Hash  string
	Count int
	Items []DuplicateMediaItem
}

// AdminMediaList renders the platform-wide media library
func (h *Handler) AdminMediaList(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Pagination
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage := 50

	// Filters
	query := r.URL.Query().Get("q")
	stationID := r.URL.Query().Get("station")
	genre := r.URL.Query().Get("genre")
	showInArchive := r.URL.Query().Get("public") // "true", "false", or "" (all)
	includeOrphans := r.URL.Query().Get("include_orphans") == "true"
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "created_at"
	}
	sortOrder := r.URL.Query().Get("order")
	if sortOrder == "" {
		sortOrder = "desc"
	}

	// Build query
	dbQuery := h.db.Model(&models.MediaItem{})

	// Search filter (use LOWER for cross-database compatibility)
	if query != "" {
		searchPattern := "%" + strings.ToLower(query) + "%"
		dbQuery = dbQuery.Where(
			"LOWER(title) LIKE ? OR LOWER(artist) LIKE ? OR LOWER(album) LIKE ?",
			searchPattern, searchPattern, searchPattern,
		)
	}

	// Station filter
	if stationID != "" {
		dbQuery = dbQuery.Where("station_id = ?", stationID)
	}

	// Genre filter
	if genre != "" {
		dbQuery = dbQuery.Where("genre = ?", genre)
	}

	// Public/private filter
	if showInArchive == "true" {
		dbQuery = dbQuery.Where("show_in_archive = ?", true)
	} else if showInArchive == "false" {
		dbQuery = dbQuery.Where("show_in_archive = ?", false)
	}

	// Count total (use Session clone to avoid mutating query state)
	var total int64
	dbQuery.Session(&gorm.Session{}).Count(&total)

	// Fetch media with pagination
	var media []models.MediaItem
	orderClause := sortBy + " " + strings.ToUpper(sortOrder)
	dbQuery.Session(&gorm.Session{}).Order(orderClause).
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&media)

	// Load station info for each media item
	stationMap := make(map[string]*models.Station)
	var stationIDs []string
	for _, m := range media {
		if m.StationID != "" {
			stationIDs = append(stationIDs, m.StationID)
		}
	}
	if len(stationIDs) > 0 {
		var stations []models.Station
		h.db.Where("id IN ?", stationIDs).Find(&stations)
		for i := range stations {
			stationMap[stations[i].ID] = &stations[i]
		}
	}

	// Build media with station info
	var mediaWithStations []MediaWithStation
	for _, m := range media {
		mws := MediaWithStation{MediaItem: m}
		if s, ok := stationMap[m.StationID]; ok {
			mws.Station = s
		}
		mediaWithStations = append(mediaWithStations, mws)
	}

	// Get all stations for filter dropdown
	var allStations []models.Station
	h.db.Order("name ASC").Find(&allStations)

	// Get unique genres for filter dropdown
	var genres []string
	h.db.Model(&models.MediaItem{}).
		Where("genre != '' AND genre IS NOT NULL").
		Distinct().
		Order("genre ASC").
		Pluck("genre", &genres)

	totalPages := int(total) / perPage
	if int(total)%perPage > 0 {
		totalPages++
	}

	// Optional orphan listing in the same platform media view.
	var orphans []models.OrphanMedia
	var orphanTotal int64
	if h.db != nil {
		orphanQuery := h.db.Model(&models.OrphanMedia{})
		if query != "" {
			searchPattern := "%" + strings.ToLower(query) + "%"
			orphanQuery = orphanQuery.Where(
				"LOWER(title) LIKE ? OR LOWER(artist) LIKE ? OR LOWER(album) LIKE ? OR LOWER(file_path) LIKE ?",
				searchPattern, searchPattern, searchPattern, searchPattern,
			)
		}
		orphanQuery.Session(&gorm.Session{}).Count(&orphanTotal)
		if includeOrphans {
			orphanQuery.Session(&gorm.Session{}).
				Order("detected_at DESC").
				Limit(100).
				Find(&orphans)
		}
	}

	// Get disk usage for media storage volume
	diskUsage := getDiskUsage(h.mediaRoot)

	h.Render(w, r, "pages/dashboard/admin/media", PageData{
		Title:    "Platform Media Library - Admin",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Media":          mediaWithStations,
			"Total":          total,
			"Page":           page,
			"PerPage":        perPage,
			"TotalPages":     totalPages,
			"Query":          query,
			"StationID":      stationID,
			"Genre":          genre,
			"ShowInArchive":  showInArchive,
			"SortBy":         sortBy,
			"SortOrder":      sortOrder,
			"AllStations":    allStations,
			"Genres":         genres,
			"Orphans":        orphans,
			"OrphanTotal":    orphanTotal,
			"IncludeOrphans": includeOrphans,
			"DiskUsage":      diskUsage,
		},
	})
}

// AdminMediaBulk handles bulk actions on media items
func (h *Handler) AdminMediaBulk(w http.ResponseWriter, r *http.Request) {
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
	case "make_public":
		result := h.db.Model(&models.MediaItem{}).Where("id IN ?", req.IDs).Update("show_in_archive", true)
		affected, err = result.RowsAffected, result.Error
	case "make_private":
		result := h.db.Model(&models.MediaItem{}).Where("id IN ?", req.IDs).Update("show_in_archive", false)
		affected, err = result.RowsAffected, result.Error
	case "move_to_station":
		if req.Value == "" {
			http.Error(w, "Target station required", http.StatusBadRequest)
			return
		}
		// Verify target station exists
		var station models.Station
		if err := h.db.First(&station, "id = ?", req.Value).Error; err != nil {
			http.Error(w, "Target station not found", http.StatusBadRequest)
			return
		}
		result := h.db.Model(&models.MediaItem{}).Where("id IN ?", req.IDs).Update("station_id", req.Value)
		affected, err = result.RowsAffected, result.Error

		h.logger.Info().
			Str("target_station", station.Name).
			Int64("count", affected).
			Str("admin_id", user.ID).
			Msg("media items moved to station")
	case "delete":
		// Note: This only deletes database records, not the actual files
		// A separate cleanup job should handle orphaned files
		result := h.db.Where("id IN ?", req.IDs).Delete(&models.MediaItem{})
		affected, err = result.RowsAffected, result.Error
	default:
		http.Error(w, "Unknown action", http.StatusBadRequest)
		return
	}

	if err != nil {
		h.logger.Error().Err(err).Str("action", req.Action).Msg("bulk media action failed")
		http.Error(w, "Operation failed", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("action", req.Action).
		Int64("affected", affected).
		Str("admin_id", user.ID).
		Msg("bulk media action completed")

	w.WriteHeader(http.StatusOK)
}

// AdminMediaTogglePublic toggles a media item's public archive visibility
func (h *Handler) AdminMediaTogglePublic(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	id := chi.URLParam(r, "id")
	var media models.MediaItem
	if err := h.db.First(&media, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	media.ShowInArchive = !media.ShowInArchive
	if err := h.db.Save(&media).Error; err != nil {
		http.Error(w, "Failed to update media", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("media_id", media.ID).
		Bool("show_in_archive", media.ShowInArchive).
		Str("admin_id", user.ID).
		Msg("media archive visibility toggled")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		return
	}
	http.Redirect(w, r, "/dashboard/admin/media", http.StatusSeeOther)
}

// AdminMediaMove moves a media item to a different station
func (h *Handler) AdminMediaMove(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	id := chi.URLParam(r, "id")
	var media models.MediaItem
	if err := h.db.First(&media, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	targetStationID := r.FormValue("station_id")
	if targetStationID == "" {
		http.Error(w, "Target station required", http.StatusBadRequest)
		return
	}

	// Verify target station exists
	var station models.Station
	if err := h.db.First(&station, "id = ?", targetStationID).Error; err != nil {
		http.Error(w, "Target station not found", http.StatusBadRequest)
		return
	}

	oldStationID := media.StationID
	media.StationID = targetStationID
	if err := h.db.Save(&media).Error; err != nil {
		http.Error(w, "Failed to move media", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("media_id", media.ID).
		Str("from_station", oldStationID).
		Str("to_station", targetStationID).
		Str("admin_id", user.ID).
		Msg("media moved to different station")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		return
	}
	http.Redirect(w, r, "/dashboard/admin/media", http.StatusSeeOther)
}

// AdminMediaDelete deletes a media item
func (h *Handler) AdminMediaDelete(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	id := chi.URLParam(r, "id")
	deleted, err := h.adminDeleteMediaByIDs([]string{id})
	if err != nil {
		h.logger.Error().Err(err).Str("media_id", id).Str("admin_id", user.ID).Msg("failed to delete media by admin")
		http.Error(w, "Failed to delete media", http.StatusInternalServerError)
		return
	}
	if deleted == 0 {
		http.NotFound(w, r)
		return
	}

	h.logger.Info().
		Str("media_id", id).
		Str("admin_id", user.ID).
		Msg("media deleted by admin")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		return
	}
	http.Redirect(w, r, "/dashboard/admin/media", http.StatusSeeOther)
}

// AdminMediaDuplicates renders possible duplicate media groups by SHA-256 content hash.
func (h *Handler) AdminMediaDuplicates(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	notice := strings.TrimSpace(r.URL.Query().Get("notice"))
	errMsg := strings.TrimSpace(r.URL.Query().Get("error"))

	// Find hashes with more than one media item.
	type dupHashCount struct {
		ContentHash string
		Count       int64
	}
	var hashCounts []dupHashCount
	if err := h.db.Model(&models.MediaItem{}).
		Select("content_hash, COUNT(*) as count").
		Where("content_hash IS NOT NULL AND content_hash <> ''").
		Group("content_hash").
		Having("COUNT(*) > 1").
		Order("count DESC, content_hash ASC").
		Limit(500).
		Scan(&hashCounts).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to load duplicate hash groups")
		http.Error(w, "Failed to load duplicate media", http.StatusInternalServerError)
		return
	}

	hashes := make([]string, 0, len(hashCounts))
	for _, hc := range hashCounts {
		if strings.TrimSpace(hc.ContentHash) == "" {
			continue
		}
		hashes = append(hashes, hc.ContentHash)
	}

	groups := make([]DuplicateHashGroup, 0, len(hashes))
	if len(hashes) > 0 {
		var items []models.MediaItem
		if err := h.db.Where("content_hash IN ?", hashes).
			Order("content_hash ASC, created_at ASC").
			Find(&items).Error; err != nil {
			h.logger.Error().Err(err).Msg("failed to load duplicate media items")
			http.Error(w, "Failed to load duplicate media", http.StatusInternalServerError)
			return
		}

		var stations []models.Station
		stationNameByID := make(map[string]string)
		stationOwnerIDByStationID := make(map[string]string)
		if err := h.db.Select("id,name,owner_id").Find(&stations).Error; err == nil {
			for _, st := range stations {
				stationNameByID[st.ID] = st.Name
				stationOwnerIDByStationID[st.ID] = strings.TrimSpace(st.OwnerID)
			}
		}

		userIDs := make([]string, 0)
		seenUser := make(map[string]struct{})
		for _, ownerID := range stationOwnerIDByStationID {
			if ownerID == "" {
				continue
			}
			if _, ok := seenUser[ownerID]; !ok {
				seenUser[ownerID] = struct{}{}
				userIDs = append(userIDs, ownerID)
			}
		}
		for _, item := range items {
			if item.CreatedBy != nil && strings.TrimSpace(*item.CreatedBy) != "" {
				userID := strings.TrimSpace(*item.CreatedBy)
				if _, ok := seenUser[userID]; !ok {
					seenUser[userID] = struct{}{}
					userIDs = append(userIDs, userID)
				}
			}
		}
		userEmailByID := make(map[string]string)
		if len(userIDs) > 0 {
			var users []models.User
			if err := h.db.Select("id,email").Where("id IN ?", userIDs).Find(&users).Error; err == nil {
				for _, u := range users {
					userEmailByID[u.ID] = u.Email
				}
			}
		}

		itemByHash := make(map[string][]DuplicateMediaItem)
		for _, item := range items {
			fullPath := filepath.Join(h.mediaRoot, item.Path)
			_, statErr := os.Stat(fullPath)
			uploader := ""
			if item.CreatedBy != nil {
				uploader = userEmailByID[strings.TrimSpace(*item.CreatedBy)]
			}
			if uploader == "" {
				uploader = userEmailByID[stationOwnerIDByStationID[item.StationID]]
			}
			itemByHash[item.ContentHash] = append(itemByHash[item.ContentHash], DuplicateMediaItem{
				MediaItem:   item,
				StationName: stationNameByID[item.StationID],
				StationUser: uploader,
				FileExists:  statErr == nil,
			})
		}

		for _, hc := range hashCounts {
			groupItems := itemByHash[hc.ContentHash]
			if len(groupItems) < 2 {
				continue
			}
			groups = append(groups, DuplicateHashGroup{
				Hash:  hc.ContentHash,
				Count: len(groupItems),
				Items: groupItems,
			})
		}
	}

	var missingHashCount int64
	_ = h.db.Model(&models.MediaItem{}).
		Where("content_hash IS NULL OR content_hash = ''").
		Count(&missingHashCount).Error

	h.Render(w, r, "pages/dashboard/admin/media-duplicates", PageData{
		Title:    "Possible Duplicate Media - Admin",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Groups":           groups,
			"GroupCount":       len(groups),
			"MissingHashCount": missingHashCount,
			"Notice":           notice,
			"Error":            errMsg,
		},
	})
}

// AdminMediaBackfillHashes computes missing SHA-256 content hashes from media files.
func (h *Handler) AdminMediaBackfillHashes(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	var media []models.MediaItem
	if err := h.db.Select("id,path,content_hash").
		Where("content_hash IS NULL OR content_hash = ''").
		Find(&media).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to load media missing hashes")
		http.Error(w, "Failed to load media", http.StatusInternalServerError)
		return
	}

	updated := 0
	missingFiles := 0
	failed := 0
	for _, item := range media {
		if strings.TrimSpace(item.Path) == "" {
			failed++
			continue
		}
		fullPath := filepath.Join(h.mediaRoot, item.Path)
		hash, err := computeSHA256File(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				missingFiles++
			} else {
				failed++
			}
			continue
		}
		if hash == "" {
			failed++
			continue
		}
		if err := h.db.Model(&models.MediaItem{}).Where("id = ?", item.ID).Update("content_hash", hash).Error; err != nil {
			failed++
			continue
		}
		updated++
	}

	msg := fmt.Sprintf("hash backfill complete: %d updated, %d missing files, %d failed", updated, missingFiles, failed)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/admin/media/duplicates?notice="+url.QueryEscape(msg))
		return
	}
	http.Redirect(w, r, "/dashboard/admin/media/duplicates?notice="+url.QueryEscape(msg), http.StatusSeeOther)
}

// AdminMediaPurgeDuplicates deletes selected duplicate media records and files.
func (h *Handler) AdminMediaPurgeDuplicates(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	ids := r.Form["ids"]
	if len(ids) == 0 {
		http.Redirect(w, r, "/dashboard/admin/media/duplicates?error="+url.QueryEscape("No items selected"), http.StatusSeeOther)
		return
	}

	// Ensure selected IDs are duplicate candidates and leave at least one item per hash.
	var selected []models.MediaItem
	if err := h.db.Select("id,content_hash").Where("id IN ?", ids).Find(&selected).Error; err != nil {
		http.Error(w, "Failed to load selected media", http.StatusInternalServerError)
		return
	}
	if len(selected) == 0 {
		http.Redirect(w, r, "/dashboard/admin/media/duplicates?error="+url.QueryEscape("No matching media found"), http.StatusSeeOther)
		return
	}

	selectedPerHash := make(map[string]int)
	hashes := make([]string, 0)
	seenHash := make(map[string]struct{})
	for _, item := range selected {
		hash := strings.TrimSpace(item.ContentHash)
		if hash == "" {
			continue
		}
		selectedPerHash[hash]++
		if _, ok := seenHash[hash]; !ok {
			seenHash[hash] = struct{}{}
			hashes = append(hashes, hash)
		}
	}
	if len(hashes) == 0 {
		http.Redirect(w, r, "/dashboard/admin/media/duplicates?error="+url.QueryEscape("Selected items do not have content hashes"), http.StatusSeeOther)
		return
	}

	type hashCount struct {
		ContentHash string
		Count       int64
	}
	var totals []hashCount
	if err := h.db.Model(&models.MediaItem{}).
		Select("content_hash, COUNT(*) as count").
		Where("content_hash IN ?", hashes).
		Group("content_hash").
		Scan(&totals).Error; err != nil {
		http.Error(w, "Failed to validate duplicates", http.StatusInternalServerError)
		return
	}

	// Validate that at least one copy survives per hash.
	for _, t := range totals {
		if selectedPerHash[t.ContentHash] >= int(t.Count) {
			msg := fmt.Sprintf("Cannot purge all copies for hash %s. Leave at least one item per hash.", shortHash(t.ContentHash))
			http.Redirect(w, r, "/dashboard/admin/media/duplicates?error="+url.QueryEscape(msg), http.StatusSeeOther)
			return
		}
	}

	// For each hash, find the survivor (first item NOT in the delete set).
	survivorByHash := make(map[string]string)
	for _, hash := range hashes {
		var survivors []models.MediaItem
		if err := h.db.Select("id").
			Where("content_hash = ? AND id NOT IN ?", hash, ids).
			Order("created_at ASC").
			Limit(1).
			Find(&survivors).Error; err == nil && len(survivors) > 0 {
			survivorByHash[hash] = survivors[0].ID
		}
	}

	var deleted int64
	err := h.db.Transaction(func(tx *gorm.DB) error {
		// Remap references from each deleted item to its survivor.
		for _, item := range selected {
			hash := strings.TrimSpace(item.ContentHash)
			survivor, ok := survivorByHash[hash]
			if !ok || survivor == "" {
				continue
			}
			if err := adminRemapMediaReferences(tx, []string{item.ID}, survivor); err != nil {
				return fmt.Errorf("remap %s: %w", item.ID, err)
			}
		}
		var txErr error
		deleted, txErr = h.adminDeleteMediaByIDsTx(tx, ids)
		return txErr
	})
	if err != nil {
		h.logger.Error().Err(err).Int("selected", len(ids)).Str("admin_id", user.ID).Msg("duplicate purge failed")
		http.Redirect(w, r, "/dashboard/admin/media/duplicates?error="+url.QueryEscape("Failed to purge selected duplicates"), http.StatusSeeOther)
		return
	}

	msg := fmt.Sprintf("Purged %d duplicate media items.", deleted)
	http.Redirect(w, r, "/dashboard/admin/media/duplicates?notice="+url.QueryEscape(msg), http.StatusSeeOther)
}

func computeSHA256File(fullPath string) (string, error) {
	f, err := os.Open(fullPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func shortHash(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

func (h *Handler) adminDeleteMediaByIDs(ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var deleted int64
	err := h.db.Transaction(func(tx *gorm.DB) error {
		n, err := h.adminDeleteMediaByIDsTx(tx, ids)
		deleted = n
		return err
	})
	return deleted, err
}

// adminDeleteMediaByIDsTx deletes media records and their references within an existing
// transaction. File deletion happens outside the transaction (best-effort, non-fatal).
func (h *Handler) adminDeleteMediaByIDsTx(tx *gorm.DB, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var items []models.MediaItem
	if err := tx.Select("id,path").Where("id IN ?", ids).Find(&items).Error; err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, nil
	}
	validIDs := make([]string, 0, len(items))
	for _, item := range items {
		validIDs = append(validIDs, item.ID)
	}

	// Remove all references to these media items.
	if err := adminDeleteMediaReferences(tx, validIDs); err != nil {
		return 0, err
	}

	result := tx.Where("id IN ?", validIDs).Delete(&models.MediaItem{})
	if result.Error != nil {
		return 0, result.Error
	}

	// Best-effort file deletion (outside transaction scope).
	for _, item := range items {
		if strings.TrimSpace(item.Path) == "" {
			continue
		}
		fullPath := filepath.Join(h.mediaRoot, item.Path)
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			h.logger.Warn().Err(err).Str("path", fullPath).Msg("failed to delete media file")
		}
	}
	return result.RowsAffected, nil
}

// adminRemapMediaReferences re-points all foreign references from oldIDs to newID
// within the given transaction. Used during duplicate purge to preserve references.
func adminRemapMediaReferences(tx *gorm.DB, oldIDs []string, newID string) error {
	if len(oldIDs) == 0 || newID == "" {
		return nil
	}

	// PlaylistItem: re-point then deduplicate
	if err := tx.Model(&models.PlaylistItem{}).
		Where("media_id IN ?", oldIDs).
		Update("media_id", newID).Error; err != nil {
		return fmt.Errorf("remap PlaylistItem: %w", err)
	}
	if err := deduplicatePlaylistItems(tx, newID); err != nil {
		return fmt.Errorf("deduplicate PlaylistItem: %w", err)
	}

	// ScheduleEntry (where SourceType="media")
	if err := tx.Model(&models.ScheduleEntry{}).
		Where("source_type = ? AND source_id IN ?", "media", oldIDs).
		Update("source_id", newID).Error; err != nil {
		return fmt.Errorf("remap ScheduleEntry: %w", err)
	}

	// MountPlayoutState
	if err := tx.Model(&models.MountPlayoutState{}).
		Where("media_id IN ?", oldIDs).
		Update("media_id", newID).Error; err != nil {
		return fmt.Errorf("remap MountPlayoutState: %w", err)
	}

	// PlayHistory
	if err := tx.Model(&models.PlayHistory{}).
		Where("media_id IN ?", oldIDs).
		Update("media_id", newID).Error; err != nil {
		return fmt.Errorf("remap PlayHistory: %w", err)
	}

	// UnderwritingObligation (MediaID is *string)
	if err := tx.Model(&models.UnderwritingObligation{}).
		Where("media_id IN ?", oldIDs).
		Update("media_id", newID).Error; err != nil {
		return fmt.Errorf("remap UnderwritingObligation: %w", err)
	}

	// ClockSlot: best-effort JSON update for hard_item payload
	for _, oldID := range oldIDs {
		tx.Exec(
			`UPDATE clock_slots SET payload = jsonb_set(payload, '{media_id}', to_jsonb(?::text))
			 WHERE type = 'hard_item' AND payload->>'media_id' = ?`,
			newID, oldID,
		)
	}

	// WebDJSession: best-effort JSON update for deck states
	for _, oldID := range oldIDs {
		tx.Exec(
			`UPDATE web_dj_sessions SET deck_a_state = jsonb_set(deck_a_state, '{media_id}', to_jsonb(?::text))
			 WHERE deck_a_state->>'media_id' = ?`,
			newID, oldID,
		)
		tx.Exec(
			`UPDATE web_dj_sessions SET deck_b_state = jsonb_set(deck_b_state, '{media_id}', to_jsonb(?::text))
			 WHERE deck_b_state->>'media_id' = ?`,
			newID, oldID,
		)
	}

	return nil
}

// deduplicatePlaylistItems removes duplicate entries within the same playlist
// after a media remap (same playlist_id + media_id), keeping the lowest position.
func deduplicatePlaylistItems(tx *gorm.DB, mediaID string) error {
	// Find playlists that have duplicates for this media_id.
	type dupKey struct {
		PlaylistID string
	}
	var dups []dupKey
	if err := tx.Model(&models.PlaylistItem{}).
		Select("playlist_id").
		Where("media_id = ?", mediaID).
		Group("playlist_id").
		Having("COUNT(*) > 1").
		Scan(&dups).Error; err != nil {
		return err
	}

	for _, dup := range dups {
		// Find all items for this playlist+media combination.
		var items []models.PlaylistItem
		if err := tx.Where("playlist_id = ? AND media_id = ?", dup.PlaylistID, mediaID).
			Order("position ASC").
			Find(&items).Error; err != nil {
			return err
		}
		if len(items) <= 1 {
			continue
		}
		// Delete all except the first (lowest position).
		deleteIDs := make([]string, 0, len(items)-1)
		for _, item := range items[1:] {
			deleteIDs = append(deleteIDs, item.ID)
		}
		if err := tx.Where("id IN ?", deleteIDs).Delete(&models.PlaylistItem{}).Error; err != nil {
			return err
		}
	}
	return nil
}

// adminDeleteMediaReferences removes or nullifies all foreign references to the
// given media IDs. Used when deleting media without a survivor to remap to.
func adminDeleteMediaReferences(tx *gorm.DB, mediaIDs []string) error {
	if len(mediaIDs) == 0 {
		return nil
	}

	// PlaylistItem: delete
	if err := tx.Where("media_id IN ?", mediaIDs).Delete(&models.PlaylistItem{}).Error; err != nil {
		return fmt.Errorf("delete PlaylistItem refs: %w", err)
	}

	// ScheduleEntry (SourceType="media"): delete
	if err := tx.Where("source_type = ? AND source_id IN ?", "media", mediaIDs).
		Delete(&models.ScheduleEntry{}).Error; err != nil {
		return fmt.Errorf("delete ScheduleEntry refs: %w", err)
	}

	// MountPlayoutState: clear MediaID
	if err := tx.Model(&models.MountPlayoutState{}).
		Where("media_id IN ?", mediaIDs).
		Update("media_id", "").Error; err != nil {
		return fmt.Errorf("clear MountPlayoutState.MediaID: %w", err)
	}

	// PlayHistory: clear MediaID (keep historical row)
	if err := tx.Model(&models.PlayHistory{}).
		Where("media_id IN ?", mediaIDs).
		Update("media_id", "").Error; err != nil {
		return fmt.Errorf("clear PlayHistory.MediaID: %w", err)
	}

	// UnderwritingObligation: nullify MediaID
	if err := tx.Model(&models.UnderwritingObligation{}).
		Where("media_id IN ?", mediaIDs).
		Update("media_id", nil).Error; err != nil {
		return fmt.Errorf("nullify UnderwritingObligation.MediaID: %w", err)
	}

	// ClockSlot: remove media_id from payload JSON (best-effort)
	for _, id := range mediaIDs {
		tx.Exec(
			`UPDATE clock_slots SET payload = payload - 'media_id'
			 WHERE type = 'hard_item' AND payload->>'media_id' = ?`, id,
		)
	}

	return nil
}

// AdminMediaStream allows platform admins to preview any media file regardless of station context.
func (h *Handler) AdminMediaStream(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	id := chi.URLParam(r, "id")
	var media models.MediaItem
	if err := h.db.Select("id,path,title,artist").First(&media, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}
	if strings.TrimSpace(media.Path) == "" {
		http.Error(w, "No media file available", http.StatusNotFound)
		return
	}

	fullPath := filepath.Join(h.mediaRoot, media.Path)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		http.Error(w, "Media file not found", http.StatusNotFound)
		return
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(media.Path), "."))
	contentTypes := map[string]string{
		"mp3":  "audio/mpeg",
		"flac": "audio/flac",
		"wav":  "audio/wav",
		"ogg":  "audio/ogg",
		"m4a":  "audio/mp4",
	}
	contentType := contentTypes[ext]
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeFile(w, r, fullPath)
}

// =============================================================================
// SYSTEM LOGS
// =============================================================================

// AdminLogs renders the system logs viewer page
func (h *Handler) AdminLogs(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	h.Render(w, r, "pages/dashboard/admin/logs", PageData{
		Title:    "System Logs - Admin",
		Stations: h.LoadStations(r),
	})
}
