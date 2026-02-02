/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import "time"

// ShowInstanceStatus defines the status of a show instance.
type ShowInstanceStatus string

const (
	ShowInstanceScheduled ShowInstanceStatus = "scheduled"
	ShowInstanceCancelled ShowInstanceStatus = "cancelled"
	ShowInstanceCompleted ShowInstanceStatus = "completed"
)

// ShowExceptionType defines types of exceptions to recurring shows.
type ShowExceptionType string

const (
	ShowExceptionCancelled   ShowExceptionType = "cancelled"
	ShowExceptionRescheduled ShowExceptionType = "rescheduled"
	ShowExceptionSubstitute  ShowExceptionType = "substitute"
)

// Show represents a recurring show definition with RRULE support.
type Show struct {
	ID                     string `gorm:"type:uuid;primaryKey"`
	StationID              string `gorm:"type:uuid;index:idx_shows_station;not null"`
	Name                   string `gorm:"type:varchar(255);not null"`
	Description            string `gorm:"type:text"`
	ArtworkPath            string `gorm:"type:varchar(512)"`
	HostUserID             *string `gorm:"type:uuid;index:idx_shows_host"`
	DefaultDurationMinutes int     `gorm:"not null;default:60"`
	Color                  string  `gorm:"type:varchar(7)"` // hex color for calendar (e.g., "#FF5733")

	// Recurrence (RFC 5545 RRULE)
	RRule    string     `gorm:"type:text"`                             // e.g., "FREQ=WEEKLY;BYDAY=MO;BYHOUR=19"
	DTStart  time.Time  `gorm:"not null"`                              // First occurrence
	DTEnd    *time.Time `gorm:"index:idx_shows_dtend"`                 // End of recurrence (NULL = forever)
	Timezone string     `gorm:"type:varchar(64);not null;default:'UTC'"` // IANA timezone name

	Active   bool           `gorm:"not null;default:true"`
	Metadata map[string]any `gorm:"type:jsonb;serializer:json"`

	// Relationships
	Station   *Station `gorm:"foreignKey:StationID"`
	Host      *User    `gorm:"foreignKey:HostUserID"`
	Instances []ShowInstance `gorm:"foreignKey:ShowID"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the table name for GORM.
func (Show) TableName() string {
	return "shows"
}

// ShowInstance represents a materialized instance of a recurring show.
type ShowInstance struct {
	ID        string `gorm:"type:uuid;primaryKey"`
	ShowID    string `gorm:"type:uuid;index:idx_show_instances_show;not null"`
	StationID string `gorm:"type:uuid;index:idx_show_instances_station_time;not null"`
	StartsAt  time.Time `gorm:"index:idx_show_instances_station_time;not null"`
	EndsAt    time.Time `gorm:"not null"`

	// Host can override show default
	HostUserID *string `gorm:"type:uuid;index:idx_show_instances_host"`

	Status ShowInstanceStatus `gorm:"type:varchar(32);not null;default:'scheduled'"`

	// Exception handling
	ExceptionType ShowExceptionType `gorm:"type:varchar(32)"`
	ExceptionNote string            `gorm:"type:text"`

	Metadata map[string]any `gorm:"type:jsonb;serializer:json"`

	// Relationships
	Show    *Show    `gorm:"foreignKey:ShowID"`
	Station *Station `gorm:"foreignKey:StationID"`
	Host    *User    `gorm:"foreignKey:HostUserID"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the table name for GORM.
func (ShowInstance) TableName() string {
	return "show_instances"
}

// IsCancelled returns true if this instance is cancelled.
func (si *ShowInstance) IsCancelled() bool {
	return si.Status == ShowInstanceCancelled || si.ExceptionType == ShowExceptionCancelled
}

// IsException returns true if this is an exception to the regular schedule.
func (si *ShowInstance) IsException() bool {
	return si.ExceptionType != ""
}
