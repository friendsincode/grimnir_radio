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
		return
	}

	http.Redirect(w, r, "/dashboard/clocks", http.StatusSeeOther)
}

// ClockSimulate runs a simulation of the clock template
func (h *Handler) ClockSimulate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var clock models.ClockHour
	if err := h.db.Preload("Slots").First(&clock, "id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// TODO: Run actual clock simulation via clock planner
	// For now return the slots with placeholder content

	type simulatedSlot struct {
		Position int
		Type     string
		Duration string
		Content  string
	}

	var simulation []simulatedSlot
	for _, slot := range clock.Slots {
		sim := simulatedSlot{
			Position: slot.Position,
			Type:     string(slot.Type),
			Duration: slot.Offset.String(),
		}

		// Get content name based on type
		switch slot.Type {
		case models.SlotTypeSmartBlock:
			if blockID, ok := slot.Payload["smart_block_id"].(string); ok {
				var block models.SmartBlock
				if h.db.First(&block, "id = ?", blockID).Error == nil {
					sim.Content = block.Name
				}
			}
		case models.SlotTypeWebstream:
			if wsID, ok := slot.Payload["webstream_id"].(string); ok {
				var ws models.Webstream
				if h.db.First(&ws, "id = ?", wsID).Error == nil {
					sim.Content = ws.Name
				}
			}
		case models.SlotTypeHardItem:
			sim.Content = "Hard-coded item"
		case models.SlotTypeStopset:
			sim.Content = "Commercial break"
		}

		simulation = append(simulation, sim)
	}

	h.RenderPartial(w, r, "partials/clock-simulation", simulation)
}
