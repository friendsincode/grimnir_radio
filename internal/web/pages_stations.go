/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// defaultMountURL derives a stream URL from the request host and mount name.
// Format: https://host/live/{mountName}
func defaultMountURL(r *http.Request, mountName string) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/live/%s", scheme, r.Host, mountName)
}

// StationsList renders the stations management page
func (h *Handler) StationsList(w http.ResponseWriter, r *http.Request) {
	var stations []models.Station
	h.db.Order("name ASC").Find(&stations)

	h.Render(w, r, "pages/dashboard/stations/list", PageData{
		Title:    "Stations",
		Stations: h.LoadStations(r),
		Data:     stations,
	})
}

// StationNew renders the new station form
func (h *Handler) StationNew(w http.ResponseWriter, r *http.Request) {
	h.Render(w, r, "pages/dashboard/stations/form", PageData{
		Title:    "New Station",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Station": models.Station{},
			"IsNew":   true,
		},
	})
}

// StationCreate handles new station creation
func (h *Handler) StationCreate(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	if err := parseURLFormSemicolonTolerant(r); err != nil {
		h.renderStationFormError(w, r, models.Station{
			Name:        r.FormValue("name"),
			Description: r.FormValue("description"),
			Timezone:    r.FormValue("timezone"),
		}, true, "Invalid form data. Please remove unsupported characters and try again.")
		return
	}

	station := models.Station{
		ID:          uuid.New().String(),
		Name:        r.FormValue("name"),
		Description: r.FormValue("description"),
		Timezone:    r.FormValue("timezone"),
		OwnerID:     user.ID,
		Active:      true,                   // Active by default
		Public:      false,                  // Private by default
		Approved:    user.IsPlatformAdmin(), // Auto-approve for admins, otherwise needs approval
		SortOrder:   parseIntOrDefault(r.FormValue("sort_order"), 0),
	}

	// Platform admins can set active status
	if user.IsPlatformAdmin() {
		station.Active = r.FormValue("active") == "on"
	}

	if station.Name == "" {
		h.renderStationFormError(w, r, station, true, "Name is required")
		return
	}
	if err := validateStationDescription(station.Description); err != nil {
		h.renderStationFormError(w, r, station, true, err.Error())
		return
	}

	if station.Timezone == "" {
		station.Timezone = "UTC"
	}

	// Create station in transaction with owner association
	tx := h.db.Begin()

	if err := tx.Create(&station).Error; err != nil {
		tx.Rollback()
		h.logger.Error().Err(err).Msg("failed to create station")
		h.renderStationFormError(w, r, station, true, "Failed to create station")
		return
	}

	// Create station-user association with owner role
	stationUser := models.StationUser{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		StationID: station.ID,
		Role:      models.StationRoleOwner,
	}

	if err := tx.Create(&stationUser).Error; err != nil {
		tx.Rollback()
		h.logger.Error().Err(err).Msg("failed to create station-user association")
		h.renderStationFormError(w, r, station, true, "Failed to create station")
		return
	}

	// Auto-generate default mount point
	mountName := models.GenerateMountName(station.Shortcode)
	if mountName == "" || mountName == "radio" {
		mountName = models.GenerateMountName(station.Name)
	}

	mount := models.Mount{
		ID:         uuid.New().String(),
		StationID:  station.ID,
		Name:       mountName,
		URL:        defaultMountURL(r, mountName),
		Format:     "mp3",
		Bitrate:    128,
		Channels:   2,
		SampleRate: 44100,
	}

	if err := tx.Create(&mount).Error; err != nil {
		tx.Rollback()
		h.logger.Error().Err(err).Msg("failed to create default mount")
		h.renderStationFormError(w, r, station, true, "Failed to create station")
		return
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		h.logger.Error().Err(err).Msg("failed to commit station create transaction")
		h.renderStationFormError(w, r, station, true, "Failed to create station")
		return
	}

	// Switch current context to the station that was just created so subsequent
	// dashboard actions target the new station by default.
	h.SetStation(w, station.ID)

	// Publish cache invalidation event
	h.publishCacheEvent(events.EventStationCreated, station.ID)

	h.logger.Info().
		Str("station_id", station.ID).
		Str("owner_id", user.ID).
		Str("name", station.Name).
		Str("mount", mountName).
		Msg("station created with default mount")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/stations")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/dashboard/stations", http.StatusSeeOther)
}

// StationEdit renders the station edit form
func (h *Handler) StationEdit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var station models.Station
	if err := h.db.First(&station, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	h.Render(w, r, "pages/dashboard/stations/form", PageData{
		Title:    "Edit Station",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Station": station,
			"IsNew":   false,
		},
	})
}

