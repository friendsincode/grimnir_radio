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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	station := models.Station{
		ID:          uuid.New().String(),
		Name:        r.FormValue("name"),
		Description: r.FormValue("description"),
		Timezone:    r.FormValue("timezone"),
		Active:      r.FormValue("active") == "on",
	}

	if station.Name == "" {
		h.renderStationFormError(w, r, station, true, "Name is required")
		return
	}

	if station.Timezone == "" {
		station.Timezone = "UTC"
	}

	if err := h.db.Create(&station).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to create station")
		h.renderStationFormError(w, r, station, true, "Failed to create station")
		return
	}

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

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/stations")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/dashboard/stations", http.StatusSeeOther)
}

// StationDelete handles station deletion
func (h *Handler) StationDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.db.Delete(&models.Station{}, "id = ?", id).Error; err != nil {
		http.Error(w, "Failed to delete station", http.StatusInternalServerError)
		return
	}

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
		h.logger.Error().Err(err).Msg("failed to create mount")
		http.Error(w, "Failed to create mount", http.StatusInternalServerError)
		return
	}

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

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		return
	}

	http.Redirect(w, r, "/dashboard/stations/"+stationID+"/mounts", http.StatusSeeOther)
}
