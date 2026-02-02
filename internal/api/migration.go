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
	"github.com/friendsincode/grimnir_radio/internal/models"
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

			// Import tracking and redo
			r.Get("/items", h.handleGetImportedItems)
			r.Post("/rollback", h.handleRollbackImport)
			r.Post("/redo", h.handleCloneForRedo)
		})

		// Staged import endpoints
		r.Route("/staged", func(r chi.Router) {
			r.Get("/{stagedID}", h.handleGetStagedImport)
			r.Put("/{stagedID}/selections", h.handleUpdateSelections)
			r.Post("/{stagedID}/commit", h.handleCommitStagedImport)
			r.Delete("/{stagedID}", h.handleRejectStagedImport)
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

// =============================================================================
// STAGED IMPORT HANDLERS
// =============================================================================

// GetStagedImportResponse represents the response for getting a staged import.
type GetStagedImportResponse struct {
	StagedImport *models.StagedImport `json:"staged_import"`
}

// handleGetStagedImport retrieves a staged import by ID.
func (h *MigrationHandler) handleGetStagedImport(w http.ResponseWriter, r *http.Request) {
	stagedID := chi.URLParam(r, "stagedID")

	staged, err := h.service.GetStagedImport(r.Context(), stagedID)
	if err != nil {
		writeError(w, http.StatusNotFound, "staged_import_not_found")
		return
	}

	writeJSON(w, http.StatusOK, GetStagedImportResponse{StagedImport: staged})
}

// UpdateSelectionsRequest represents a request to update staged import selections.
type UpdateSelectionsRequest struct {
	Selections models.ImportSelections `json:"selections"`
}

// UpdateSelectionsResponse represents the response for updating selections.
type UpdateSelectionsResponse struct {
	Message string `json:"message"`
}

// handleUpdateSelections updates user selections on a staged import.
func (h *MigrationHandler) handleUpdateSelections(w http.ResponseWriter, r *http.Request) {
	stagedID := chi.URLParam(r, "stagedID")

	var req UpdateSelectionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := h.service.UpdateSelections(r.Context(), stagedID, req.Selections); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, UpdateSelectionsResponse{
		Message: "selections updated",
	})
}

// CommitStagedImportResponse represents the response for committing a staged import.
type CommitStagedImportResponse struct {
	Message string            `json:"message"`
	Result  *migration.Result `json:"result,omitempty"`
}

// handleCommitStagedImport commits a staged import.
func (h *MigrationHandler) handleCommitStagedImport(w http.ResponseWriter, r *http.Request) {
	stagedID := chi.URLParam(r, "stagedID")

	// Get staged import
	staged, err := h.service.GetStagedImport(r.Context(), stagedID)
	if err != nil {
		writeError(w, http.StatusNotFound, "staged_import_not_found")
		return
	}

	// Get the job
	job, err := h.service.GetJob(r.Context(), staged.JobID)
	if err != nil {
		writeError(w, http.StatusNotFound, "job_not_found")
		return
	}

	// Get the importer and commit
	// Note: This is a simplified version - in production, you'd want to
	// run this in a background goroutine with progress tracking
	switch job.SourceType {
	case migration.SourceTypeLibreTime:
		// The commit will be handled by starting the job
		if err := h.service.StartJob(r.Context(), job.ID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "unsupported source type for staged import")
		return
	}

	writeJSON(w, http.StatusOK, CommitStagedImportResponse{
		Message: "staged import commit started",
	})
}

// RejectStagedImportResponse represents the response for rejecting a staged import.
type RejectStagedImportResponse struct {
	Message string `json:"message"`
}

// handleRejectStagedImport rejects and removes a staged import.
func (h *MigrationHandler) handleRejectStagedImport(w http.ResponseWriter, r *http.Request) {
	stagedID := chi.URLParam(r, "stagedID")

	if err := h.service.RejectStagedImport(r.Context(), stagedID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, RejectStagedImportResponse{
		Message: "staged import rejected",
	})
}

// =============================================================================
// IMPORT TRACKING HANDLERS
// =============================================================================

// GetImportedItemsResponse represents the response for getting imported items.
type GetImportedItemsResponse struct {
	Items *migration.ImportedItems `json:"items"`
}

// handleGetImportedItems retrieves items created by a specific import job.
func (h *MigrationHandler) handleGetImportedItems(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	items, err := h.service.GetImportedItems(r.Context(), jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, GetImportedItemsResponse{Items: items})
}

// RollbackImportResponse represents the response for rolling back an import.
type RollbackImportResponse struct {
	Message string `json:"message"`
}

// handleRollbackImport deletes all items created by a specific import.
func (h *MigrationHandler) handleRollbackImport(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	if err := h.service.RollbackImport(r.Context(), jobID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, RollbackImportResponse{
		Message: "import rolled back successfully",
	})
}

// CloneForRedoResponse represents the response for cloning a job for redo.
type CloneForRedoResponse struct {
	Job *migration.Job `json:"job"`
}

// handleCloneForRedo creates a new job with the same options for re-running.
func (h *MigrationHandler) handleCloneForRedo(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	newJob, err := h.service.CloneJobForRedo(r.Context(), jobID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, CloneForRedoResponse{Job: newJob})
}
