package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/migration/azuracast"
	"github.com/friendsincode/grimnir_radio/internal/migration/libretime"
)

// MigrationHandler handles migration API endpoints
type MigrationHandler struct {
	db      *gorm.DB
	logger  zerolog.Logger
	jobs    map[string]*MigrationJob
	jobsMux sync.RWMutex
}

// MigrationJob tracks a running migration job
type MigrationJob struct {
	ID          string                    `json:"id"`
	Type        migration.MigrationType   `json:"type"`
	Status      migration.MigrationStatus `json:"status"`
	Progress    int                       `json:"progress"` // 0-100
	TotalSteps  int                       `json:"total_steps"`
	CurrentStep int                       `json:"current_step"`
	StepName    string                    `json:"step_name"`
	Error       string                    `json:"error,omitempty"`
	DryRun      bool                      `json:"dry_run"`
	StartedAt   time.Time                 `json:"started_at"`
	CompletedAt *time.Time                `json:"completed_at,omitempty"`
	Stats       migration.MigrationStats  `json:"stats"`

	// Internal
	cancel context.CancelFunc
}

// NewMigrationHandler creates a new migration handler
func NewMigrationHandler(db *gorm.DB, logger zerolog.Logger) *MigrationHandler {
	return &MigrationHandler{
		db:     db,
		logger: logger.With().Str("component", "migration_api").Logger(),
		jobs:   make(map[string]*MigrationJob),
	}
}

// RegisterRoutes registers migration routes
func (h *MigrationHandler) RegisterRoutes(r chi.Router) {
	r.Route("/migrations", func(r chi.Router) {
		r.Post("/azuracast", h.StartAzuraCastImport)
		r.Post("/libretime", h.StartLibreTimeImport)
		r.Get("/", h.ListMigrations)
		r.Get("/{id}", h.GetMigration)
		r.Delete("/{id}", h.CancelMigration)
	})
}

// StartAzuraCastImportRequest represents the request to start an AzuraCast import
type StartAzuraCastImportRequest struct {
	BackupPath      string `json:"backup_path"`
	DryRun          bool   `json:"dry_run"`
	SkipMedia       bool   `json:"skip_media"`
	MediaCopyMethod string `json:"media_copy_method"` // copy, symlink, none
}

// StartLibreTimeImportRequest represents the request to start a LibreTime import
type StartLibreTimeImportRequest struct {
	DatabaseDSN     string `json:"database_dsn"`
	DryRun          bool   `json:"dry_run"`
	SkipMedia       bool   `json:"skip_media"`
	MediaCopyMethod string `json:"media_copy_method"` // copy, symlink, none
}

