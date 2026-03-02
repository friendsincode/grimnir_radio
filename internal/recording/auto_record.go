/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package recording

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/events"
)

// AutoRecordHandler subscribes to EventRecordingAutoStart and starts
// a recording via the recording Service when a DJ connects to a station
// that has auto-record enabled.
type AutoRecordHandler struct {
	svc    *Service
	bus    *events.Bus
	logger zerolog.Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewAutoRecordHandler creates a handler that auto-starts recordings on DJ connect.
func NewAutoRecordHandler(svc *Service, bus *events.Bus, logger zerolog.Logger) *AutoRecordHandler {
	return &AutoRecordHandler{
		svc:    svc,
		bus:    bus,
		logger: logger.With().Str("component", "recording-auto-record").Logger(),
	}
}

// Start begins listening for auto-record events.
func (h *AutoRecordHandler) Start(ctx context.Context) {
	ctx, h.cancel = context.WithCancel(ctx)

	sub := h.bus.Subscribe(events.EventRecordingAutoStart)

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		defer h.bus.Unsubscribe(events.EventRecordingAutoStart, sub)

		for {
			select {
			case <-ctx.Done():
				return
			case payload := <-sub:
				h.handleAutoRecord(ctx, payload)
			}
		}
	}()

	h.logger.Info().Msg("auto-record handler started")
}

// Stop stops the handler.
func (h *AutoRecordHandler) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
	h.wg.Wait()
}

func (h *AutoRecordHandler) handleAutoRecord(ctx context.Context, payload events.Payload) {
	stationID, _ := payload["station_id"].(string)
	mountID, _ := payload["mount_id"].(string)
	userID, _ := payload["user_id"].(string)
	username, _ := payload["username"].(string)

	if stationID == "" || mountID == "" || userID == "" {
		return
	}

	title := fmt.Sprintf("Live: %s %s", username, time.Now().Format("2006-01-02 15:04"))

	rec, err := h.svc.StartRecording(ctx, StartRequest{
		StationID: stationID,
		MountID:   mountID,
		UserID:    userID,
		Title:     title,
	})
	if err != nil {
		h.logger.Warn().
			Err(err).
			Str("station_id", stationID).
			Str("user_id", userID).
			Msg("auto-record failed to start")
		return
	}

	h.logger.Info().
		Str("recording_id", rec.ID).
		Str("station_id", stationID).
		Str("user_id", userID).
		Msg("auto-record started")
}
