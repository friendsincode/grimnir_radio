/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package scheduling

import (
	"fmt"
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
	Display    string // Human-friendly label for novice-facing messages
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
			overlapStart := maxTime(item.StartsAt, other.StartsAt)
			overlapEnd := minTime(item.EndsAt, other.EndsAt)
			overlapMinutes := int(overlapEnd.Sub(overlapStart).Minutes())
			if overlapMinutes < 0 {
				overlapMinutes = 0
			}

			violations = append(violations, models.ValidationViolation{
				RuleType:    models.RuleTypeOverlap,
				RuleName:    "Schedule Overlap",
				Severity:    models.RuleSeverityError,
				Message:     fmt.Sprintf("This item overlaps with %s from %s to %s (%d minute overlap). Move either item so only one plays at a time.", itemLabel(other), overlapStart.Format(time.RFC3339), overlapEnd.Format(time.RFC3339), overlapMinutes),
				StartsAt:    item.StartsAt,
				EndsAt:      item.EndsAt,
				AffectedIDs: []string{item.ID, other.ID},
				Details: map[string]any{
					"overlap_start":   overlapStart,
					"overlap_end":     overlapEnd,
					"overlap_minutes": overlapMinutes,
					"item_a": map[string]any{
						"id":        item.ID,
						"type":      item.Type,
						"label":     itemLabel(item),
						"starts_at": item.StartsAt,
						"ends_at":   item.EndsAt,
					},
					"item_b": map[string]any{
						"id":        other.ID,
						"type":      other.Type,
						"label":     itemLabel(other),
						"starts_at": other.StartsAt,
						"ends_at":   other.EndsAt,
					},
					"suggestion": "Adjust one of these time windows so they no longer overlap.",
				},
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
	v.db.Where("station_id = ? AND starts_at < ? AND ends_at > ? AND status != ?",
		stationID, end, start, models.ShowInstanceCancelled).Find(&instances)

	showNames := map[string]string{}
	if len(instances) > 0 {
		showIDs := make([]string, 0, len(instances))
		for _, inst := range instances {
			if inst.ShowID != "" {
				showIDs = append(showIDs, inst.ShowID)
			}
		}
		if len(showIDs) > 0 {
			var shows []models.Show
			v.db.Select("id, name").Where("id IN ?", showIDs).Find(&shows)
			for _, sh := range shows {
				showNames[sh.ID] = sh.Name
			}
		}
	}

	for _, inst := range instances {
		display := "Show"
		if name := showNames[inst.ShowID]; name != "" {
			display = "Show: " + name
		}
		items = append(items, ScheduleItem{
			ID:         inst.ID,
			Type:       "show_instance",
			Display:    display,
			StationID:  inst.StationID,
			StartsAt:   inst.StartsAt,
			EndsAt:     inst.EndsAt,
			HostUserID: inst.HostUserID,
			Metadata:   inst.Metadata,
		})
	}

	// Fetch schedule entries (non-instance)
	var entries []models.ScheduleEntry
	v.db.Where("station_id = ? AND starts_at < ? AND ends_at > ?",
		stationID, end, start).Find(&entries)

	var playlistIDs, smartBlockIDs, clockIDs, webstreamIDs, mediaIDs []string
	for _, entry := range entries {
		switch entry.SourceType {
		case "playlist":
			playlistIDs = append(playlistIDs, entry.SourceID)
		case "smart_block":
			smartBlockIDs = append(smartBlockIDs, entry.SourceID)
		case "clock_template":
			clockIDs = append(clockIDs, entry.SourceID)
		case "webstream":
			webstreamIDs = append(webstreamIDs, entry.SourceID)
		case "media":
			mediaIDs = append(mediaIDs, entry.SourceID)
		}
	}

	playlistNames := map[string]string{}
	if len(playlistIDs) > 0 {
		var playlists []models.Playlist
		v.db.Select("id, name").Where("id IN ?", playlistIDs).Find(&playlists)
		for _, p := range playlists {
			playlistNames[p.ID] = p.Name
		}
	}
	smartBlockNames := map[string]string{}
	if len(smartBlockIDs) > 0 {
		var blocks []models.SmartBlock
		v.db.Select("id, name").Where("id IN ?", smartBlockIDs).Find(&blocks)
		for _, b := range blocks {
			smartBlockNames[b.ID] = b.Name
		}
	}
	clockNames := map[string]string{}
	if len(clockIDs) > 0 {
		var clocks []models.ClockHour
		v.db.Select("id, name").Where("id IN ?", clockIDs).Find(&clocks)
		for _, c := range clocks {
			clockNames[c.ID] = c.Name
		}
	}
	webstreamNames := map[string]string{}
	if len(webstreamIDs) > 0 {
		var streams []models.Webstream
		v.db.Select("id, name").Where("id IN ?", webstreamIDs).Find(&streams)
		for _, s := range streams {
			webstreamNames[s.ID] = s.Name
		}
	}
	mediaNames := map[string]string{}
	if len(mediaIDs) > 0 {
		var media []models.MediaItem
		v.db.Select("id, title, artist").Where("id IN ?", mediaIDs).Find(&media)
		for _, m := range media {
			if m.Artist != "" {
				mediaNames[m.ID] = m.Artist + " - " + m.Title
			} else {
				mediaNames[m.ID] = m.Title
			}
		}
	}

	for _, entry := range entries {
		display := entry.SourceType
		switch entry.SourceType {
		case "playlist":
			if n := playlistNames[entry.SourceID]; n != "" {
				display = "Playlist: " + n
			} else {
				display = "Playlist"
			}
		case "smart_block":
			if n := smartBlockNames[entry.SourceID]; n != "" {
				display = "Smart Block: " + n
			} else {
				display = "Smart Block"
			}
		case "clock_template":
			if n := clockNames[entry.SourceID]; n != "" {
				display = "Clock: " + n
			} else {
				display = "Clock"
			}
		case "webstream":
			if n := webstreamNames[entry.SourceID]; n != "" {
				display = "Webstream: " + n
			} else {
				display = "Webstream"
			}
		case "media":
			if n := mediaNames[entry.SourceID]; n != "" {
				display = "Track: " + n
			} else {
				display = "Track"
			}
		}

		items = append(items, ScheduleItem{
			ID:         entry.ID,
			Type:       "schedule_entry",
			Display:    display,
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
				overlapStart := maxTime(items[i].StartsAt, items[j].StartsAt)
				overlapEnd := minTime(items[i].EndsAt, items[j].EndsAt)
				overlapMinutes := int(overlapEnd.Sub(overlapStart).Minutes())
				if overlapMinutes < 0 {
					overlapMinutes = 0
				}

				violations = append(violations, models.ValidationViolation{
					RuleType:    models.RuleTypeOverlap,
					RuleName:    "Schedule Overlap",
					Severity:    models.RuleSeverityError,
					Message:     fmt.Sprintf("Overlap detected: %s and %s both run from %s to %s (%d minute overlap). Keep only one item in that window.", itemLabel(items[i]), itemLabel(items[j]), overlapStart.Format(time.RFC3339), overlapEnd.Format(time.RFC3339), overlapMinutes),
					StartsAt:    items[i].StartsAt,
					EndsAt:      items[i].EndsAt,
					AffectedIDs: []string{items[i].ID, items[j].ID},
					Details: map[string]any{
						"overlap_start":   overlapStart,
						"overlap_end":     overlapEnd,
						"overlap_minutes": overlapMinutes,
						"item_a": map[string]any{
							"id":        items[i].ID,
							"type":      items[i].Type,
							"label":     itemLabel(items[i]),
							"starts_at": items[i].StartsAt,
							"ends_at":   items[i].EndsAt,
						},
						"item_b": map[string]any{
							"id":        items[j].ID,
							"type":      items[j].Type,
							"label":     itemLabel(items[j]),
							"starts_at": items[j].StartsAt,
							"ends_at":   items[j].EndsAt,
						},
						"suggestion": "Move or trim one of these entries so only one program runs at a time.",
					},
				})
			}
		}
	}

	return violations
}

func itemLabel(item ScheduleItem) string {
	if item.Display != "" {
		return item.Display
	}
	if item.SourceType != "" {
		return item.SourceType
	}
	return item.Type
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
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
