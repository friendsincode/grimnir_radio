/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package migration

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// Service manages migration jobs.
type Service struct {
	db        *gorm.DB
	bus       *events.Bus
	logger    zerolog.Logger
	importers map[SourceType]Importer

	mu      sync.RWMutex
	jobs    map[string]*Job
	cancels map[string]context.CancelFunc
}

// NewService creates a new migration service.
func NewService(db *gorm.DB, bus *events.Bus, logger zerolog.Logger) *Service {
	return &Service{
		db:        db,
		bus:       bus,
		logger:    logger.With().Str("component", "migration").Logger(),
		importers: make(map[SourceType]Importer),
		jobs:      make(map[string]*Job),
		cancels:   make(map[string]context.CancelFunc),
	}
}

// RecoverStaleJobs marks any jobs stuck in "running" status as failed.
// This should be called on server startup to handle jobs that were interrupted
// by a server restart or crash.
func (s *Service) RecoverStaleJobs(ctx context.Context) error {
	var staleJobs []*Job
	if err := s.db.WithContext(ctx).Where("status = ?", JobStatusRunning).Find(&staleJobs).Error; err != nil {
		return fmt.Errorf("find stale jobs: %w", err)
	}

	if len(staleJobs) == 0 {
		return nil
	}

	s.logger.Warn().Int("count", len(staleJobs)).Msg("found stale migration jobs from previous run")

	now := time.Now()
	for _, job := range staleJobs {
		job.Status = JobStatusFailed
		job.Error = "import interrupted by server restart - use restart button to try again"
		job.CompletedAt = &now

		if err := s.db.WithContext(ctx).Save(job).Error; err != nil {
			s.logger.Error().Err(err).Str("job_id", job.ID).Msg("failed to mark stale job as failed")
			continue
		}

		s.logger.Info().
			Str("job_id", job.ID).
			Str("source_type", string(job.SourceType)).
			Msg("marked stale job as failed")
	}

	return nil
}

// RegisterImporter registers an importer for a source type.
func (s *Service) RegisterImporter(sourceType SourceType, importer Importer) {
	s.importers[sourceType] = importer
	s.logger.Info().Str("source_type", string(sourceType)).Msg("registered migration importer")
}

// CreateJob creates a new migration job.
func (s *Service) CreateJob(ctx context.Context, sourceType SourceType, options Options) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate source type
	importer, ok := s.importers[sourceType]
	if !ok {
		return nil, fmt.Errorf("no importer registered for source type: %s", sourceType)
	}

	// Validate options
	if err := importer.Validate(ctx, options); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Create job
	job := &Job{
		ID:         uuid.New().String(),
		SourceType: sourceType,
		Status:     JobStatusPending,
		DryRun:     options.SkipMedia && options.SkipSchedules && options.SkipPlaylists && options.SkipUsers, // Auto-detect dry run
		Options:    options,
		Progress: Progress{
			Phase:      "created",
			TotalSteps: 0,
			StartTime:  time.Now(),
		},
		CreatedAt: time.Now(),
	}
	// Stamp job id into options for provenance tracking.
	job.Options.JobID = job.ID

	// Save to database
	if err := s.db.WithContext(ctx).Create(job).Error; err != nil {
		return nil, fmt.Errorf("save job: %w", err)
	}

	// Store in memory
	s.jobs[job.ID] = job

	s.logger.Info().
		Str("job_id", job.ID).
		Str("source_type", string(sourceType)).
		Bool("dry_run", job.DryRun).
		Msg("migration job created")

	// Publish event
	s.bus.Publish(events.EventMigration, events.Payload{
		"job_id":      job.ID,
		"source_type": string(sourceType),
		"status":      string(JobStatusPending),
	})

	return job, nil
}

// StartJob starts a migration job.
func (s *Service) StartJob(parentCtx context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		// Try loading from database
		job = &Job{}
		if err := s.db.WithContext(parentCtx).First(job, "id = ?", jobID).Error; err != nil {
			return fmt.Errorf("job not found: %w", err)
		}
		s.jobs[jobID] = job
	}

	if job.Status != JobStatusPending {
		return fmt.Errorf("job is not in pending state: %s", job.Status)
	}

	importer, ok := s.importers[job.SourceType]
	if !ok {
		return fmt.Errorf("no importer registered for source type: %s", job.SourceType)
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(parentCtx)
	s.cancels[jobID] = cancel

	// Start job in background
	go func() {
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error().
					Interface("panic", r).
					Str("job_id", jobID).
					Msg("migration job panicked")

				// Update job status to failed
				s.mu.Lock()
				job.Status = JobStatusFailed
				job.Error = fmt.Sprintf("panic: %v", r)
				now := time.Now()
				job.CompletedAt = &now
				s.mu.Unlock()
				_ = s.updateJob(context.Background(), job)
			}
		}()
		s.runJob(ctx, job, importer)
	}()

	s.logger.Info().Str("job_id", jobID).Msg("migration job started")
	return nil
}

