package migration

import (
	"time"
)

// MigrationStatus represents the state of a migration
type MigrationStatus string

const (
	MigrationStatusPending   MigrationStatus = "pending"
	MigrationStatusRunning   MigrationStatus = "running"
	MigrationStatusCompleted MigrationStatus = "completed"
	MigrationStatusFailed    MigrationStatus = "failed"
)

// MigrationType represents the source system being migrated from
type MigrationType string

const (
	MigrationTypeAzuraCast  MigrationType = "azuracast"
	MigrationTypeLibreTime  MigrationType = "libretime"
)

// Migration tracks a migration job
type Migration struct {
	ID          string          `json:"id"`
	Type        MigrationType   `json:"type"`
	Status      MigrationStatus `json:"status"`
	Progress    int             `json:"progress"` // 0-100
	TotalSteps  int             `json:"total_steps"`
	CurrentStep int             `json:"current_step"`
	StepName    string          `json:"step_name"`
	Error       string          `json:"error,omitempty"`
	DryRun      bool            `json:"dry_run"`
	StartedAt   time.Time       `json:"started_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	Stats       MigrationStats  `json:"stats"`
}

// MigrationStats contains statistics about what was migrated
type MigrationStats struct {
	StationsImported     int `json:"stations_imported"`
	MountsImported       int `json:"mounts_imported"`
	MediaImported        int `json:"media_imported"`
	PlaylistsImported    int `json:"playlists_imported"`
	SchedulesImported    int `json:"schedules_imported"`
	UsersImported        int `json:"users_imported"`
	ErrorsEncountered    int `json:"errors_encountered"`
}

// MigrationOptions contains common migration options
type MigrationOptions struct {
	DryRun              bool   `json:"dry_run"`
	SkipMedia           bool   `json:"skip_media"`
	MediaCopyMethod     string `json:"media_copy_method"` // "copy", "symlink", "none"
	OverwriteExisting   bool   `json:"overwrite_existing"`
	DefaultStationOwner string `json:"default_station_owner"`
}

// ProgressCallback is called to report migration progress
type ProgressCallback func(step int, total int, message string)
