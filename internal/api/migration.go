/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package api

import (
	"encoding/json"
	"net/http"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/media"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// MigrationHandler handles migration-related HTTP endpoints.
type MigrationHandler struct {
	service *migration.Service
	logger  zerolog.Logger
}

// NewMigrationHandler creates a new migration handler.
func NewMigrationHandler(db *gorm.DB, mediaService *media.Service, bus *events.Bus, logger zerolog.Logger) *MigrationHandler {
	service := migration.NewService(db, bus, logger)

	// Register importers
	service.RegisterImporter(migration.SourceTypeAzuraCast, migration.NewAzuraCastImporter(db, mediaService, logger))
	service.RegisterImporter(migration.SourceTypeLibreTime, migration.NewLibreTimeImporter(db, mediaService, logger))

	return &MigrationHandler{
		service: service,
		logger:  logger.With().Str("handler", "migration").Logger(),
	}
}

// RegisterRoutes registers migration routes on the provided router.
func (h *MigrationHandler) RegisterRoutes(r chi.Router) {
	r.Route("/migrations", func(r chi.Router) {
		r.Post("/", h.handleCreateMigrationJob)
		r.Get("/", h.handleListMigrationJobs)

		r.Route("/{id}", func(r chi.Router) {
			r.Post("/start", h.handleStartMigrationJob)
			r.Get("/", h.handleGetMigrationJob)
			r.Post("/cancel", h.handleCancelMigrationJob)
			r.Delete("/", h.handleDeleteMigrationJob)
		})
	})
}

// CreateMigrationJobRequest represents a request to create a migration job.
type CreateMigrationJobRequest struct {
	SourceType migration.SourceType `json:"source_type"`
	Options    migration.Options    `json:"options"`
}

// CreateMigrationJobResponse represents the response for creating a migration job.
type CreateMigrationJobResponse struct {
	Job *migration.Job `json:"job"`
}

// handleCreateMigrationJob creates a new migration job.
func (h *MigrationHandler) handleCreateMigrationJob(w http.ResponseWriter, r *http.Request) {
	var req CreateMigrationJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	job, err := h.service.CreateJob(r.Context(), req.SourceType, req.Options)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, CreateMigrationJobResponse{Job: job})
}

// StartMigrationJobRequest represents a request to start a migration job.
type StartMigrationJobRequest struct{}

// StartMigrationJobResponse represents the response for starting a migration job.
type StartMigrationJobResponse struct {
	Message string `json:"message"`
}

// handleStartMigrationJob starts a migration job.
func (h *MigrationHandler) handleStartMigrationJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	if err := h.service.StartJob(r.Context(), jobID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, StartMigrationJobResponse{
		Message: "migration job started",
	})
}

// GetMigrationJobResponse represents the response for getting a migration job.
type GetMigrationJobResponse struct {
	Job *migration.Job `json:"job"`
}

// handleGetMigrationJob retrieves a migration job.
func (h *MigrationHandler) handleGetMigrationJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	job, err := h.service.GetJob(r.Context(), jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, "job_not_found")
		return
	}

	writeJSON(w, http.StatusOK, GetMigrationJobResponse{Job: job})
}

// ListMigrationJobsResponse represents the response for listing migration jobs.
type ListMigrationJobsResponse struct {
	Jobs []*migration.Job `json:"jobs"`
}

// handleListMigrationJobs lists all migration jobs.
func (h *MigrationHandler) handleListMigrationJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.service.ListJobs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed_to_list_jobs")
		return
	}

	writeJSON(w, http.StatusOK, ListMigrationJobsResponse{Jobs: jobs})
}

// CancelMigrationJobResponse represents the response for cancelling a migration job.
type CancelMigrationJobResponse struct {
	Message string `json:"message"`
}

// handleCancelMigrationJob cancels a running migration job.
func (h *MigrationHandler) handleCancelMigrationJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	if err := h.service.CancelJob(r.Context(), jobID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, CancelMigrationJobResponse{
		Message: "migration job cancelled",
	})
}

// DeleteMigrationJobResponse represents the response for deleting a migration job.
type DeleteMigrationJobResponse struct {
	Message string `json:"message"`
}

// handleDeleteMigrationJob deletes a migration job.
func (h *MigrationHandler) handleDeleteMigrationJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	if err := h.service.DeleteJob(r.Context(), jobID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, DeleteMigrationJobResponse{
		Message: "migration job deleted",
	})
}