// StationUpdate handles station updates
func (h *Handler) StationUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var station models.Station
	if err := h.db.First(&station, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := parseURLFormSemicolonTolerant(r); err != nil {
		h.renderStationFormError(w, r, station, false, "Invalid form data. Please remove unsupported characters and try again.")
		return
	}

	station.Name = r.FormValue("name")
	station.Description = r.FormValue("description")
	station.Timezone = r.FormValue("timezone")
	station.Active = r.FormValue("active") == "on"
	station.SortOrder = parseIntOrDefault(r.FormValue("sort_order"), station.SortOrder)

	if station.Name == "" {
		h.renderStationFormError(w, r, station, false, "Name is required")
		return
	}
	if err := validateStationDescription(station.Description); err != nil {
		h.renderStationFormError(w, r, station, false, err.Error())
		return
	}

	if err := h.db.Save(&station).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to update station")
		h.renderStationFormError(w, r, station, false, "Failed to update station")
		return
	}

	// Publish cache invalidation event
	h.publishCacheEvent(events.EventStationUpdated, station.ID)

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/stations")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/dashboard/stations", http.StatusSeeOther)
}

// StationDelete handles station deletion with full cascade
func (h *Handler) StationDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Verify station exists
	var station models.Station
	if err := h.db.First(&station, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Delete in transaction to ensure consistency
	err := h.db.Transaction(func(tx *gorm.DB) error {
		return cascadeDeleteStation(tx, id, &station)
	})

	if err != nil {
		h.logger.Error().Err(err).Str("station_id", id).Msg("failed to delete station")
		http.Error(w, "Failed to delete station", http.StatusInternalServerError)
		return
	}

	// Publish cache invalidation event
	h.publishCacheEvent(events.EventStationDeleted, id)

	h.logger.Info().Str("station_id", id).Str("station_name", station.Name).Msg("station deleted with all data")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/dashboard/stations", http.StatusSeeOther)
}

