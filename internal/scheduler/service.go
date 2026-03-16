/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
	db          *gorm.DB
	planner     *clock.Planner
	engine      *smartblock.Engine
	stateStore  *state.Store
	cache       *cache.Cache
	logger      zerolog.Logger
	lookahead   time.Duration
	warnMu      sync.Mutex
	warnedKeys  map[string]struct{}
	mu          sync.Mutex
	lastCleanup time.Time
}

// New constructs the scheduler service.
func New(db *gorm.DB, planner *clock.Planner, engine *smartblock.Engine, stateStore *state.Store, lookahead time.Duration, logger zerolog.Logger) *Service {
	if lookahead <= 0 {
		lookahead = 24 * time.Hour
	}
	return &Service{
		db:         db,
		planner:    planner,
		engine:     engine,
		stateStore: stateStore,
		lookahead:  lookahead,
		logger:     logger,
		warnedKeys: make(map[string]struct{}),
	}
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

	// Periodically clean up old materialized entries (once per hour)
	s.maybeCleanupOldEntries(ctx)
}

// maybeCleanupOldEntries deletes materialized schedule entries older than 7 days
// and sweeps for orphaned future entries whose source no longer exists.
// Runs at most once per hour to avoid unnecessary DB churn.
func (s *Service) maybeCleanupOldEntries(ctx context.Context) {
	s.mu.Lock()
	if time.Since(s.lastCleanup) < time.Hour {
		s.mu.Unlock()
		return
	}
	s.lastCleanup = time.Now()
	s.mu.Unlock()

	// 1. Delete old materialized instances (>7 days past).
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	result := s.db.WithContext(ctx).
		Where("ends_at < ? AND is_instance = ?", cutoff, true).
		Delete(&models.ScheduleEntry{})
	if result.Error != nil {
		s.logger.Warn().Err(result.Error).Msg("failed to clean up old schedule entries")
	} else if result.RowsAffected > 0 {
		s.logger.Info().Int64("deleted", result.RowsAffected).Msg("cleaned up old materialized schedule entries")
	}

	// 2. Orphan sweep: delete future schedule entries whose source no longer exists.
	// This is the safety net for anything deleted without cascading properly.
	type orphanQuery struct {
		sourceType string
		sql        string
	}
	queries := []orphanQuery{
		{"webstream", `DELETE FROM schedule_entries WHERE source_type = 'webstream' AND starts_at > NOW() AND source_id NOT IN (SELECT id FROM webstreams)`},
		{"smart_block", `DELETE FROM schedule_entries WHERE source_type = 'smart_block' AND starts_at > NOW() AND source_id NOT IN (SELECT id FROM smart_blocks)`},
		{"playlist", `DELETE FROM schedule_entries WHERE source_type = 'playlist' AND starts_at > NOW() AND source_id NOT IN (SELECT id FROM playlists)`},
		{"media_orphan", `DELETE FROM schedule_entries WHERE source_type = 'media' AND starts_at > NOW() AND metadata->>'smart_block_id' IS NOT NULL AND metadata->>'smart_block_id' NOT IN (SELECT id::text FROM smart_blocks)`},
	}
	for _, q := range queries {
		res := s.db.WithContext(ctx).Exec(q.sql)
		if res.Error != nil {
			s.logger.Warn().Err(res.Error).Str("type", q.sourceType).Msg("orphan sweep failed")
		} else if res.RowsAffected > 0 {
			s.logger.Warn().Int64("deleted", res.RowsAffected).Str("source_type", q.sourceType).Msg("orphan sweep removed schedule entries with missing source")
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
		reason, details, action := s.explainNoPlans(ctx, stationID)
		// Stations running on manually-created schedule entries (not clock-template based)
		// will always produce zero clock plans — that is expected and not an error.
		// Only surface the warning when the station has no entries at all in the window.
		var existingCount int64
		s.db.WithContext(ctx).Model(&models.ScheduleEntry{}).
			Where("station_id = ? AND starts_at >= ? AND starts_at < ?", stationID, start, start.Add(s.lookahead)).
			Count(&existingCount)
		if existingCount > 0 {
			s.logger.Debug().
				Str("station_id", stationID).
				Str("reason", reason).
				Int64("existing_entries", existingCount).
				Msg("no clock plans (station uses manual schedule entries)")
		} else {
			s.logger.Info().
				Str("station", stationID).
				Str("reason", reason).
				Str("details", details).
				Str("action", action).
				Msg("no clock plans to generate")
		}
		// Do not return here — continue to direct smart block entry pass below.
	}

	entriesCreated := 0
	for _, plan := range plans {
		if plan.StartsAt.Before(start) {
			continue
		}
		if !s.validatePlanPayload(stationID, plan) {
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
		case string(models.SlotTypeHardItem):
			if err := s.createHardItemEntry(ctx, stationID, plan); err != nil {
				telemetry.SchedulerErrorsTotal.WithLabelValues(stationID, "create_hard_item_entry").Inc()
				return err
			}
			entriesCreated++
		case string(models.SlotTypeStopset):
			if err := s.createStopsetEntry(ctx, stationID, plan); err != nil {
				telemetry.SchedulerErrorsTotal.WithLabelValues(stationID, "create_stopset_entry").Inc()
				return err
			}
			entriesCreated++
		case string(models.SlotTypeWebstream):
			if err := s.createWebstreamEntry(ctx, stationID, plan); err != nil {
				telemetry.SchedulerErrorsTotal.WithLabelValues(stationID, "create_webstream_entry").Inc()
				return err
			}
			entriesCreated++
		default:
			s.logger.Debug().Str("slot_type", plan.SlotType).Msg("unhandled slot type")
		}
	}

	// Also materialize smart block entries placed directly on the schedule
	// (source_type = 'smart_block') without going through a clock template.
	if err := s.materializeDirectSmartBlockEntries(ctx, stationID, start); err != nil {
		s.logger.Warn().Err(err).Str("station", stationID).Msg("direct smart block materialization failed")
		telemetry.SchedulerErrorsTotal.WithLabelValues(stationID, "direct_smart_block").Inc()
	}

	// Record metrics
	duration := time.Since(startTime).Seconds()
	telemetry.ScheduleBuildDuration.WithLabelValues(stationID).Observe(duration)
	telemetry.ScheduleEntriesTotal.WithLabelValues(stationID).Add(float64(entriesCreated))

	return nil
}

// materializeDirectSmartBlockEntries processes schedule entries where source_type='smart_block'
// that were placed directly on the calendar (not via a clock template). The scheduler normally
// only processes clock-template-derived plans, so these entries would never be materialized
// into concrete media entries without this second pass.
func (s *Service) materializeDirectSmartBlockEntries(ctx context.Context, stationID string, start time.Time) error {
	var entries []models.ScheduleEntry
	// Use ends_at > start so that in-progress slots (starts_at < now but not yet ended)
	// are also caught, not just future slots.
	err := s.db.WithContext(ctx).
		Where("station_id = ? AND source_type = 'smart_block' AND ends_at > ? AND starts_at <= ?",
			stationID, start, start.Add(s.lookahead)).
		Find(&entries).Error
	if err != nil {
		return err
	}

	for _, entry := range entries {
		mountID := entry.MountID
		if mountID == "" {
			mountID = s.getDefaultMountID(ctx, stationID)
		}

		// Check if already materialized: any media entry in this window generated by this smart block.
		var count int64
		if err := s.db.WithContext(ctx).Model(&models.ScheduleEntry{}).
			Where("station_id = ? AND mount_id = ? AND source_type = 'media' AND metadata->>'smart_block_id' = ? AND starts_at >= ? AND starts_at < ?",
				stationID, mountID, entry.SourceID, entry.StartsAt, entry.EndsAt).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue // Already materialized; skip.
		}

		plan := clock.SlotPlan{
			SlotID:   entry.ID,
			StartsAt: entry.StartsAt,
			EndsAt:   entry.EndsAt,
			Duration: entry.EndsAt.Sub(entry.StartsAt),
			SlotType: string(models.SlotTypeSmartBlock),
			Payload: map[string]any{
				"smart_block_id": entry.SourceID,
				"mount_id":       mountID,
			},
		}

		if err := s.materializeSmartBlock(ctx, stationID, plan); err != nil {
			s.logger.Warn().Err(err).
				Str("station", stationID).
				Str("entry_id", entry.ID).
				Str("smart_block_id", entry.SourceID).
				Msg("failed to materialize direct smart block entry")
			// Continue with remaining entries rather than aborting.
			continue
		}
	}
	return nil
}