// runJob executes a migration job.
func (s *Service) runJob(ctx context.Context, job *Job, importer Importer) {
	startTime := time.Now()
	now := startTime

	s.mu.Lock()
	job.StartedAt = &now
	job.Status = JobStatusRunning
	s.mu.Unlock()

	// Update status to running
	if err := s.updateJob(ctx, job); err != nil {
		s.logger.Error().Err(err).Str("job_id", job.ID).Msg("failed to update job status")
		return
	}

	// Create progress callback
	progressCallback := func(progress Progress) {
		s.mu.Lock()
		// Preserve and extend step history for UI visibility.
		history := job.Progress.StepHistory
		if len(history) == 0 && len(progress.StepHistory) > 0 {
			history = progress.StepHistory
		}
		// Append when changed (phase/step).
		lastStep := ""
		lastPhase := ""
		if n := len(history); n > 0 {
			lastStep = history[n-1].Step
			lastPhase = history[n-1].Phase
		}
		if progress.CurrentStep != "" || progress.Phase != "" {
			if progress.CurrentStep != lastStep || progress.Phase != lastPhase {
				history = append(history, ProgressStep{
					At:         time.Now(),
					Phase:      progress.Phase,
					Step:       progress.CurrentStep,
					Percentage: progress.Percentage,
				})
				// Cap to last 50 entries.
				if len(history) > 50 {
					history = history[len(history)-50:]
				}
			}
		}
		progress.StepHistory = history
		job.Progress = progress
		status := job.Status
		s.mu.Unlock()

		if err := s.updateJob(ctx, job); err != nil {
			s.logger.Error().Err(err).Str("job_id", job.ID).Msg("failed to update progress")
		}

		// Publish progress event
		s.bus.Publish(events.EventMigration, events.Payload{
			"job_id":     job.ID,
			"status":     string(status),
			"progress":   progress,
			"percentage": progress.Percentage,
		})
	}

	// Run import
	result, err := importer.Import(ctx, job.Options, progressCallback)
	duration := time.Since(startTime)

	s.mu.Lock()
	if result != nil {
		job.Result = result
		job.AnomalyReport = BuildAnomalyReport(result)
	}

	if err != nil {
		job.Status = JobStatusFailed
		job.Error = err.Error()
	} else {
		job.Status = JobStatusCompleted
		result.DurationSeconds = duration.Seconds()
		job.Result = result
	}

	// Update completion time
	now = time.Now()
	job.CompletedAt = &now
	finalStatus := job.Status
	finalError := job.Error
	delete(s.cancels, job.ID)
	s.mu.Unlock()

	if err != nil {
		s.logger.Error().Err(err).Str("job_id", job.ID).Msg("migration failed")
	} else {
		s.logger.Info().
			Str("job_id", job.ID).
			Dur("duration", duration).
			Int("stations", result.StationsCreated).
			Int("media", result.MediaItemsImported).
			Int("playlists", result.PlaylistsCreated).
			Msg("migration completed")
	}

	// Final update
	if err := s.updateJob(ctx, job); err != nil {
		s.logger.Error().Err(err).Str("job_id", job.ID).Msg("failed to update final job status")
	}

	// Publish completion event
	s.bus.Publish(events.EventMigration, events.Payload{
		"job_id": job.ID,
		"status": string(finalStatus),
		"result": result,
		"error":  finalError,
	})
}

