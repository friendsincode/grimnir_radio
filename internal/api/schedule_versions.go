/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// AddScheduleVersionRoutes registers schedule version routes.
func (a *API) AddScheduleVersionRoutes(r chi.Router) {
	r.Route("/schedule/versions", func(r chi.Router) {
		r.Get("/", a.handleVersionsList)
		r.Route("/{versionID}", func(r chi.Router) {
			r.Get("/", a.handleVersionsGet)
			r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/restore", a.handleVersionsRestore)
			r.Get("/diff", a.handleVersionsDiff)
		})
	})
}

// handleVersionsList returns all schedule versions for a station.
func (a *API) handleVersionsList(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	var versions []models.ScheduleVersion
	var total int64

	a.db.WithContext(r.Context()).
		Model(&models.ScheduleVersion{}).
		Where("station_id = ?", stationID).
		Count(&total)

	query := a.db.WithContext(r.Context()).
		Where("station_id = ?", stationID).
		Preload("ChangedBy").
		Order("version_number DESC").
		Limit(limit).
		Offset(offset)

	if err := query.Find(&versions).Error; err != nil {
		a.logger.Error().Err(err).Msg("list schedule versions failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Strip snapshot_data from list response to reduce payload
	for i := range versions {
		versions[i].SnapshotData = nil
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"versions": versions,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// handleVersionsGet returns a single schedule version with full snapshot.
func (a *API) handleVersionsGet(w http.ResponseWriter, r *http.Request) {
	versionID := chi.URLParam(r, "versionID")
	if versionID == "" {
		writeError(w, http.StatusBadRequest, "version_id_required")
		return
	}

	var version models.ScheduleVersion
	result := a.db.WithContext(r.Context()).
		Preload("ChangedBy").
		First(&version, "id = ?", versionID)

	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, version)
}

// handleVersionsRestore restores the schedule to a previous version.
func (a *API) handleVersionsRestore(w http.ResponseWriter, r *http.Request) {
	versionID := chi.URLParam(r, "versionID")
	if versionID == "" {
		writeError(w, http.StatusBadRequest, "version_id_required")
		return
	}

	var version models.ScheduleVersion
	result := a.db.WithContext(r.Context()).First(&version, "id = ?", versionID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "version_not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Create version snapshot before restore
	if err := a.createScheduleVersion(r.Context(), version.StationID, "restore", "Restored to version "+strconv.Itoa(version.VersionNumber)); err != nil {
		a.logger.Warn().Err(err).Msg("failed to create version before restore")
	}

	// Parse snapshot entries
	entriesRaw, ok := version.SnapshotData["entries"]
	if !ok {
		writeError(w, http.StatusInternalServerError, "invalid_snapshot_data")
		return
	}

	entriesJSON, err := json.Marshal(entriesRaw)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid_snapshot_data")
		return
	}

	var entries []models.VersionSnapshotEntry
	if err := json.Unmarshal(entriesJSON, &entries); err != nil {
		writeError(w, http.StatusInternalServerError, "invalid_snapshot_data")
		return
	}

	// Get date range from snapshot
	rangeStart, _ := version.SnapshotData["range_start"].(string)
	rangeEnd, _ := version.SnapshotData["range_end"].(string)

	var startTime, endTime time.Time
	if rangeStart != "" {
		startTime, _ = time.Parse(time.RFC3339, rangeStart)
	}
	if rangeEnd != "" {
		endTime, _ = time.Parse(time.RFC3339, rangeEnd)
	}

	// Delete current entries in the snapshot range
	if !startTime.IsZero() && !endTime.IsZero() {
		if err := a.db.WithContext(r.Context()).
			Where("station_id = ? AND starts_at >= ? AND starts_at < ?", version.StationID, startTime, endTime).
			Delete(&models.ScheduleEntry{}).Error; err != nil {
			a.logger.Error().Err(err).Msg("clear entries for restore failed")
			writeError(w, http.StatusInternalServerError, "restore_failed")
			return
		}
	}

	// Recreate entries from snapshot
	var restored int
	for _, se := range entries {
		entry := models.ScheduleEntry{
			ID:         uuid.NewString(), // New ID to avoid conflicts
			StationID:  version.StationID,
			StartsAt:   se.StartsAt,
			EndsAt:     se.EndsAt,
			SourceType: se.SourceType,
			SourceID:   se.SourceID,
			MountID:    se.MountID,
			Metadata:   se.Metadata,
		}
		if err := a.db.WithContext(r.Context()).Create(&entry).Error; err != nil {
			a.logger.Warn().Err(err).Msg("restore entry failed")
			continue
		}
		restored++
	}

	a.logger.Info().
		Str("version_id", versionID).
		Int("version_number", version.VersionNumber).
		Int("restored", restored).
		Msg("schedule restored from version")

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "restored",
		"restored": restored,
		"version":  version.VersionNumber,
	})
}

// handleVersionsDiff returns the diff between two versions.
func (a *API) handleVersionsDiff(w http.ResponseWriter, r *http.Request) {
	versionID := chi.URLParam(r, "versionID")
	if versionID == "" {
		writeError(w, http.StatusBadRequest, "version_id_required")
		return
	}

	compareToID := r.URL.Query().Get("compare_to")

	// Get the target version
	var version models.ScheduleVersion
	result := a.db.WithContext(r.Context()).First(&version, "id = ?", versionID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "version_not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Get comparison version (default to previous version)
	var compareVersion models.ScheduleVersion
	if compareToID != "" {
		result = a.db.WithContext(r.Context()).First(&compareVersion, "id = ?", compareToID)
	} else {
		// Get previous version by version number
		result = a.db.WithContext(r.Context()).
			Where("station_id = ? AND version_number < ?", version.StationID, version.VersionNumber).
			Order("version_number DESC").
			First(&compareVersion)
	}

	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		// No previous version - everything is "added"
		entries := parseSnapshotEntries(version.SnapshotData)
		diff := models.VersionDiff{
			FromVersion: 0,
			ToVersion:   version.VersionNumber,
			Added:       entries,
			Removed:     []models.VersionSnapshotEntry{},
			Modified:    []models.VersionDiffEntry{},
		}
		writeJSON(w, http.StatusOK, diff)
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Calculate diff
	fromEntries := parseSnapshotEntries(compareVersion.SnapshotData)
	toEntries := parseSnapshotEntries(version.SnapshotData)

	diff := calculateDiff(compareVersion.VersionNumber, version.VersionNumber, fromEntries, toEntries)
	writeJSON(w, http.StatusOK, diff)
}

// createScheduleVersion creates a new version snapshot for a station.
func (a *API) createScheduleVersion(ctx context.Context, stationID, changeType, summary string) error {
	// Get current max version number
	var maxVersion int
	a.db.WithContext(ctx).
		Model(&models.ScheduleVersion{}).
		Where("station_id = ?", stationID).
		Select("COALESCE(MAX(version_number), 0)").
		Scan(&maxVersion)

	// Capture current schedule state (next 7 days)
	now := time.Now()
	rangeStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	rangeEnd := rangeStart.AddDate(0, 0, 7)

	var entries []models.ScheduleEntry
	if err := a.db.WithContext(ctx).
		Where("station_id = ? AND starts_at >= ? AND starts_at < ?", stationID, rangeStart, rangeEnd).
		Order("starts_at ASC").
		Find(&entries).Error; err != nil {
		return err
	}

	// Convert to snapshot entries
	snapshotEntries := make([]models.VersionSnapshotEntry, 0, len(entries))
	for _, e := range entries {
		snapshotEntries = append(snapshotEntries, models.VersionSnapshotEntry{
			ID:         e.ID,
			StartsAt:   e.StartsAt,
			EndsAt:     e.EndsAt,
			SourceType: e.SourceType,
			SourceID:   e.SourceID,
			MountID:    e.MountID,
			Metadata:   e.Metadata,
		})
	}

	// Get user ID from context
	var changedByID *string
	if claims, ok := auth.ClaimsFromContext(ctx); ok && claims.UserID != "" {
		changedByID = &claims.UserID
	}

	version := models.ScheduleVersion{
		ID:            uuid.NewString(),
		StationID:     stationID,
		VersionNumber: maxVersion + 1,
		SnapshotData: map[string]any{
			"entries":     snapshotEntries,
			"range_start": rangeStart.Format(time.RFC3339),
			"range_end":   rangeEnd.Format(time.RFC3339),
		},
		ChangeSummary: summary,
		ChangeType:    changeType,
		ChangedByID:   changedByID,
	}

	return a.db.WithContext(ctx).Create(&version).Error
}

// parseSnapshotEntries extracts entries from snapshot data.
func parseSnapshotEntries(data map[string]any) []models.VersionSnapshotEntry {
	entriesRaw, ok := data["entries"]
	if !ok {
		return nil
	}

	entriesJSON, err := json.Marshal(entriesRaw)
	if err != nil {
		return nil
	}

	var entries []models.VersionSnapshotEntry
	if err := json.Unmarshal(entriesJSON, &entries); err != nil {
		return nil
	}

	return entries
}

// calculateDiff computes the difference between two sets of entries.
func calculateDiff(fromVersion, toVersion int, from, to []models.VersionSnapshotEntry) models.VersionDiff {
	diff := models.VersionDiff{
		FromVersion: fromVersion,
		ToVersion:   toVersion,
		Added:       []models.VersionSnapshotEntry{},
		Removed:     []models.VersionSnapshotEntry{},
		Modified:    []models.VersionDiffEntry{},
	}

	// Create maps for efficient lookup
	fromMap := make(map[string]models.VersionSnapshotEntry)
	for _, e := range from {
		fromMap[e.ID] = e
	}

	toMap := make(map[string]models.VersionSnapshotEntry)
	for _, e := range to {
		toMap[e.ID] = e
	}

	// Find added and modified entries
	for _, toEntry := range to {
		fromEntry, exists := fromMap[toEntry.ID]
		if !exists {
			diff.Added = append(diff.Added, toEntry)
		} else if hasChanges(fromEntry, toEntry) {
			diff.Modified = append(diff.Modified, models.VersionDiffEntry{
				ID:      toEntry.ID,
				Before:  fromEntry,
				After:   toEntry,
				Changes: getChanges(fromEntry, toEntry),
			})
		}
	}

	// Find removed entries
	for _, fromEntry := range from {
		if _, exists := toMap[fromEntry.ID]; !exists {
			diff.Removed = append(diff.Removed, fromEntry)
		}
	}

	return diff
}

// hasChanges checks if two entries differ.
func hasChanges(a, b models.VersionSnapshotEntry) bool {
	return !a.StartsAt.Equal(b.StartsAt) ||
		!a.EndsAt.Equal(b.EndsAt) ||
		a.SourceType != b.SourceType ||
		a.SourceID != b.SourceID ||
		a.MountID != b.MountID
}

// getChanges returns a map of changed field names.
func getChanges(a, b models.VersionSnapshotEntry) map[string]any {
	changes := make(map[string]any)

	if !a.StartsAt.Equal(b.StartsAt) {
		changes["starts_at"] = map[string]any{"from": a.StartsAt, "to": b.StartsAt}
	}
	if !a.EndsAt.Equal(b.EndsAt) {
		changes["ends_at"] = map[string]any{"from": a.EndsAt, "to": b.EndsAt}
	}
	if a.SourceType != b.SourceType {
		changes["source_type"] = map[string]any{"from": a.SourceType, "to": b.SourceType}
	}
	if a.SourceID != b.SourceID {
		changes["source_id"] = map[string]any{"from": a.SourceID, "to": b.SourceID}
	}
	if a.MountID != b.MountID {
		changes["mount_id"] = map[string]any{"from": a.MountID, "to": b.MountID}
	}

	return changes
}
