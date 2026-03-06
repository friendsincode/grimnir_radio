/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

type playoutQueueCreateRequest struct {
	StationID string `json:"station_id"`
	MountID   string `json:"mount_id"`
	MediaID   string `json:"media_id"`
	Position  *int   `json:"position"`
}

type playoutQueueReorderRequest struct {
	Position int `json:"position"`
}

type playoutQueueItemResponse struct {
	ID        string  `json:"id"`
	StationID string  `json:"station_id"`
	MountID   string  `json:"mount_id"`
	MediaID   string  `json:"media_id"`
	Position  int     `json:"position"`
	Title     string  `json:"title,omitempty"`
	Artist    string  `json:"artist,omitempty"`
	Duration  float64 `json:"duration_seconds,omitempty"`
	CreatedAt string  `json:"created_at,omitempty"`
}

func (a *API) handlePlayoutQueueList(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		if claims, ok := auth.ClaimsFromContext(r.Context()); ok && claims != nil {
			stationID = claims.StationID
		}
	}
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}
	if !a.requireStationAccess(w, r, stationID) {
		return
	}
	if !a.requireStationQueueRole(w, r, stationID, true) {
		return
	}

	mountID := r.URL.Query().Get("mount_id")
	query := a.db.WithContext(r.Context()).Where("station_id = ?", stationID)
	if mountID != "" {
		if !a.mountBelongsToStation(r, stationID, mountID) {
			writeError(w, http.StatusBadRequest, "mount_not_in_station")
			return
		}
		query = query.Where("mount_id = ?", mountID)
	}

	var items []models.PlayoutQueueItem
	if err := query.Order("mount_id ASC, position ASC, created_at ASC").Find(&items).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	mediaIDs := make([]string, 0, len(items))
	for _, item := range items {
		mediaIDs = append(mediaIDs, item.MediaID)
	}

	mediaByID := make(map[string]models.MediaItem, len(mediaIDs))
	if len(mediaIDs) > 0 {
		var mediaItems []models.MediaItem
		if err := a.db.WithContext(r.Context()).
			Select("id, title, artist, duration").
			Where("id IN ?", mediaIDs).
			Find(&mediaItems).Error; err == nil {
			for _, m := range mediaItems {
				mediaByID[m.ID] = m
			}
		}
	}

	resp := make([]playoutQueueItemResponse, 0, len(items))
	for _, item := range items {
		entry := playoutQueueItemResponse{
			ID:        item.ID,
			StationID: item.StationID,
			MountID:   item.MountID,
			MediaID:   item.MediaID,
			Position:  item.Position,
			CreatedAt: item.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		}
		if m, ok := mediaByID[item.MediaID]; ok {
			entry.Title = m.Title
			entry.Artist = m.Artist
			entry.Duration = m.Duration.Seconds()
		}
		resp = append(resp, entry)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"station_id": stationID,
		"mount_id":   mountID,
		"items":      resp,
		"count":      len(resp),
	})
}

