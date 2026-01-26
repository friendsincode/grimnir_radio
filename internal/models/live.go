/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package models

import (
	"time"
)

// LiveSession represents an active live DJ session.
type LiveSession struct {
	ID          string    `gorm:"type:uuid;primaryKey"`
	StationID   string    `gorm:"type:uuid;index:idx_live_session_station_active"`
	MountID     string    `gorm:"type:uuid;index"`
	UserID      string    `gorm:"type:uuid;index"` // DJ user ID
	Username    string    `gorm:"type:varchar(255)"`
	Priority    PriorityLevel `gorm:"type:int"`    // Live override (1) or scheduled (2)

	// Connection details
	SourceIP    string    `gorm:"type:varchar(45)"` // IPv4/IPv6 address
	SourcePort  int       `gorm:"type:int"`
	UserAgent   string    `gorm:"type:varchar(255)"`

	// Session state
	Active      bool      `gorm:"index:idx_live_session_station_active"`
	ConnectedAt time.Time
	DisconnectedAt *time.Time // NULL if still connected

	// Authorization token (one-time use)
	Token       string    `gorm:"type:varchar(255);uniqueIndex"`
	TokenUsed   bool      `gorm:"default:false"`

	// Metadata
	Metadata    map[string]any `gorm:"serializer:json"`

	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TableName overrides for GORM.
func (LiveSession) TableName() string {
	return "live_sessions"
}

// IsActive checks if this live session is currently active.
func (ls *LiveSession) IsActive() bool {
	return ls.Active && ls.DisconnectedAt == nil
}

// Disconnect marks a live session as disconnected.
func (ls *LiveSession) Disconnect() {
	now := time.Now()
	ls.Active = false
	ls.DisconnectedAt = &now
}

// Duration calculates the session duration.
func (ls *LiveSession) Duration() time.Duration {
	if ls.DisconnectedAt != nil {
		return ls.DisconnectedAt.Sub(ls.ConnectedAt)
	}
	return time.Since(ls.ConnectedAt)
}