func (s *Service) validatePlanPayload(stationID string, plan clock.SlotPlan) bool {
	switch plan.SlotType {
	case string(models.SlotTypeHardItem):
		if stringValue(plan.Payload["media_id"]) == "" {
			s.warnOnce("hard_item_missing_media_id:"+stationID+":"+plan.SlotID, func(e *zerolog.Event) {
				e.Str("station", stationID).Str("slot", plan.SlotID).Msg("hard item slot missing media_id")
			})
			return false
		}
	case string(models.SlotTypePlaylist):
		if stringValue(plan.Payload["playlist_id"]) == "" {
			s.warnOnce("playlist_missing_playlist_id:"+stationID+":"+plan.SlotID, func(e *zerolog.Event) {
				e.Str("station", stationID).Str("slot", plan.SlotID).Msg("playlist slot missing playlist_id")
			})
			return false
		}
	case string(models.SlotTypeWebstream):
		if stringValue(plan.Payload["webstream_id"]) == "" {
			s.warnOnce("webstream_missing_webstream_id:"+stationID+":"+plan.SlotID, func(e *zerolog.Event) {
				e.Str("station", stationID).Str("slot", plan.SlotID).Msg("webstream slot missing webstream_id")
			})
			return false
		}
	}

	return true
}

func (s *Service) warnOnce(key string, logFn func(e *zerolog.Event)) {
	s.warnMu.Lock()
	if s.warnedKeys == nil {
		s.warnedKeys = make(map[string]struct{})
	}
	if _, ok := s.warnedKeys[key]; ok {
		s.warnMu.Unlock()
		return
	}
	s.warnedKeys[key] = struct{}{}
	s.warnMu.Unlock()

	logFn(s.logger.Warn())
}