func (a *API) handlePlayoutQueueCreate(w http.ResponseWriter, r *http.Request) {
	var req playoutQueueCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.MediaID == "" {
		writeError(w, http.StatusBadRequest, "media_id_required")
		return
	}

	claims, _ := auth.ClaimsFromContext(r.Context())
	if req.StationID == "" && claims != nil {
		req.StationID = claims.StationID
	}
	if req.StationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}
	if !a.requireStationAccess(w, r, req.StationID) {
		return
	}
	if !a.requireStationQueueRole(w, r, req.StationID, false) {
		return
	}

	mountID := req.MountID
	if mountID == "" {
		var mount models.Mount
		if err := a.db.WithContext(r.Context()).
			Where("station_id = ?", req.StationID).
			Order("name ASC").
			First(&mount).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				writeError(w, http.StatusBadRequest, "mount_id_required")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error")
			return
		}
		mountID = mount.ID
	} else if !a.mountBelongsToStation(r, req.StationID, mountID) {
		writeError(w, http.StatusBadRequest, "mount_not_in_station")
		return
	}

	var mediaItem models.MediaItem
	if err := a.db.WithContext(r.Context()).
		Select("id, station_id, title, artist, duration").
		First(&mediaItem, "id = ?", req.MediaID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "media_not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	if mediaItem.StationID != req.StationID {
		writeError(w, http.StatusBadRequest, "media_not_in_station")
		return
	}

	item := models.PlayoutQueueItem{
		ID:        uuid.NewString(),
		StationID: req.StationID,
		MountID:   mountID,
		MediaID:   req.MediaID,
	}
	if claims != nil && claims.UserID != "" {
		item.QueuedBy = &claims.UserID
	}

	if err := a.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&models.PlayoutQueueItem{}).
			Where("station_id = ? AND mount_id = ?", req.StationID, mountID).
			Count(&count).Error; err != nil {
			return err
		}

		newPos := int(count) + 1
		if req.Position != nil && *req.Position > 0 {
			newPos = *req.Position
			if newPos > int(count)+1 {
				newPos = int(count) + 1
			}
		}

		if err := tx.Model(&models.PlayoutQueueItem{}).
			Where("station_id = ? AND mount_id = ? AND position >= ?", req.StationID, mountID, newPos).
			Update("position", gorm.Expr("position + 1")).Error; err != nil {
			return err
		}

		item.Position = newPos
		return tx.Create(&item).Error
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	a.publishQueueEvent("created", item)
	a.publishAuditEvent(r, events.EventAuditPlayoutQueueAdd, events.Payload{
		"station_id":    item.StationID,
		"resource_type": "playout_queue_item",
		"resource_id":   item.ID,
		"mount_id":      item.MountID,
		"media_id":      item.MediaID,
		"position":      item.Position,
	})

	writeJSON(w, http.StatusCreated, playoutQueueItemResponse{
		ID:        item.ID,
		StationID: item.StationID,
		MountID:   item.MountID,
		MediaID:   item.MediaID,
		Position:  item.Position,
		Title:     mediaItem.Title,
		Artist:    mediaItem.Artist,
		Duration:  mediaItem.Duration.Seconds(),
		CreatedAt: item.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	})
}

