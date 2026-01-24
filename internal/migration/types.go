/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package migration

import (
	"context"
	"time"
)

// JobStatus represents the current state of a migration job.
type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusRunning    JobStatus = "running"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
	JobStatusCancelled  JobStatus = "cancelled"
	JobStatusValidating JobStatus = "validating"
)

// SourceType represents the type of system being migrated from.
type SourceType string

const (
	SourceTypeAzuraCast  SourceType = "azuracast"
	SourceTypeLibreTime  SourceType = "libretime"
	SourceTypeAirtime    SourceType = "airtime"
	SourceTypeLiquidsoap SourceType = "liquidsoap"
)

// Job represents a migration job.
type Job struct {
	ID          string     `json:"id" gorm:"primaryKey"`
	SourceType  SourceType `json:"source_type" gorm:"type:varchar(50);not null"`
	Status      JobStatus  `json:"status" gorm:"type:varchar(50);not null;default:'pending'"`
	DryRun      bool       `json:"dry_run" gorm:"not null;default:false"`
	Options     Options    `json:"options" gorm:"type:jsonb"`
	Progress    Progress   `json:"progress" gorm:"type:jsonb"`
	Result      *Result    `json:"result,omitempty" gorm:"type:jsonb"`
	Error       string     `json:"error,omitempty" gorm:"type:text"`
	CreatedAt   time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Options contains migration-specific configuration.
type Options struct {
	// Common options
	SkipMedia     bool `json:"skip_media"`
	SkipSchedules bool `json:"skip_schedules"`
	SkipPlaylists bool `json:"skip_playlists"`
	SkipUsers     bool `json:"skip_users"`

	// AzuraCast options
	AzuraCastBackupPath string `json:"azuracast_backup_path,omitempty"`
	AzuraCastDBType     string `json:"azuracast_db_type,omitempty"` // mysql, postgres

	// LibreTime options
	LibreTimeDBHost     string `json:"libretime_db_host,omitempty"`
	LibreTimeDBPort     int    `json:"libretime_db_port,omitempty"`
	LibreTimeDBName     string `json:"libretime_db_name,omitempty"`
	LibreTimeDBUser     string `json:"libretime_db_user,omitempty"`
	LibreTimeDBPassword string `json:"libretime_db_password,omitempty"`
	LibreTimeMediaPath  string `json:"libretime_media_path,omitempty"`

	// Target options
	TargetStationID string            `json:"target_station_id,omitempty"`
	FieldMappings   map[string]string `json:"field_mappings,omitempty"`
}

// Progress tracks migration progress.
type Progress struct {
	Phase             string    `json:"phase"`
	TotalSteps        int       `json:"total_steps"`
	CompletedSteps    int       `json:"completed_steps"`
	CurrentStep       string    `json:"current_step"`
	StationsTotal     int       `json:"stations_total"`
	StationsImported  int       `json:"stations_imported"`
	MediaTotal        int       `json:"media_total"`
	MediaImported     int       `json:"media_imported"`
	MediaCopied       int       `json:"media_copied"`
	PlaylistsTotal    int       `json:"playlists_total"`
	PlaylistsImported int       `json:"playlists_imported"`
	SchedulesTotal    int       `json:"schedules_total"`
	SchedulesImported int       `json:"schedules_imported"`
	UsersTotal        int       `json:"users_total"`
	UsersImported     int       `json:"users_imported"`
	Percentage        float64   `json:"percentage"`
	EstimatedRemaining string   `json:"estimated_remaining,omitempty"`
	StartTime         time.Time `json:"start_time"`
}

// Result contains the final migration results.
type Result struct {
	StationsCreated    int                `json:"stations_created"`
	MediaItemsImported int                `json:"media_items_imported"`
	PlaylistsCreated   int                `json:"playlists_created"`
	SchedulesCreated   int                `json:"schedules_created"`
	UsersCreated       int                `json:"users_created"`
	Warnings           []string           `json:"warnings,omitempty"`
	Skipped            map[string]int     `json:"skipped,omitempty"`
	Mappings           map[string]Mapping `json:"mappings,omitempty"`
	DurationSeconds    float64            `json:"duration_seconds"`
}

// Mapping tracks ID mappings from source to target system.
type Mapping struct {
	OldID   string `json:"old_id"`
	NewID   string `json:"new_id"`
	Type    string `json:"type"` // station, media, playlist, etc.
	Name    string `json:"name"`
	Skipped bool   `json:"skipped,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// Importer defines the interface for migration importers.
type Importer interface {
	// Validate checks if the migration can proceed with the given options.
	Validate(ctx context.Context, options Options) error

	// Analyze performs a dry-run analysis without making changes.
	Analyze(ctx context.Context, options Options) (*Result, error)

	// Import performs the actual migration.
	Import(ctx context.Context, options Options, progressCallback ProgressCallback) (*Result, error)
}

// ProgressCallback is called during migration to report progress.
type ProgressCallback func(progress Progress)

// ValidationError represents a validation error with details.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// ValidationErrors represents multiple validation errors.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return "validation failed"
	}
	return e[0].Error()
}
