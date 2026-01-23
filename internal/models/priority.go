/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package models

import (
	"time"
)

// PriorityLevel defines the 5-tier priority system for audio sources.
type PriorityLevel int

const (
	// PriorityEmergency (0) - EAS, emergency broadcasts, highest priority
	PriorityEmergency PriorityLevel = 0

	// PriorityLiveOverride (1) - Manual DJ override, preempts automation
	PriorityLiveOverride PriorityLevel = 1

	// PriorityLiveScheduled (2) - Scheduled live shows
	PriorityLiveScheduled PriorityLevel = 2

	// PriorityAutomation (3) - Normal automated playout from schedule
	PriorityAutomation PriorityLevel = 3

	// PriorityFallback (4) - Safety/filler content when nothing else available
	PriorityFallback PriorityLevel = 4
)

// SourceType enumerates the types of audio sources.
type SourceType string

const (
	SourceTypeMedia     SourceType = "media"      // Single media file
	SourceTypeLive      SourceType = "live"       // Live input stream
	SourceTypeWebstream SourceType = "webstream"  // Remote HTTP stream
	SourceTypeEmergency SourceType = "emergency"  // EAS or emergency content
	SourceTypeFallback  SourceType = "fallback"   // Safety playlist
)

// PrioritySource represents an active audio source with its priority level.
type PrioritySource struct {
	ID         string        `gorm:"type:uuid;primaryKey"`
	StationID  string        `gorm:"type:uuid;index:idx_station_active"`
	MountID    string        `gorm:"type:uuid;index"`
	Priority   PriorityLevel `gorm:"type:int;index:idx_station_active"`
	SourceType SourceType    `gorm:"type:varchar(32)"`
	SourceID   string        `gorm:"type:uuid"` // ID of media, webstream, etc.
	Metadata   map[string]any `gorm:"serializer:json"`
	Active     bool          `gorm:"index:idx_station_active"` // Is this source currently active?
	ActivatedAt time.Time
	DeactivatedAt *time.Time // NULL if still active
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ExecutorStateEnum defines the possible states of an executor.
type ExecutorStateEnum string

const (
	ExecutorStateIdle      ExecutorStateEnum = "idle"       // No content playing
	ExecutorStatePreloading ExecutorStateEnum = "preloading" // Loading next track
	ExecutorStatePlaying   ExecutorStateEnum = "playing"    // Currently playing content
	ExecutorStateFading    ExecutorStateEnum = "fading"     // Crossfading between tracks
	ExecutorStateLive      ExecutorStateEnum = "live"       // Live input active
	ExecutorStateEmergency ExecutorStateEnum = "emergency"  // Emergency content active
)

// ExecutorState tracks the runtime state of a station's executor.
type ExecutorState struct {
	ID              string            `gorm:"type:uuid;primaryKey"`
	StationID       string            `gorm:"type:uuid;uniqueIndex"` // One state per station
	MountID         string            `gorm:"type:uuid"`
	State           ExecutorStateEnum `gorm:"type:varchar(32)"`
	CurrentPriority PriorityLevel     `gorm:"type:int"`
	CurrentSourceID string            `gorm:"type:uuid"` // ID of active PrioritySource
	NextSourceID    string            `gorm:"type:uuid"` // ID of preloaded PrioritySource

	// Telemetry data
	AudioLevelL     float64 `gorm:"type:float"` // Left channel RMS level (-60 to 0 dBFS)
	AudioLevelR     float64 `gorm:"type:float"` // Right channel RMS level
	LoudnessLUFS    float64 `gorm:"type:float"` // Current LUFS measurement
	UnderrunCount   int64   // Buffer underrun events
	LastHeartbeat   time.Time

	// Buffer state
	BufferDepthMS   int64   `gorm:"type:bigint"` // Milliseconds of buffered audio

	// Metadata from currently playing item
	Metadata        map[string]any `gorm:"serializer:json"`

	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TableName overrides for GORM.
func (PrioritySource) TableName() string {
	return "priority_sources"
}

func (ExecutorState) TableName() string {
	return "executor_states"
}

// IsActive checks if a priority source is currently active.
func (ps *PrioritySource) IsActive() bool {
	return ps.Active && ps.DeactivatedAt == nil
}

// Deactivate marks a priority source as inactive.
func (ps *PrioritySource) Deactivate() {
	now := time.Now()
	ps.Active = false
	ps.DeactivatedAt = &now
}

// IsEmergency checks if this is an emergency priority source.
func (ps *PrioritySource) IsEmergency() bool {
	return ps.Priority == PriorityEmergency
}

// IsLive checks if this is any type of live source.
func (ps *PrioritySource) IsLive() bool {
	return ps.SourceType == SourceTypeLive ||
	       ps.Priority == PriorityLiveOverride ||
	       ps.Priority == PriorityLiveScheduled
}

// String returns a human-readable priority level name.
func (pl PriorityLevel) String() string {
	switch pl {
	case PriorityEmergency:
		return "Emergency"
	case PriorityLiveOverride:
		return "Live Override"
	case PriorityLiveScheduled:
		return "Live Scheduled"
	case PriorityAutomation:
		return "Automation"
	case PriorityFallback:
		return "Fallback"
	default:
		return "Unknown"
	}
}

// IsHealthy checks if the executor has received a heartbeat recently.
func (es *ExecutorState) IsHealthy() bool {
	return time.Since(es.LastHeartbeat) < 10*time.Second
}

// IsPlaying checks if the executor is actively playing content.
func (es *ExecutorState) IsPlaying() bool {
	return es.State == ExecutorStatePlaying ||
	       es.State == ExecutorStateFading ||
	       es.State == ExecutorStateLive ||
	       es.State == ExecutorStateEmergency
}