// StartStagedJob runs analysis for a staged import job and produces a staged import record for review.
func (s *Service) StartStagedJob(parentCtx context.Context, jobID string) error {
	s.mu.Lock()

	job, ok := s.jobs[jobID]
	if !ok {
		job = &Job{}
		if err := s.db.WithContext(parentCtx).First(job, "id = ?", jobID).Error; err != nil {
			s.mu.Unlock()
			return fmt.Errorf("job not found: %w", err)
		}
		s.jobs[jobID] = job
	}

	if job.Status != JobStatusAnalyzing {
		s.mu.Unlock()
		return fmt.Errorf("job is not in analyzing state: %s", job.Status)
	}

	importer, ok := s.importers[job.SourceType]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("no importer registered for source type: %s", job.SourceType)
	}

	analyzer, ok := importer.(interface {
		AnalyzeForStaging(ctx context.Context, jobID string, options Options) (*models.StagedImport, error)
	})
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("importer does not support staged analysis: %s", job.SourceType)
	}

	ctx, cancel := context.WithCancel(parentCtx)
	s.cancels[jobID] = cancel

	// Mark started time if not set.
	now := time.Now()
	job.StartedAt = &now
	job.Progress = Progress{
		Phase:       "analyzing",
		TotalSteps:  1,
		StartTime:   now,
		Percentage:  0,
		CurrentStep: "Analyzing source data for review...",
		StepHistory: []ProgressStep{{
			At:    now,
			Phase: "analyzing",
			Step:  "Analyzing source data for review...",
		}},
	}
	s.mu.Unlock()
	_ = s.updateJob(ctx, job)

	go func() {
		defer cancel()
		defer func() {
			s.mu.Lock()
			delete(s.cancels, jobID)
			s.mu.Unlock()
		}()

		staged, err := analyzer.AnalyzeForStaging(ctx, jobID, job.Options)
		if err != nil {
			s.logger.Error().Err(err).Str("job_id", jobID).Msg("staged analysis failed")
			s.mu.Lock()
			job.Status = JobStatusFailed
			job.Error = err.Error()
			done := time.Now()
			job.CompletedAt = &done
			job.Progress.Phase = "failed"
			job.Progress.CurrentStep = "Analysis failed"
			status := job.Status
			errMsg := job.Error
			s.mu.Unlock()
			_ = s.updateJob(context.Background(), job)
			s.bus.Publish(events.EventMigration, events.Payload{
				"job_id":      jobID,
				"status":      string(status),
				"staged_mode": true,
				"error":       errMsg,
			})
			return
		}
		if staged == nil || staged.ID == "" {
			errMsg := "staged analysis produced no review data"
			s.logger.Error().Str("job_id", jobID).Msg(errMsg)
			s.mu.Lock()
			job.Status = JobStatusFailed
			job.Error = errMsg
			done := time.Now()
			job.CompletedAt = &done
			job.Progress.Phase = "failed"
			job.Progress.CurrentStep = "Analysis failed"
			s.mu.Unlock()
			_ = s.updateJob(context.Background(), job)
			s.bus.Publish(events.EventMigration, events.Payload{
				"job_id":      jobID,
				"status":      string(JobStatusFailed),
				"staged_mode": true,
				"error":       errMsg,
			})
			return
		}

		s.mu.Lock()
		job.Status = JobStatusStaged
		job.Error = ""
		job.StagedImportID = &staged.ID
		job.Progress.Phase = "staged"
		job.Progress.Percentage = 100
		job.Progress.CurrentStep = "Analysis complete. Ready for review."
		job.Progress.StepHistory = append(job.Progress.StepHistory, ProgressStep{
			At:         time.Now(),
			Phase:      "staged",
			Step:       "Analysis complete. Ready for review.",
			Percentage: 100,
		})
		stagedID := staged.ID
		s.mu.Unlock()
		_ = s.updateJob(context.Background(), job)

		s.bus.Publish(events.EventMigration, events.Payload{
			"job_id":      jobID,
			"status":      string(JobStatusStaged),
			"staged_mode": true,
			"staged_id":   &stagedID,
		})
	}()

	s.logger.Info().Str("job_id", jobID).Msg("staged migration analysis started")
	return nil
}

// CommitStagedImport starts importing selected items from a staged import.
func (s *Service) CommitStagedImport(parentCtx context.Context, stagedID string) error {
	// Load staged import and job.
	staged, err := s.GetStagedImport(parentCtx, stagedID)
	if err != nil {
		return fmt.Errorf("staged import not found: %w", err)
	}
	if staged.Status != models.StagedImportStatusReady {
		return fmt.Errorf("staged import is not ready: %s", staged.Status)
	}

	job, err := s.getJobLive(parentCtx, staged.JobID)
	if err != nil {
		return fmt.Errorf("job not found: %w", err)
	}

	importer, ok := s.importers[job.SourceType]
	if !ok {
		return fmt.Errorf("no importer registered for source type: %s", job.SourceType)
	}

	committer, ok := importer.(interface {
		CommitStagedImport(ctx context.Context, staged *models.StagedImport, jobID string, options Options, cb ProgressCallback) (*Result, error)
	})
	if !ok {
		return fmt.Errorf("importer does not support staged commit: %s", job.SourceType)
	}

	// Create cancellable context.
	ctx, cancel := context.WithCancel(parentCtx)
	s.mu.Lock()
	s.cancels[job.ID] = cancel
	s.mu.Unlock()

	// Run commit in background.
	go func() {
		defer cancel()
		defer func() {
			s.mu.Lock()
			delete(s.cancels, job.ID)
			s.mu.Unlock()
		}()

		startTime := time.Now()
		s.mu.Lock()
		job.Status = JobStatusRunning
		job.Error = ""
		job.StartedAt = &startTime
		job.CompletedAt = nil
		job.Progress = Progress{
			Phase:       "importing_selected",
			TotalSteps:  1,
			StartTime:   startTime,
			Percentage:  0,
			CurrentStep: "Starting staged import...",
			StepHistory: []ProgressStep{{
				At:    startTime,
				Phase: "importing_selected",
				Step:  "Starting staged import...",
			}},
		}
		s.mu.Unlock()
		_ = s.updateJob(context.Background(), job)

		progressCallback := func(p Progress) {
			s.mu.Lock()
			// Preserve and extend step history.
			history := job.Progress.StepHistory
			lastStep := ""
			lastPhase := ""
			if n := len(history); n > 0 {
				lastStep = history[n-1].Step
				lastPhase = history[n-1].Phase
			}
			if p.CurrentStep != "" || p.Phase != "" {
				if p.CurrentStep != lastStep || p.Phase != lastPhase {
					history = append(history, ProgressStep{
						At:         time.Now(),
						Phase:      p.Phase,
						Step:       p.CurrentStep,
						Percentage: p.Percentage,
					})
					if len(history) > 50 {
						history = history[len(history)-50:]
					}
				}
			}
			p.StepHistory = history
			job.Progress = p
			status := job.Status
			s.mu.Unlock()
			_ = s.updateJob(context.Background(), job)
			s.bus.Publish(events.EventMigration, events.Payload{
				"job_id":     job.ID,
				"status":     string(status),
				"progress":   p,
				"percentage": p.Percentage,
				"staged_id":  stagedID,
			})
		}

		result, err := committer.CommitStagedImport(ctx, staged, job.ID, job.Options, progressCallback)
		s.mu.Lock()
		if result != nil {
			job.Result = result
			job.AnomalyReport = BuildAnomalyReport(result)
		}
		if err != nil {
			job.Status = JobStatusFailed
			job.Error = err.Error()
		} else {
			job.Status = JobStatusCompleted
		}
		done := time.Now()
		job.CompletedAt = &done
		finalStatus := job.Status
		finalError := job.Error
		s.mu.Unlock()

		if err != nil {
			s.logger.Error().Err(err).Str("job_id", job.ID).Msg("staged import commit failed")
		}

		_ = s.updateJob(context.Background(), job)

		s.bus.Publish(events.EventMigration, events.Payload{
			"job_id":    job.ID,
			"status":    string(finalStatus),
			"result":    result,
			"error":     finalError,
			"staged_id": stagedID,
		})
	}()

	return nil
}

