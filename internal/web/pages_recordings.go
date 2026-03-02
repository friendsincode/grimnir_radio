/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/recording"
)

// RecordingsList renders the recordings management page.
func (h *Handler) RecordingsList(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	var recordings []models.Recording
	var total int64
	h.db.Model(&models.Recording{}).Where("station_id = ?", station.ID).Count(&total)
	h.db.Where("station_id = ?", station.ID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&recordings)

	// Check for active recording
	var activeRecording *models.Recording
	for i, rec := range recordings {
		if rec.Status == models.RecordingStatusActive {
			activeRecording = &recordings[i]
			break
		}
	}

	// Quota info
	quotaBytes := station.RecordingQuotaBytes
	usedBytes := station.RecordingStorageUsed
	var quotaPercent float64
	if quotaBytes > 0 {
		quotaPercent = float64(usedBytes) / float64(quotaBytes) * 100
		if quotaPercent > 100 {
			quotaPercent = 100
		}
	}

	h.Render(w, r, "pages/dashboard/recordings/list", PageData{
		Title:    "Recordings",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Recordings":      recordings,
			"Total":           total,
			"Limit":           limit,
			"Offset":          offset,
			"ActiveRecording": activeRecording,
			"QuotaBytes":      quotaBytes,
			"UsedBytes":       usedBytes,
			"QuotaPercent":    quotaPercent,
			"QuotaEnabled":    quotaBytes > 0,
			"DefaultFormat":   station.RecordingDefaultFormat,
			"AutoRecord":      station.RecordingAutoRecord,
		},
	})
}

// RecordingDetail renders a single recording with chapters.
func (h *Handler) RecordingDetail(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	recordingID := chi.URLParam(r, "id")
	var recording models.Recording
	if err := h.db.Preload("Chapters", func(db *gorm.DB) *gorm.DB {
		return db.Order("position ASC")
	}).First(&recording, "id = ? AND station_id = ?", recordingID, station.ID).Error; err != nil {
		http.Redirect(w, r, "/dashboard/recordings", http.StatusSeeOther)
		return
	}

	h.Render(w, r, "pages/dashboard/recordings/detail", PageData{
		Title:    "Recording: " + recording.Title,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Recording": recording,
		},
	})
}

// RecordingStart handles starting a recording via the recording service.
func (h *Handler) RecordingStart(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "no station", http.StatusBadRequest)
		return
	}
	user := h.GetUser(r)

	mountID := r.FormValue("mount_id")
	title := r.FormValue("title")
	format := r.FormValue("format")

	if mountID == "" {
		var mount models.Mount
		if err := h.db.Where("station_id = ?", station.ID).First(&mount).Error; err != nil {
			http.Redirect(w, r, "/dashboard/recordings", http.StatusSeeOther)
			return
		}
		mountID = mount.ID
	}

	if title == "" {
		title = fmt.Sprintf("Recording %s", time.Now().Format("2006-01-02 15:04"))
	}

	if h.recordingSvc == nil {
		h.logger.Error().Msg("recording service not available")
		http.Redirect(w, r, "/dashboard/recordings", http.StatusSeeOther)
		return
	}

	if _, err := h.recordingSvc.StartRecording(r.Context(), recording.StartRequest{
		StationID: station.ID,
		MountID:   mountID,
		UserID:    user.ID,
		Title:     title,
		Format:    format,
	}); err != nil {
		h.logger.Error().Err(err).Msg("failed to start recording")
		http.Redirect(w, r, "/dashboard/recordings", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/dashboard/recordings", http.StatusSeeOther)
}

// RecordingStop handles stopping a recording via the recording service.
func (h *Handler) RecordingStop(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "no station", http.StatusBadRequest)
		return
	}

	recordingID := chi.URLParam(r, "id")

	// Verify the recording belongs to this station before stopping.
	var rec models.Recording
	if err := h.db.First(&rec, "id = ? AND station_id = ?", recordingID, station.ID).Error; err != nil {
		http.Redirect(w, r, "/dashboard/recordings", http.StatusSeeOther)
		return
	}

	if h.recordingSvc != nil {
		if _, err := h.recordingSvc.StopRecording(r.Context(), recordingID); err != nil {
			h.logger.Error().Err(err).Str("recording_id", recordingID).Msg("failed to stop recording")
		}
	}

	http.Redirect(w, r, "/dashboard/recordings", http.StatusSeeOther)
}

// RecordingDelete handles deleting a recording.
func (h *Handler) RecordingDelete(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "no station", http.StatusBadRequest)
		return
	}

	recordingID := chi.URLParam(r, "id")
	var recording models.Recording
	if err := h.db.First(&recording, "id = ? AND station_id = ?", recordingID, station.ID).Error; err != nil {
		http.Redirect(w, r, "/dashboard/recordings", http.StatusSeeOther)
		return
	}

	if recording.Status == models.RecordingStatusActive {
		http.Redirect(w, r, "/dashboard/recordings", http.StatusSeeOther)
		return
	}

	h.db.Where("recording_id = ?", recordingID).Delete(&models.RecordingChapter{})
	h.db.Delete(&recording)

	if recording.SizeBytes > 0 {
		h.db.Model(&models.Station{}).
			Where("id = ?", recording.StationID).
			Update("recording_storage_used", gorm.Expr("GREATEST(recording_storage_used - ?, 0)", recording.SizeBytes))
	}

	http.Redirect(w, r, "/dashboard/recordings", http.StatusSeeOther)
}

// RecordingUpdateVisibility toggles recording visibility.
func (h *Handler) RecordingUpdateVisibility(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "no station", http.StatusBadRequest)
		return
	}

	recordingID := chi.URLParam(r, "id")
	visibility := r.FormValue("visibility")

	h.db.Model(&models.Recording{}).
		Where("id = ? AND station_id = ?", recordingID, station.ID).
		Update("visibility", visibility)

	w.WriteHeader(http.StatusOK)
}
