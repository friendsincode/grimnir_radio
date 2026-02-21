/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package migration

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// StagedAnalyzer analyzes import data for staged review.
type StagedAnalyzer struct {
	db     *gorm.DB
	logger zerolog.Logger
}

// NewStagedAnalyzer creates a new staged analyzer.
func NewStagedAnalyzer(db *gorm.DB, logger zerolog.Logger) *StagedAnalyzer {
	return &StagedAnalyzer{
		db:     db,
		logger: logger.With().Str("component", "staged_analyzer").Logger(),
	}
}

// ShowInstance represents a single occurrence of a show for recurrence analysis.
type ShowInstance struct {
	SourceID string
	ShowID   string
	ShowName string
	StartsAt time.Time
	EndsAt   time.Time
	Timezone string
}

// RecurrenceResult contains the results of recurrence detection.
type RecurrenceResult struct {
	RRule           string    `json:"rrule"`
	Confidence      float64   `json:"confidence"` // 0.0-1.0
	Pattern         string    `json:"pattern"`    // Human-readable pattern
	DTStart         time.Time `json:"dtstart"`
	DurationMinutes int       `json:"duration_minutes"`
	Timezone        string    `json:"timezone"`
	MatchedCount    int       `json:"matched_count"`
	ExceptionCount  int       `json:"exception_count"`
}

// DetectRecurrence analyzes show instances to find recurring patterns.
// Returns nil if no pattern can be detected (fewer than 3 instances).
func (a *StagedAnalyzer) DetectRecurrence(instances []ShowInstance) *RecurrenceResult {
	if len(instances) < 3 {
		return nil
	}

	// Sort by start time
	sort.Slice(instances, func(i, j int) bool {
		return instances[i].StartsAt.Before(instances[j].StartsAt)
	})

	// Calculate average duration
	var totalDuration time.Duration
	for _, inst := range instances {
		totalDuration += inst.EndsAt.Sub(inst.StartsAt)
	}
	avgDuration := totalDuration / time.Duration(len(instances))
	durationMinutes := int(avgDuration.Minutes())

	// Group instances by "DayOfWeek-HH:MM" pattern
	dayTimeGroups := make(map[string][]ShowInstance)
	for _, inst := range instances {
		// Use the instance's timezone if available, otherwise UTC
		tz := inst.Timezone
		if tz == "" {
			tz = "UTC"
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			loc = time.UTC
		}
		localTime := inst.StartsAt.In(loc)
		key := fmt.Sprintf("%s-%02d:%02d", localTime.Weekday().String(), localTime.Hour(), localTime.Minute())
		dayTimeGroups[key] = append(dayTimeGroups[key], inst)
	}

	// Find the dominant pattern(s)
	var bestPatterns []string
	var bestCount int
	for key, group := range dayTimeGroups {
		if len(group) > bestCount {
			bestPatterns = []string{key}
			bestCount = len(group)
		} else if len(group) == bestCount {
			bestPatterns = append(bestPatterns, key)
		}
	}

	if bestCount < 2 {
		// Not enough instances matching any pattern
		return nil
	}

	// Parse the best patterns to build RRULE
	type dayTime struct {
		day  time.Weekday
		hour int
		min  int
	}
	var patterns []dayTime
	for _, p := range bestPatterns {
		parts := strings.Split(p, "-")
		if len(parts) != 2 {
			continue
		}
		dayStr := parts[0]
		timeStr := parts[1]

		var day time.Weekday
		switch dayStr {
		case "Sunday":
			day = time.Sunday
		case "Monday":
			day = time.Monday
		case "Tuesday":
			day = time.Tuesday
		case "Wednesday":
			day = time.Wednesday
		case "Thursday":
			day = time.Thursday
		case "Friday":
			day = time.Friday
		case "Saturday":
			day = time.Saturday
		default:
			continue
		}

		var hour, min int
		fmt.Sscanf(timeStr, "%02d:%02d", &hour, &min)
		patterns = append(patterns, dayTime{day: day, hour: hour, min: min})
	}

	if len(patterns) == 0 {
		return nil
	}

	// Generate RRULE
	var rrule string
	var patternDesc string

	// Count matched instances
	matchedCount := 0
	for _, group := range dayTimeGroups {
		for _, key := range bestPatterns {
			if contains(dayTimeGroups[key], group) {
				matchedCount += len(group)
			}
		}
	}
	matchedCount = bestCount * len(bestPatterns)

	// Check if it's daily (same time every day)
	if len(patterns) == 7 || a.looksDaily(instances) {
		// Check if all at same time
		hour := patterns[0].hour
		min := patterns[0].min
		rrule = fmt.Sprintf("FREQ=DAILY;BYHOUR=%d;BYMINUTE=%d", hour, min)
		patternDesc = fmt.Sprintf("Daily at %02d:%02d", hour, min)
	} else if len(patterns) == 1 {
		// Single day weekly
		dayAbbrev := dayToAbbrev(patterns[0].day)
		rrule = fmt.Sprintf("FREQ=WEEKLY;BYDAY=%s;BYHOUR=%d;BYMINUTE=%d",
			dayAbbrev, patterns[0].hour, patterns[0].min)
		patternDesc = fmt.Sprintf("Weekly on %s at %02d:%02d",
			patterns[0].day.String(), patterns[0].hour, patterns[0].min)
	} else {
		// Multiple days weekly
		var days []string
		var dayNames []string
		for _, p := range patterns {
			days = append(days, dayToAbbrev(p.day))
			dayNames = append(dayNames, p.day.String())
		}
		// Assume same time for all (use first)
		rrule = fmt.Sprintf("FREQ=WEEKLY;BYDAY=%s;BYHOUR=%d;BYMINUTE=%d",
			strings.Join(days, ","), patterns[0].hour, patterns[0].min)
		patternDesc = fmt.Sprintf("Weekly on %s at %02d:%02d",
			strings.Join(dayNames, ", "), patterns[0].hour, patterns[0].min)
	}

	// Calculate confidence
	confidence := float64(matchedCount) / float64(len(instances))
	exceptionCount := len(instances) - matchedCount

	// Get DTStart from first instance
	dtStart := instances[0].StartsAt
	timezone := instances[0].Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	return &RecurrenceResult{
		RRule:           rrule,
		Confidence:      confidence,
		Pattern:         patternDesc,
		DTStart:         dtStart,
		DurationMinutes: durationMinutes,
		Timezone:        timezone,
		MatchedCount:    matchedCount,
		ExceptionCount:  exceptionCount,
	}
}

