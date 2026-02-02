/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package scheduling

import (
	"sort"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ScheduleItem represents any scheduled item for validation purposes.
type ScheduleItem struct {
	ID         string
	Type       string // "show_instance", "schedule_entry"
	StationID  string
	StartsAt   time.Time
	EndsAt     time.Time
	HostUserID *string
	SourceType string // For schedule entries
	SourceID   string
	Metadata   map[string]any
}

// Validator validates schedules against rules.
type Validator struct {
	db     *gorm.DB
	logger zerolog.Logger
}

// NewValidator creates a new schedule validator.
func NewValidator(db *gorm.DB, logger zerolog.Logger) *Validator {
	return &Validator{
		db:     db,
		logger: logger.With().Str("component", "scheduler_validator").Logger(),
	}
}

// Validate checks the schedule for a station within a date range.
func (v *Validator) Validate(stationID string, start, end time.Time) (*models.ValidationResult, error) {
	result := &models.ValidationResult{
		Valid:      true,
		Errors:     []models.ValidationViolation{},
		Warnings:   []models.ValidationViolation{},
		Info:       []models.ValidationViolation{},
		CheckedAt:  time.Now(),
		RangeStart: start,
		RangeEnd:   end,
	}

	// Fetch all items in range
	items, err := v.fetchScheduleItems(stationID, start, end)
	if err != nil {
		return nil, err
	}

	// Fetch custom rules for this station
	var rules []models.ScheduleRule
	v.db.Where("station_id = ? AND active = ?", stationID, true).Find(&rules)

	// Always run built-in overlap check
	overlaps := v.checkOverlaps(items)
	for _, violation := range overlaps {
		result.Errors = append(result.Errors, violation)
		result.Valid = false
	}

	// Run custom rules
	for _, rule := range rules {
		violations := v.runRule(rule, items, start, end)
		for _, violation := range violations {
			switch violation.Severity {
			case models.RuleSeverityError:
				result.Errors = append(result.Errors, violation)
				result.Valid = false
			case models.RuleSeverityWarning:
				result.Warnings = append(result.Warnings, violation)
			case models.RuleSeverityInfo:
				result.Info = append(result.Info, violation)
			}
		}
	}

	return result, nil
}

// ValidateItem checks a single item against all rules (for real-time validation).
func (v *Validator) ValidateItem(item ScheduleItem) ([]models.ValidationViolation, error) {
	// Fetch existing items that might conflict
	items, err := v.fetchScheduleItems(item.StationID, item.StartsAt.Add(-24*time.Hour), item.EndsAt.Add(24*time.Hour))
	if err != nil {
		return nil, err
	}

	// Add the item being validated
	items = append(items, item)

	// Check for overlaps with this item
	var violations []models.ValidationViolation
	for _, other := range items {
		if other.ID == item.ID {
			continue
		}
		if v.itemsOverlap(item, other) {
			violations = append(violations, models.ValidationViolation{
				RuleType:    models.RuleTypeOverlap,
				RuleName:    "Overlap Detection",
				Severity:    models.RuleSeverityError,
				Message:     "This time slot overlaps with another scheduled item",
				StartsAt:    item.StartsAt,
				EndsAt:      item.EndsAt,
				AffectedIDs: []string{item.ID, other.ID},
			})
			break
		}
	}

	// Fetch and run custom rules
	var rules []models.ScheduleRule
	v.db.Where("station_id = ? AND active = ?", item.StationID, true).Find(&rules)

	for _, rule := range rules {
		ruleViolations := v.runRuleForItem(rule, item, items)
		violations = append(violations, ruleViolations...)
	}

	return violations, nil
}

// fetchScheduleItems retrieves all scheduled items for a station in a time range.
func (v *Validator) fetchScheduleItems(stationID string, start, end time.Time) ([]ScheduleItem, error) {
	var items []ScheduleItem

	// Fetch show instances
	var instances []models.ShowInstance
	v.db.Where("station_id = ? AND starts_at >= ? AND starts_at <= ? AND status != ?",
		stationID, start, end, models.ShowInstanceCancelled).Find(&instances)

	for _, inst := range instances {
		items = append(items, ScheduleItem{
			ID:         inst.ID,
			Type:       "show_instance",
			StationID:  inst.StationID,
			StartsAt:   inst.StartsAt,
			EndsAt:     inst.EndsAt,
			HostUserID: inst.HostUserID,
			Metadata:   inst.Metadata,
		})
	}

	// Fetch schedule entries (non-instance)
	var entries []models.ScheduleEntry
	v.db.Where("station_id = ? AND starts_at >= ? AND starts_at <= ?",
		stationID, start, end).Find(&entries)

	for _, entry := range entries {
		items = append(items, ScheduleItem{
			ID:         entry.ID,
			Type:       "schedule_entry",
			StationID:  entry.StationID,
			StartsAt:   entry.StartsAt,
			EndsAt:     entry.EndsAt,
			SourceType: entry.SourceType,
			SourceID:   entry.SourceID,
			Metadata:   entry.Metadata,
		})
	}

	// Sort by start time
	sort.Slice(items, func(i, j int) bool {
		return items[i].StartsAt.Before(items[j].StartsAt)
	})

	return items, nil
}

// checkOverlaps detects overlapping items.
func (v *Validator) checkOverlaps(items []ScheduleItem) []models.ValidationViolation {
	var violations []models.ValidationViolation

	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if v.itemsOverlap(items[i], items[j]) {
				violations = append(violations, models.ValidationViolation{
					RuleType:    models.RuleTypeOverlap,
					RuleName:    "Overlap Detection",
					Severity:    models.RuleSeverityError,
					Message:     "Two items are scheduled at the same time",
					StartsAt:    items[i].StartsAt,
					EndsAt:      items[j].EndsAt,
					AffectedIDs: []string{items[i].ID, items[j].ID},
				})
			}
		}
	}

	return violations
}

