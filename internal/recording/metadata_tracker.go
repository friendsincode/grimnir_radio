/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package recording

import (
	"context"
	"sync"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// MetadataTracker subscribes to now-playing events and automatically adds
// chapter markers to active recordings when track metadata changes.
type MetadataTracker struct {
	db     *gorm.DB
	svc    *Service
	bus    *events.Bus
	logger zerolog.Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Track last-seen metadata per station to avoid duplicate chapters.
	mu       sync.Mutex
	lastMeta map[string]chapterMeta // station_id -> last published meta
}

type chapterMeta struct {
	Title  string
	Artist string
	Album  string
}

// NewMetadataTracker creates a tracker that watches now-playing events.
func NewMetadataTracker(db *gorm.DB, svc *Service, bus *events.Bus, logger zerolog.Logger) *MetadataTracker {
	return &MetadataTracker{
		db:       db,
		svc:      svc,
		bus:      bus,
		logger:   logger.With().Str("component", "recording-metadata-tracker").Logger(),
		lastMeta: make(map[string]chapterMeta),
	}
}

// Start begins listening for now-playing events.
func (mt *MetadataTracker) Start(ctx context.Context) {
	ctx, mt.cancel = context.WithCancel(ctx)

	sub := mt.bus.Subscribe(events.EventNowPlaying)

	mt.wg.Add(1)
	go func() {
		defer mt.wg.Done()
		defer mt.bus.Unsubscribe(events.EventNowPlaying, sub)

		for {
			select {
			case <-ctx.Done():
				return
			case payload := <-sub:
				mt.handleNowPlaying(ctx, payload)
			}
		}
	}()

	mt.logger.Info().Msg("metadata tracker started")
}

// Stop stops the tracker.
func (mt *MetadataTracker) Stop() {
	if mt.cancel != nil {
		mt.cancel()
	}
	mt.wg.Wait()
}

func (mt *MetadataTracker) handleNowPlaying(ctx context.Context, payload events.Payload) {
	stationID, _ := payload["station_id"].(string)
	if stationID == "" {
		return
	}

	title, _ := payload["title"].(string)
	artist, _ := payload["artist"].(string)
	album, _ := payload["album"].(string)

	if title == "" && artist == "" {
		return
	}

	// Check if metadata actually changed for this station.
	meta := chapterMeta{Title: title, Artist: artist, Album: album}
	mt.mu.Lock()
	if last, ok := mt.lastMeta[stationID]; ok && last == meta {
		mt.mu.Unlock()
		return
	}
	mt.lastMeta[stationID] = meta
	mt.mu.Unlock()

	// Find active recording for this station.
	var rec models.Recording
	err := mt.db.Where("station_id = ? AND status = ?", stationID, models.RecordingStatusActive).
		First(&rec).Error
	if err != nil {
		return // No active recording — nothing to do.
	}

	// Add chapter marker.
	if err := mt.svc.AddChapter(ctx, rec.ID, title, artist, album); err != nil {
		mt.logger.Warn().
			Err(err).
			Str("recording_id", rec.ID).
			Str("station_id", stationID).
			Msg("failed to add metadata chapter")
		return
	}

	mt.logger.Debug().
		Str("recording_id", rec.ID).
		Str("station_id", stationID).
		Str("title", title).
		Str("artist", artist).
		Msg("chapter added from now-playing metadata")
}
