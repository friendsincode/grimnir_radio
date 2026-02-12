/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package analytics

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ListenerCounter is implemented by components that can return current listeners for a station.
type ListenerCounter interface {
	ListenerCount(ctx context.Context, stationID string) (int, error)
}

// ListenerAnalyticsService periodically captures listener snapshots for trend analytics.
type ListenerAnalyticsService struct {
	db      *gorm.DB
	counter ListenerCounter
	logger  zerolog.Logger

	interval  time.Duration
	retention time.Duration
}

// NewListenerAnalyticsService creates a new listener analytics snapshot service.
func NewListenerAnalyticsService(db *gorm.DB, counter ListenerCounter, logger zerolog.Logger) *ListenerAnalyticsService {
	return &ListenerAnalyticsService{
		db:        db,
		counter:   counter,
		logger:    logger.With().Str("component", "listener_analytics").Logger(),
		interval:  time.Minute,
		retention: 30 * 24 * time.Hour,
	}
}

// Start begins periodic listener snapshot capture.
func (s *ListenerAnalyticsService) Start(ctx context.Context) {
	if s.counter == nil {
		s.logger.Warn().Msg("listener analytics disabled: no listener counter available")
		return
	}

	s.logger.Info().Dur("interval", s.interval).Dur("retention", s.retention).Msg("listener analytics sampler started")

	// Capture once immediately so data appears quickly after startup.
	s.captureSnapshot(ctx, time.Now())

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info().Msg("listener analytics sampler stopped")
			return
		case t := <-ticker.C:
			s.captureSnapshot(ctx, t)
			s.pruneOldSamples(ctx, t)
		}
	}
}

func (s *ListenerAnalyticsService) captureSnapshot(ctx context.Context, now time.Time) {
	var stations []models.Station
	if err := s.db.WithContext(ctx).Select("id").Where("active = ?", true).Find(&stations).Error; err != nil {
		s.logger.Warn().Err(err).Msg("failed to load stations for listener snapshot")
		return
	}

	for _, station := range stations {
		count, err := s.counter.ListenerCount(ctx, station.ID)
		if err != nil {
			s.logger.Warn().Err(err).Str("station_id", station.ID).Msg("failed to read listener count")
			continue
		}

		sample := models.ListenerSample{
			ID:         uuid.NewString(),
			StationID:  station.ID,
			Listeners:  count,
			CapturedAt: now.UTC(),
			CreatedAt:  now.UTC(),
		}
		if err := s.db.WithContext(ctx).Create(&sample).Error; err != nil {
			s.logger.Warn().Err(err).Str("station_id", station.ID).Msg("failed to store listener snapshot")
		}
	}
}

func (s *ListenerAnalyticsService) pruneOldSamples(ctx context.Context, now time.Time) {
	cutoff := now.Add(-s.retention).UTC()
	if err := s.db.WithContext(ctx).Where("captured_at < ?", cutoff).Delete(&models.ListenerSample{}).Error; err != nil {
		s.logger.Warn().Err(err).Msg("failed to prune old listener samples")
	}
}