// itemsOverlap checks if two items overlap in time.
func (v *Validator) itemsOverlap(a, b ScheduleItem) bool {
	// a starts before b ends AND a ends after b starts
	return a.StartsAt.Before(b.EndsAt) && a.EndsAt.After(b.StartsAt)
}

// runRule executes a single rule against all items.
func (v *Validator) runRule(rule models.ScheduleRule, items []ScheduleItem, start, end time.Time) []models.ValidationViolation {
	switch rule.RuleType {
	case models.RuleTypeGap:
		return v.checkGaps(rule, items, start, end)
	case models.RuleTypeDJDoubleBooking:
		return v.checkDJDoubleBooking(rule, items)
	case models.RuleTypeMinDuration:
		return v.checkMinDuration(rule, items)
	case models.RuleTypeMaxDuration:
		return v.checkMaxDuration(rule, items)
	case models.RuleTypeMaxConsecutive:
		return v.checkMaxConsecutive(rule, items)
	default:
		return nil
	}
}

// runRuleForItem executes a rule for a single item.
func (v *Validator) runRuleForItem(rule models.ScheduleRule, item ScheduleItem, allItems []ScheduleItem) []models.ValidationViolation {
	switch rule.RuleType {
	case models.RuleTypeMinDuration:
		return v.checkMinDurationForItem(rule, item)
	case models.RuleTypeMaxDuration:
		return v.checkMaxDurationForItem(rule, item)
	case models.RuleTypeDJDoubleBooking:
		return v.checkDJDoubleBookingForItem(rule, item, allItems)
	default:
		return nil
	}
}

// checkGaps detects gaps in the schedule exceeding the threshold.
func (v *Validator) checkGaps(rule models.ScheduleRule, items []ScheduleItem, start, end time.Time) []models.ValidationViolation {
	var violations []models.ValidationViolation

	maxGapMinutes := 30 // default
	if val, ok := rule.Config["max_gap_minutes"].(float64); ok {
		maxGapMinutes = int(val)
	}

	ignoreHours := make(map[int]bool)
	if hours, ok := rule.Config["ignore_hours"].([]any); ok {
		for _, h := range hours {
			if hour, ok := h.(float64); ok {
				ignoreHours[int(hour)] = true
			}
		}
	}

	// Check gaps between consecutive items
	for i := 0; i < len(items)-1; i++ {
		gapStart := items[i].EndsAt
		gapEnd := items[i+1].StartsAt

		// Skip if gap is in ignored hours
		if ignoreHours[gapStart.Hour()] || ignoreHours[gapEnd.Hour()] {
			continue
		}

		gapMinutes := int(gapEnd.Sub(gapStart).Minutes())
		if gapMinutes > maxGapMinutes {
			violations = append(violations, models.ValidationViolation{
				RuleID:   rule.ID,
				RuleType: models.RuleTypeGap,
				RuleName: rule.Name,
				Severity: rule.Severity,
				Message:  "Schedule gap exceeds " + string(rune(maxGapMinutes)) + " minutes",
				StartsAt: gapStart,
				EndsAt:   gapEnd,
				Details: map[string]any{
					"gap_minutes": gapMinutes,
					"max_allowed": maxGapMinutes,
				},
			})
		}
	}

	return violations
}

