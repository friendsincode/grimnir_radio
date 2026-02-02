/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import "time"

// ScheduleTemplate stores a reusable schedule pattern (typically a week).
type ScheduleTemplate struct {
	ID           string         `gorm:"type:uuid;primaryKey" json:"id"`
	StationID    string         `gorm:"type:uuid;index:idx_schedule_templates_station;not null" json:"station_id"`
	Name         string         `gorm:"type:varchar(255);not null" json:"name"`
	Description  string         `gorm:"type:text" json:"description,omitempty"`
	TemplateData map[string]any `gorm:"type:jsonb;serializer:json;not null" json:"template_data"` // Serialized week of shows/entries

	// Creator
	CreatedByID *string `gorm:"type:uuid" json:"created_by_id,omitempty"`
	CreatedBy   *User   `gorm:"foreignKey:CreatedByID" json:"created_by,omitempty"`

	// Relationships
	Station *Station `gorm:"foreignKey:StationID" json:"station,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for GORM.
func (ScheduleTemplate) TableName() string {
	return "schedule_templates"
}

// TemplateEntry represents a single entry in a schedule template.
// This is stored within TemplateData.
type TemplateEntry struct {
	DayOfWeek       int            `json:"day_of_week"` // 0=Sunday, 6=Saturday
	StartTime       string         `json:"start_time"`  // HH:MM format
	DurationMinutes int            `json:"duration_minutes"`
	SourceType      string         `json:"source_type"` // media, playlist, smart_block, etc.
	SourceID        string         `json:"source_id,omitempty"`
	ShowID          string         `json:"show_id,omitempty"` // If from a show
	ShowName        string         `json:"show_name,omitempty"`
	Title           string         `json:"title"` // Display title
	Metadata        map[string]any `json:"metadata,omitempty"`
}

// ScheduleVersion stores a snapshot of the schedule for history/rollback.
type ScheduleVersion struct {
	ID            string         `gorm:"type:uuid;primaryKey" json:"id"`
	StationID     string         `gorm:"type:uuid;index:idx_schedule_versions_station;not null" json:"station_id"`
	VersionNumber int            `gorm:"not null" json:"version_number"`
	SnapshotData  map[string]any `gorm:"type:jsonb;serializer:json;not null" json:"snapshot_data"` // Full schedule state
	ChangeSummary string         `gorm:"type:text" json:"change_summary,omitempty"`
	ChangeType    string         `gorm:"type:varchar(64)" json:"change_type,omitempty"` // create, update, delete, bulk, apply_template, restore

	// Who made the change
	ChangedByID *string `gorm:"type:uuid" json:"changed_by_id,omitempty"`
	ChangedBy   *User   `gorm:"foreignKey:ChangedByID" json:"changed_by,omitempty"`

	// Relationships
	Station *Station `gorm:"foreignKey:StationID" json:"station,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// TableName returns the table name for GORM.
func (ScheduleVersion) TableName() string {
	return "schedule_versions"
}

// VersionSnapshotEntry represents a single entry in a version snapshot.
type VersionSnapshotEntry struct {
	ID         string         `json:"id"`
	StartsAt   time.Time      `json:"starts_at"`
	EndsAt     time.Time      `json:"ends_at"`
	SourceType string         `json:"source_type"`
	SourceID   string         `json:"source_id,omitempty"`
	MountID    string         `json:"mount_id,omitempty"`
	Title      string         `json:"title"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// VersionDiff represents the difference between two versions.
type VersionDiff struct {
	FromVersion int                    `json:"from_version"`
	ToVersion   int                    `json:"to_version"`
	Added       []VersionSnapshotEntry `json:"added"`
	Removed     []VersionSnapshotEntry `json:"removed"`
	Modified    []VersionDiffEntry     `json:"modified"`
}

// VersionDiffEntry represents a modified entry between versions.
type VersionDiffEntry struct {
	ID      string               `json:"id"`
	Before  VersionSnapshotEntry `json:"before"`
	After   VersionSnapshotEntry `json:"after"`
	Changes map[string]any       `json:"changes"` // Field names that changed
}
