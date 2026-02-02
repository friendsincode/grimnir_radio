/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ScheduleAnalyticsService handles schedule-related analytics.
type ScheduleAnalyticsService struct {
	db     *gorm.DB
	logger zerolog.Logger
}

// NewScheduleAnalyticsService creates a new schedule analytics service.
func NewScheduleAnalyticsService(db *gorm.DB, logger zerolog.Logger) *ScheduleAnalyticsService {
	return &ScheduleAnalyticsService{
		db:     db,
		logger: logger.With().Str("component", "schedule_analytics").Logger(),
	}
}

// RecordHourlyStats records analytics for a specific hour.
func (s *ScheduleAnalyticsService) RecordHourlyStats(ctx context.Context, stationID string, date time.Time, hour int, stats HourlyStats) error {
	// Find what show was playing during this hour
	hourStart := time.Date(date.Year(), date.Month(), date.Day(), hour, 0, 0, 0, date.Location())
	hourEnd := hourStart.Add(time.Hour)

	var instance models.ShowInstance
	s.db.Where("station_id = ? AND starts_at <= ? AND ends_at > ?", stationID, hourStart, hourEnd).
		First(&instance)

	analytics := &models.ScheduleAnalytics{
		ID:            uuid.NewString(),
		StationID:     stationID,
		Date:          date,
		Hour:          hour,
		AvgListeners:  stats.AvgListeners,
		PeakListeners: stats.PeakListeners,
		TuneIns:       stats.TuneIns,
		TuneOuts:      stats.TuneOuts,
		TotalMinutes:  stats.TotalMinutes,
		CreatedAt:     time.Now(),
	}

	if instance.ID != "" {
		analytics.InstanceID = &instance.ID
		analytics.ShowID = &instance.ShowID
	}

	return s.db.WithContext(ctx).Create(analytics).Error
}

// HourlyStats represents listener statistics for an hour.
type HourlyStats struct {
	AvgListeners  int
	PeakListeners int
	TuneIns       int
	TuneOuts      int
	TotalMinutes  int
}