// GetJob retrieves a migration job by ID (returns a snapshot safe for concurrent reads).
func (s *Service) GetJob(ctx context.Context, jobID string) (*Job, error) {
	job, err := s.getJobLive(ctx, jobID)
	if err != nil {
		return nil, err
	}
	// Return a snapshot to avoid racing with background goroutines.
	s.mu.RLock()
	snap := *job
	s.mu.RUnlock()
	return &snap, nil
}

// getJobLive returns the live (mutable) pointer for internal use.
func (s *Service) getJobLive(ctx context.Context, jobID string) (*Job, error) {
	s.mu.RLock()
	job, ok := s.jobs[jobID]
	s.mu.RUnlock()

	if ok {
		s.hydrateStagedImportRef(ctx, job)
		return job, nil
	}

	// Load from database
	job = &Job{}
	if err := s.db.WithContext(ctx).First(job, "id = ?", jobID).Error; err != nil {
		return nil, err
	}

	// Cache in memory
	s.mu.Lock()
	s.jobs[jobID] = job
	s.mu.Unlock()
	s.hydrateStagedImportRef(ctx, job)

	return job, nil
}

// ListJobs lists all migration jobs.
func (s *Service) ListJobs(ctx context.Context) ([]*Job, error) {
	var jobs []*Job
	if err := s.db.WithContext(ctx).Order("created_at DESC").Find(&jobs).Error; err != nil {
		return nil, err
	}
	for _, job := range jobs {
		s.hydrateStagedImportRef(ctx, job)
	}
	return jobs, nil
}

// CancelJob cancels a running migration job.
func (s *Service) CancelJob(ctx context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if job.Status != JobStatusRunning {
		return fmt.Errorf("job is not running: %s", job.Status)
	}

	// Call cancel function
	if cancel, ok := s.cancels[jobID]; ok {
		cancel()
	}

	// Update status
	job.Status = JobStatusCancelled
	if err := s.updateJob(ctx, job); err != nil {
		return fmt.Errorf("update job status: %w", err)
	}

	s.logger.Info().Str("job_id", jobID).Msg("migration job cancelled")

	// Publish event
	s.bus.Publish(events.EventMigration, events.Payload{
		"job_id": jobID,
		"status": string(JobStatusCancelled),
	})

	return nil
}

// DeleteJob deletes a migration job.
func (s *Service) DeleteJob(ctx context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		// Load from database to check existence
		job = &Job{}
		if err := s.db.WithContext(ctx).First(job, "id = ?", jobID).Error; err != nil {
			return err
		}
	}

	// Can't delete running jobs
	if job.Status == JobStatusRunning {
		return fmt.Errorf("cannot delete running job")
	}

	// Delete from database
	if err := s.db.WithContext(ctx).Delete(&Job{}, "id = ?", jobID).Error; err != nil {
		return fmt.Errorf("delete job: %w", err)
	}

	// Remove from memory
	delete(s.jobs, jobID)

	s.logger.Info().Str("job_id", jobID).Msg("migration job deleted")
	return nil
}

// updateJob persists the job to the database. It saves a copy so that GORM's
// internal field writes (e.g. UpdatedAt) don't race with concurrent readers.
func (s *Service) updateJob(ctx context.Context, job *Job) error {
	s.mu.RLock()
	snap := *job
	s.mu.RUnlock()
	return s.db.WithContext(ctx).Save(&snap).Error
}

// hydrateStagedImportRef ensures staged jobs have staged_import_id populated.
// Older rows or partial writes may leave this denormalized field empty.
func (s *Service) hydrateStagedImportRef(ctx context.Context, job *Job) {
	s.mu.RLock()
	skip := job == nil || job.Status != JobStatusStaged || (job.StagedImportID != nil && *job.StagedImportID != "")
	s.mu.RUnlock()
	if skip {
		return
	}
	var staged models.StagedImport
	if err := s.db.WithContext(ctx).
		Where("job_id = ?", job.ID).
		Order("created_at DESC").
		First(&staged).Error; err != nil || staged.ID == "" {
		return
	}
	stagedID := staged.ID
	s.mu.Lock()
	job.StagedImportID = &stagedID
	s.mu.Unlock()

	// Best-effort backfill for future queries.
	if err := s.db.WithContext(ctx).
		Model(&Job{}).
		Where("id = ?", job.ID).
		Update("staged_import_id", stagedID).Error; err != nil {
		s.logger.Warn().
			Err(err).
			Str("job_id", job.ID).
			Str("staged_id", stagedID).
			Msg("failed to backfill staged_import_id on staged job")
	}
}