func (s *Service) explainNoPlans(ctx context.Context, stationID string) (reason, details, action string) {
	var clockHours []models.ClockHour
	err := s.db.WithContext(ctx).
		Where("station_id = ?", stationID).
		Preload("Slots").
		Order("created_at ASC").
		Find(&clockHours).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || len(clockHours) == 0 {
		return "no_clock_template", "No clock template exists for this station.", "Create a Clock Template and add at least one slot."
	}
	if err != nil {
		return "clock_lookup_failed", "Scheduler could not inspect clock configuration: " + err.Error(), "Check database health and retry scheduler."
	}
	for _, clockHour := range clockHours {
		if len(clockHour.Slots) > 0 {
			return "no_slots_generated", "Clock templates exist, but no slot plans were generated for the requested window.", "Verify clock start/end hour windows, slot offsets/durations, and scheduler lookahead."
		}
	}
	return "clock_has_no_slots", "Clock templates exist, but all are empty (zero slots).", "Edit a Clock Template and add at least one slot (playlist, smart block, webstream, etc.)."
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
		Where("mount_id = ?", mountID).
		Where("starts_at < ? AND ends_at > ?", plan.EndsAt, plan.StartsAt).
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

	// Load loop-to-fill preference from the smart block's saved rules.
	var loopToFill bool
	var sb models.SmartBlock
	if err := s.db.WithContext(ctx).Select("rules").First(&sb, "id = ?", blockID).Error; err == nil {
		if v, ok := sb.Rules["loopToFill"].(bool); ok {
			loopToFill = v
		}
	}

	result, err := s.engine.Generate(ctx, smartblock.GenerateRequest{
		SmartBlockID: blockID,
		Seed:         plan.StartsAt.Unix(),
		Duration:     targetDuration.Milliseconds(),
		StationID:    stationID,
		MountID:      mountID,
		LoopToFill:   loopToFill,
	})

	// Record smart block materialization duration
	duration := time.Since(startTime).Seconds()
	telemetry.SmartBlockMaterializeDuration.WithLabelValues(stationID, blockID).Observe(duration)

	if err != nil {
		if errors.Is(err, smartblock.ErrUnresolved) {
			s.logger.Warn().Str("smart_block", blockID).Str("station", stationID).Msg("smart block unresolved - attempting emergency fallback")
			return s.pickRandomTrack(ctx, stationID, mountID, plan)
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
			IsInstance: true,
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
		s.warnOnce("playlist_missing_playlist_id:"+stationID+":"+plan.SlotID, func(e *zerolog.Event) {
			e.Str("station", stationID).Str("slot", plan.SlotID).Msg("playlist slot missing playlist_id")
		})
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
		IsInstance: true,
		Metadata:   plan.Payload,
	}
	return s.db.WithContext(ctx).Create(&entry).Error
}

func (s *Service) createHardItemEntry(ctx context.Context, stationID string, plan clock.SlotPlan) error {
	mountID := stringValue(plan.Payload["mount_id"])
	if mountID == "" {
		mountID = s.getDefaultMountID(ctx, stationID)
	}
	if mountID == "" {
		s.logger.Warn().Str("slot", plan.SlotID).Str("station", stationID).Msg("no mount found for hard item entry")
		return nil
	}
	mediaID := stringValue(plan.Payload["media_id"])
	if mediaID == "" {
		s.warnOnce("hard_item_missing_media_id:"+stationID+":"+plan.SlotID, func(e *zerolog.Event) {
			e.Str("station", stationID).Str("slot", plan.SlotID).Msg("hard item slot missing media_id")
		})
		return nil
	}
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   plan.StartsAt,
		EndsAt:     plan.EndsAt,
		SourceType: "media",
		SourceID:   mediaID,
		IsInstance: true,
		Metadata: map[string]any{
			"slot_type": string(models.SlotTypeHardItem),
		},
	}
	for k, v := range plan.Payload {
		entry.Metadata[k] = v
	}
	return s.db.WithContext(ctx).Create(&entry).Error
}

