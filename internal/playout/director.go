/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package playout

import (
	"context"
	"fmt"
	"sync"
	"time"

    "github.com/friendsincode/grimnir_radio/internal/events"
    "github.com/friendsincode/grimnir_radio/internal/models"
    "github.com/friendsincode/grimnir_radio/internal/webstream"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type playoutState struct {
	MediaID   string
	EntryID   string
	StationID string
	Started   time.Time
	Ends      time.Time
}

// Director drives schedule execution and emits now playing events.
type Director struct {
	db             *gorm.DB
	manager        *Manager
	bus            *events.Bus
	webstreamSvc   *webstream.Service
	logger         zerolog.Logger

	mu     sync.Mutex
	played map[string]time.Time
	active map[string]playoutState
}

// NewDirector creates a playout director.
func NewDirector(db *gorm.DB, manager *Manager, bus *events.Bus, webstreamSvc *webstream.Service, logger zerolog.Logger) *Director {
	return &Director{
		db:           db,
		manager:      manager,
		bus:          bus,
		webstreamSvc: webstreamSvc,
		logger:       logger,
		played:       make(map[string]time.Time),
		active:       make(map[string]playoutState),
	}
}

// Run executes the director loop until context cancellation.
func (d *Director) Run(ctx context.Context) error {
	d.logger.Info().Msg("playout director started")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.logger.Info().Msg("playout director stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := d.tick(ctx); err != nil {
				d.logger.Error().Err(err).Msg("playout director tick failed")
			}
		}
	}
}

func (d *Director) tick(ctx context.Context) error {
	now := time.Now().UTC()
	d.prunePlayed(now)

	var entries []models.ScheduleEntry
	err := d.db.WithContext(ctx).
		Where("starts_at <= ?", now.Add(5*time.Second)).
		Where("ends_at >= ?", now.Add(-30*time.Second)).
		Order("starts_at ASC").
		Find(&entries).Error
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.StartsAt.After(now) {
			continue
		}

		if d.isPlayed(entry.ID) {
			continue
		}

		if err := d.handleEntry(ctx, entry); err != nil {
			d.logger.Warn().Err(err).Str("entry", entry.ID).Msg("failed to handle schedule entry")
			continue
		}

		d.markPlayed(entry.ID, entry.EndsAt)
	}

	d.emitHealthSnapshot()
	return nil
}

func (d *Director) handleEntry(ctx context.Context, entry models.ScheduleEntry) error {
	switch entry.SourceType {
	case "media":
		return d.startMediaEntry(ctx, entry)
	case "webstream":
		return d.startWebstreamEntry(ctx, entry)
	default:
		d.publishNowPlaying(entry, nil)
		return nil
	}
}

func (d *Director) startMediaEntry(ctx context.Context, entry models.ScheduleEntry) error {
	var media models.MediaItem
	err := d.db.WithContext(ctx).First(&media, "id = ?", entry.SourceID).Error
	if err != nil {
		return err
	}

	launch := fmt.Sprintf("filesrc location=%q ! decodebin ! audioconvert ! audioresample ! queue max-size-buffers=0 max-size-time=0 ! audioconvert ! autoaudiosink sync=true", media.Path)

	d.mu.Lock()
	prev, hasPrev := d.active[entry.MountID]
	d.active[entry.MountID] = playoutState{MediaID: media.ID, EntryID: entry.ID, StationID: entry.StationID, Started: entry.StartsAt, Ends: entry.EndsAt}
	d.mu.Unlock()

	if err := d.manager.StopPipeline(entry.MountID); err != nil {
		d.logger.Debug().Err(err).Str("mount", entry.MountID).Msg("stop pipeline failed")
	}

	if err := d.manager.EnsurePipeline(ctx, entry.MountID, launch); err != nil {
		d.logger.Warn().Err(err).Str("mount", entry.MountID).Msg("failed to start pipeline")
	}

	payload := map[string]any{
		"media_id": media.ID,
		"title":    media.Title,
		"artist":   media.Artist,
		"album":    media.Album,
	}

	if hasPrev && prev.MediaID != media.ID {
		d.bus.Publish(events.EventHealth, events.Payload{
			"station_id":        entry.StationID,
			"mount_id":          entry.MountID,
			"previous_media":    prev.MediaID,
			"previous_entry_id": prev.EntryID,
			"current_media":     media.ID,
			"entry_id":          entry.ID,
			"event":             "crossfade",
		})
	}

	d.publishNowPlaying(entry, payload)
	d.scheduleStop(entry.MountID, entry.EndsAt)

	return nil
}

