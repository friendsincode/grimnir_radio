/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/cache"
	"github.com/friendsincode/grimnir_radio/internal/clock"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/scheduler/state"
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
	"github.com/friendsincode/grimnir_radio/internal/telemetry"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// Service orchestrates the rolling playout plan.
type Service struct {
	db         *gorm.DB
	planner    *clock.Planner
	engine     *smartblock.Engine
	stateStore *state.Store
	cache      *cache.Cache
	logger     zerolog.Logger
	lookahead  time.Duration
}

// New constructs the scheduler service.
func New(db *gorm.DB, planner *clock.Planner, engine *smartblock.Engine, stateStore *state.Store, lookahead time.Duration, logger zerolog.Logger) *Service {
	if lookahead <= 0 {
		lookahead = 24 * time.Hour
	}
	return &Service{db: db, planner: planner, engine: engine, stateStore: stateStore, lookahead: lookahead, logger: logger}
}

// SetCache sets the cache instance for the scheduler.
func (s *Service) SetCache(c *cache.Cache) {
	s.cache = c
}

// Run executes the scheduler loop until the context is cancelled.
func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	s.logger.Info().Msg("scheduler loop started")
	for {
		select {
		case <-ctx.Done():
			s.logger.Info().Msg("scheduler loop stopped")
			return ctx.Err()
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	telemetry.SchedulerTicksTotal.Inc()

	// Try to get station list from cache first
	stationIDs, err := s.getStationIDs(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("scheduler failed to load stations")
		telemetry.SchedulerErrorsTotal.WithLabelValues("", "load_stations").Inc()
		return
	}

	for _, stationID := range stationIDs {
		if err := s.scheduleStation(ctx, stationID); err != nil {
			s.logger.Warn().Err(err).Str("station", stationID).Msg("station scheduling failed")
			telemetry.SchedulerErrorsTotal.WithLabelValues(stationID, "schedule_station").Inc()
		}
	}
}

// getStationIDs retrieves station IDs, using cache when available.
func (s *Service) getStationIDs(ctx context.Context) ([]string, error) {
	// Try cache first
	if s.cache != nil {
		if cached, ok := s.cache.GetStationList(ctx); ok {
			ids := make([]string, len(cached))
			for i, station := range cached {
				ids[i] = station.ID
			}
			return ids, nil
		}
	}

	// Fallback to database
	var stations []models.Station
	if err := s.db.WithContext(ctx).Select("id").Find(&stations).Error; err != nil {
		return nil, err
	}

	// Populate cache for next time
	if s.cache != nil {
		cached := make([]cache.CachedStation, len(stations))
		for i, station := range stations {
			cached[i] = cache.CachedStation{
				ID: station.ID,
			}
		}
		if err := s.cache.SetStationList(ctx, cached); err != nil {
			s.logger.Debug().Err(err).Msg("failed to cache station list")
		}
	}

	ids := make([]string, len(stations))
	for i, station := range stations {
		ids[i] = station.ID
	}
	return ids, nil
}

func (s *Service) scheduleStation(ctx context.Context, stationID string) error {
	// Start tracing span
	ctx, span := telemetry.StartSpan(ctx, "scheduler", "scheduleStation")
	defer span.End()
	telemetry.AddSpanAttributes(span, map[string]any{
		"station_id": stationID,
	})

	startTime := time.Now()
	start := startTime.UTC()

	plans, err := s.planner.Compile(stationID, start, s.lookahead)
	if err != nil {
		telemetry.RecordError(span, err)
		telemetry.SchedulerErrorsTotal.WithLabelValues(stationID, "compile").Inc()
		return err
	}

	if len(plans) == 0 {
		s.logger.Debug().Str("station", stationID).Msg("no plans to generate")
		telemetry.ScheduleBuildDuration.WithLabelValues(stationID).Observe(time.Since(startTime).Seconds())
		return nil
	}

	entriesCreated := 0
	for _, plan := range plans {
		if plan.StartsAt.Before(start) {
			continue
		}

		alreadyScheduled, err := s.slotAlreadyScheduled(ctx, stationID, plan)
		if err != nil {
			telemetry.SchedulerErrorsTotal.WithLabelValues(stationID, "check_scheduled").Inc()
			return err
		}
		if alreadyScheduled {
			continue
		}

		switch plan.SlotType {
		case string(models.SlotTypeSmartBlock):
			if err := s.materializeSmartBlock(ctx, stationID, plan); err != nil {
				telemetry.SchedulerErrorsTotal.WithLabelValues(stationID, "materialize_smart_block").Inc()
				return err
			}
			entriesCreated++
		case string(models.SlotTypePlaylist):
			if err := s.createPlaylistEntry(ctx, stationID, plan); err != nil {
				telemetry.SchedulerErrorsTotal.WithLabelValues(stationID, "create_playlist_entry").Inc()
				return err
			}
			entriesCreated++
		case string(models.SlotTypeHardItem), string(models.SlotTypeStopset), string(models.SlotTypeWebstream):
			if err := s.createPlaceholderEntry(ctx, stationID, plan); err != nil {
				telemetry.SchedulerErrorsTotal.WithLabelValues(stationID, "create_placeholder").Inc()
				return err
			}
			entriesCreated++
		default:
			s.logger.Debug().Str("slot_type", plan.SlotType).Msg("unhandled slot type")
		}
	}

	// Record metrics
	duration := time.Since(startTime).Seconds()
	telemetry.ScheduleBuildDuration.WithLabelValues(stationID).Observe(duration)
	telemetry.ScheduleEntriesTotal.WithLabelValues(stationID).Add(float64(entriesCreated))

	return nil
}

func (s *Service) slotAlreadyScheduled(ctx context.Context, stationID string, plan clock.SlotPlan) (bool, error) {
	mountID := stringValue(plan.Payload["mount_id"])
	// If mount_id is empty, try to get the station's default mount
	if mountID == "" {
		mountID = s.getDefaultMountID(ctx, stationID)
	}
	// If still no mount, we can't check for duplicates, so return false to allow
	// the entry creation which will also handle the missing mount_id
	if mountID == "" {
		return false, nil
	}
	var count int64
	err := s.db.WithContext(ctx).
		Model(&models.ScheduleEntry{}).
		Where("station_id = ?", stationID).
		Where("starts_at = ?", plan.StartsAt).
		Where("mount_id = ?", mountID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// getDefaultMountID retrieves the first mount for a station, using cache when available.
func (s *Service) getDefaultMountID(ctx context.Context, stationID string) string {
	// Try cache first
	if s.cache != nil {
		if cached, ok := s.cache.GetDefaultMount(ctx, stationID); ok {
			return cached.ID
		}
	}

	// Fallback to database
	var mount models.Mount
	err := s.db.WithContext(ctx).
		Where("station_id = ?", stationID).
		Order("created_at ASC").
		First(&mount).Error
	if err != nil {
		return ""
	}

	// Cache the result
	if s.cache != nil {
		cached := &cache.CachedMount{
			ID:         mount.ID,
			StationID:  mount.StationID,
			Name:       mount.Name,
			URL:        mount.URL,
			Format:     mount.Format,
			Bitrate:    mount.Bitrate,
			Channels:   mount.Channels,
			SampleRate: mount.SampleRate,
		}
		if err := s.cache.SetDefaultMount(ctx, stationID, cached); err != nil {
			s.logger.Debug().Err(err).Str("station_id", stationID).Msg("failed to cache default mount")
		}
	}

	return mount.ID
}

func (s *Service) materializeSmartBlock(ctx context.Context, stationID string, plan clock.SlotPlan) error {
	startTime := time.Now()

	blockID := stringValue(plan.Payload["smart_block_id"])
	mountID := stringValue(plan.Payload["mount_id"])
	// If mount_id is missing, use the station's default mount
	if mountID == "" {
		mountID = s.getDefaultMountID(ctx, stationID)
	}
	if blockID == "" {
		s.logger.Warn().Str("slot", plan.SlotID).Msg("smart block slot missing smart_block_id")
		return nil
	}
	if mountID == "" {
		s.logger.Warn().Str("slot", plan.SlotID).Str("station", stationID).Msg("no mount found for station")
		return nil
	}

	targetDuration := plan.Duration
	if targetDuration <= 0 {
		targetDuration = plan.EndsAt.Sub(plan.StartsAt)
	}

	result, err := s.engine.Generate(ctx, smartblock.GenerateRequest{
		SmartBlockID: blockID,
		Seed:         plan.StartsAt.Unix(),
		Duration:     targetDuration.Milliseconds(),
		StationID:    stationID,
		MountID:      mountID,
	})

	// Record smart block materialization duration
	duration := time.Since(startTime).Seconds()
	telemetry.SmartBlockMaterializeDuration.WithLabelValues(stationID, blockID).Observe(duration)

	if err != nil {
		if errors.Is(err, smartblock.ErrUnresolved) {
			s.logger.Debug().Str("smart_block", blockID).Msg("smart block unresolved - no analyzed media available")
			return nil
		}
		return err
	}

	entries := make([]models.ScheduleEntry, 0, len(result.Items))
	for _, item := range result.Items {
		entry := models.ScheduleEntry{
			ID:         uuid.NewString(),
			StationID:  stationID,
			MountID:    mountID,
			StartsAt:   plan.StartsAt.Add(time.Duration(item.StartsAtMS) * time.Millisecond),
			EndsAt:     plan.StartsAt.Add(time.Duration(item.EndsAtMS) * time.Millisecond),
			SourceType: "media",
			SourceID:   item.MediaID,
			Metadata: map[string]any{
				"smart_block_id": blockID,
				"intro_end":      item.IntroEnd,
				"outro_in":       item.OutroIn,
				"energy":         item.Energy,
			},
		}
		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return nil
	}

	return s.db.WithContext(ctx).Create(&entries).Error
}

func (s *Service) createPlaylistEntry(ctx context.Context, stationID string, plan clock.SlotPlan) error {
	mountID := stringValue(plan.Payload["mount_id"])
	if mountID == "" {
		mountID = s.getDefaultMountID(ctx, stationID)
	}
	if mountID == "" {
		s.logger.Warn().Str("slot", plan.SlotID).Str("station", stationID).Msg("no mount found for playlist entry")
		return nil
	}

	playlistID := stringValue(plan.Payload["playlist_id"])
	if playlistID == "" {
		s.logger.Warn().Str("slot", plan.SlotID).Msg("playlist slot missing playlist_id")
		return nil
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   plan.StartsAt,
		EndsAt:     plan.EndsAt,
		SourceType: "playlist",
		SourceID:   playlistID,
		Metadata:   plan.Payload,
	}
	return s.db.WithContext(ctx).Create(&entry).Error
}

func (s *Service) createPlaceholderEntry(ctx context.Context, stationID string, plan clock.SlotPlan) error {
	mountID := stringValue(plan.Payload["mount_id"])
	// If mount_id is missing, use the station's default mount
	if mountID == "" {
		mountID = s.getDefaultMountID(ctx, stationID)
	}
	if mountID == "" {
		s.logger.Warn().Str("slot", plan.SlotID).Str("station", stationID).Msg("no mount found for placeholder entry")
		return nil
	}
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   plan.StartsAt,
		EndsAt:     plan.EndsAt,
		SourceType: plan.SlotType,
		Metadata:   plan.Payload,
	}
	return s.db.WithContext(ctx).Create(&entry).Error
}

// Materialize exposes smart block generation for APIs.
func (s *Service) Materialize(ctx context.Context, req smartblock.GenerateRequest) (smartblock.GenerateResult, error) {
	return s.engine.Generate(ctx, req)
}

// RefreshStation triggers immediate scheduling for a station.
func (s *Service) RefreshStation(ctx context.Context, stationID string) error {
	return s.scheduleStation(ctx, stationID)
}

// Upcoming returns upcoming schedule entries within horizon.
// Simulate returns slot plans calculated by the planner.
func (s *Service) Simulate(ctx context.Context, stationID string, start time.Time, horizon time.Duration) ([]clock.SlotPlan, error) {
	return s.planner.Compile(stationID, start, horizon)
}

func (s *Service) SimulateClock(ctx context.Context, clockID string, start time.Time, horizon time.Duration) ([]clock.SlotPlan, error) {
	return s.planner.CompileForClock(clockID, start, horizon)
}

func (s *Service) Upcoming(ctx context.Context, stationID string, from time.Time, horizon time.Duration) ([]models.ScheduleEntry, error) {
	if horizon <= 0 {
		horizon = 24 * time.Hour
	}
	var entries []models.ScheduleEntry
	err := s.db.WithContext(ctx).
		Where("station_id = ?", stationID).
		Where("starts_at >= ?", from).
		Where("starts_at <= ?", from.Add(horizon)).
		Order("starts_at ASC").
		Find(&entries).Error
	return entries, err
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}
