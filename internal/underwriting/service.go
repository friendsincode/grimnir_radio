/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package underwriting

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Service handles sponsor and underwriting management.
type Service struct {
	db     *gorm.DB
	logger zerolog.Logger
}

// NewService creates a new underwriting service.
func NewService(db *gorm.DB, logger zerolog.Logger) *Service {
	return &Service{
		db:     db,
		logger: logger.With().Str("component", "underwriting").Logger(),
	}
}

// CreateSponsor creates a new sponsor.
func (s *Service) CreateSponsor(ctx context.Context, sponsor *models.Sponsor) error {
	if sponsor.ID == "" {
		sponsor.ID = uuid.NewString()
	}

	if err := s.db.WithContext(ctx).Create(sponsor).Error; err != nil {
		return fmt.Errorf("failed to create sponsor: %w", err)
	}

	s.logger.Info().Str("sponsor", sponsor.ID).Str("name", sponsor.Name).Msg("sponsor created")
	return nil
}

// GetSponsor retrieves a sponsor by ID.
func (s *Service) GetSponsor(ctx context.Context, id string) (*models.Sponsor, error) {
	var sponsor models.Sponsor
	if err := s.db.WithContext(ctx).
		Preload("Obligations").
		First(&sponsor, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("sponsor not found: %w", err)
	}
	return &sponsor, nil
}

// ListSponsors returns all sponsors for a station.
func (s *Service) ListSponsors(ctx context.Context, stationID string, activeOnly bool) ([]models.Sponsor, error) {
	var sponsors []models.Sponsor
	query := s.db.WithContext(ctx).Where("station_id = ?", stationID)
	if activeOnly {
		query = query.Where("active = ?", true)
	}
	if err := query.Preload("Obligations").Order("name ASC").Find(&sponsors).Error; err != nil {
		return nil, err
	}
	return sponsors, nil
}

