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

// ClockList renders the clock templates page
func (h *Handler) ClockList(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	var clocks []models.ClockHour
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&clocks)

	h.Render(w, r, "pages/dashboard/clocks/list", PageData{
		Title:    "Clock Templates",
		Stations: h.LoadStations(r),
		Data:     clocks,
	})
}

// ClockNew renders the new clock form
func (h *Handler) ClockNew(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)

	// Get smart blocks for slot selection
	var smartBlocks []models.SmartBlock
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&smartBlocks)

	// Get webstreams
	var webstreams []models.Webstream
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&webstreams)

	h.Render(w, r, "pages/dashboard/clocks/form", PageData{
		Title:    "New Clock Template",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Clock":       models.ClockHour{},
			"IsNew":       true,
			"SmartBlocks": smartBlocks,
			"Webstreams":  webstreams,
		},
	})
}

// ClockCreate handles clock template creation
func (h *Handler) ClockCreate(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	clock := models.ClockHour{
		ID:        uuid.New().String(),
		StationID: station.ID,
		Name:      r.FormValue("name"),
	}

	// Parse slots from JSON
	if slotsJSON := r.FormValue("slots"); slotsJSON != "" {
		var slots []models.ClockSlot
		if err := json.Unmarshal([]byte(slotsJSON), &slots); err != nil {
			http.Error(w, "Invalid slots format", http.StatusBadRequest)
			return
		}
		// Assign IDs and clock ID
		for i := range slots {
			slots[i].ID = uuid.New().String()
			slots[i].ClockHourID = clock.ID
		}
		clock.Slots = slots
	}

	if clock.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	if err := h.db.Create(&clock).Error; err != nil {
		http.Error(w, "Failed to create clock", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/clocks/"+clock.ID)
		return
	}

	http.Redirect(w, r, "/dashboard/clocks/"+clock.ID, http.StatusSeeOther)
}

// ClockDetail renders the clock detail page
func (h *Handler) ClockDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var clock models.ClockHour
	if err := h.db.Preload("Slots").First(&clock, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	h.Render(w, r, "pages/dashboard/clocks/detail", PageData{
		Title:    clock.Name,
		Stations: h.LoadStations(r),
		Data:     clock,
	})
}

// ClockEdit renders the clock edit form
func (h *Handler) ClockEdit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var clock models.ClockHour
	if err := h.db.Preload("Slots").First(&clock, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	station := h.GetStation(r)

	var smartBlocks []models.SmartBlock
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&smartBlocks)

	var webstreams []models.Webstream
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&webstreams)

	h.Render(w, r, "pages/dashboard/clocks/form", PageData{
		Title:    "Edit: " + clock.Name,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Clock":       clock,
			"IsNew":       false,
			"SmartBlocks": smartBlocks,
			"Webstreams":  webstreams,
		},
	})
}

// ClockUpdate handles clock template updates
func (h *Handler) ClockUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var clock models.ClockHour
	if err := h.db.First(&clock, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	clock.Name = r.FormValue("name")

	// Delete existing slots
	h.db.Delete(&models.ClockSlot{}, "clock_hour_id = ?", id)

	// Parse new slots
	if slotsJSON := r.FormValue("slots"); slotsJSON != "" {
		var slots []models.ClockSlot
		if err := json.Unmarshal([]byte(slotsJSON), &slots); err != nil {
			http.Error(w, "Invalid slots format", http.StatusBadRequest)
			return
		}
		for i := range slots {
			slots[i].ID = uuid.New().String()
			slots[i].ClockHourID = clock.ID
		}
		clock.Slots = slots
	}

	if err := h.db.Save(&clock).Error; err != nil {
		http.Error(w, "Failed to update clock", http.StatusInternalServerError)
		return
	}

	// Save slots
	for _, slot := range clock.Slots {
		h.db.Create(&slot)
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/clocks/"+id)
		return
	}

	http.Redirect(w, r, "/dashboard/clocks/"+id, http.StatusSeeOther)
}

// ClockDelete handles clock template deletion
func (h *Handler) ClockDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Delete slots first
	h.db.Delete(&models.ClockSlot{}, "clock_hour_id = ?", id)

	if err := h.db.Delete(&models.ClockHour{}, "id = ?", id).Error; err != nil {
		http.Error(w, "Failed to delete clock", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/clocks")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/dashboard/clocks", http.StatusSeeOther)
}

