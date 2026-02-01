/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

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

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
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
	}

	// Platform admins can set active status
	if user.IsPlatformAdmin() {
		station.Active = r.FormValue("active") == "on"
	}

	if station.Name == "" {
		h.renderStationFormError(w, r, station, true, "Name is required")
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

	tx.Commit()

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

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	station.Name = r.FormValue("name")
	station.Description = r.FormValue("description")
	station.Timezone = r.FormValue("timezone")
	station.Active = r.FormValue("active") == "on"

	if station.Name == "" {
		h.renderStationFormError(w, r, station, false, "Name is required")
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
		// Delete schedule entries
		if err := tx.Where("station_id = ?", id).Delete(&models.ScheduleEntry{}).Error; err != nil {
			return err
		}

		// Delete clock slots (via clock hours)
		var clockHourIDs []string
		tx.Model(&models.ClockHour{}).Where("station_id = ?", id).Pluck("id", &clockHourIDs)
		if len(clockHourIDs) > 0 {
			if err := tx.Where("clock_hour_id IN ?", clockHourIDs).Delete(&models.ClockSlot{}).Error; err != nil {
				return err
			}
		}

		// Delete clock hours
		if err := tx.Where("station_id = ?", id).Delete(&models.ClockHour{}).Error; err != nil {
			return err
		}

		// Delete clocks
		if err := tx.Where("station_id = ?", id).Delete(&models.Clock{}).Error; err != nil {
			return err
		}

		// Delete playlist items (via playlists)
		var playlistIDs []string
		tx.Model(&models.Playlist{}).Where("station_id = ?", id).Pluck("id", &playlistIDs)
		if len(playlistIDs) > 0 {
			if err := tx.Where("playlist_id IN ?", playlistIDs).Delete(&models.PlaylistItem{}).Error; err != nil {
				return err
			}
		}

		// Delete playlists
		if err := tx.Where("station_id = ?", id).Delete(&models.Playlist{}).Error; err != nil {
			return err
		}

		// Delete smart blocks
		if err := tx.Where("station_id = ?", id).Delete(&models.SmartBlock{}).Error; err != nil {
			return err
		}

		// Delete media items
		if err := tx.Where("station_id = ?", id).Delete(&models.MediaItem{}).Error; err != nil {
			return err
		}

		// Delete webstreams
		if err := tx.Where("station_id = ?", id).Delete(&models.Webstream{}).Error; err != nil {
			return err
		}

		// Delete mounts
		if err := tx.Where("station_id = ?", id).Delete(&models.Mount{}).Error; err != nil {
			return err
		}

		// Delete station users
		if err := tx.Where("station_id = ?", id).Delete(&models.StationUser{}).Error; err != nil {
			return err
		}

		// Delete play history
		if err := tx.Where("station_id = ?", id).Delete(&models.PlayHistory{}).Error; err != nil {
			return err
		}

		// Finally delete the station
		if err := tx.Delete(&station).Error; err != nil {
			return err
		}

		return nil
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

	mount := models.Mount{
		ID:        uuid.New().String(),
		StationID: stationID,
		Name:      r.FormValue("name"),
		URL:       r.FormValue("url"),
		Format:    r.FormValue("format"),
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

	mount.Name = r.FormValue("name")
	mount.URL = r.FormValue("url")
	mount.Format = r.FormValue("format")

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

	if err := h.db.Delete(&models.Mount{}, "id = ?", id).Error; err != nil {
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
