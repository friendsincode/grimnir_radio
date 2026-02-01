/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"time"
)

// Webstream represents a relayed HTTP/ICY stream with failover support.
type Webstream struct {
	ID          string `gorm:"type:uuid;primaryKey"`
	StationID   string `gorm:"type:uuid;index"`
	Name        string `gorm:"type:varchar(255)"`
	Description string `gorm:"type:text"`

	// URLs for failover chain (primary -> backup -> backup2, etc.)
	URLs []string `gorm:"serializer:json"`

	// Health check configuration
	// Note: Defaults are set in CreateWebstream, not GORM tags, to allow explicit false values
	HealthCheckEnabled  bool          `gorm:""`
	HealthCheckInterval time.Duration `gorm:"type:bigint"` // Stored as nanoseconds
	HealthCheckTimeout  time.Duration `gorm:"type:bigint"`
	HealthCheckMethod   string        `gorm:"type:varchar(10);default:'HEAD'"` // HEAD or GET

	// Failover settings
	FailoverEnabled    bool `gorm:""`
	FailoverGraceMs    int  `gorm:"type:int;default:5000"` // Grace period before failover
	AutoRecoverEnabled bool `gorm:""`                      // Auto-recover to primary when healthy

	// Connection settings
	PreflightCheck       bool `gorm:""` // Test connection before scheduling
	BufferSizeMS         int  `gorm:"type:int;default:2000"`
	ReconnectDelayMS     int  `gorm:"type:int;default:1000"`
	MaxReconnectAttempts int  `gorm:"type:int;default:5"`

	// Metadata
	PassthroughMetadata bool           `gorm:"default:true"`  // Pass through ICY metadata
	OverrideMetadata    bool           `gorm:"default:false"` // Override with custom metadata
	CustomMetadata      map[string]any `gorm:"serializer:json"`

	// State tracking
	Active          bool   `gorm:"index"`
	CurrentURL      string `gorm:"type:text"` // Currently active URL
	CurrentIndex    int    `gorm:"type:int"`  // Index in URLs array
	LastHealthCheck *time.Time
	HealthStatus    string `gorm:"type:varchar(50)"` // healthy, degraded, unhealthy

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName overrides for GORM.
func (Webstream) TableName() string {
	return "webstreams"
}

// IsHealthy checks if the webstream is currently healthy.
func (ws *Webstream) IsHealthy() bool {
	return ws.HealthStatus == "healthy"
}

// GetPrimaryURL returns the primary (first) URL in the failover chain.
func (ws *Webstream) GetPrimaryURL() string {
	if len(ws.URLs) == 0 {
		return ""
	}
	return ws.URLs[0]
}

// GetCurrentURL returns the currently active URL.
func (ws *Webstream) GetCurrentURL() string {
	if ws.CurrentURL != "" {
		return ws.CurrentURL
	}
	return ws.GetPrimaryURL()
}

// GetNextFailoverURL returns the next URL in the failover chain.
func (ws *Webstream) GetNextFailoverURL() string {
	if !ws.FailoverEnabled || len(ws.URLs) <= 1 {
		return ""
	}

	nextIndex := ws.CurrentIndex + 1
	if nextIndex >= len(ws.URLs) {
		// Wrap around to primary if auto-recover enabled
		if ws.AutoRecoverEnabled {
			return ws.URLs[0]
		}
		return "" // No more failover options
	}

	return ws.URLs[nextIndex]
}

// FailoverToNext advances to the next URL in the failover chain.
func (ws *Webstream) FailoverToNext() bool {
	next := ws.GetNextFailoverURL()
	if next == "" {
		return false
	}

	for i, url := range ws.URLs {
		if url == next {
			ws.CurrentURL = url
			ws.CurrentIndex = i
			return true
		}
	}

	return false
}

// ResetToPrimary resets the webstream to use the primary URL.
func (ws *Webstream) ResetToPrimary() {
	if len(ws.URLs) > 0 {
		ws.CurrentURL = ws.URLs[0]
		ws.CurrentIndex = 0
	}
}

// MarkHealthy marks the webstream as healthy.
func (ws *Webstream) MarkHealthy() {
	now := time.Now()
	ws.HealthStatus = "healthy"
	ws.LastHealthCheck = &now
}

// MarkUnhealthy marks the webstream as unhealthy.
func (ws *Webstream) MarkUnhealthy() {
	now := time.Now()
	ws.HealthStatus = "unhealthy"
	ws.LastHealthCheck = &now
}

// MarkDegraded marks the webstream as degraded (some issues but still functional).
func (ws *Webstream) MarkDegraded() {
	now := time.Now()
	ws.HealthStatus = "degraded"
	ws.LastHealthCheck = &now
}