// ResetImportedData clears all imported data from the database.
// This deletes stations, media, playlists, schedules, etc. but preserves users.
// Use with caution - this is destructive and cannot be undone.
func (s *Service) ResetImportedData(ctx context.Context) error {
	s.logger.Warn().Msg("resetting all imported data - this is destructive!")

	// Use a transaction for atomicity
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Order matters due to foreign key constraints
		// Delete in reverse order of dependencies

		// Clear play history
		if err := tx.Exec("DELETE FROM play_histories").Error; err != nil {
			return fmt.Errorf("clear play_histories: %w", err)
		}
		s.logger.Info().Msg("cleared play_histories")

		// Clear schedule entries
		if err := tx.Exec("DELETE FROM schedule_entries").Error; err != nil {
			return fmt.Errorf("clear schedule_entries: %w", err)
		}
		s.logger.Info().Msg("cleared schedule_entries")

		// Clear clock slots
		if err := tx.Exec("DELETE FROM clock_slots").Error; err != nil {
			return fmt.Errorf("clear clock_slots: %w", err)
		}
		s.logger.Info().Msg("cleared clock_slots")

		// Clear clock hours
		if err := tx.Exec("DELETE FROM clock_hours").Error; err != nil {
			return fmt.Errorf("clear clock_hours: %w", err)
		}
		s.logger.Info().Msg("cleared clock_hours")

		// Clear clocks
		if err := tx.Exec("DELETE FROM clocks").Error; err != nil {
			return fmt.Errorf("clear clocks: %w", err)
		}
		s.logger.Info().Msg("cleared clocks")

		// Clear playlist items
		if err := tx.Exec("DELETE FROM playlist_items").Error; err != nil {
			return fmt.Errorf("clear playlist_items: %w", err)
		}
		s.logger.Info().Msg("cleared playlist_items")

		// Clear playlists
		if err := tx.Exec("DELETE FROM playlists").Error; err != nil {
			return fmt.Errorf("clear playlists: %w", err)
		}
		s.logger.Info().Msg("cleared playlists")

		// Clear smart blocks
		if err := tx.Exec("DELETE FROM smart_blocks").Error; err != nil {
			return fmt.Errorf("clear smart_blocks: %w", err)
		}
		s.logger.Info().Msg("cleared smart_blocks")

		// Clear media tag links
		if err := tx.Exec("DELETE FROM media_tag_links").Error; err != nil {
			return fmt.Errorf("clear media_tag_links: %w", err)
		}
		s.logger.Info().Msg("cleared media_tag_links")

		// Clear tags
		if err := tx.Exec("DELETE FROM tags").Error; err != nil {
			return fmt.Errorf("clear tags: %w", err)
		}
		s.logger.Info().Msg("cleared tags")

		// Clear analysis jobs
		if err := tx.Exec("DELETE FROM analysis_jobs").Error; err != nil {
			return fmt.Errorf("clear analysis_jobs: %w", err)
		}
		s.logger.Info().Msg("cleared analysis_jobs")

		// Clear media items
		if err := tx.Exec("DELETE FROM media_items").Error; err != nil {
			return fmt.Errorf("clear media_items: %w", err)
		}
		s.logger.Info().Msg("cleared media_items")

		// Clear webstreams
		if err := tx.Exec("DELETE FROM webstreams").Error; err != nil {
			return fmt.Errorf("clear webstreams: %w", err)
		}
		s.logger.Info().Msg("cleared webstreams")

		// Clear live sessions
		if err := tx.Exec("DELETE FROM live_sessions").Error; err != nil {
			return fmt.Errorf("clear live_sessions: %w", err)
		}
		s.logger.Info().Msg("cleared live_sessions")

		// Clear priority sources (playout priority queue)
		if err := tx.Exec("DELETE FROM priority_sources").Error; err != nil {
			return fmt.Errorf("clear priority_sources: %w", err)
		}
		s.logger.Info().Msg("cleared priority_sources")

		// Clear executor states (playout state)
		if err := tx.Exec("DELETE FROM executor_states").Error; err != nil {
			return fmt.Errorf("clear executor_states: %w", err)
		}
		s.logger.Info().Msg("cleared executor_states")

		// Clear mounts
		if err := tx.Exec("DELETE FROM mounts").Error; err != nil {
			return fmt.Errorf("clear mounts: %w", err)
		}
		s.logger.Info().Msg("cleared mounts")

		// Clear station users (but not the users themselves)
		if err := tx.Exec("DELETE FROM station_users").Error; err != nil {
			return fmt.Errorf("clear station_users: %w", err)
		}
		s.logger.Info().Msg("cleared station_users")

		// Clear station group members
		if err := tx.Exec("DELETE FROM station_group_members").Error; err != nil {
			return fmt.Errorf("clear station_group_members: %w", err)
		}
		s.logger.Info().Msg("cleared station_group_members")

		// Clear station groups
		if err := tx.Exec("DELETE FROM station_groups").Error; err != nil {
			return fmt.Errorf("clear station_groups: %w", err)
		}
		s.logger.Info().Msg("cleared station_groups")

		// Clear stations
		if err := tx.Exec("DELETE FROM stations").Error; err != nil {
			return fmt.Errorf("clear stations: %w", err)
		}
		s.logger.Info().Msg("cleared stations")

		// Clear migration jobs
		if err := tx.Exec("DELETE FROM jobs").Error; err != nil {
			return fmt.Errorf("clear jobs: %w", err)
		}
		s.logger.Info().Msg("cleared migration jobs")

		s.logger.Warn().Msg("database reset complete - all imported data has been cleared")
		return nil
	})
}