// cascadeDeleteStation deletes a station and all its associated data within a transaction.
// Call this inside db.Transaction(). station must already be fetched.
func cascadeDeleteStation(tx *gorm.DB, id string, station *models.Station) error {
	// --- leaf records first (no dependents) ---

	// Schedule entries
	if err := tx.Where("station_id = ?", id).Delete(&models.ScheduleEntry{}).Error; err != nil {
		return err
	}
	// Schedule rules, templates, versions
	if err := tx.Where("station_id = ?", id).Delete(&models.ScheduleRule{}).Error; err != nil {
		return err
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.ScheduleTemplate{}).Error; err != nil {
		return err
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.ScheduleVersion{}).Error; err != nil {
		return err
	}

	// Shows and instances
	if err := tx.Where("station_id = ?", id).Delete(&models.ShowInstance{}).Error; err != nil {
		return err
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.Show{}).Error; err != nil {
		return err
	}

	// DJ self-service
	if err := tx.Where("station_id = ?", id).Delete(&models.ScheduleRequest{}).Error; err != nil {
		return err
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.DJAvailability{}).Error; err != nil {
		return err
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.ScheduleLock{}).Error; err != nil {
		return err
	}

	// Webhooks
	var webhookTargetIDs []string
	tx.Model(&models.WebhookTarget{}).Where("station_id = ?", id).Pluck("id", &webhookTargetIDs)
	if len(webhookTargetIDs) > 0 {
		if err := tx.Where("target_id IN ?", webhookTargetIDs).Delete(&models.WebhookLog{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.WebhookTarget{}).Error; err != nil {
		return err
	}

	// Analytics
	if err := tx.Where("station_id = ?", id).Delete(&models.ListenerSample{}).Error; err != nil {
		return err
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.ScheduleAnalytics{}).Error; err != nil {
		return err
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.ScheduleAnalyticsDaily{}).Error; err != nil {
		return err
	}

	// Syndication
	if err := tx.Where("station_id = ?", id).Delete(&models.NetworkSubscription{}).Error; err != nil {
		return err
	}

	// Underwriting
	var obligationIDs []string
	tx.Model(&models.UnderwritingObligation{}).Where("station_id = ?", id).Pluck("id", &obligationIDs)
	if len(obligationIDs) > 0 {
		if err := tx.Where("obligation_id IN ?", obligationIDs).Delete(&models.UnderwritingSpot{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.UnderwritingObligation{}).Error; err != nil {
		return err
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.Sponsor{}).Error; err != nil {
		return err
	}

	// Landing page
	if err := tx.Where("station_id = ?", id).Delete(&models.LandingPageAsset{}).Error; err != nil {
		return err
	}
	var landingPageIDs []string
	tx.Model(&models.LandingPage{}).Where("station_id = ?", id).Pluck("id", &landingPageIDs)
	if len(landingPageIDs) > 0 {
		if err := tx.Where("landing_page_id IN ?", landingPageIDs).Delete(&models.LandingPageVersion{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.LandingPage{}).Error; err != nil {
		return err
	}

	// Recordings
	var recordingIDs []string
	tx.Model(&models.Recording{}).Where("station_id = ?", id).Pluck("id", &recordingIDs)
	if len(recordingIDs) > 0 {
		if err := tx.Where("recording_id IN ?", recordingIDs).Delete(&models.RecordingChapter{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.Recording{}).Error; err != nil {
		return err
	}

	// Clocks and clock slots
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

	// Playlists
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

	// Smart blocks
	if err := tx.Where("station_id = ?", id).Delete(&models.SmartBlock{}).Error; err != nil {
		return err
	}

	// Media items — collect IDs first so we can clean up analysis jobs
	var mediaIDs []string
	tx.Model(&models.MediaItem{}).Where("station_id = ?", id).Pluck("id", &mediaIDs)
	if len(mediaIDs) > 0 {
		if err := tx.Where("media_id IN ?", mediaIDs).Delete(&models.AnalysisJob{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.MediaItem{}).Error; err != nil {
		return err
	}

	// Runtime state tables
	if err := tx.Where("station_id = ?", id).Delete(&models.PlayoutQueueItem{}).Error; err != nil {
		return err
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.MountPlayoutState{}).Error; err != nil {
		return err
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.PrioritySource{}).Error; err != nil {
		return err
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.ExecutorState{}).Error; err != nil {
		return err
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.LiveSession{}).Error; err != nil {
		return err
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.WebDJSession{}).Error; err != nil {
		return err
	}

	// Webstreams
	if err := tx.Where("station_id = ?", id).Delete(&models.Webstream{}).Error; err != nil {
		return err
	}

	// Mounts
	if err := tx.Where("station_id = ?", id).Delete(&models.Mount{}).Error; err != nil {
		return err
	}

	// Station groups
	var groupIDs []string
	tx.Model(&models.StationGroup{}).Where("station_id = ?", id).Pluck("id", &groupIDs)
	if len(groupIDs) > 0 {
		if err := tx.Where("group_id IN ?", groupIDs).Delete(&models.StationGroupMember{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.StationGroup{}).Error; err != nil {
		return err
	}

	// Station users and play history
	if err := tx.Where("station_id = ?", id).Delete(&models.StationUser{}).Error; err != nil {
		return err
	}
	if err := tx.Where("station_id = ?", id).Delete(&models.PlayHistory{}).Error; err != nil {
		return err
	}

	// Finally delete the station itself
	return tx.Delete(station).Error
}

func (h *Handler) renderStationFormError(w http.ResponseWriter, r *http.Request, station models.Station, isNew bool, message string) {
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<div class="alert alert-danger">` + message + `</div>`))
		return
	}

	h.Render(w, r, "pages/dashboard/stations/form", PageData{
		Title:    "Station",
		Stations: h.LoadStations(r),
		Flash:    &FlashMessage{Type: "error", Message: message},
		Data: map[string]any{
			"Station": station,
			"IsNew":   isNew,
		},
	})
}

// Mount handlers

// MountsList renders the mounts for a station
func (h *Handler) MountsList(w http.ResponseWriter, r *http.Request) {
	stationID := chi.URLParam(r, "stationID")

	var station models.Station
	if err := h.db.First(&station, "id = ?", stationID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	var mounts []models.Mount
	h.db.Where("station_id = ?", stationID).Order("name ASC").Find(&mounts)

	h.Render(w, r, "pages/dashboard/stations/mounts", PageData{
		Title:    "Mounts - " + station.Name,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Station": station,
			"Mounts":  mounts,
		},
	})
}

// MountNew renders the new mount form
func (h *Handler) MountNew(w http.ResponseWriter, r *http.Request) {
	stationID := chi.URLParam(r, "stationID")

	var station models.Station
	if err := h.db.First(&station, "id = ?", stationID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	h.Render(w, r, "pages/dashboard/stations/mount-form", PageData{
		Title:    "New Mount",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Station": station,
			"Mount":   models.Mount{StationID: stationID},
			"IsNew":   true,
		},
	})
}

// MountCreate handles new mount creation
func (h *Handler) MountCreate(w http.ResponseWriter, r *http.Request) {
	stationID := chi.URLParam(r, "stationID")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	mountName := strings.TrimSpace(r.FormValue("name"))
	mountURL := strings.TrimSpace(r.FormValue("url"))
	if mountURL == "" {
		mountURL = defaultMountURL(r, models.GenerateMountName(mountName))
	}

	mount := models.Mount{
		ID:         uuid.New().String(),
		StationID:  stationID,
		Name:       mountName,
		URL:        mountURL,
		Format:     r.FormValue("format"),
		Bitrate:    parseIntOrDefault(r.FormValue("bitrate"), 128),
		Channels:   parseIntOrDefault(r.FormValue("channels"), 2),
		SampleRate: parseIntOrDefault(r.FormValue("sample_rate"), 44100),
	}

	if err := h.db.Create(&mount).Error; err != nil {
		h.logger.Error().Err(err).Str("station_id", stationID).Str("name", mount.Name).Msg("failed to create mount")
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`<div class="alert alert-danger">Failed to create mount: ` + err.Error() + `</div>`))
			return
		}
		http.Error(w, "Failed to create mount: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Publish cache invalidation event
	h.publishMountCacheEvent(events.EventMountCreated, mount.ID, stationID)

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/stations/"+stationID+"/mounts")
		return
	}

	http.Redirect(w, r, "/dashboard/stations/"+stationID+"/mounts", http.StatusSeeOther)
}

// MountEdit renders the mount edit form
func (h *Handler) MountEdit(w http.ResponseWriter, r *http.Request) {
	stationID := chi.URLParam(r, "stationID")
	id := chi.URLParam(r, "id")

	var station models.Station
	if err := h.db.First(&station, "id = ?", stationID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	var mount models.Mount
	if err := h.db.First(&mount, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	h.Render(w, r, "pages/dashboard/stations/mount-form", PageData{
		Title:    "Edit Mount",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Station": station,
			"Mount":   mount,
			"IsNew":   false,
		},
	})
}

// MountUpdate handles mount updates
func (h *Handler) MountUpdate(w http.ResponseWriter, r *http.Request) {
	stationID := chi.URLParam(r, "stationID")
	id := chi.URLParam(r, "id")

	var mount models.Mount
	if err := h.db.First(&mount, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	mount.Name = strings.TrimSpace(r.FormValue("name"))
	mount.URL = strings.TrimSpace(r.FormValue("url"))
	if mount.URL == "" {
		mount.URL = defaultMountURL(r, models.GenerateMountName(mount.Name))
	}
	mount.Format = r.FormValue("format")
	mount.Bitrate = parseIntOrDefault(r.FormValue("bitrate"), mount.Bitrate)
	mount.Channels = parseIntOrDefault(r.FormValue("channels"), mount.Channels)
	mount.SampleRate = parseIntOrDefault(r.FormValue("sample_rate"), mount.SampleRate)

	if err := h.db.Save(&mount).Error; err != nil {
		http.Error(w, "Failed to update mount", http.StatusInternalServerError)
		return
	}

	// Publish cache invalidation event
	h.publishMountCacheEvent(events.EventMountUpdated, mount.ID, stationID)

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/stations/"+stationID+"/mounts")
		return
	}

	http.Redirect(w, r, "/dashboard/stations/"+stationID+"/mounts", http.StatusSeeOther)
}

// MountDelete handles mount deletion
func (h *Handler) MountDelete(w http.ResponseWriter, r *http.Request) {
	stationID := chi.URLParam(r, "stationID")
	id := chi.URLParam(r, "id")

	if err := h.db.Transaction(func(tx *gorm.DB) error {
		// Cascade: remove future schedule entries that target this mount so they
		// don't silently pile up as orphans after the mount is gone.
		tx.Exec(`DELETE FROM schedule_entries WHERE mount_id = ? AND starts_at > NOW()`, id)
		return tx.Delete(&models.Mount{}, "id = ?", id).Error
	}); err != nil {
		http.Error(w, "Failed to delete mount", http.StatusInternalServerError)
		return
	}

	// Publish cache invalidation event
	h.publishMountCacheEvent(events.EventMountDeleted, id, stationID)

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		return
	}

	http.Redirect(w, r, "/dashboard/stations/"+stationID+"/mounts", http.StatusSeeOther)
}

// publishCacheEvent publishes a cache invalidation event for a station.
func (h *Handler) publishCacheEvent(eventType events.EventType, stationID string) {
	if h.eventBus == nil {
		return
	}
	h.eventBus.Publish(eventType, events.Payload{
		"station_id": stationID,
	})
}

// publishMountCacheEvent publishes a cache invalidation event for a mount.
func (h *Handler) publishMountCacheEvent(eventType events.EventType, mountID, stationID string) {
	if h.eventBus == nil {
		return
	}
	h.eventBus.Publish(eventType, events.Payload{
		"mount_id":   mountID,
		"station_id": stationID,
	})
}

func parseIntOrDefault(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}