// StartAzuraCastImport starts an AzuraCast import job
func (h *MigrationHandler) StartAzuraCastImport(w http.ResponseWriter, r *http.Request) {
	var req StartAzuraCastImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.BackupPath == "" {
		http.Error(w, "backup_path is required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.MediaCopyMethod == "" {
		req.MediaCopyMethod = "copy"
	}

	// Create migration job
	job := &MigrationJob{
		ID:         uuid.New().String(),
		Type:       migration.MigrationTypeAzuraCast,
		Status:     migration.MigrationStatusPending,
		DryRun:     req.DryRun,
		StartedAt:  time.Now(),
		TotalSteps: 10,
	}

	h.jobsMux.Lock()
	h.jobs[job.ID] = job
	h.jobsMux.Unlock()

	h.logger.Info().
		Str("job_id", job.ID).
		Str("backup_path", req.BackupPath).
		Bool("dry_run", req.DryRun).
		Msg("starting AzuraCast import job")

	// Start import in background
	ctx, cancel := context.WithCancel(context.Background())
	job.cancel = cancel

	go h.runAzuraCastImport(ctx, job, req)

	// Return job info
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(job)
}

// StartLibreTimeImport starts a LibreTime import job
func (h *MigrationHandler) StartLibreTimeImport(w http.ResponseWriter, r *http.Request) {
	var req StartLibreTimeImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.DatabaseDSN == "" {
		http.Error(w, "database_dsn is required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.MediaCopyMethod == "" {
		req.MediaCopyMethod = "copy"
	}

	// Create migration job
	job := &MigrationJob{
		ID:         uuid.New().String(),
		Type:       migration.MigrationTypeLibreTime,
		Status:     migration.MigrationStatusPending,
		DryRun:     req.DryRun,
		StartedAt:  time.Now(),
		TotalSteps: 10,
	}

	h.jobsMux.Lock()
	h.jobs[job.ID] = job
	h.jobsMux.Unlock()

	h.logger.Info().
		Str("job_id", job.ID).
		Bool("dry_run", req.DryRun).
		Msg("starting LibreTime import job")

	// Start import in background
	ctx, cancel := context.WithCancel(context.Background())
	job.cancel = cancel

	go h.runLibreTimeImport(ctx, job, req)

	// Return job info
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(job)
}

// ListMigrations lists all migration jobs
func (h *MigrationHandler) ListMigrations(w http.ResponseWriter, r *http.Request) {
	h.jobsMux.RLock()
	defer h.jobsMux.RUnlock()

	jobs := make([]*MigrationJob, 0, len(h.jobs))
	for _, job := range h.jobs {
		jobs = append(jobs, job)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"migrations": jobs,
		"count":      len(jobs),
	})
}

// GetMigration gets a specific migration job
func (h *MigrationHandler) GetMigration(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	h.jobsMux.RLock()
	job, ok := h.jobs[id]
	h.jobsMux.RUnlock()

	if !ok {
		http.Error(w, "migration not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

// CancelMigration cancels a running migration job
func (h *MigrationHandler) CancelMigration(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	h.jobsMux.Lock()
	job, ok := h.jobs[id]
	h.jobsMux.Unlock()

	if !ok {
		http.Error(w, "migration not found", http.StatusNotFound)
		return
	}

	if job.Status != migration.MigrationStatusRunning && job.Status != migration.MigrationStatusPending {
		http.Error(w, "migration cannot be cancelled", http.StatusBadRequest)
		return
	}

	// Cancel context
	if job.cancel != nil {
		job.cancel()
	}

	job.Status = migration.MigrationStatusFailed
	job.Error = "cancelled by user"
	now := time.Now()
	job.CompletedAt = &now

	h.logger.Info().Str("job_id", id).Msg("migration cancelled")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

// runAzuraCastImport runs an AzuraCast import in the background
func (h *MigrationHandler) runAzuraCastImport(ctx context.Context, job *MigrationJob, req StartAzuraCastImportRequest) {
	job.Status = migration.MigrationStatusRunning

	options := migration.MigrationOptions{
		DryRun:          req.DryRun,
		SkipMedia:       req.SkipMedia,
		MediaCopyMethod: req.MediaCopyMethod,
	}

	importer := azuracast.NewImporter(h.db, h.logger, options)

	// Set progress callback
	importer.SetProgressCallback(func(step, total int, message string) {
		h.jobsMux.Lock()
		job.CurrentStep = step
		job.TotalSteps = total
		job.StepName = message
		if total > 0 {
			job.Progress = int(float64(step) / float64(total) * 100)
		}
		h.jobsMux.Unlock()
	})

	// Run import
	stats, err := importer.Import(ctx, req.BackupPath)
	now := time.Now()
	job.CompletedAt = &now

	if err != nil {
		job.Status = migration.MigrationStatusFailed
		job.Error = err.Error()
		h.logger.Error().Err(err).Str("job_id", job.ID).Msg("AzuraCast import failed")
	} else {
		job.Status = migration.MigrationStatusCompleted
		job.Progress = 100
		if stats != nil {
			job.Stats = *stats
		}
		h.logger.Info().Str("job_id", job.ID).Msg("AzuraCast import completed")
	}
}

// runLibreTimeImport runs a LibreTime import in the background
func (h *MigrationHandler) runLibreTimeImport(ctx context.Context, job *MigrationJob, req StartLibreTimeImportRequest) {
	job.Status = migration.MigrationStatusRunning

	options := migration.MigrationOptions{
		DryRun:          req.DryRun,
		SkipMedia:       req.SkipMedia,
		MediaCopyMethod: req.MediaCopyMethod,
	}

	importer := libretime.NewImporter(h.db, h.logger, options)

	// Set progress callback
	importer.SetProgressCallback(func(step, total int, message string) {
		h.jobsMux.Lock()
		job.CurrentStep = step
		job.TotalSteps = total
		job.StepName = message
		if total > 0 {
			job.Progress = int(float64(step) / float64(total) * 100)
		}
		h.jobsMux.Unlock()
	})

	// Run import
	stats, err := importer.Import(ctx, req.DatabaseDSN)
	now := time.Now()
	job.CompletedAt = &now

	if err != nil {
		job.Status = migration.MigrationStatusFailed
		job.Error = err.Error()
		h.logger.Error().Err(err).Str("job_id", job.ID).Msg("LibreTime import failed")
	} else {
		job.Status = migration.MigrationStatusCompleted
		job.Progress = 100
		if stats != nil {
			job.Stats = *stats
		}
		h.logger.Info().Str("job_id", job.ID).Msg("LibreTime import completed")
	}
}
