/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"time"

	"gorm.io/gorm"
)

// SystemSettings stores runtime-configurable platform settings.
// Uses singleton pattern with a fixed ID=1 row.
type SystemSettings struct {
	ID                 int    `gorm:"primaryKey"`
	SchedulerLookahead string `gorm:"type:varchar(16);default:'48h'"`
	AnalysisEnabled    bool   `gorm:"default:true"`
	WebsocketEnabled   bool   `gorm:"default:true"`
	MetricsEnabled     bool   `gorm:"default:true"`
	LogLevel           string `gorm:"type:varchar(16);default:'info'"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// TableName returns the table name for GORM.
func (SystemSettings) TableName() string {
	return "system_settings"
}

// ValidSchedulerLookaheads contains the allowed values for scheduler lookahead.
var ValidSchedulerLookaheads = []string{"24h", "48h", "72h", "168h"}

// ValidLogLevels contains the allowed values for log level.
var ValidLogLevels = []string{"debug", "info", "warn", "error"}

// IsValidSchedulerLookahead checks if a value is a valid scheduler lookahead.
func IsValidSchedulerLookahead(val string) bool {
	for _, v := range ValidSchedulerLookaheads {
		if v == val {
			return true
		}
	}
	return false
}

// IsValidLogLevel checks if a value is a valid log level.
func IsValidLogLevel(val string) bool {
	for _, v := range ValidLogLevels {
		if v == val {
			return true
		}
	}
	return false
}

// GetSystemSettings retrieves the singleton settings row, creating it if it doesn't exist.
func GetSystemSettings(db *gorm.DB) (*SystemSettings, error) {
	var settings SystemSettings
	result := db.FirstOrCreate(&settings, SystemSettings{ID: 1})
	if result.Error != nil {
		return nil, result.Error
	}
	return &settings, nil
}
