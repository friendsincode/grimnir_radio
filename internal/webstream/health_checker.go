/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/telemetry"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// HealthChecker performs periodic health checks on a webstream.
type HealthChecker struct {
	webstreamID      string
	db               *gorm.DB
	bus              *events.Bus
	logger           zerolog.Logger
	stopCh           chan struct{}
	consecutiveFails int
}

// NewHealthChecker creates a new health checker for a webstream.
func NewHealthChecker(webstreamID string, db *gorm.DB, bus *events.Bus, logger zerolog.Logger) *HealthChecker {
	return &HealthChecker{
		webstreamID: webstreamID,
		db:          db,
		bus:         bus,
		logger:      logger.With().Str("webstream_id", webstreamID).Logger(),
		stopCh:      make(chan struct{}),
	}
}

// Run starts the health check loop.
func (hc *HealthChecker) Run(ctx context.Context) {
	hc.logger.Debug().Msg("health checker started")

	// Get initial configuration
	var ws models.Webstream
	if err := hc.db.First(&ws, "id = ?", hc.webstreamID).Error; err != nil {
		hc.logger.Error().Err(err).Msg("failed to load webstream config")
		return
	}

	interval := ws.HealthCheckInterval
	if interval <= 0 {
		interval = 30 * time.Second // Default to 30 seconds if not set
		hc.logger.Warn().Dur("default_interval", interval).Msg("health check interval not set, using default")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run initial health check immediately
	hc.performHealthCheck(&ws)

	for {
		select {
		case <-ctx.Done():
			hc.logger.Debug().Msg("health checker stopped (context)")
			return
		case <-hc.stopCh:
			hc.logger.Debug().Msg("health checker stopped")
			return
		case <-ticker.C:
			// Reload webstream config in case it changed
			if err := hc.db.First(&ws, "id = ?", hc.webstreamID).Error; err != nil {
				hc.logger.Error().Err(err).Msg("failed to reload webstream config")
				continue
			}

			// Adjust ticker interval if changed
			interval = ws.HealthCheckInterval
			if interval <= 0 {
				interval = 30 * time.Second
			}
			ticker.Stop()
			ticker = time.NewTicker(interval)

			hc.performHealthCheck(&ws)
		}
	}
}

// Stop stops the health checker.
func (hc *HealthChecker) Stop() {
	close(hc.stopCh)
}

func (hc *HealthChecker) performHealthCheck(ws *models.Webstream) {
	currentURL := ws.GetCurrentURL()
	if currentURL == "" {
		hc.logger.Warn().Msg("no current URL configured")
		return
	}

	hc.logger.Debug().
		Str("url", currentURL).
		Str("method", ws.HealthCheckMethod).
		Msg("performing health check")

	err := hc.checkURL(currentURL, ws.HealthCheckMethod, ws.HealthCheckTimeout)

	if err != nil {
		hc.handleFailedCheck(ws, err)
		telemetry.WebstreamHealthChecksTotal.WithLabelValues(ws.ID, "failed").Inc()
	} else {
		hc.handleSuccessfulCheck(ws)
		telemetry.WebstreamHealthChecksTotal.WithLabelValues(ws.ID, "success").Inc()
	}
}

func (hc *HealthChecker) handleSuccessfulCheck(ws *models.Webstream) {
	wasUnhealthy := ws.HealthStatus == "unhealthy" || ws.HealthStatus == "degraded"

	// Reset consecutive fail counter
	hc.consecutiveFails = 0

	// Mark as healthy
	ws.MarkHealthy()
	if err := hc.db.Save(ws).Error; err != nil {
		hc.logger.Error().Err(err).Msg("failed to update health status")
		return
	}

	// Update health status metric (2=healthy)
	telemetry.WebstreamHealthStatus.WithLabelValues(ws.ID, ws.StationID).Set(2)

	// Publish health status event for live UI updates
	hc.bus.Publish(events.EventWebstreamHealth, events.Payload{
		"webstream_id": ws.ID,
		"station_id":   ws.StationID,
		"status":       ws.HealthStatus,
		"url":          ws.GetCurrentURL(),
	})

	hc.logger.Debug().Str("url", ws.GetCurrentURL()).Msg("health check passed")

	// If we recovered from unhealthy state and auto-recover is enabled
	if wasUnhealthy && ws.AutoRecoverEnabled && ws.CurrentIndex != 0 {
		// Check if primary is healthy
		primaryURL := ws.GetPrimaryURL()
		if hc.checkURL(primaryURL, ws.HealthCheckMethod, ws.HealthCheckTimeout) == nil {
			hc.logger.Info().
				Str("from_url", ws.CurrentURL).
				Str("to_url", primaryURL).
				Msg("auto-recovering to primary URL")

			oldURL := ws.CurrentURL
			ws.ResetToPrimary()
			ws.MarkHealthy()

			if err := hc.db.Save(ws).Error; err != nil {
				hc.logger.Error().Err(err).Msg("failed to save auto-recovery state")
				return
			}

			// Emit recovery event
			hc.bus.Publish(events.EventWebstreamRecovered, events.Payload{
				"webstream_id": ws.ID,
				"station_id":   ws.StationID,
				"from_url":     oldURL,
				"to_url":       ws.CurrentURL,
				"auto":         true,
			})
		}
	}
}

func (hc *HealthChecker) handleFailedCheck(ws *models.Webstream, err error) {
	hc.consecutiveFails++

	hc.logger.Warn().
		Err(err).
		Str("webstream_id", ws.ID).
		Str("station_id", ws.StationID).
		Str("webstream_name", ws.Name).
		Str("url", ws.GetCurrentURL()).
		Int("consecutive_fails", hc.consecutiveFails).
		Msg("webstream health check failed; failover may trigger if failures continue")

	// Mark as degraded after first failure
	if hc.consecutiveFails == 1 {
		ws.MarkDegraded()
		if err := hc.db.Save(ws).Error; err != nil {
			hc.logger.Error().Err(err).Msg("failed to update health status")
		}
		// Update health status metric (1=degraded)
		telemetry.WebstreamHealthStatus.WithLabelValues(ws.ID, ws.StationID).Set(1)
		hc.bus.Publish(events.EventWebstreamHealth, events.Payload{
			"webstream_id": ws.ID,
			"station_id":   ws.StationID,
			"status":       ws.HealthStatus,
			"url":          ws.GetCurrentURL(),
		})
		return
	}

	// Trigger failover after multiple failures
	failoverThreshold := 3 // Configurable threshold
	if hc.consecutiveFails >= failoverThreshold {
		hc.triggerFailover(ws)
	} else {
		// Update status but don't failover yet
		ws.MarkUnhealthy()
		if err := hc.db.Save(ws).Error; err != nil {
			hc.logger.Error().Err(err).Msg("failed to update health status")
		}
		// Update health status metric (0=unhealthy)
		telemetry.WebstreamHealthStatus.WithLabelValues(ws.ID, ws.StationID).Set(0)
		hc.bus.Publish(events.EventWebstreamHealth, events.Payload{
			"webstream_id": ws.ID,
			"station_id":   ws.StationID,
			"status":       ws.HealthStatus,
			"url":          ws.GetCurrentURL(),
		})
	}
}

func (hc *HealthChecker) triggerFailover(ws *models.Webstream) {
	if !ws.FailoverEnabled {
		hc.logger.Warn().
			Str("webstream_id", ws.ID).
			Str("station_id", ws.StationID).
			Str("webstream_name", ws.Name).
			Str("url", ws.GetCurrentURL()).
			Msg("webstream failover disabled; stream marked unhealthy until source recovers")
		ws.MarkUnhealthy()
		if err := hc.db.Save(ws).Error; err != nil {
			hc.logger.Error().Err(err).Msg("failed to update health status")
		}
		return
	}

	nextURL := ws.GetNextFailoverURL()
	if nextURL == "" {
		hc.logger.Error().Msg("no failover URLs available")
		ws.MarkUnhealthy()
		if err := hc.db.Save(ws).Error; err != nil {
			hc.logger.Error().Err(err).Msg("failed to update health status")
		}
		return
	}

	// Test the next URL before failing over
	if err := hc.checkURL(nextURL, ws.HealthCheckMethod, ws.HealthCheckTimeout); err != nil {
		hc.logger.Warn().
			Err(err).
			Str("next_url", nextURL).
			Msg("next failover URL also unhealthy, skipping")

		// Try to failover anyway (it will try the next URL after this one)
		if ws.FailoverToNext() {
			hc.consecutiveFails = 0 // Reset counter
			if err := hc.db.Save(ws).Error; err != nil {
				hc.logger.Error().Err(err).Msg("failed to save failover state")
			}
		}
		return
	}

	// Perform failover
	oldURL := ws.CurrentURL
	oldIndex := ws.CurrentIndex

	if ws.FailoverToNext() {
		ws.MarkHealthy()
		hc.consecutiveFails = 0 // Reset counter

		if err := hc.db.Save(ws).Error; err != nil {
			hc.logger.Error().Err(err).Msg("failed to save failover state")
			return
		}

		// Update health status metric (2=healthy after failover)
		telemetry.WebstreamHealthStatus.WithLabelValues(ws.ID, ws.StationID).Set(2)

		// Increment failover counter
		telemetry.WebstreamFailoversTotal.WithLabelValues(ws.ID, ws.StationID, oldURL, ws.CurrentURL).Inc()

		hc.logger.Warn().
			Str("from_url", oldURL).
			Str("to_url", ws.CurrentURL).
			Int("from_index", oldIndex).
			Int("to_index", ws.CurrentIndex).
			Msg("automatic failover triggered")

		// Emit failover event
		hc.bus.Publish(events.EventWebstreamFailover, events.Payload{
			"webstream_id": ws.ID,
			"station_id":   ws.StationID,
			"name":         ws.Name,
			"from_url":     oldURL,
			"to_url":       ws.CurrentURL,
			"from_index":   oldIndex,
			"to_index":     ws.CurrentIndex,
			"auto":         true,
			"reason":       "health_check_failed",
		})
	}
}

func (hc *HealthChecker) checkURL(url, method string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "Grimnir-Radio/1.0")
	req.Header.Set("Icy-MetaData", "1")

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Allow up to 3 redirects
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	return nil
}