// =============================================================================
// STAGED IMPORT METHODS
// =============================================================================

// CreateStagedJob creates a new migration job in staged mode.
// It creates both the job and a staged import record for review.
func (s *Service) CreateStagedJob(ctx context.Context, sourceType SourceType, options Options) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate source type
	importer, ok := s.importers[sourceType]
	if !ok {
		return nil, fmt.Errorf("no importer registered for source type: %s", sourceType)
	}

	// Validate options
	if err := importer.Validate(ctx, options); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Create job in staged mode
	job := &Job{
		ID:         uuid.New().String(),
		SourceType: sourceType,
		Status:     JobStatusAnalyzing,
		StagedMode: true,
		Options:    options,
		Progress: Progress{
			Phase:      "analyzing",
			TotalSteps: 0,
			StartTime:  time.Now(),
		},
		CreatedAt: time.Now(),
	}
	// Stamp job id into options for provenance tracking.
	job.Options.JobID = job.ID

	// Save job to database
	if err := s.db.WithContext(ctx).Create(job).Error; err != nil {
		return nil, fmt.Errorf("save job: %w", err)
	}

	// Store in memory
	s.jobs[job.ID] = job

	s.logger.Info().
		Str("job_id", job.ID).
		Str("source_type", string(sourceType)).
		Bool("staged_mode", true).
		Msg("staged migration job created")

	// Publish event
	s.bus.Publish(events.EventMigration, events.Payload{
		"job_id":      job.ID,
		"source_type": string(sourceType),
		"status":      string(JobStatusAnalyzing),
		"staged_mode": true,
	})

	return job, nil
}