// checkDJDoubleBooking detects the same DJ scheduled on multiple stations.
func (v *Validator) checkDJDoubleBooking(rule models.ScheduleRule, items []ScheduleItem) []models.ValidationViolation {
	var violations []models.ValidationViolation

	// Group items by host
	byHost := make(map[string][]ScheduleItem)
	for _, item := range items {
		if item.HostUserID != nil && *item.HostUserID != "" {
			byHost[*item.HostUserID] = append(byHost[*item.HostUserID], item)
		}
	}

	// For each host, check if they're booked on other stations at the same time
	for hostID, hostItems := range byHost {
		// Fetch items for this host on OTHER stations during this time range
		if len(hostItems) == 0 {
			continue
		}

		earliest := hostItems[0].StartsAt
		latest := hostItems[0].EndsAt
		for _, item := range hostItems {
			if item.StartsAt.Before(earliest) {
				earliest = item.StartsAt
			}
			if item.EndsAt.After(latest) {
				latest = item.EndsAt
			}
		}

		var otherStationInstances []models.ShowInstance
		v.db.Where("host_user_id = ? AND station_id != ? AND starts_at < ? AND ends_at > ? AND status != ?",
			hostID, hostItems[0].StationID, latest, earliest, models.ShowInstanceCancelled).
			Find(&otherStationInstances)

		for _, inst := range otherStationInstances {
			// Check if any of our items overlap with this
			for _, item := range hostItems {
				if item.StartsAt.Before(inst.EndsAt) && item.EndsAt.After(inst.StartsAt) {
					violations = append(violations, models.ValidationViolation{
						RuleID:      rule.ID,
						RuleType:    models.RuleTypeDJDoubleBooking,
						RuleName:    rule.Name,
						Severity:    rule.Severity,
						Message:     "DJ is scheduled on another station at the same time",
						StartsAt:    item.StartsAt,
						EndsAt:      item.EndsAt,
						AffectedIDs: []string{item.ID, inst.ID},
						Details: map[string]any{
							"host_user_id":     hostID,
							"other_station_id": inst.StationID,
						},
					})
				}
			}
		}
	}

	return violations
}

// checkDJDoubleBookingForItem checks a single item for DJ double-booking.
func (v *Validator) checkDJDoubleBookingForItem(rule models.ScheduleRule, item ScheduleItem, _ []ScheduleItem) []models.ValidationViolation {
	if item.HostUserID == nil || *item.HostUserID == "" {
		return nil
	}

	var otherInstances []models.ShowInstance
	v.db.Where("host_user_id = ? AND station_id != ? AND starts_at < ? AND ends_at > ? AND status != ?",
		*item.HostUserID, item.StationID, item.EndsAt, item.StartsAt, models.ShowInstanceCancelled).
		Find(&otherInstances)

	if len(otherInstances) > 0 {
		return []models.ValidationViolation{{
			RuleID:   rule.ID,
			RuleType: models.RuleTypeDJDoubleBooking,
			RuleName: rule.Name,
			Severity: rule.Severity,
			Message:  "DJ is scheduled on another station at the same time",
			StartsAt: item.StartsAt,
			EndsAt:   item.EndsAt,
			Details: map[string]any{
				"host_user_id":     *item.HostUserID,
				"other_station_id": otherInstances[0].StationID,
			},
		}}
	}

	return nil
}

// checkMinDuration checks if items meet minimum duration.
func (v *Validator) checkMinDuration(rule models.ScheduleRule, items []ScheduleItem) []models.ValidationViolation {
	var violations []models.ValidationViolation

	minMinutes := 15 // default
	if val, ok := rule.Config["minutes"].(float64); ok {
		minMinutes = int(val)
	}

	for _, item := range items {
		duration := int(item.EndsAt.Sub(item.StartsAt).Minutes())
		if duration < minMinutes {
			violations = append(violations, models.ValidationViolation{
				RuleID:      rule.ID,
				RuleType:    models.RuleTypeMinDuration,
				RuleName:    rule.Name,
				Severity:    rule.Severity,
				Message:     "Item duration is less than minimum required",
				StartsAt:    item.StartsAt,
				EndsAt:      item.EndsAt,
				AffectedIDs: []string{item.ID},
				Details: map[string]any{
					"duration_minutes": duration,
					"min_required":     minMinutes,
				},
			})
		}
	}

	return violations
}

// checkMinDurationForItem checks a single item for minimum duration.
func (v *Validator) checkMinDurationForItem(rule models.ScheduleRule, item ScheduleItem) []models.ValidationViolation {
	minMinutes := 15
	if val, ok := rule.Config["minutes"].(float64); ok {
		minMinutes = int(val)
	}

	duration := int(item.EndsAt.Sub(item.StartsAt).Minutes())
	if duration < minMinutes {
		return []models.ValidationViolation{{
			RuleID:      rule.ID,
			RuleType:    models.RuleTypeMinDuration,
			RuleName:    rule.Name,
			Severity:    rule.Severity,
			Message:     "Item duration is less than minimum required",
			StartsAt:    item.StartsAt,
			EndsAt:      item.EndsAt,
			AffectedIDs: []string{item.ID},
			Details: map[string]any{
				"duration_minutes": duration,
				"min_required":     minMinutes,
			},
		}}
	}

	return nil
}

