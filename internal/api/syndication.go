/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/syndication"
)

// SyndicationAPI handles syndication endpoints.
type SyndicationAPI struct {
	*API
	syndicationSvc *syndication.Service
}

// NewSyndicationAPI creates a new syndication API handler.
func NewSyndicationAPI(api *API, svc *syndication.Service) *SyndicationAPI {
	return &SyndicationAPI{
		API:            api,
		syndicationSvc: svc,
	}
}

// RegisterRoutes registers syndication routes.
func (s *SyndicationAPI) RegisterRoutes(r chi.Router) {
	// Networks
	r.Route("/networks", func(r chi.Router) {
		r.Get("/", s.handleListNetworks)
		r.Post("/", s.handleCreateNetwork)
		r.Get("/{id}", s.handleGetNetwork)
		r.Put("/{id}", s.handleUpdateNetwork)
		r.Delete("/{id}", s.handleDeleteNetwork)
	})

	// Network Shows
	r.Route("/network-shows", func(r chi.Router) {
		r.Get("/", s.handleListNetworkShows)
		r.Post("/", s.handleCreateNetworkShow)
		r.Get("/{id}", s.handleGetNetworkShow)
		r.Put("/{id}", s.handleUpdateNetworkShow)
		r.Delete("/{id}", s.handleDeleteNetworkShow)
	})

	// Subscriptions
	r.Route("/subscriptions", func(r chi.Router) {
		r.Get("/", s.handleListSubscriptions)
		r.Post("/", s.handleCreateSubscription)
		r.Delete("/{id}", s.handleDeleteSubscription)
		r.Post("/materialize", s.handleMaterialize)
	})
}

// handleListNetworks lists all networks.
func (s *SyndicationAPI) handleListNetworks(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())

	ownerID := ""
	if claims != nil && !isPlatformAdmin(claims) {
		ownerID = claims.UserID
	}

	networks, err := s.syndicationSvc.ListNetworks(r.Context(), ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list networks")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"networks": networks,
	})
}

// handleCreateNetwork creates a new network.
func (s *SyndicationAPI) handleCreateNetwork(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}

	network, err := s.syndicationSvc.CreateNetwork(r.Context(), req.Name, req.Description, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create network")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"network": network,
	})
}

// handleGetNetwork retrieves a network by ID.
func (s *SyndicationAPI) handleGetNetwork(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	network, err := s.syndicationSvc.GetNetwork(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"network": network,
	})
}

// handleUpdateNetwork updates a network.
func (s *SyndicationAPI) handleUpdateNetwork(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Active      *bool   `json:"active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updates := make(map[string]any)
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Active != nil {
		updates["active"] = *req.Active
	}

	if err := s.db.Model(&models.Network{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update network")
		return
	}

	network, _ := s.syndicationSvc.GetNetwork(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{
		"network": network,
	})
}

// handleDeleteNetwork deletes a network.
func (s *SyndicationAPI) handleDeleteNetwork(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := s.db.Delete(&models.Network{}, "id = ?", id).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete network")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "network deleted",
	})
}

// handleListNetworkShows lists network shows.
func (s *SyndicationAPI) handleListNetworkShows(w http.ResponseWriter, r *http.Request) {
	networkID := r.URL.Query().Get("network_id")

	shows, err := s.syndicationSvc.ListNetworkShows(r.Context(), networkID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list network shows")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"network_shows": shows,
	})
}

// handleCreateNetworkShow creates a new network show.
func (s *SyndicationAPI) handleCreateNetworkShow(w http.ResponseWriter, r *http.Request) {
	var show models.NetworkShow
	if err := json.NewDecoder(r.Body).Decode(&show); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if show.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}

	if err := s.syndicationSvc.CreateNetworkShow(r.Context(), &show); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create network show")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"network_show": show,
	})
}

// handleGetNetworkShow retrieves a network show by ID.
func (s *SyndicationAPI) handleGetNetworkShow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	show, err := s.syndicationSvc.GetNetworkShow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "network show not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"network_show": show,
	})
}

// handleUpdateNetworkShow updates a network show.
func (s *SyndicationAPI) handleUpdateNetworkShow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.syndicationSvc.UpdateNetworkShow(r.Context(), id, req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update network show")
		return
	}

	show, _ := s.syndicationSvc.GetNetworkShow(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{
		"network_show": show,
	})
}

// handleDeleteNetworkShow deletes a network show.
func (s *SyndicationAPI) handleDeleteNetworkShow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := s.syndicationSvc.DeleteNetworkShow(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete network show")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "network show deleted",
	})
}

// handleListSubscriptions lists subscriptions for a station.
func (s *SyndicationAPI) handleListSubscriptions(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	subs, err := s.syndicationSvc.GetStationSubscriptions(r.Context(), stationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list subscriptions")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"subscriptions": subs,
	})
}

// handleCreateSubscription creates a subscription.
func (s *SyndicationAPI) handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StationID     string `json:"station_id"`
		NetworkShowID string `json:"network_show_id"`
		LocalTime     string `json:"local_time"`
		LocalDays     string `json:"local_days"`
		Timezone      string `json:"timezone"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.StationID == "" || req.NetworkShowID == "" {
		writeError(w, http.StatusBadRequest, "station_id and network_show_id required")
		return
	}

	sub, err := s.syndicationSvc.Subscribe(r.Context(), req.StationID, req.NetworkShowID, req.LocalTime, req.LocalDays, req.Timezone)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"subscription": sub,
	})
}

// handleDeleteSubscription deletes a subscription.
func (s *SyndicationAPI) handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := s.syndicationSvc.Unsubscribe(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete subscription")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "subscription deleted",
	})
}

// handleMaterialize creates show instances from subscriptions.
func (s *SyndicationAPI) handleMaterialize(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StationID string `json:"station_id"`
		Start     string `json:"start"`
		End       string `json:"end"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.StationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	start := time.Now()
	end := start.AddDate(0, 0, 7)

	if req.Start != "" {
		if t, err := time.Parse("2006-01-02", req.Start); err == nil {
			start = t
		}
	}
	if req.End != "" {
		if t, err := time.Parse("2006-01-02", req.End); err == nil {
			end = t
		}
	}

	instances, err := s.syndicationSvc.MaterializeSubscriptions(r.Context(), req.StationID, start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to materialize subscriptions")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"instances_created": len(instances),
		"instances":         instances,
	})
}

func isPlatformAdmin(claims *auth.Claims) bool {
	for _, role := range claims.Roles {
		if role == string(models.PlatformRoleAdmin) {
			return true
		}
	}
	return false
}
