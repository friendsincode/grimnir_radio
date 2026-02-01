/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/telemetry"
	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// Supervisor monitors pipeline health and manages recovery
type Supervisor struct {
	cfg             *Config
	logger          zerolog.Logger
	pipelineManager *PipelineManager

	mu                 sync.RWMutex
	monitoredPipelines map[string]*PipelineHealth
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
}

// PipelineHealth tracks health metrics for a pipeline
type PipelineHealth struct {
	StationID        string
	LastHeartbeat    time.Time
	ConsecutiveFails int
	State            pb.PlaybackState
	UnderrunCount    int64
	RestartCount     int
	LastRestart      time.Time
}

const (
	// Health check interval
	healthCheckInterval = 5 * time.Second

	// Maximum consecutive failures before restart
	maxConsecutiveFails = 3

	// Maximum restarts within window
	maxRestartsInWindow = 5
	restartWindow       = 5 * time.Minute

	// Heartbeat timeout
	heartbeatTimeout = 15 * time.Second
)

// NewSupervisor creates a new pipeline supervisor
func NewSupervisor(cfg *Config, logger zerolog.Logger, pipelineManager *PipelineManager) *Supervisor {
	ctx, cancel := context.WithCancel(context.Background())

	return &Supervisor{
		cfg:                cfg,
		logger:             logger.With().Str("component", "supervisor").Logger(),
		pipelineManager:    pipelineManager,
		monitoredPipelines: make(map[string]*PipelineHealth),
		ctx:                ctx,
		cancel:             cancel,
	}
}

// Start begins supervision of all pipelines
func (s *Supervisor) Start() {
	s.logger.Info().Msg("starting pipeline supervisor")

	s.wg.Add(1)
	go s.healthCheckLoop()
}

// Stop gracefully stops the supervisor
func (s *Supervisor) Stop() {
	s.logger.Info().Msg("stopping pipeline supervisor")
	s.cancel()
	s.wg.Wait()
	s.logger.Info().Msg("pipeline supervisor stopped")
}

// MonitorPipeline adds a pipeline to supervision
func (s *Supervisor) MonitorPipeline(stationID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.monitoredPipelines[stationID]; exists {
		return
	}

	s.monitoredPipelines[stationID] = &PipelineHealth{
		StationID:     stationID,
		LastHeartbeat: time.Now(),
		State:         pb.PlaybackState_PLAYBACK_STATE_IDLE,
	}

	s.logger.Info().Str("station_id", stationID).Msg("monitoring pipeline")
}

// UnmonitorPipeline removes a pipeline from supervision
func (s *Supervisor) UnmonitorPipeline(stationID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.monitoredPipelines, stationID)

	s.logger.Info().Str("station_id", stationID).Msg("unmonitoring pipeline")
}

// UpdateHeartbeat updates the last heartbeat time for a pipeline
func (s *Supervisor) UpdateHeartbeat(stationID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if health, exists := s.monitoredPipelines[stationID]; exists {
		health.LastHeartbeat = time.Now()
		health.ConsecutiveFails = 0
	}
}

// healthCheckLoop periodically checks pipeline health
func (s *Supervisor) healthCheckLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.performHealthCheck()
		}
	}
}

// performHealthCheck checks all monitored pipelines
func (s *Supervisor) performHealthCheck() {
	s.mu.RLock()
	stationIDs := make([]string, 0, len(s.monitoredPipelines))
	for stationID := range s.monitoredPipelines {
		stationIDs = append(stationIDs, stationID)
	}
	s.mu.RUnlock()

	for _, stationID := range stationIDs {
		s.checkPipeline(stationID)
	}
}