// SimulatedSlot represents a slot in the clock simulation with materialized content
type SimulatedSlot struct {
	Position   int
	Type       string
	TypeBadge  string // CSS class for badge
	Name       string
	Tracks     []models.MediaItem
	DurationMs int64
}

// ClockSimulate runs a simulation of the clock template with real tracks
func (h *Handler) ClockSimulate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var clock models.ClockHour
	if err := h.db.Preload("Slots").First(&clock, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	var simulation []SimulatedSlot
	var totalDurationMs int64

	for _, slot := range clock.Slots {
		sim := SimulatedSlot{
			Position: slot.Position,
			Type:     string(slot.Type),
		}

		switch slot.Type {
		case models.SlotTypeSmartBlock:
			sim.TypeBadge = "bg-primary"
			if blockID, ok := slot.Payload["smart_block_id"].(string); ok {
				var block models.SmartBlock
				if h.db.First(&block, "id = ?", blockID).Error == nil {
					sim.Name = block.Name

					// Materialize the smart block using preview logic
					tracks, durationMs := h.materializeSmartBlock(station.ID, block)
					sim.Tracks = tracks
					sim.DurationMs = durationMs
					totalDurationMs += durationMs
				}
			}

		case models.SlotTypeWebstream:
			sim.TypeBadge = "bg-info"
			if wsID, ok := slot.Payload["webstream_id"].(string); ok {
				var ws models.Webstream
				if h.db.First(&ws, "id = ?", wsID).Error == nil {
					sim.Name = ws.Name
				}
			}
			// Webstreams have variable duration, use slot duration if specified
			if durMs, ok := slot.Payload["duration_ms"].(float64); ok {
				sim.DurationMs = int64(durMs)
				totalDurationMs += sim.DurationMs
			}

		case models.SlotTypeHardItem:
			sim.TypeBadge = "bg-warning text-dark"
			sim.Name = "Hard-coded item"
			if itemID, ok := slot.Payload["media_id"].(string); ok {
				var item models.MediaItem
				if h.db.First(&item, "id = ?", itemID).Error == nil {
					sim.Name = item.Title
					sim.Tracks = []models.MediaItem{item}
					sim.DurationMs = item.Duration.Milliseconds()
					totalDurationMs += sim.DurationMs
				}
			}

		case models.SlotTypeStopset:
			sim.TypeBadge = "bg-danger"
			sim.Name = "Commercial break"
			if durMs, ok := slot.Payload["duration_ms"].(float64); ok {
				sim.DurationMs = int64(durMs)
				totalDurationMs += sim.DurationMs
			}
		}

		simulation = append(simulation, sim)
	}

	h.RenderPartial(w, r, "partials/clock-simulation", map[string]any{
		"Slots":           simulation,
		"TotalDurationMs": totalDurationMs,
		"TargetMs":        int64(60 * 60 * 1000), // 1 hour target
	})
}

// materializeSmartBlock generates actual tracks for a smart block
func (h *Handler) materializeSmartBlock(stationID string, block models.SmartBlock) ([]models.MediaItem, int64) {
	// Extract config from block rules
	cfg := h.extractPreviewConfig(block.Rules, block.Sequence)

	// Fetch music tracks
	musicTracks := h.fetchMusicTracks(stationID, block.Rules)
	if len(musicTracks) == 0 {
		return nil, 0
	}

	// Fetch ad tracks if enabled
	var adTracks []models.MediaItem
	if cfg.adsEnabled {
		adTracks = h.fetchAdTracks(stationID, cfg)
	}

	// Fetch fallback tracks
	var fallbackTracks []models.MediaItem
	if len(cfg.fallbacks) > 0 {
		fallbackTracks = h.fetchFallbackTracks(stationID, cfg.fallbacks)
	}

	// Build the sequence (with looping enabled by default for clock simulation)
	preview := h.buildPreviewSequence(musicTracks, adTracks, fallbackTracks, cfg, true)

	// Convert to media items and calculate duration
	var tracks []models.MediaItem
	var totalMs int64
	for _, item := range preview {
		tracks = append(tracks, item.Media)
		totalMs += item.Media.Duration.Milliseconds()
	}

	return tracks, totalMs
}
