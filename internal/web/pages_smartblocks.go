/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// SmartBlockList renders the smart blocks page
func (h *Handler) SmartBlockList(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	var blocks []models.SmartBlock
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&blocks)

	h.Render(w, r, "pages/dashboard/smartblocks/list", PageData{
		Title:    "Smart Blocks",
		Stations: h.LoadStations(r),
		Data:     blocks,
	})
}

// SmartBlockNew renders the new smart block form
func (h *Handler) SmartBlockNew(w http.ResponseWriter, r *http.Request) {
	// Get genres and other metadata for rule builder
	station := h.GetStation(r)

	var genres []string
	h.db.Model(&models.MediaItem{}).
		Where("station_id = ? AND genre != ''", station.ID).
		Distinct().Pluck("genre", &genres)

	var artists []string
	h.db.Model(&models.MediaItem{}).
		Where("station_id = ? AND artist != ''", station.ID).
		Distinct().Pluck("artist", &artists)

	h.Render(w, r, "pages/dashboard/smartblocks/form", PageData{
		Title:    "New Smart Block",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Block":   models.SmartBlock{},
			"IsNew":   true,
			"Genres":  genres,
			"Artists": artists,
		},
	})
}

// SmartBlockCreate handles smart block creation
func (h *Handler) SmartBlockCreate(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	// Parse rules from JSON
	var rules map[string]any
	if rulesJSON := r.FormValue("rules"); rulesJSON != "" {
		if err := json.Unmarshal([]byte(rulesJSON), &rules); err != nil {
			http.Error(w, "Invalid rules format", http.StatusBadRequest)
			return
		}
	}

	// Parse sequence from JSON
	var sequence map[string]any
	if seqJSON := r.FormValue("sequence"); seqJSON != "" {
		if err := json.Unmarshal([]byte(seqJSON), &sequence); err != nil {
			http.Error(w, "Invalid sequence format", http.StatusBadRequest)
			return
		}
	}

	block := models.SmartBlock{
		ID:          uuid.New().String(),
		StationID:   station.ID,
		Name:        r.FormValue("name"),
		Description: r.FormValue("description"),
		Rules:       rules,
		Sequence:    sequence,
	}

	if block.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	if err := h.db.Create(&block).Error; err != nil {
		http.Error(w, "Failed to create smart block", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/smart-blocks/"+block.ID)
		return
	}

	http.Redirect(w, r, "/dashboard/smart-blocks/"+block.ID, http.StatusSeeOther)
}

// SmartBlockDetail renders the smart block detail page
func (h *Handler) SmartBlockDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var block models.SmartBlock
	if err := h.db.First(&block, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	h.Render(w, r, "pages/dashboard/smartblocks/detail", PageData{
		Title:    block.Name,
		Stations: h.LoadStations(r),
		Data:     block,
	})
}

// SmartBlockEdit renders the smart block edit form
func (h *Handler) SmartBlockEdit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var block models.SmartBlock
	if err := h.db.First(&block, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	station := h.GetStation(r)

	var genres []string
	h.db.Model(&models.MediaItem{}).
		Where("station_id = ? AND genre != ''", station.ID).
		Distinct().Pluck("genre", &genres)

	var artists []string
	h.db.Model(&models.MediaItem{}).
		Where("station_id = ? AND artist != ''", station.ID).
		Distinct().Pluck("artist", &artists)

	h.Render(w, r, "pages/dashboard/smartblocks/form", PageData{
		Title:    "Edit: " + block.Name,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Block":   block,
			"IsNew":   false,
			"Genres":  genres,
			"Artists": artists,
		},
	})
}

// SmartBlockUpdate handles smart block updates
func (h *Handler) SmartBlockUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var block models.SmartBlock
	if err := h.db.First(&block, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	block.Name = r.FormValue("name")
	block.Description = r.FormValue("description")

	// Parse rules from JSON
	if rulesJSON := r.FormValue("rules"); rulesJSON != "" {
		var rules map[string]any
		if err := json.Unmarshal([]byte(rulesJSON), &rules); err != nil {
			http.Error(w, "Invalid rules format", http.StatusBadRequest)
			return
		}
		block.Rules = rules
	}

	// Parse sequence from JSON
	if seqJSON := r.FormValue("sequence"); seqJSON != "" {
		var sequence map[string]any
		if err := json.Unmarshal([]byte(seqJSON), &sequence); err != nil {
			http.Error(w, "Invalid sequence format", http.StatusBadRequest)
			return
		}
		block.Sequence = sequence
	}

	if err := h.db.Save(&block).Error; err != nil {
		http.Error(w, "Failed to update smart block", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/smart-blocks/"+id)
		return
	}

	http.Redirect(w, r, "/dashboard/smart-blocks/"+id, http.StatusSeeOther)
}

// SmartBlockDelete handles smart block deletion
func (h *Handler) SmartBlockDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.db.Delete(&models.SmartBlock{}, "id = ?", id).Error; err != nil {
		http.Error(w, "Failed to delete smart block", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/smart-blocks")
		return
	}

	http.Redirect(w, r, "/dashboard/smart-blocks", http.StatusSeeOther)
}

// SmartBlockPreview generates a preview of the smart block
func (h *Handler) SmartBlockPreview(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var block models.SmartBlock
	if err := h.db.First(&block, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// TODO: Call smartblock engine to materialize preview
	// For now, return a simple preview based on rules

	station := h.GetStation(r)

	var media []models.MediaItem
	query := h.db.Where("station_id = ?", station.ID)

	// Apply basic rule filters (simplified)
	if block.Rules != nil {
		if genre, ok := block.Rules["genre"].(string); ok && genre != "" {
			query = query.Where("genre = ?", genre)
		}
		if artist, ok := block.Rules["artist"].(string); ok && artist != "" {
			query = query.Where("artist = ?", artist)
		}
	}

	query.Order("RANDOM()").Limit(10).Find(&media)

	h.RenderPartial(w, r, "partials/smartblock-preview", media)
}
