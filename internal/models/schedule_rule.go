/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import "time"

// RuleType defines the type of schedule validation rule.
type RuleType string

const (
	// Built-in rules (always active)
	RuleTypeOverlap RuleType = "overlap" // Two shows at same time

	// Configurable rules
	RuleTypeGap             RuleType = "gap"                 // Unscheduled time exceeds threshold
	RuleTypeDJDoubleBooking RuleType = "dj_double_booking"   // Same DJ on multiple stations
	RuleTypeStationID       RuleType = "station_id_interval" // Station ID required every N minutes
	RuleTypeContentRestrict RuleType = "content_restriction" // Content only in specific dayparts
	RuleTypeMinDuration     RuleType = "min_duration"        // Show must be at least N minutes
	RuleTypeMaxDuration     RuleType = "max_duration"        // Show cannot exceed N minutes
	RuleTypeRequiredBreak   RuleType = "required_break"      // Must have break at specific times
	RuleTypeMaxConsecutive  RuleType = "max_consecutive"     // Max consecutive hours for a DJ
)

// RuleSeverity defines how serious a rule violation is.
type RuleSeverity string

const (
	RuleSeverityError   RuleSeverity = "error"   // Must be fixed
	RuleSeverityWarning RuleSeverity = "warning" // Should be reviewed
	RuleSeverityInfo    RuleSeverity = "info"    // Informational only
)

// ScheduleRule defines a validation rule for the schedule.
type ScheduleRule struct {
	ID        string         `gorm:"type:uuid;primaryKey"`
	StationID string         `gorm:"type:uuid;index:idx_schedule_rules_station;not null"`
	Name      string         `gorm:"type:varchar(255);not null"`
	RuleType  RuleType       `gorm:"type:varchar(64);not null"`
	Config    map[string]any `gorm:"type:jsonb;serializer:json;not null"` // Rule-specific config
	Severity  RuleSeverity   `gorm:"type:varchar(32);not null;default:'warning'"`
	Active    bool           `gorm:"not null;default:true"`

	// Relationships
	Station *Station `gorm:"foreignKey:StationID"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the table name for GORM.
func (ScheduleRule) TableName() string {
	return "schedule_rules"
}

// ValidationViolation represents a single rule violation.
type ValidationViolation struct {
	RuleID      string         `json:"rule_id,omitempty"`
	RuleType    RuleType       `json:"rule_type"`
	RuleName    string         `json:"rule_name"`
	Severity    RuleSeverity   `json:"severity"`
	Message     string         `json:"message"`
	StartsAt    time.Time      `json:"starts_at"`
	EndsAt      time.Time      `json:"ends_at"`
	AffectedIDs []string       `json:"affected_ids,omitempty"` // Show/instance IDs involved
	Details     map[string]any `json:"details,omitempty"`
}

// ValidationResult contains the result of validating a schedule range.
type ValidationResult struct {
	Valid      bool                  `json:"valid"`    // True if no errors (warnings OK)
	Errors     []ValidationViolation `json:"errors"`   // Severity = error
	Warnings   []ValidationViolation `json:"warnings"` // Severity = warning
	Info       []ValidationViolation `json:"info"`     // Severity = info
	CheckedAt  time.Time             `json:"checked_at"`
	RangeStart time.Time             `json:"range_start"`
	RangeEnd   time.Time             `json:"range_end"`
}

// GapRuleConfig is the configuration for gap detection rules.
type GapRuleConfig struct {
	MaxGapMinutes int   `json:"max_gap_minutes"` // Maximum allowed gap
	IgnoreHours   []int `json:"ignore_hours"`    // Hours to ignore (e.g., overnight)
	IgnoreDays    []int `json:"ignore_days"`     // Days to ignore (0=Sunday)
}

// DurationRuleConfig is the configuration for min/max duration rules.
type DurationRuleConfig struct {
	Minutes int `json:"minutes"` // Duration in minutes
}

// StationIDRuleConfig is the configuration for station ID interval rules.
type StationIDRuleConfig struct {
	IntervalMinutes int `json:"interval_minutes"` // Must play station ID every N minutes
}

// ContentRestrictionConfig is the configuration for content restriction rules.
type ContentRestrictionConfig struct {
	RestrictedTags []string `json:"restricted_tags"` // Tags that are restricted
	AllowedHours   []int    `json:"allowed_hours"`   // Hours when content IS allowed
	AllowedDays    []int    `json:"allowed_days"`    // Days when content IS allowed (0=Sunday)
}

// RequiredBreakConfig is the configuration for required break rules.
type RequiredBreakConfig struct {
	Hours           []int `json:"hours"`             // Hours when break is required (e.g., [12] for noon)
	MinBreakMinutes int   `json:"min_break_minutes"` // Minimum break duration
}

// MaxConsecutiveConfig is the configuration for max consecutive hours rules.
type MaxConsecutiveConfig struct {
	MaxHours int `json:"max_hours"` // Maximum consecutive hours for same host
}