// UpdateSponsor updates a sponsor.
func (s *Service) UpdateSponsor(ctx context.Context, id string, updates map[string]any) error {
	if err := s.db.WithContext(ctx).Model(&models.Sponsor{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update sponsor: %w", err)
	}
	return nil
}

// DeleteSponsor deletes a sponsor and all related data.
func (s *Service) DeleteSponsor(ctx context.Context, id string) error {
	// Get obligations
	var obligations []models.UnderwritingObligation
	s.db.Where("sponsor_id = ?", id).Find(&obligations)

	// Delete spots for each obligation
	for _, obl := range obligations {
		s.db.Where("obligation_id = ?", obl.ID).Delete(&models.UnderwritingSpot{})
	}

	// Delete obligations
	s.db.Where("sponsor_id = ?", id).Delete(&models.UnderwritingObligation{})

	// Delete sponsor
	if err := s.db.WithContext(ctx).Delete(&models.Sponsor{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("failed to delete sponsor: %w", err)
	}

	s.logger.Info().Str("sponsor", id).Msg("sponsor deleted")
	return nil
}

// CreateObligation creates a new underwriting obligation.
func (s *Service) CreateObligation(ctx context.Context, obligation *models.UnderwritingObligation) error {
	if obligation.ID == "" {
		obligation.ID = uuid.NewString()
	}

	if err := s.db.WithContext(ctx).Create(obligation).Error; err != nil {
		return fmt.Errorf("failed to create obligation: %w", err)
	}

	s.logger.Info().
		Str("obligation", obligation.ID).
		Str("sponsor", obligation.SponsorID).
		Int("spots_per_week", obligation.SpotsPerWeek).
		Msg("obligation created")

	return nil
}

// GetObligation retrieves an obligation by ID.
func (s *Service) GetObligation(ctx context.Context, id string) (*models.UnderwritingObligation, error) {
	var obl models.UnderwritingObligation
	if err := s.db.WithContext(ctx).
		Preload("Sponsor").
		Preload("Spots").
		Preload("Media").
		First(&obl, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("obligation not found: %w", err)
	}
	return &obl, nil
}

// ListObligations returns obligations, optionally filtered.
func (s *Service) ListObligations(ctx context.Context, stationID, sponsorID string, activeOnly bool) ([]models.UnderwritingObligation, error) {
	var obligations []models.UnderwritingObligation
	query := s.db.WithContext(ctx)

	if stationID != "" {
		query = query.Where("station_id = ?", stationID)
	}
	if sponsorID != "" {
		query = query.Where("sponsor_id = ?", sponsorID)
	}
	if activeOnly {
		query = query.Where("active = ?", true)
		query = query.Where("end_date IS NULL OR end_date >= ?", time.Now())
	}

	if err := query.Preload("Sponsor").Order("start_date DESC").Find(&obligations).Error; err != nil {
		return nil, err
	}
	return obligations, nil
}

// UpdateObligation updates an obligation.
func (s *Service) UpdateObligation(ctx context.Context, id string, updates map[string]any) error {
	if err := s.db.WithContext(ctx).Model(&models.UnderwritingObligation{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update obligation: %w", err)
	}
	return nil
}

// DeleteObligation deletes an obligation.
func (s *Service) DeleteObligation(ctx context.Context, id string) error {
	// Delete spots first
	s.db.Where("obligation_id = ?", id).Delete(&models.UnderwritingSpot{})

	if err := s.db.WithContext(ctx).Delete(&models.UnderwritingObligation{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("failed to delete obligation: %w", err)
	}
	return nil
}

// ScheduleSpot schedules a single underwriting spot.
func (s *Service) ScheduleSpot(ctx context.Context, obligationID string, scheduledAt time.Time, instanceID *string) (*models.UnderwritingSpot, error) {
	spot := models.NewUnderwritingSpot(obligationID, scheduledAt)
	spot.InstanceID = instanceID

	if err := s.db.WithContext(ctx).Create(spot).Error; err != nil {
		return nil, fmt.Errorf("failed to schedule spot: %w", err)
	}

	return spot, nil
}

// ScheduleWeeklySpots schedules spots for a week based on obligations.
func (s *Service) ScheduleWeeklySpots(ctx context.Context, stationID string, weekStart time.Time) ([]models.UnderwritingSpot, error) {
	weekEnd := weekStart.AddDate(0, 0, 7)

	// Get active obligations
	obligations, err := s.ListObligations(ctx, stationID, "", true)
	if err != nil {
		return nil, err
	}

	var allSpots []models.UnderwritingSpot

	for _, obl := range obligations {
		// Check how many spots already scheduled this week
		var scheduledCount int64
		s.db.Model(&models.UnderwritingSpot{}).
			Where("obligation_id = ?", obl.ID).
			Where("scheduled_at >= ? AND scheduled_at < ?", weekStart, weekEnd).
			Count(&scheduledCount)

		spotsNeeded := obl.SpotsPerWeek - int(scheduledCount)
		if spotsNeeded <= 0 {
			continue
		}

		// Get available show instances for this week
		instances, err := s.getAvailableInstances(ctx, stationID, weekStart, weekEnd, obl.PreferredDayparts, obl.PreferredShows)
		if err != nil {
			s.logger.Warn().Err(err).Str("obligation", obl.ID).Msg("failed to get available instances")
			continue
		}

		// Schedule spots evenly throughout the week
		for i := 0; i < spotsNeeded && i < len(instances); i++ {
			spot, err := s.ScheduleSpot(ctx, obl.ID, instances[i].StartsAt, &instances[i].ID)
			if err != nil {
				s.logger.Warn().Err(err).Msg("failed to schedule spot")
				continue
			}
			allSpots = append(allSpots, *spot)
		}
	}

	s.logger.Info().
		Str("station", stationID).
		Time("week_start", weekStart).
		Int("spots_scheduled", len(allSpots)).
		Msg("weekly spots scheduled")

	return allSpots, nil
}

// getAvailableInstances returns show instances that match daypart/show preferences.
func (s *Service) getAvailableInstances(ctx context.Context, stationID string, start, end time.Time, preferredDayparts, preferredShows string) ([]models.ShowInstance, error) {
	var instances []models.ShowInstance

	query := s.db.WithContext(ctx).
		Where("station_id = ?", stationID).
		Where("starts_at >= ? AND starts_at < ?", start, end).
		Where("status = ?", models.ShowInstanceScheduled)

	// Filter by preferred shows if specified
	if preferredShows != "" {
		showIDs := strings.Split(preferredShows, ",")
		query = query.Where("show_id IN ?", showIDs)
	}

	if err := query.Order("starts_at ASC").Find(&instances).Error; err != nil {
		return nil, err
	}

	// Filter by daypart if specified
	if preferredDayparts != "" {
		dayparts := strings.Split(strings.ToLower(preferredDayparts), ",")
		var filtered []models.ShowInstance
		for _, inst := range instances {
			hour := inst.StartsAt.Hour()
			daypart := getDaypart(hour)
			for _, dp := range dayparts {
				if strings.TrimSpace(dp) == string(daypart) {
					filtered = append(filtered, inst)
					break
				}
			}
		}
		instances = filtered
	}

	return instances, nil
}

// getDaypart returns the daypart for an hour.
func getDaypart(hour int) models.Daypart {
	switch {
	case hour >= 6 && hour < 10:
		return models.DaypartMorning
	case hour >= 10 && hour < 15:
		return models.DaypartMidday
	case hour >= 15 && hour < 19:
		return models.DaypartAfternoon
	case hour >= 19 && hour < 24:
		return models.DaypartEvening
	default:
		return models.DaypartOvernight
	}
}

// MarkSpotAired marks a spot as aired.
func (s *Service) MarkSpotAired(ctx context.Context, spotID string) error {
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&models.UnderwritingSpot{}).
		Where("id = ?", spotID).
		Updates(map[string]any{
			"status":   models.SpotStatusAired,
			"aired_at": now,
		}).Error; err != nil {
		return fmt.Errorf("failed to mark spot aired: %w", err)
	}
	return nil
}

// MarkSpotMissed marks a spot as missed.
func (s *Service) MarkSpotMissed(ctx context.Context, spotID string) error {
	if err := s.db.WithContext(ctx).Model(&models.UnderwritingSpot{}).
		Where("id = ?", spotID).
		Update("status", models.SpotStatusMissed).Error; err != nil {
		return fmt.Errorf("failed to mark spot missed: %w", err)
	}
	return nil
}

// GetFulfillmentReport generates a fulfillment report for a sponsor/obligation.
func (s *Service) GetFulfillmentReport(ctx context.Context, obligationID string, periodStart, periodEnd time.Time) (*models.FulfillmentReport, error) {
	obl, err := s.GetObligation(ctx, obligationID)
	if err != nil {
		return nil, err
	}

	// Calculate weeks in period
	weeks := int(periodEnd.Sub(periodStart).Hours() / 24 / 7)
	if weeks < 1 {
		weeks = 1
	}
	spotsRequired := obl.SpotsPerWeek * weeks

	// Count spots by status
	var scheduled, aired, missed int64
	s.db.Model(&models.UnderwritingSpot{}).
		Where("obligation_id = ?", obligationID).
		Where("scheduled_at >= ? AND scheduled_at < ?", periodStart, periodEnd).
		Where("status = ?", models.SpotStatusScheduled).
		Count(&scheduled)

	s.db.Model(&models.UnderwritingSpot{}).
		Where("obligation_id = ?", obligationID).
		Where("scheduled_at >= ? AND scheduled_at < ?", periodStart, periodEnd).
		Where("status = ?", models.SpotStatusAired).
		Count(&aired)

	s.db.Model(&models.UnderwritingSpot{}).
		Where("obligation_id = ?", obligationID).
		Where("scheduled_at >= ? AND scheduled_at < ?", periodStart, periodEnd).
		Where("status = ?", models.SpotStatusMissed).
		Count(&missed)

	fulfillmentRate := 0.0
	if spotsRequired > 0 {
		fulfillmentRate = float64(aired) / float64(spotsRequired) * 100
	}

	status := "on_track"
	if fulfillmentRate >= 100 {
		status = "fulfilled"
	} else if fulfillmentRate < 80 {
		status = "behind"
	}

	sponsorName := ""
	if obl.Sponsor != nil {
		sponsorName = obl.Sponsor.Name
	}

	return &models.FulfillmentReport{
		SponsorID:       obl.SponsorID,
		SponsorName:     sponsorName,
		ObligationID:    obl.ID,
		ObligationName:  obl.Name,
		PeriodStart:     periodStart,
		PeriodEnd:       periodEnd,
		SpotsRequired:   spotsRequired,
		SpotsScheduled:  int(scheduled + aired + missed),
		SpotsAired:      int(aired),
		SpotsMissed:     int(missed),
		FulfillmentRate: fulfillmentRate,
		Status:          status,
	}, nil
}

// GetAllFulfillmentReports generates reports for all active obligations.
func (s *Service) GetAllFulfillmentReports(ctx context.Context, stationID string, periodStart, periodEnd time.Time) ([]models.FulfillmentReport, error) {
	obligations, err := s.ListObligations(ctx, stationID, "", true)
	if err != nil {
		return nil, err
	}

	var reports []models.FulfillmentReport
	for _, obl := range obligations {
		report, err := s.GetFulfillmentReport(ctx, obl.ID, periodStart, periodEnd)
		if err != nil {
			s.logger.Warn().Err(err).Str("obligation", obl.ID).Msg("failed to generate fulfillment report")
			continue
		}
		reports = append(reports, *report)
	}

	return reports, nil
}

// CheckMissedSpots checks for spots that should have aired but weren't marked.
func (s *Service) CheckMissedSpots(ctx context.Context) (int, error) {
	now := time.Now()

	result := s.db.WithContext(ctx).Model(&models.UnderwritingSpot{}).
		Where("status = ?", models.SpotStatusScheduled).
		Where("scheduled_at < ?", now.Add(-time.Hour)). // 1 hour grace period
		Update("status", models.SpotStatusMissed)

	if result.Error != nil {
		return 0, fmt.Errorf("failed to check missed spots: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		s.logger.Warn().Int64("count", result.RowsAffected).Msg("spots marked as missed")
	}

	return int(result.RowsAffected), nil
}