func (a *API) handlePlayoutQueueReorder(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	if queueID == "" {
		writeError(w, http.StatusBadRequest, "queue_id_required")
		return
	}

	var req playoutQueueReorderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.Position <= 0 {
		writeError(w, http.StatusBadRequest, "position_required")
		return
	}

	var item models.PlayoutQueueItem
	if err := a.db.WithContext(r.Context()).First(&item, "id = ?", queueID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	if !a.requireStationAccess(w, r, item.StationID) {
		return
	}
	if !a.requireStationQueueRole(w, r, item.StationID, false) {
		return
	}

	if err := a.reorderQueueItem(r, item, req.Position); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	var updated models.PlayoutQueueItem
	if err := a.db.WithContext(r.Context()).First(&updated, "id = ?", item.ID).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	a.publishQueueEvent("reordered", updated)
	a.publishAuditEvent(r, events.EventAuditPlayoutQueueMove, events.Payload{
		"station_id":    updated.StationID,
		"resource_type": "playout_queue_item",
		"resource_id":   updated.ID,
		"mount_id":      updated.MountID,
		"media_id":      updated.MediaID,
		"position":      updated.Position,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         updated.ID,
		"station_id": updated.StationID,
		"mount_id":   updated.MountID,
		"media_id":   updated.MediaID,
		"position":   updated.Position,
	})
}

func (a *API) handlePlayoutQueueDelete(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	if queueID == "" {
		writeError(w, http.StatusBadRequest, "queue_id_required")
		return
	}

	var item models.PlayoutQueueItem
	if err := a.db.WithContext(r.Context()).First(&item, "id = ?", queueID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	if !a.requireStationAccess(w, r, item.StationID) {
		return
	}
	if !a.requireStationQueueRole(w, r, item.StationID, false) {
		return
	}

	if err := a.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&models.PlayoutQueueItem{}, "id = ?", item.ID).Error; err != nil {
			return err
		}
		return tx.Model(&models.PlayoutQueueItem{}).
			Where("station_id = ? AND mount_id = ? AND position > ?", item.StationID, item.MountID, item.Position).
			Update("position", gorm.Expr("position - 1")).Error
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	a.publishQueueEvent("deleted", item)
	a.publishAuditEvent(r, events.EventAuditPlayoutQueueDelete, events.Payload{
		"station_id":    item.StationID,
		"resource_type": "playout_queue_item",
		"resource_id":   item.ID,
		"mount_id":      item.MountID,
		"media_id":      item.MediaID,
		"position":      item.Position,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "deleted",
		"id":     item.ID,
	})
}

func (a *API) reorderQueueItem(r *http.Request, item models.PlayoutQueueItem, newPosition int) error {
	return a.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		var items []models.PlayoutQueueItem
		if err := tx.Where("station_id = ? AND mount_id = ?", item.StationID, item.MountID).
			Order("position ASC, created_at ASC").
			Find(&items).Error; err != nil {
			return err
		}

		currentIdx := -1
		for i := range items {
			if items[i].ID == item.ID {
				currentIdx = i
				break
			}
		}
		if currentIdx < 0 {
			return gorm.ErrRecordNotFound
		}

		targetIdx := newPosition - 1
		if targetIdx < 0 {
			targetIdx = 0
		}
		if targetIdx >= len(items) {
			targetIdx = len(items) - 1
		}
		if currentIdx == targetIdx {
			return nil
		}

		moved := items[currentIdx]
		items = append(items[:currentIdx], items[currentIdx+1:]...)
		if targetIdx >= len(items) {
			items = append(items, moved)
		} else {
			items = append(items[:targetIdx], append([]models.PlayoutQueueItem{moved}, items[targetIdx:]...)...)
		}

		for i := range items {
			nextPos := i + 1
			if items[i].Position == nextPos {
				continue
			}
			if err := tx.Model(&models.PlayoutQueueItem{}).
				Where("id = ?", items[i].ID).
				Update("position", nextPos).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (a *API) mountBelongsToStation(r *http.Request, stationID, mountID string) bool {
	if mountID == "" {
		return false
	}
	var count int64
	if err := a.db.WithContext(r.Context()).
		Model(&models.Mount{}).
		Where("id = ? AND station_id = ?", mountID, stationID).
		Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func (a *API) requireStationQueueRole(w http.ResponseWriter, r *http.Request, stationID string, allowDJ bool) bool {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	if claimsHasPlatformAdmin(claims) {
		return true
	}
	if claims.UserID == "" {
		writeError(w, http.StatusForbidden, "insufficient_role")
		return false
	}

	var su models.StationUser
	if err := a.db.WithContext(r.Context()).
		Where("station_id = ? AND user_id = ?", stationID, claims.UserID).
		First(&su).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusForbidden, "insufficient_role")
			return false
		}
		writeError(w, http.StatusInternalServerError, "db_error")
		return false
	}

	switch su.Role {
	case models.StationRoleOwner, models.StationRoleAdmin, models.StationRoleManager:
		return true
	case models.StationRoleDJ:
		if allowDJ {
			return true
		}
	}

	writeError(w, http.StatusForbidden, "insufficient_role")
	return false
}

func (a *API) publishQueueEvent(action string, item models.PlayoutQueueItem) {
	if a.bus == nil {
		return
	}
	a.bus.Publish(events.EventPlayoutQueueChange, events.Payload{
		"action":     action,
		"id":         item.ID,
		"station_id": item.StationID,
		"mount_id":   item.MountID,
		"media_id":   item.MediaID,
		"position":   item.Position,
	})
}