// checkMaxDuration checks if items exceed maximum duration.
func (v *Validator) checkMaxDuration(rule models.ScheduleRule, items []ScheduleItem) []models.ValidationViolation {
	var violations []models.ValidationViolation

	maxMinutes := 240 // default 4 hours
	if val, ok := rule.Config["minutes"].(float64); ok {
		maxMinutes = int(val)
	}

	for _, item := range items {
		duration := int(item.EndsAt.Sub(item.StartsAt).Minutes())
		if duration > maxMinutes {
			violations = append(violations, models.ValidationViolation{
				RuleID:      rule.ID,
				RuleType:    models.RuleTypeMaxDuration,
				RuleName:    rule.Name,
				Severity:    rule.Severity,
				Message:     "Item duration exceeds maximum allowed",
				StartsAt:    item.StartsAt,
				EndsAt:      item.EndsAt,
				AffectedIDs: []string{item.ID},
				Details: map[string]any{
					"duration_minutes": duration,
					"max_allowed":      maxMinutes,
				},
			})
		}
	}

	return violations
}

// checkMaxDurationForItem checks a single item for maximum duration.
func (v *Validator) checkMaxDurationForItem(rule models.ScheduleRule, item ScheduleItem) []models.ValidationViolation {
	maxMinutes := 240
	if val, ok := rule.Config["minutes"].(float64); ok {
		maxMinutes = int(val)
	}

	duration := int(item.EndsAt.Sub(item.StartsAt).Minutes())
	if duration > maxMinutes {
		return []models.ValidationViolation{{
			RuleID:      rule.ID,
			RuleType:    models.RuleTypeMaxDuration,
			RuleName:    rule.Name,
			Severity:    rule.Severity,
			Message:     "Item duration exceeds maximum allowed",
			StartsAt:    item.StartsAt,
			EndsAt:      item.EndsAt,
			AffectedIDs: []string{item.ID},
			Details: map[string]any{
				"duration_minutes": duration,
				"max_allowed":      maxMinutes,
			},
		}}
	}

	return nil
}

// checkMaxConsecutive checks if any DJ has too many consecutive hours.
func (v *Validator) checkMaxConsecutive(rule models.ScheduleRule, items []ScheduleItem) []models.ValidationViolation {
	var violations []models.ValidationViolation

	maxHours := 4 // default
	if val, ok := rule.Config["max_hours"].(float64); ok {
		maxHours = int(val)
	}

	// Group consecutive items by host
	byHost := make(map[string][]ScheduleItem)
	for _, item := range items {
		if item.HostUserID != nil && *item.HostUserID != "" {
			byHost[*item.HostUserID] = append(byHost[*item.HostUserID], item)
		}
	}

	for hostID, hostItems := range byHost {
		// Sort by start time
		sort.Slice(hostItems, func(i, j int) bool {
			return hostItems[i].StartsAt.Before(hostItems[j].StartsAt)
		})

		// Find consecutive blocks
		var blockStart, blockEnd time.Time
		for i, item := range hostItems {
			if i == 0 {
				blockStart = item.StartsAt
				blockEnd = item.EndsAt
				continue
			}

			// Check if this item is adjacent (within 15 minutes) to previous
			gap := item.StartsAt.Sub(blockEnd)
			if gap <= 15*time.Minute {
				// Extend the block
				blockEnd = item.EndsAt
			} else {
				// Check if previous block exceeded max
				blockHours := blockEnd.Sub(blockStart).Hours()
				if blockHours > float64(maxHours) {
					violations = append(violations, models.ValidationViolation{
						RuleID:   rule.ID,
						RuleType: models.RuleTypeMaxConsecutive,
						RuleName: rule.Name,
						Severity: rule.Severity,
						Message:  "DJ scheduled for more than maximum consecutive hours",
						StartsAt: blockStart,
						EndsAt:   blockEnd,
						Details: map[string]any{
							"host_user_id":      hostID,
							"consecutive_hours": blockHours,
							"max_allowed":       maxHours,
						},
					})
				}
				// Start new block
				blockStart = item.StartsAt
				blockEnd = item.EndsAt
			}
		}

		// Check final block
		if !blockStart.IsZero() {
			blockHours := blockEnd.Sub(blockStart).Hours()
			if blockHours > float64(maxHours) {
				violations = append(violations, models.ValidationViolation{
					RuleID:   rule.ID,
					RuleType: models.RuleTypeMaxConsecutive,
					RuleName: rule.Name,
					Severity: rule.Severity,
					Message:  "DJ scheduled for more than maximum consecutive hours",
					StartsAt: blockStart,
					EndsAt:   blockEnd,
					Details: map[string]any{
						"host_user_id":      hostID,
						"consecutive_hours": blockHours,
						"max_allowed":       maxHours,
					},
				})
			}
		}
	}

	return violations
}