func (d *Director) startWebstreamEntry(ctx context.Context, entry models.ScheduleEntry) error {
	// Get webstream ID from metadata or SourceID
	webstreamID := entry.SourceID
	if webstreamID == "" {
		if id, ok := entry.Metadata["webstream_id"].(string); ok {
			webstreamID = id
		}
	}

	if webstreamID == "" {
		return fmt.Errorf("webstream_id not found in entry")
	}

	// Load webstream from database
	ws, err := d.webstreamSvc.GetWebstream(ctx, webstreamID)
	if err != nil {
		return fmt.Errorf("failed to load webstream: %w", err)
	}

	// Get current URL (respects failover state)
	currentURL := ws.GetCurrentURL()
	if currentURL == "" {
		return fmt.Errorf("no URL configured for webstream %s", webstreamID)
	}

	// Build GStreamer pipeline for webstream
	// souphttpsrc for HTTP/Icecast streams with ICY metadata
	pipeline := fmt.Sprintf("souphttpsrc location=%q is-live=true do-timestamp=true", currentURL)

	// Add ICY metadata extraction if passthrough is enabled
	if ws.PassthroughMetadata {
		pipeline += " iradio-mode=true"
	}

	// Add buffer
	if ws.BufferSizeMS > 0 {
		pipeline += fmt.Sprintf(" ! queue max-size-time=%d000000", ws.BufferSizeMS) // Convert ms to ns
	}

	// Add decoder and output
	pipeline += " ! decodebin ! audioconvert ! audioresample ! queue max-size-buffers=0 max-size-time=0 ! audioconvert ! autoaudiosink sync=true"

	d.mu.Lock()
	prev, hasPrev := d.active[entry.MountID]
	d.active[entry.MountID] = playoutState{
		MediaID:   webstreamID, // Store webstream ID in MediaID field for tracking
		EntryID:   entry.ID,
		StationID: entry.StationID,
		Started:   entry.StartsAt,
		Ends:      entry.EndsAt,
	}
	d.mu.Unlock()

	// Stop previous pipeline
	if err := d.manager.StopPipeline(entry.MountID); err != nil {
		d.logger.Debug().Err(err).Str("mount", entry.MountID).Msg("stop pipeline failed")
	}

	// Start webstream pipeline
	if err := d.manager.EnsurePipeline(ctx, entry.MountID, pipeline); err != nil {
		d.logger.Warn().Err(err).Str("mount", entry.MountID).Msg("failed to start webstream pipeline")
		return err
	}

	// Build metadata payload
	payload := map[string]any{
		"webstream_id":   ws.ID,
		"webstream_name": ws.Name,
		"url":            currentURL,
		"health_status":  ws.HealthStatus,
	}

	// Add custom metadata if override is enabled
	if ws.OverrideMetadata && ws.CustomMetadata != nil {
		for k, v := range ws.CustomMetadata {
			payload[k] = v
		}
	}

	if hasPrev && prev.MediaID != webstreamID {
		d.bus.Publish(events.EventHealth, events.Payload{
			"station_id":        entry.StationID,
			"mount_id":          entry.MountID,
			"previous_source":   prev.MediaID,
			"previous_entry_id": prev.EntryID,
			"current_source":    webstreamID,
			"entry_id":          entry.ID,
			"event":             "source_change",
		})
	}

	d.publishNowPlaying(entry, payload)
	d.scheduleStop(entry.MountID, entry.EndsAt)

	return nil
}

func (d *Director) scheduleStop(mountID string, endsAt time.Time) {
	delay := time.Until(endsAt)
	if delay < 0 {
		delay = 0
	}
	go func(expected time.Time) {
		timer := time.NewTimer(delay + 200*time.Millisecond)
		defer timer.Stop()
		<-timer.C

		d.mu.Lock()
		state, ok := d.active[mountID]
		if !ok || state.Ends.After(expected.Add(500*time.Millisecond)) {
			d.mu.Unlock()
			return
		}
		delete(d.active, mountID)
		d.mu.Unlock()

		if err := d.manager.StopPipeline(mountID); err != nil {
			d.logger.Debug().Err(err).Str("mount", mountID).Msg("scheduled stop failed")
		}
		d.bus.Publish(events.EventHealth, events.Payload{
			"station_id": state.StationID,
			"mount_id":   mountID,
			"entry_id":   state.EntryID,
			"media_id":   state.MediaID,
			"starts_at":  state.Started,
			"ends_at":    state.Ends,
			"event":      "ended",
			"status":     "ended",
		})
	}(endsAt)
}

func (d *Director) emitHealthSnapshot() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for mountID, state := range d.active {
		d.bus.Publish(events.EventHealth, events.Payload{
			"station_id": state.StationID,
			"mount_id":   mountID,
			"entry_id":   state.EntryID,
			"media_id":   state.MediaID,
			"started_at": state.Started,
			"starts_at":  state.Started,
			"ends_at":    state.Ends,
			"status":     "playing",
		})
	}
}

func (d *Director) publishNowPlaying(entry models.ScheduleEntry, extra map[string]any) {
	payload := events.Payload{
		"entry_id":    entry.ID,
		"station_id":  entry.StationID,
		"mount_id":    entry.MountID,
		"source_type": entry.SourceType,
		"source_id":   entry.SourceID,
		"starts_at":   entry.StartsAt,
		"ends_at":     entry.EndsAt,
	}
	for k, v := range entry.Metadata {
		payload[k] = v
	}
	payload["metadata"] = entry.Metadata
	for k, v := range extra {
		payload[k] = v
	}
	d.bus.Publish(events.EventNowPlaying, payload)
}

func (d *Director) isPlayed(entryID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.played[entryID]
	return ok
}

func (d *Director) markPlayed(entryID string, endsAt time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.played[entryID] = endsAt
}

func (d *Director) prunePlayed(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, endsAt := range d.played {
		if endsAt.Add(30 * time.Minute).Before(now) {
			delete(d.played, id)
		}
	}
}