// checkPipeline checks the health of a single pipeline
func (s *Supervisor) checkPipeline(stationID string) {
	pipeline, err := s.pipelineManager.GetPipeline(stationID)
	if err != nil {
		s.logger.Warn().
			Str("station_id", stationID).
			Err(err).
			Msg("pipeline not found in manager")
		s.UnmonitorPipeline(stationID)
		return
	}

	s.mu.Lock()
	health, exists := s.monitoredPipelines[stationID]
	if !exists {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	// Get current telemetry
	telemetry := pipeline.GetTelemetry()

	// Check for heartbeat timeout
	if time.Since(health.LastHeartbeat) > heartbeatTimeout {
		health.ConsecutiveFails++

		s.logger.Warn().
			Str("station_id", stationID).
			Int("consecutive_fails", health.ConsecutiveFails).
			Msg("pipeline heartbeat timeout")

		if health.ConsecutiveFails >= maxConsecutiveFails {
			s.restartPipeline(stationID, "heartbeat timeout")
		}
		return
	}

	// Check for excessive underruns
	if telemetry.UnderrunCount > health.UnderrunCount+10 {
		s.logger.Warn().
			Str("station_id", stationID).
			Int64("underrun_count", telemetry.UnderrunCount).
			Msg("excessive buffer underruns detected")

		// Note: This is a warning but doesn't trigger restart
		// Underruns can be caused by slow disk/network, not necessarily a crash
	}

	health.UnderrunCount = telemetry.UnderrunCount
	health.State = telemetry.State

	// Check for stuck state
	if telemetry.State == pb.PlaybackState_PLAYBACK_STATE_LOADING {
		// If pipeline has been in loading state for too long, it might be stuck
		// This would require tracking state duration, which we could add
		s.logger.Debug().
			Str("station_id", stationID).
			Msg("pipeline in loading state")
	}
}

// restartPipeline attempts to restart a failed pipeline
func (s *Supervisor) restartPipeline(stationID string, reason string) {
	s.mu.Lock()
	health, exists := s.monitoredPipelines[stationID]
	if !exists {
		s.mu.Unlock()
		return
	}

	// Check restart rate limit
	if health.RestartCount >= maxRestartsInWindow {
		if time.Since(health.LastRestart) < restartWindow {
			s.mu.Unlock()
			s.logger.Error().
				Str("station_id", stationID).
				Int("restart_count", health.RestartCount).
				Msg("restart rate limit exceeded, giving up")
			return
		}
		// Reset counter if outside window
		health.RestartCount = 0
	}

	health.RestartCount++
	health.LastRestart = time.Now()
	health.ConsecutiveFails = 0
	s.mu.Unlock()

	s.logger.Warn().
		Str("station_id", stationID).
		Str("reason", reason).
		Int("restart_count", health.RestartCount).
		Msg("restarting pipeline")

	// Track pipeline restart metric
	// Use station ID as mount ID (we don't have mount ID in supervisor context)
	telemetry.MediaEnginePipelineRestarts.WithLabelValues(stationID, stationID, reason).Inc()

	// Destroy old pipeline
	if err := s.pipelineManager.DestroyPipeline(stationID); err != nil {
		s.logger.Error().
			Str("station_id", stationID).
			Err(err).
			Msg("failed to destroy pipeline during restart")
	}

	// TODO: Recreate pipeline with saved configuration
	// This would require storing the original graph configuration
	// For now, we just destroy the pipeline and wait for control plane to recreate

	s.logger.Info().
		Str("station_id", stationID).
		Msg("pipeline restart initiated")
}

// GetHealth returns health status for a pipeline
func (s *Supervisor) GetHealth(stationID string) (*PipelineHealth, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	health, exists := s.monitoredPipelines[stationID]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid race conditions
	healthCopy := &PipelineHealth{
		StationID:        health.StationID,
		LastHeartbeat:    health.LastHeartbeat,
		ConsecutiveFails: health.ConsecutiveFails,
		State:            health.State,
		UnderrunCount:    health.UnderrunCount,
		RestartCount:     health.RestartCount,
		LastRestart:      health.LastRestart,
	}

	return healthCopy, true
}

// GetAllHealth returns health status for all monitored pipelines
func (s *Supervisor) GetAllHealth() map[string]*PipelineHealth {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*PipelineHealth, len(s.monitoredPipelines))
	for stationID, health := range s.monitoredPipelines {
		result[stationID] = &PipelineHealth{
			StationID:        health.StationID,
			LastHeartbeat:    health.LastHeartbeat,
			ConsecutiveFails: health.ConsecutiveFails,
			State:            health.State,
			UnderrunCount:    health.UnderrunCount,
			RestartCount:     health.RestartCount,
			LastRestart:      health.LastRestart,
		}
	}

	return result
}