// looksDaily checks if instances appear to be daily.
func (a *StagedAnalyzer) looksDaily(instances []ShowInstance) bool {
	if len(instances) < 5 {
		return false
	}

	// Check if there are instances on at least 5 different days of the week
	daysOfWeek := make(map[time.Weekday]bool)
	for _, inst := range instances {
		daysOfWeek[inst.StartsAt.Weekday()] = true
	}
	return len(daysOfWeek) >= 5
}

// dayToAbbrev converts a weekday to RRULE abbreviation.
func dayToAbbrev(d time.Weekday) string {
	switch d {
	case time.Sunday:
		return "SU"
	case time.Monday:
		return "MO"
	case time.Tuesday:
		return "TU"
	case time.Wednesday:
		return "WE"
	case time.Thursday:
		return "TH"
	case time.Friday:
		return "FR"
	case time.Saturday:
		return "SA"
	default:
		return "MO"
	}
}

// contains checks if slice contains any instance from group.
func contains(slice, group []ShowInstance) bool {
	for _, s := range slice {
		for _, g := range group {
			if s.SourceID == g.SourceID {
				return true
			}
		}
	}
	return false
}

// DetectDuplicates checks staged media items against existing database records.
// It updates the IsDuplicate and DuplicateOfID fields on matching items.
func (a *StagedAnalyzer) DetectDuplicates(ctx context.Context, media []models.StagedMediaItem, stationID string) []models.StagedMediaItem {
	if len(media) == 0 {
		return media
	}

	// Collect all content hashes and metadata fallback candidates.
	var hashes []string
	hashToIndex := make(map[string][]int)
	type fallbackKey struct {
		Title  string
		Artist string
		Album  string
	}
	fallbackToIndex := make(map[fallbackKey][]int)
	for i, m := range media {
		if m.ContentHash != "" {
			hashes = append(hashes, m.ContentHash)
			hashToIndex[m.ContentHash] = append(hashToIndex[m.ContentHash], i)
			continue
		}
		title := normalizeImportText(m.Title)
		artist := normalizeImportText(m.Artist)
		if title == "" || artist == "" {
			continue
		}
		album := normalizeImportText(m.Album)
		key := fallbackKey{Title: title, Artist: artist, Album: album}
		fallbackToIndex[key] = append(fallbackToIndex[key], i)
	}

	hashDuplicateCount := 0
	fallbackDuplicateCount := 0

	// Query existing media by content hash.
	if len(hashes) > 0 {
		var existing []models.MediaItem
		query := a.db.WithContext(ctx).Where("content_hash IN ?", hashes)
		if stationID != "" {
			// Only check duplicates within the same station.
			query = query.Where("station_id = ?", stationID)
		}
		if err := query.Find(&existing).Error; err != nil {
			a.logger.Error().Err(err).Msg("failed to query existing media for hash duplicate detection")
		} else {
			for _, ex := range existing {
				if indices, ok := hashToIndex[ex.ContentHash]; ok {
					for _, i := range indices {
						if media[i].IsDuplicate {
							continue
						}
						media[i].IsDuplicate = true
						media[i].DuplicateOfID = ex.ID
						hashDuplicateCount++
					}
				}
			}
		}
	}

	// Metadata fallback matching when hash is unavailable.
	normalizeExpr := func(column string) string {
		return fmt.Sprintf("LOWER(TRIM(REPLACE(REPLACE(REPLACE(%s, '  ', ' '), '  ', ' '), '  ', ' ')))", column)
	}
	for key, indices := range fallbackToIndex {
		var existing models.MediaItem
		query := a.db.WithContext(ctx).
			Where(normalizeExpr("title")+" = ?", key.Title).
			Where(normalizeExpr("artist")+" = ?", key.Artist).
			Where(normalizeExpr("COALESCE(album, '')")+" = ?", key.Album)
		if stationID != "" {
			query = query.Where("station_id = ?", stationID)
		}
		if err := query.Order("created_at ASC").First(&existing).Error; err != nil {
			continue
		}
		for _, i := range indices {
			if media[i].IsDuplicate {
				continue
			}
			media[i].IsDuplicate = true
			media[i].DuplicateOfID = existing.ID
			fallbackDuplicateCount++
		}
	}

	a.logger.Info().
		Int("total_items", len(media)).
		Int("hash_duplicates", hashDuplicateCount).
		Int("fallback_duplicates", fallbackDuplicateCount).
		Msg("duplicate detection complete")

	return media
}