// GetStagedImport retrieves a staged import by ID.
func (s *Service) GetStagedImport(ctx context.Context, id string) (*models.StagedImport, error) {
	var staged models.StagedImport
	if err := s.db.WithContext(ctx).First(&staged, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &staged, nil
}

// GetStagedImportByJobID retrieves a staged import by job ID.
func (s *Service) GetStagedImportByJobID(ctx context.Context, jobID string) (*models.StagedImport, error) {
	var staged models.StagedImport
	if err := s.db.WithContext(ctx).First(&staged, "job_id = ?", jobID).Error; err != nil {
		return nil, err
	}
	return &staged, nil
}

// UpdateSelections updates the user's selections on a staged import.
func (s *Service) UpdateSelections(ctx context.Context, stagedID string, selections models.ImportSelections) error {
	var staged models.StagedImport
	if err := s.db.WithContext(ctx).First(&staged, "id = ?", stagedID).Error; err != nil {
		return fmt.Errorf("staged import not found: %w", err)
	}

	if staged.Status != models.StagedImportStatusReady {
		return fmt.Errorf("staged import is not ready for selection updates: %s", staged.Status)
	}

	staged.Selections = selections
	stationFilter := make(map[int]struct{}, len(selections.StationIDs))
	for _, raw := range selections.StationIDs {
		id, err := strconv.Atoi(raw)
		if err != nil {
			continue
		}
		stationFilter[id] = struct{}{}
	}

	// Update individual item selections based on IDs in selections
	for i := range staged.StagedMedia {
		staged.StagedMedia[i].Selected = sourcePassesStationFilter(staged.StagedMedia[i].SourceID, stationFilter) &&
			containsString(selections.MediaIDs, staged.StagedMedia[i].SourceID)
	}
	for i := range staged.StagedPlaylists {
		staged.StagedPlaylists[i].Selected = sourcePassesStationFilter(staged.StagedPlaylists[i].SourceID, stationFilter) &&
			containsString(selections.PlaylistIDs, staged.StagedPlaylists[i].SourceID)
	}
	for i := range staged.StagedSmartBlocks {
		staged.StagedSmartBlocks[i].Selected = sourcePassesStationFilter(staged.StagedSmartBlocks[i].SourceID, stationFilter) &&
			containsString(selections.SmartBlockIDs, staged.StagedSmartBlocks[i].SourceID)
	}
	for i := range staged.StagedWebstreams {
		staged.StagedWebstreams[i].Selected = sourcePassesStationFilter(staged.StagedWebstreams[i].SourceID, stationFilter) &&
			containsString(selections.WebstreamIDs, staged.StagedWebstreams[i].SourceID)
	}

	// Update show selections with Show vs Clock preference
	for i := range staged.StagedShows {
		sourceID := staged.StagedShows[i].SourceID
		staged.StagedShows[i].Selected = sourcePassesStationFilter(sourceID, stationFilter) &&
			containsString(selections.ShowIDs, sourceID)
		staged.StagedShows[i].CreateShow = containsString(selections.ShowsAsShows, sourceID)
		staged.StagedShows[i].CreateClock = containsString(selections.ShowsAsClocks, sourceID)
		if customRRule, ok := selections.CustomRRules[sourceID]; ok {
			staged.StagedShows[i].CustomRRule = customRRule
		}
	}

	if err := s.db.WithContext(ctx).Save(&staged).Error; err != nil {
		return fmt.Errorf("save selections: %w", err)
	}

	s.logger.Info().
		Str("staged_id", stagedID).
		Int("selected_count", staged.SelectedCount()).
		Msg("staged import selections updated")

	return nil
}

// RejectStagedImport marks a staged import as rejected and cleans up.
func (s *Service) RejectStagedImport(ctx context.Context, stagedID string) error {
	var staged models.StagedImport
	if err := s.db.WithContext(ctx).First(&staged, "id = ?", stagedID).Error; err != nil {
		return fmt.Errorf("staged import not found: %w", err)
	}

	if staged.Status == models.StagedImportStatusCommitted {
		return fmt.Errorf("cannot reject already committed import")
	}

	staged.Status = models.StagedImportStatusRejected

	if err := s.db.WithContext(ctx).Save(&staged).Error; err != nil {
		return fmt.Errorf("save staged import: %w", err)
	}

	// Update the associated job
	var job Job
	if err := s.db.WithContext(ctx).First(&job, "id = ?", staged.JobID).Error; err == nil {
		job.Status = JobStatusCancelled
		now := time.Now()
		job.CompletedAt = &now
		s.db.WithContext(ctx).Save(&job)
	}

	s.logger.Info().
		Str("staged_id", stagedID).
		Str("job_id", staged.JobID).
		Msg("staged import rejected")

	// Publish event
	s.bus.Publish(events.EventMigration, events.Payload{
		"staged_id": stagedID,
		"job_id":    staged.JobID,
		"status":    string(models.StagedImportStatusRejected),
	})

	return nil
}

// GetImportedItems retrieves the items created by a specific import job.
func (s *Service) GetImportedItems(ctx context.Context, jobID string) (*ImportedItems, error) {
	var job Job
	if err := s.db.WithContext(ctx).First(&job, "id = ?", jobID).Error; err != nil {
		return nil, fmt.Errorf("job not found: %w", err)
	}

	if job.ImportedItems != nil {
		return job.ImportedItems, nil
	}

	// If ImportedItems not stored on job, query by provenance fields
	items := &ImportedItems{}

	// Query media items
	var mediaIDs []string
	s.db.WithContext(ctx).Model(&models.MediaItem{}).
		Where("import_job_id = ?", jobID).
		Pluck("id", &mediaIDs)
	items.MediaIDs = mediaIDs

	// Query smart blocks
	var smartBlockIDs []string
	s.db.WithContext(ctx).Model(&models.SmartBlock{}).
		Where("import_job_id = ?", jobID).
		Pluck("id", &smartBlockIDs)
	items.SmartBlockIDs = smartBlockIDs

	// Query playlists
	var playlistIDs []string
	s.db.WithContext(ctx).Model(&models.Playlist{}).
		Where("import_job_id = ?", jobID).
		Pluck("id", &playlistIDs)
	items.PlaylistIDs = playlistIDs

	// Query shows
	var showIDs []string
	s.db.WithContext(ctx).Model(&models.Show{}).
		Where("import_job_id = ?", jobID).
		Pluck("id", &showIDs)
	items.ShowIDs = showIDs

	// Query clock hours
	var clockIDs []string
	s.db.WithContext(ctx).Model(&models.ClockHour{}).
		Where("import_job_id = ?", jobID).
		Pluck("id", &clockIDs)
	items.ClockIDs = clockIDs

	// Query webstreams
	var webstreamIDs []string
	s.db.WithContext(ctx).Model(&models.Webstream{}).
		Where("import_job_id = ?", jobID).
		Pluck("id", &webstreamIDs)
	items.WebstreamIDs = webstreamIDs

	return items, nil
}

// RollbackImport deletes all items created by a specific import job.
func (s *Service) RollbackImport(ctx context.Context, jobID string) error {
	var job Job
	if err := s.db.WithContext(ctx).First(&job, "id = ?", jobID).Error; err != nil {
		return fmt.Errorf("job not found: %w", err)
	}

	if job.Status != JobStatusCompleted {
		return fmt.Errorf("can only rollback completed imports")
	}

	s.logger.Warn().
		Str("job_id", jobID).
		Msg("starting import rollback - this will delete all items from this import")

	// Get imported items
	items, err := s.GetImportedItems(ctx, jobID)
	if err != nil {
		return fmt.Errorf("get imported items: %w", err)
	}

	// Use a transaction for atomicity
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete in reverse order of dependencies

		// Delete webstreams
		if len(items.WebstreamIDs) > 0 {
			if err := tx.Where("id IN ?", items.WebstreamIDs).Delete(&models.Webstream{}).Error; err != nil {
				return fmt.Errorf("delete webstreams: %w", err)
			}
			s.logger.Info().Int("count", len(items.WebstreamIDs)).Msg("deleted webstreams")
		}

		// Delete clock hours
		if len(items.ClockIDs) > 0 {
			if err := tx.Where("id IN ?", items.ClockIDs).Delete(&models.ClockHour{}).Error; err != nil {
				return fmt.Errorf("delete clock hours: %w", err)
			}
			s.logger.Info().Int("count", len(items.ClockIDs)).Msg("deleted clock hours")
		}

		// Delete show instances first, then shows
		if len(items.ShowIDs) > 0 {
			if err := tx.Where("show_id IN ?", items.ShowIDs).Delete(&models.ShowInstance{}).Error; err != nil {
				return fmt.Errorf("delete show instances: %w", err)
			}
			if err := tx.Where("id IN ?", items.ShowIDs).Delete(&models.Show{}).Error; err != nil {
				return fmt.Errorf("delete shows: %w", err)
			}
			s.logger.Info().Int("count", len(items.ShowIDs)).Msg("deleted shows")
		}

		// Delete playlist items first, then playlists
		if len(items.PlaylistIDs) > 0 {
			if err := tx.Where("playlist_id IN ?", items.PlaylistIDs).Delete(&models.PlaylistItem{}).Error; err != nil {
				return fmt.Errorf("delete playlist items: %w", err)
			}
			if err := tx.Where("id IN ?", items.PlaylistIDs).Delete(&models.Playlist{}).Error; err != nil {
				return fmt.Errorf("delete playlists: %w", err)
			}
			s.logger.Info().Int("count", len(items.PlaylistIDs)).Msg("deleted playlists")
		}

		// Delete smart blocks
		if len(items.SmartBlockIDs) > 0 {
			if err := tx.Where("id IN ?", items.SmartBlockIDs).Delete(&models.SmartBlock{}).Error; err != nil {
				return fmt.Errorf("delete smart blocks: %w", err)
			}
			s.logger.Info().Int("count", len(items.SmartBlockIDs)).Msg("deleted smart blocks")
		}

		// Delete media items
		if len(items.MediaIDs) > 0 {
			// First delete media tag links
			if err := tx.Where("media_item_id IN ?", items.MediaIDs).Delete(&models.MediaTagLink{}).Error; err != nil {
				return fmt.Errorf("delete media tag links: %w", err)
			}
			if err := tx.Where("id IN ?", items.MediaIDs).Delete(&models.MediaItem{}).Error; err != nil {
				return fmt.Errorf("delete media items: %w", err)
			}
			s.logger.Info().Int("count", len(items.MediaIDs)).Msg("deleted media items")
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("rollback transaction failed: %w", err)
	}

	// Update job status
	job.Status = JobStatusRolledBack
	if err := s.db.WithContext(ctx).Save(&job).Error; err != nil {
		s.logger.Error().Err(err).Str("job_id", jobID).Msg("failed to update job status after rollback")
	}

	s.logger.Warn().
		Str("job_id", jobID).
		Int("total_deleted", items.TotalCount()).
		Msg("import rollback complete")

	// Publish event
	s.bus.Publish(events.EventMigration, events.Payload{
		"job_id":        jobID,
		"status":        string(JobStatusRolledBack),
		"items_deleted": items.TotalCount(),
	})

	return nil
}

// CloneJobForRedo creates a new job with the same options for re-running an import.
func (s *Service) CloneJobForRedo(ctx context.Context, jobID string) (*Job, error) {
	var originalJob Job
	if err := s.db.WithContext(ctx).First(&originalJob, "id = ?", jobID).Error; err != nil {
		return nil, fmt.Errorf("original job not found: %w", err)
	}

	// Create new job with same options
	newJob := &Job{
		ID:          uuid.New().String(),
		SourceType:  originalJob.SourceType,
		Status:      JobStatusPending,
		StagedMode:  originalJob.StagedMode,
		Options:     originalJob.Options,
		RedoOfJobID: &jobID,
		Progress: Progress{
			Phase:      "created",
			TotalSteps: 0,
			StartTime:  time.Now(),
		},
		CreatedAt: time.Now(),
	}

	if err := s.db.WithContext(ctx).Create(newJob).Error; err != nil {
		return nil, fmt.Errorf("create redo job: %w", err)
	}

	s.mu.Lock()
	s.jobs[newJob.ID] = newJob
	s.mu.Unlock()

	s.logger.Info().
		Str("new_job_id", newJob.ID).
		Str("original_job_id", jobID).
		Msg("created redo job")

	return newJob, nil
}

// containsString checks if a string slice contains a specific string.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func sourcePassesStationFilter(sourceID string, stationFilter map[int]struct{}) bool {
	if len(stationFilter) == 0 {
		return true
	}
	stationID, _, err := parseScopedSourceID(sourceID)
	if err != nil {
		// Keep unscoped IDs selectable.
		return true
	}
	_, ok := stationFilter[stationID]
	return ok
}
