package api

import (
	"encoding/json"
	"net/http"

	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/go-chi/chi/v5"
)

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
func (a *API) handleCreateMigrationJob(w http.ResponseWriter, r *http.Request) {
	var req CreateMigrationJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	job, err := a.migrationSvc.CreateJob(r.Context(), req.SourceType, req.Options)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, CreateMigrationJobResponse{Job: job})
}

// StartMigrationJobRequest represents a request to start a migration job.
type StartMigrationJobRequest struct{}

// StartMigrationJobResponse represents the response for starting a migration job.
type StartMigrationJobResponse struct {
	Message string `json:"message"`
}

// handleStartMigrationJob starts a migration job.
func (a *API) handleStartMigrationJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	if err := a.migrationSvc.StartJob(r.Context(), jobID); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, StartMigrationJobResponse{
		Message: "migration job started",
	})
}

// GetMigrationJobResponse represents the response for getting a migration job.
type GetMigrationJobResponse struct {
	Job *migration.Job `json:"job"`
}

// handleGetMigrationJob retrieves a migration job.
func (a *API) handleGetMigrationJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	job, err := a.migrationSvc.GetJob(r.Context(), jobID)
	if err != nil {
		respondError(w, http.StatusNotFound, "job not found")
		return
	}

	respondJSON(w, http.StatusOK, GetMigrationJobResponse{Job: job})
}

// ListMigrationJobsResponse represents the response for listing migration jobs.
type ListMigrationJobsResponse struct {
	Jobs []*migration.Job `json:"jobs"`
}

// handleListMigrationJobs lists all migration jobs.
func (a *API) handleListMigrationJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := a.migrationSvc.ListJobs(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}

	respondJSON(w, http.StatusOK, ListMigrationJobsResponse{Jobs: jobs})
}

// CancelMigrationJobResponse represents the response for cancelling a migration job.
type CancelMigrationJobResponse struct {
	Message string `json:"message"`
}

// handleCancelMigrationJob cancels a running migration job.
func (a *API) handleCancelMigrationJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	if err := a.migrationSvc.CancelJob(r.Context(), jobID); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, CancelMigrationJobResponse{
		Message: "migration job cancelled",
	})
}

// DeleteMigrationJobResponse represents the response for deleting a migration job.
type DeleteMigrationJobResponse struct {
	Message string `json:"message"`
}

// handleDeleteMigrationJob deletes a migration job.
func (a *API) handleDeleteMigrationJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	if err := a.migrationSvc.DeleteJob(r.Context(), jobID); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, DeleteMigrationJobResponse{
		Message: "migration job deleted",
	})
}
