/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"time"
)

// Recording status constants.
const (
	RecordingStatusActive     = "active"
	RecordingStatusFinalizing = "finalizing"
	RecordingStatusComplete   = "complete"
	RecordingStatusFailed     = "failed"
)

// Recording visibility constants.
const (
	RecordingVisibilityDefault = "default"
	RecordingVisibilityPublic  = "public"
	RecordingVisibilityPrivate = "private"
)

// Recording format constants.
const (
	RecordingFormatFLAC = "flac"
	RecordingFormatOpus = "opus"
)

// Recording represents a recorded live session or scheduled broadcast.
type Recording struct {
	ID             string  `gorm:"type:uuid;primaryKey"`
	StationID      string  `gorm:"type:uuid;index;not null"`
	LiveSessionID  *string `gorm:"type:uuid;index"`
	UserID         string  `gorm:"type:uuid;index;not null"`
	ShowInstanceID *string `gorm:"type:uuid;index"`
	MountID        string  `gorm:"type:uuid;index"`

	Title       string `gorm:"type:varchar(255)"`
	Description string `gorm:"type:text"`

	// File storage (relative path, like media items)
	Path   string `gorm:"type:varchar(512)"`
	Format string `gorm:"type:varchar(8);not null;default:'flac'"` // flac or opus

	// File metadata
	SizeBytes  int64 `gorm:"not null;default:0"`
	DurationMs int64 `gorm:"not null;default:0"`
	SampleRate int   `gorm:"not null;default:44100"`
	Channels   int   `gorm:"not null;default:2"`

	// State
	Status     string `gorm:"type:varchar(16);not null;default:'active';index"`
	Visibility string `gorm:"type:varchar(16);not null;default:'default'"`

	AllowDownload bool `gorm:"not null;default:true"`

	// Chapters
	Chapters []RecordingChapter `gorm:"foreignKey:RecordingID"`

	// Timestamps
	StartedAt time.Time
	StoppedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// RecordingChapter marks a point in a recording where the content changed
// (e.g., a new song started playing during a live session).
type RecordingChapter struct {
	ID          string `gorm:"type:uuid;primaryKey"`
	RecordingID string `gorm:"type:uuid;index;not null"`
	Position    int    `gorm:"not null;default:0"` // Sequential chapter number
	OffsetMs    int64  `gorm:"not null;default:0"` // Offset from recording start

	Title  string `gorm:"type:varchar(255)"`
	Artist string `gorm:"type:varchar(255)"`
	Album  string `gorm:"type:varchar(255)"`

	CreatedAt time.Time
}

// IsActive returns true if the recording is currently in progress.
func (r *Recording) IsActive() bool {
	return r.Status == RecordingStatusActive
}

// IsComplete returns true if the recording finished successfully.
func (r *Recording) IsComplete() bool {
	return r.Status == RecordingStatusComplete
}

// Duration returns the recording duration.
func (r *Recording) Duration() time.Duration {
	return time.Duration(r.DurationMs) * time.Millisecond
}
