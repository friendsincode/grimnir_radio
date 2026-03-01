/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"fmt"
	"time"
)

// OrphanMedia represents a media file found on disk but not in the database.
// This typically happens after a database reset when files remain on disk.
type OrphanMedia struct {
	ID             string     `gorm:"type:uuid;primaryKey" json:"id"`
	FilePath       string     `gorm:"type:text;not null;uniqueIndex" json:"file_path"` // Relative path on disk
	ContentHash    string     `gorm:"type:varchar(64);index" json:"content_hash"`      // SHA-256 for matching
	FileSize       int64      `gorm:"not null" json:"file_size"`
	DetectedAt     time.Time  `gorm:"not null" json:"detected_at"`
	FileModifiedAt *time.Time `json:"file_modified_at"`

	// Extracted metadata (best effort from filename or ID3 tags)
	Title    string        `gorm:"type:varchar(255)" json:"title,omitempty"`
	Artist   string        `gorm:"type:varchar(255)" json:"artist,omitempty"`
	Album    string        `gorm:"type:varchar(255)" json:"album,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for GORM.
func (OrphanMedia) TableName() string {
	return "orphan_media"
}

// ScanResult holds the results of an orphan media scan.
type ScanResult struct {
	TotalFiles   int           `json:"total_files"`
	NewOrphans   int           `json:"new_orphans"`
	AlreadyKnown int           `json:"already_known"`
	Errors       int           `json:"errors"`
	Duration     time.Duration `json:"duration"`
	TotalSize    int64         `json:"total_size"`
}

// FormatSize returns a human-readable size string.
func (r *ScanResult) FormatSize() string {
	return formatBytes(r.TotalSize)
}

// formatBytes converts bytes to human-readable format.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), []string{"KB", "MB", "GB", "TB"}[exp])
}