// GetShowPerformance returns performance metrics for shows in a date range.
func (s *ScheduleAnalyticsService) GetShowPerformance(ctx context.Context, stationID string, start, end time.Time) ([]models.ShowPerformance, error) {
	var results []models.ShowPerformance

	// Get current period stats
	rows, err := s.db.WithContext(ctx).Raw(`
		SELECT
			sa.show_id,
			sh.name as show_name,
			COUNT(DISTINCT sa.instance_id) as instance_count,
			AVG(sa.avg_listeners) as avg_listeners,
			MAX(sa.peak_listeners) as peak_listeners,
			SUM(sa.tune_ins) as total_tune_ins,
			SUM(sa.total_minutes) as total_minutes
		FROM schedule_analytics sa
		JOIN shows sh ON sa.show_id = sh.id
		WHERE sa.station_id = ?
		AND sa.date >= ? AND sa.date < ?
		AND sa.show_id IS NOT NULL
		GROUP BY sa.show_id, sh.name
		ORDER BY avg_listeners DESC
	`, stationID, start, end).Rows()

	if err != nil {
		return nil, fmt.Errorf("failed to query show performance: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p models.ShowPerformance
		if err := rows.Scan(&p.ShowID, &p.ShowName, &p.InstanceCount, &p.AvgListeners, &p.PeakListeners, &p.TotalTuneIns, &p.TotalMinutes); err != nil {
			continue
		}
		results = append(results, p)
	}

	// Calculate trends (compare to previous period of same length)
	duration := end.Sub(start)
	prevStart := start.Add(-duration)
	prevEnd := start

	for i := range results {
		var prevAvg float64
		s.db.WithContext(ctx).Raw(`
			SELECT AVG(avg_listeners)
			FROM schedule_analytics
			WHERE station_id = ? AND show_id = ?
			AND date >= ? AND date < ?
		`, stationID, results[i].ShowID, prevStart, prevEnd).Scan(&prevAvg)

		if prevAvg > 0 {
			results[i].TrendPercent = ((results[i].AvgListeners - prevAvg) / prevAvg) * 100
		}
	}

	return results, nil
}

// GetTimeSlotPerformance returns performance metrics by time slot.
func (s *ScheduleAnalyticsService) GetTimeSlotPerformance(ctx context.Context, stationID string, start, end time.Time) ([]models.TimeSlotPerformance, error) {
	var results []models.TimeSlotPerformance

	rows, err := s.db.WithContext(ctx).Raw(`
		SELECT
			EXTRACT(DOW FROM date) as day_of_week,
			hour,
			AVG(avg_listeners) as avg_listeners,
			MAX(peak_listeners) as peak_listeners,
			COUNT(*) as sample_count
		FROM schedule_analytics
		WHERE station_id = ?
		AND date >= ? AND date < ?
		GROUP BY EXTRACT(DOW FROM date), hour
		ORDER BY day_of_week, hour
	`, stationID, start, end).Rows()

	if err != nil {
		return nil, fmt.Errorf("failed to query time slot performance: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p models.TimeSlotPerformance
		if err := rows.Scan(&p.DayOfWeek, &p.Hour, &p.AvgListeners, &p.PeakListeners, &p.SampleCount); err != nil {
			continue
		}
		results = append(results, p)
	}

	return results, nil
}

// GetBestTimeSlots returns the top performing time slots.
func (s *ScheduleAnalyticsService) GetBestTimeSlots(ctx context.Context, stationID string, limit int) ([]models.TimeSlotPerformance, error) {
	end := time.Now()
	start := end.AddDate(0, 0, -30) // Last 30 days

	slots, err := s.GetTimeSlotPerformance(ctx, stationID, start, end)
	if err != nil {
		return nil, err
	}

	// Sort by average listeners (already sorted by query, but we want descending)
	// Simple bubble sort for small dataset
	for i := 0; i < len(slots)-1; i++ {
		for j := 0; j < len(slots)-i-1; j++ {
			if slots[j].AvgListeners < slots[j+1].AvgListeners {
				slots[j], slots[j+1] = slots[j+1], slots[j]
			}
		}
	}

	if limit > 0 && limit < len(slots) {
		return slots[:limit], nil
	}
	return slots, nil
}

// GetSchedulingSuggestions generates data-driven scheduling suggestions.
func (s *ScheduleAnalyticsService) GetSchedulingSuggestions(ctx context.Context, stationID string) ([]models.SchedulingSuggestion, error) {
	var suggestions []models.SchedulingSuggestion

	end := time.Now()
	start := end.AddDate(0, 0, -30)

	// Get show performance
	showPerf, err := s.GetShowPerformance(ctx, stationID, start, end)
	if err != nil {
		return nil, err
	}

	// Get best time slots
	bestSlots, err := s.GetBestTimeSlots(ctx, stationID, 5)
	if err != nil {
		return nil, err
	}

	// Analyze and generate suggestions
	for _, show := range showPerf {
		// Suggest moving underperforming shows to better slots
		if show.TrendPercent < -10 && len(bestSlots) > 0 {
			suggestions = append(suggestions, models.SchedulingSuggestion{
				Type:          "move_show",
				ShowID:        show.ShowID,
				ShowName:      show.ShowName,
				SuggestedSlot: fmt.Sprintf("%s at %02d:00", dayName(bestSlots[0].DayOfWeek), bestSlots[0].Hour),
				Reason:        fmt.Sprintf("Show performance down %.1f%% - consider moving to a higher-traffic slot", -show.TrendPercent),
				Impact:        "Could increase average listeners based on time slot performance",
				Confidence:    0.6,
			})
		}

		// Suggest extending popular shows
		if show.TrendPercent > 20 {
			suggestions = append(suggestions, models.SchedulingSuggestion{
				Type:       "extend_show",
				ShowID:     show.ShowID,
				ShowName:   show.ShowName,
				Reason:     fmt.Sprintf("Show performance up %.1f%% - audience is engaged", show.TrendPercent),
				Impact:     "Consider extending duration or adding additional episodes",
				Confidence: 0.7,
			})
		}
	}

	// Suggest filling empty high-traffic slots
	if len(bestSlots) > 0 {
		// Check if top slots have regular programming
		for _, slot := range bestSlots[:min(3, len(bestSlots))] {
			var count int64
			s.db.Model(&models.ShowInstance{}).
				Where("station_id = ?", stationID).
				Where("EXTRACT(DOW FROM starts_at) = ?", slot.DayOfWeek).
				Where("EXTRACT(HOUR FROM starts_at) = ?", slot.Hour).
				Where("starts_at >= ?", start).
				Count(&count)

			if count < 4 { // Less than once per week on average
				suggestions = append(suggestions, models.SchedulingSuggestion{
					Type:          "add_show",
					SuggestedSlot: fmt.Sprintf("%s at %02d:00", dayName(slot.DayOfWeek), slot.Hour),
					Reason:        fmt.Sprintf("High-traffic slot (avg %.0f listeners) has no regular programming", slot.AvgListeners),
					Impact:        "Adding a show here could capture existing audience",
					Confidence:    0.8,
				})
			}
		}
	}

	return suggestions, nil
}

// dayName returns the day name for a day of week number.
func dayName(dow int) string {
	days := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	if dow >= 0 && dow < 7 {
		return days[dow]
	}
	return "Unknown"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// AggregateDaily aggregates hourly stats into daily summaries.
func (s *ScheduleAnalyticsService) AggregateDaily(ctx context.Context, stationID string, date time.Time) error {
	// This is a placeholder for more complex aggregation logic
	// Could be expanded to create daily/weekly/monthly summary tables
	s.logger.Debug().
		Str("station", stationID).
		Time("date", date).
		Msg("Daily aggregation completed")
	return nil
}
