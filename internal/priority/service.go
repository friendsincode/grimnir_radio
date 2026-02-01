/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package priority

import (
	"context"
	"fmt"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// Service provides high-level priority management with event integration.
type Service struct {
	db       *gorm.DB
	resolver *Resolver
	bus      *events.Bus
	logger   zerolog.Logger
}

// NewService creates a priority service instance.
func NewService(db *gorm.DB, bus *events.Bus, logger zerolog.Logger) *Service {
	return &Service{
		db:       db,
		resolver: NewResolver(db, logger),
		bus:      bus,
		logger:   logger,
	}
}

// InsertEmergencyRequest describes an emergency broadcast request.
type InsertEmergencyRequest struct {
	StationID string
	MediaID   string
	MountID   string
	Metadata  map[string]any
}

// InsertEmergency immediately activates emergency content, preempting all other sources.
func (s *Service) InsertEmergency(ctx context.Context, req InsertEmergencyRequest) (*TransitionResult, error) {
	s.logger.Warn().
		Str("station_id", req.StationID).
		Str("media_id", req.MediaID).
		Msg("emergency broadcast requested")

	result, err := s.resolver.InsertEmergency(ctx, req.StationID, req.MediaID, req.Metadata)
	if err != nil {
		s.logger.Error().Err(err).Msg("emergency insertion failed")
		return nil, fmt.Errorf("insert emergency: %w", err)
	}

	// Publish emergency event
	s.publishEvent(events.EventPriorityEmergency, result, req.StationID)

	return result, nil
}

// StartOverrideRequest describes a manual DJ override request.
type StartOverrideRequest struct {
	StationID  string
	MountID    string
	SourceType models.SourceType
	SourceID   string
	Metadata   map[string]any
}

// StartOverride activates a live override source (manual DJ takeover).
func (s *Service) StartOverride(ctx context.Context, req StartOverrideRequest) (*TransitionResult, error) {
	s.logger.Info().
		Str("station_id", req.StationID).
		Str("source_id", req.SourceID).
		Msg("live override requested")

	transitionReq := TransitionRequest{
		StationID:    req.StationID,
		MountID:      req.MountID,
		NewPriority:  models.PriorityLiveOverride,
		SourceType:   req.SourceType,
		SourceID:     req.SourceID,
		Metadata:     req.Metadata,
		ForcePreempt: false,
	}

	result, err := s.resolver.Transition(ctx, transitionReq)
	if err != nil {
		s.logger.Error().Err(err).Msg("override start failed")
		return nil, fmt.Errorf("start override: %w", err)
	}

	if result.TransitionType != TransitionNone {
		s.publishEvent(events.EventPriorityOverride, result, req.StationID)
	}

	return result, nil
}

// StartScheduledLiveRequest describes a scheduled live show activation.
type StartScheduledLiveRequest struct {
	StationID string
	MountID   string
	SourceID  string
	Metadata  map[string]any
}

// StartScheduledLive activates a scheduled live broadcast.
func (s *Service) StartScheduledLive(ctx context.Context, req StartScheduledLiveRequest) (*TransitionResult, error) {
	s.logger.Info().
		Str("station_id", req.StationID).
		Str("source_id", req.SourceID).
		Msg("scheduled live show requested")

	transitionReq := TransitionRequest{
		StationID:    req.StationID,
		MountID:      req.MountID,
		NewPriority:  models.PriorityLiveScheduled,
		SourceType:   models.SourceTypeLive,
		SourceID:     req.SourceID,
		Metadata:     req.Metadata,
		ForcePreempt: false,
	}

	result, err := s.resolver.Transition(ctx, transitionReq)
	if err != nil {
		s.logger.Error().Err(err).Msg("scheduled live start failed")
		return nil, fmt.Errorf("start scheduled live: %w", err)
	}

	if result.TransitionType != TransitionNone {
		s.publishEvent(events.EventPriorityChange, result, req.StationID)
	}

	return result, nil
}

// ActivateAutomationRequest describes automated playout activation.
type ActivateAutomationRequest struct {
	StationID  string
	MountID    string
	SourceType models.SourceType
	SourceID   string
	Metadata   map[string]any
}

// ActivateAutomation transitions to automated playout.
func (s *Service) ActivateAutomation(ctx context.Context, req ActivateAutomationRequest) (*TransitionResult, error) {
	transitionReq := TransitionRequest{
		StationID:    req.StationID,
		MountID:      req.MountID,
		NewPriority:  models.PriorityAutomation,
		SourceType:   req.SourceType,
		SourceID:     req.SourceID,
		Metadata:     req.Metadata,
		ForcePreempt: false,
	}

	result, err := s.resolver.Transition(ctx, transitionReq)
	if err != nil {
		return nil, fmt.Errorf("activate automation: %w", err)
	}

	if result.TransitionType != TransitionNone {
		s.publishEvent(events.EventPriorityChange, result, req.StationID)
	}

	return result, nil
}

// Release deactivates a priority source and returns to the next priority level.
func (s *Service) Release(ctx context.Context, stationID, sourceID string) (*TransitionResult, error) {
	s.logger.Info().
		Str("station_id", stationID).
		Str("source_id", sourceID).
		Msg("releasing priority source")

	result, err := s.resolver.Release(ctx, stationID, sourceID)
	if err != nil {
		s.logger.Error().Err(err).Msg("release failed")
		return nil, fmt.Errorf("release: %w", err)
	}

	s.publishEvent(events.EventPriorityReleased, result, stationID)

	return result, nil
}

// GetCurrent returns the currently active priority source for a station.
func (s *Service) GetCurrent(ctx context.Context, stationID string) (*models.PrioritySource, error) {
	source, err := s.resolver.GetCurrentSource(ctx, stationID)
	if err != nil {
		return nil, fmt.Errorf("get current: %w", err)
	}
	return source, nil
}

// GetActive returns all active priority sources for a station.
func (s *Service) GetActive(ctx context.Context, stationID string) ([]models.PrioritySource, error) {
	sources, err := s.resolver.GetActiveSourcesByPriority(ctx, stationID)
	if err != nil {
		return nil, fmt.Errorf("get active: %w", err)
	}
	return sources, nil
}

// Helper methods

func (s *Service) publishEvent(eventType events.EventType, result *TransitionResult, stationID string) {
	payload := events.Payload{
		"station_id":      stationID,
		"transition_type": string(result.TransitionType),
		"preempted":       result.Preempted,
		"requires_fade":   result.RequiresFade,
	}

	if result.OldSource != nil {
		payload["old_source_id"] = result.OldSource.ID
		payload["old_priority"] = int(result.OldSource.Priority)
		payload["old_source_type"] = string(result.OldSource.SourceType)
	}

	if result.NewSource != nil {
		payload["new_source_id"] = result.NewSource.ID
		payload["new_priority"] = int(result.NewSource.Priority)
		payload["new_source_type"] = string(result.NewSource.SourceType)
		payload["new_media_id"] = result.NewSource.SourceID

		// Include metadata from new source
		if result.NewSource.Metadata != nil {
			for k, v := range result.NewSource.Metadata {
				payload[k] = v
			}
		}
	}

	s.bus.Publish(eventType, payload)

	s.logger.Debug().
		Str("event_type", string(eventType)).
		Str("station_id", stationID).
		Msg("published priority event")
}