func normalizeImportText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

// CreateStagedImport creates a new staged import record with initial analysis.
func (a *StagedAnalyzer) CreateStagedImport(ctx context.Context, jobID string, sourceType string) (*models.StagedImport, error) {
	staged := &models.StagedImport{
		ID:         uuid.New().String(),
		JobID:      jobID,
		SourceType: sourceType,
		Status:     models.StagedImportStatusAnalyzing,
		Selections: models.ImportSelections{
			CustomRRules: make(map[string]string),
		},
	}

	if err := a.db.WithContext(ctx).Create(staged).Error; err != nil {
		return nil, fmt.Errorf("create staged import: %w", err)
	}

	return staged, nil
}

// UpdateStagedImport updates a staged import with analysis results.
func (a *StagedAnalyzer) UpdateStagedImport(ctx context.Context, staged *models.StagedImport) error {
	return a.db.WithContext(ctx).Save(staged).Error
}

// GetStagedImport retrieves a staged import by ID.
func (a *StagedAnalyzer) GetStagedImport(ctx context.Context, id string) (*models.StagedImport, error) {
	var staged models.StagedImport
	if err := a.db.WithContext(ctx).First(&staged, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &staged, nil
}

// GetStagedImportByJobID retrieves a staged import by job ID.
func (a *StagedAnalyzer) GetStagedImportByJobID(ctx context.Context, jobID string) (*models.StagedImport, error) {
	var staged models.StagedImport
	if err := a.db.WithContext(ctx).First(&staged, "job_id = ?", jobID).Error; err != nil {
		return nil, err
	}
	return &staged, nil
}

// ApplyDefaultSelections marks all non-duplicate items as selected by default.
func (a *StagedAnalyzer) ApplyDefaultSelections(staged *models.StagedImport) {
	// Select all media that isn't a duplicate
	for i := range staged.StagedMedia {
		staged.StagedMedia[i].Selected = !staged.StagedMedia[i].IsDuplicate
	}

	// Select all playlists
	for i := range staged.StagedPlaylists {
		staged.StagedPlaylists[i].Selected = true
	}

	// Select all smart blocks
	for i := range staged.StagedSmartBlocks {
		staged.StagedSmartBlocks[i].Selected = true
	}

	// Select all shows with high-confidence patterns as Shows, others as Clocks
	for i := range staged.StagedShows {
		staged.StagedShows[i].Selected = true
		if staged.StagedShows[i].PatternConfidence >= 0.75 && staged.StagedShows[i].DetectedRRule != "" {
			staged.StagedShows[i].CreateShow = true
			staged.StagedShows[i].CreateClock = false
		} else {
			staged.StagedShows[i].CreateShow = false
			staged.StagedShows[i].CreateClock = true
		}
	}

	// Select all webstreams
	for i := range staged.StagedWebstreams {
		staged.StagedWebstreams[i].Selected = true
	}
}

// GenerateWarnings analyzes staged data and generates warnings.
func (a *StagedAnalyzer) GenerateWarnings(staged *models.StagedImport) {
	var warnings []models.ImportWarning

	// Warn about duplicates
	dupCount := 0
	for _, m := range staged.StagedMedia {
		if m.IsDuplicate {
			dupCount++
		}
	}
	if dupCount > 0 {
		warnings = append(warnings, models.ImportWarning{
			Code:     "duplicate_media",
			Severity: "warning",
			Message:  fmt.Sprintf("%d media files already exist in your library", dupCount),
			Details:  "Duplicate files are deselected by default. You can still import them if needed.",
		})
	}

	// Warn about shows with low confidence patterns
	lowConfCount := 0
	for _, sh := range staged.StagedShows {
		if sh.DetectedRRule != "" && sh.PatternConfidence < 0.75 {
			lowConfCount++
		}
	}
	if lowConfCount > 0 {
		warnings = append(warnings, models.ImportWarning{
			Code:     "low_confidence_schedule",
			Severity: "info",
			Message:  fmt.Sprintf("%d shows have uncertain schedule patterns", lowConfCount),
			Details:  "These shows will be imported as Clocks by default. You can edit the RRULE to create recurring Shows.",
		})
	}

	// Warn about shows with no detected pattern
	noPatternCount := 0
	for _, sh := range staged.StagedShows {
		if sh.DetectedRRule == "" && sh.InstanceCount > 0 {
			noPatternCount++
		}
	}
	if noPatternCount > 0 {
		warnings = append(warnings, models.ImportWarning{
			Code:     "no_schedule_pattern",
			Severity: "info",
			Message:  fmt.Sprintf("%d shows had no detectable schedule pattern", noPatternCount),
			Details:  "These shows will be imported as Clocks. Add a schedule manually if needed.",
		})
	}

	// Warn about empty playlists
	emptyPlaylists := 0
	for _, pl := range staged.StagedPlaylists {
		if pl.ItemCount == 0 {
			emptyPlaylists++
		}
	}
	if emptyPlaylists > 0 {
		warnings = append(warnings, models.ImportWarning{
			Code:     "empty_playlists",
			Severity: "info",
			Message:  fmt.Sprintf("%d playlists are empty", emptyPlaylists),
		})
	}

	staged.Warnings = warnings
}

// GenerateSuggestions creates suggestions for improving the import.
func (a *StagedAnalyzer) GenerateSuggestions(staged *models.StagedImport) {
	var suggestions []models.ImportSuggestion

	// Suggest skipping duplicates
	dupCount := 0
	for _, m := range staged.StagedMedia {
		if m.IsDuplicate {
			dupCount++
		}
	}
	if dupCount > 0 {
		suggestions = append(suggestions, models.ImportSuggestion{
			Code:    "skip_duplicates",
			Message: fmt.Sprintf("Skip %d duplicate media files to save storage space", dupCount),
			Action:  "deselect_duplicates",
		})
	}

	// Suggest reviewing shows with schedule patterns
	showsWithPattern := 0
	for _, sh := range staged.StagedShows {
		if sh.DetectedRRule != "" {
			showsWithPattern++
		}
	}
	if showsWithPattern > 0 {
		suggestions = append(suggestions, models.ImportSuggestion{
			Code:    "review_schedules",
			Message: fmt.Sprintf("Review %d detected show schedules to ensure accuracy", showsWithPattern),
			Action:  "review_shows",
		})
	}

	staged.Suggestions = suggestions
}