func (s *Service) createStopsetEntry(ctx context.Context, stationID string, plan clock.SlotPlan) error {
	mountID := stringValue(plan.Payload["mount_id"])
	if mountID == "" {
		mountID = s.getDefaultMountID(ctx, stationID)
	}
	if mountID == "" {
		s.logger.Warn().Str("slot", plan.SlotID).Str("station", stationID).Msg("no mount found for stopset entry")
		return nil
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   plan.StartsAt,
		EndsAt:     plan.EndsAt,
		SourceType: "stopset",
		IsInstance: true,
		Metadata: map[string]any{
			"slot_type": string(models.SlotTypeStopset),
		},
	}
	for k, v := range plan.Payload {
		entry.Metadata[k] = v
	}

	if playlistID := stringValue(plan.Payload["playlist_id"]); playlistID != "" {
		entry.SourceType = "playlist"
		entry.SourceID = playlistID
	} else if mediaID := stringValue(plan.Payload["media_id"]); mediaID != "" {
		entry.SourceType = "media"
		entry.SourceID = mediaID
	}
	return s.db.WithContext(ctx).Create(&entry).Error
}

func (s *Service) createWebstreamEntry(ctx context.Context, stationID string, plan clock.SlotPlan) error {
	mountID := stringValue(plan.Payload["mount_id"])
	if mountID == "" {
		mountID = s.getDefaultMountID(ctx, stationID)
	}
	if mountID == "" {
		s.logger.Warn().Str("slot", plan.SlotID).Str("station", stationID).Msg("no mount found for webstream entry")
		return nil
	}
	webstreamID := stringValue(plan.Payload["webstream_id"])
	if webstreamID == "" {
		s.warnOnce("webstream_missing_webstream_id:"+stationID+":"+plan.SlotID, func(e *zerolog.Event) {
			e.Str("station", stationID).Str("slot", plan.SlotID).Msg("webstream slot missing webstream_id")
		})
		return nil
	}
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   plan.StartsAt,
		EndsAt:     plan.EndsAt,
		SourceType: "webstream",
		SourceID:   webstreamID,
		IsInstance: true,
		Metadata: map[string]any{
			"slot_type": string(models.SlotTypeWebstream),
		},
	}
	for k, v := range plan.Payload {
		entry.Metadata[k] = v
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

// pickRandomTrack selects one random analyzed track for the station and creates
// an emergency schedule entry. This is the last-resort safety net to prevent dead air
// when smart block generation fails completely.
func (s *Service) pickRandomTrack(ctx context.Context, stationID, mountID string, plan clock.SlotPlan) error {
	var item models.MediaItem
	err := s.db.WithContext(ctx).
		Where("station_id = ? AND analysis_state != ? AND duration > 0", stationID, models.AnalysisFailed).
		Order("RANDOM()").
		First(&item).Error
	if err != nil {
		s.logger.Error().Err(err).
			Str("station", stationID).
			Msg("CRITICAL: dead air possible - no analyzed media for emergency fallback")
		return fmt.Errorf("no analyzed media available for station %s: %w", stationID, err)
	}

	dur := item.Duration
	if dur <= 0 {
		dur = 3 * time.Minute
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   plan.StartsAt,
		EndsAt:     plan.StartsAt.Add(dur),
		SourceType: "media",
		SourceID:   item.ID,
		IsInstance: true,
		Metadata: map[string]any{
			"emergency_fallback": true,
		},
	}

	s.logger.Warn().
		Str("station", stationID).
		Str("media_id", item.ID).
		Str("title", item.Title).
		Msg("using emergency fallback track to prevent dead air")

	return s.db.WithContext(ctx).Create(&entry).Error
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
