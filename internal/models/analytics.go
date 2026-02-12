/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"time"
)

// NilUUIDString is a valid UUID literal used as a sentinel "no UUID" value in places where
// we want NOT NULL uuid columns but still need a "none" representation (e.g. station-scope rollups).
const NilUUIDString = "00000000-0000-0000-0000-000000000000"

// ListenerSample stores time-series listener snapshots for a station.
type ListenerSample struct {
	ID         string    `gorm:"type:uuid;primaryKey" json:"id"`
	StationID  string    `gorm:"type:uuid;index;not null" json:"station_id"`
	Listeners  int       `gorm:"not null" json:"listeners"`
	CapturedAt time.Time `gorm:"index;not null" json:"captured_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// TableName returns the table name for GORM.
func (ListenerSample) TableName() string {
	return "listener_samples"
}

// ScheduleAnalytics stores aggregated listener data per hour/show.
type ScheduleAnalytics struct {
	ID            string    `gorm:"type:uuid;primaryKey" json:"id"`
	StationID     string    `gorm:"type:uuid;index;not null" json:"station_id"`
	ShowID        *string   `gorm:"type:uuid;index" json:"show_id,omitempty"`
	InstanceID    *string   `gorm:"type:uuid;index" json:"instance_id,omitempty"`
	Date          time.Time `gorm:"type:date;index;not null" json:"date"`
	Hour          int       `gorm:"not null" json:"hour"` // 0-23
	AvgListeners  int       `json:"avg_listeners"`
	PeakListeners int       `json:"peak_listeners"`
	TuneIns       int       `json:"tune_ins"`
	TuneOuts      int       `json:"tune_outs"`
	TotalMinutes  int       `json:"total_minutes"` // Total listener-minutes

	// Relationships
	Station  *Station      `gorm:"foreignKey:StationID" json:"station,omitempty"`
	Show     *Show         `gorm:"foreignKey:ShowID" json:"show,omitempty"`
	Instance *ShowInstance `gorm:"foreignKey:InstanceID" json:"instance,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// TableName returns the table name for GORM.
func (ScheduleAnalytics) TableName() string {
	return "schedule_analytics"
}

// ScheduleAnalyticsDaily stores daily rollups derived from schedule_analytics.
// Scope determines whether the row is a station summary or a show summary.
// For station summaries, ShowID is NilUUIDString.
type ScheduleAnalyticsDaily struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	StationID string    `gorm:"type:uuid;not null;uniqueIndex:idx_schedule_analytics_daily,priority:1" json:"station_id"`
	Date      time.Time `gorm:"type:date;not null;uniqueIndex:idx_schedule_analytics_daily,priority:2" json:"date"`
	Scope     string    `gorm:"type:varchar(16);not null;uniqueIndex:idx_schedule_analytics_daily,priority:3" json:"scope"` // "station"|"show"
	ShowID    string    `gorm:"type:uuid;not null;default:'00000000-0000-0000-0000-000000000000';uniqueIndex:idx_schedule_analytics_daily,priority:4" json:"show_id,omitempty"`

	InstanceCount        int     `json:"instance_count"`
	AvgListeners         float64 `json:"avg_listeners"`
	PeakListeners        int     `json:"peak_listeners"`
	TuneIns              int     `json:"tune_ins"`
	TuneOuts             int     `json:"tune_outs"`
	TotalListenerMinutes int     `json:"total_listener_minutes"`
	HoursCovered         int     `json:"hours_covered"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for GORM.
func (ScheduleAnalyticsDaily) TableName() string {
	return "schedule_analytics_daily"
}

// ShowPerformance represents aggregated performance metrics for a show.
type ShowPerformance struct {
	ShowID        string  `json:"show_id"`
	ShowName      string  `json:"show_name"`
	InstanceCount int     `json:"instance_count"`
	AvgListeners  float64 `json:"avg_listeners"`
	PeakListeners int     `json:"peak_listeners"`
	TotalTuneIns  int     `json:"total_tune_ins"`
	TotalMinutes  int     `json:"total_listener_minutes"`
	TrendPercent  float64 `json:"trend_percent"` // Change vs previous period
}

// TimeSlotPerformance represents performance metrics for a time slot.
type TimeSlotPerformance struct {
	DayOfWeek     int     `json:"day_of_week"` // 0=Sunday, 6=Saturday
	Hour          int     `json:"hour"`        // 0-23
	AvgListeners  float64 `json:"avg_listeners"`
	PeakListeners int     `json:"peak_listeners"`
	SampleCount   int     `json:"sample_count"`
}

// SchedulingSuggestion represents a data-driven scheduling suggestion.
type SchedulingSuggestion struct {
	Type          string  `json:"type"` // "move_show", "extend_show", "reduce_show", "add_show"
	ShowID        string  `json:"show_id,omitempty"`
	ShowName      string  `json:"show_name,omitempty"`
	CurrentSlot   string  `json:"current_slot,omitempty"`
	SuggestedSlot string  `json:"suggested_slot,omitempty"`
	Reason        string  `json:"reason"`
	Impact        string  `json:"impact"`     // Expected impact description
	Confidence    float64 `json:"confidence"` // 0-1 confidence score
}
