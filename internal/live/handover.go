package live

import (
	"context"
	"fmt"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
)

// HandoverRequest contains parameters for initiating a live handover.
type HandoverRequest struct {
	SessionID string
	StationID string
	MountID   string
	UserID    string
	Priority  models.PriorityLevel // PriorityLiveOverride (1) or PriorityLiveScheduled (2)

	// Handover behavior
	Immediate   bool // If true, preempt current source immediately
	FadeTimeMs  int  // Fade transition duration (0 = use default)
	RollbackOnError bool // If true, rollback to automation on handover failure
}

// HandoverResult contains the result of a live handover operation.
type HandoverResult struct {
	SessionID   string
	Success     bool
	HandoverAt  time.Time
	PreviousSource *PrioritySourceInfo
	NewSource      *PrioritySourceInfo
	TransitionType string // "immediate", "faded", "delayed"
	Error          string
}

// PrioritySourceInfo contains information about a priority source.
type PrioritySourceInfo struct {
	Priority   models.PriorityLevel
	SourceType models.SourceType
	SourceID   string
	Metadata   map[string]any
}

// StartHandover initiates a live DJ handover with priority transition.
//
// This coordinates the transition from the current playback source to a live
// DJ session. The process involves:
// 1. Validating the live session is active and authorized
// 2. Creating a priority source for the live session
// 3. Signaling the executor to transition to the live source
// 4. Coordinating with the media engine for audio routing
// 5. Handling rollback on failure if requested
func (s *Service) StartHandover(ctx context.Context, req HandoverRequest) (*HandoverResult, error) {
	startTime := time.Now()

	s.logger.Info().
		Str("session_id", req.SessionID).
		Str("station_id", req.StationID).
		Str("user_id", req.UserID).
		Int("priority", int(req.Priority)).
		Bool("immediate", req.Immediate).
		Msg("starting live handover")

	// Validate live session exists and is active
	session, err := s.GetSession(ctx, req.SessionID)
	if err != nil {
		return &HandoverResult{
			SessionID: req.SessionID,
			Success:   false,
			Error:     fmt.Sprintf("session not found: %v", err),
		}, fmt.Errorf("get session: %w", err)
	}

	if !session.Active {
		return &HandoverResult{
			SessionID: req.SessionID,
			Success:   false,
			Error:     "session is not active",
		}, fmt.Errorf("session %s is not active", req.SessionID)
	}

	// Verify user matches
	if session.UserID != req.UserID {
		return &HandoverResult{
			SessionID: req.SessionID,
			Success:   false,
			Error:     "user mismatch",
		}, ErrUnauthorized
	}

	// Get current priority state (for rollback)
	previousSource, err := s.getCurrentPrioritySource(ctx, req.StationID)
	if err != nil {
		s.logger.Warn().Err(err).Msg("failed to get current priority source")
		// Don't fail handover if we can't get previous source
		previousSource = nil
	}

	// Start priority transition based on type
	var priorityResult *priority.TransitionResult
	var transitionErr error

	switch req.Priority {
	case models.PriorityLiveOverride:
		// Manual DJ override (priority 1)
		priorityResult, transitionErr = s.prioritySvc.StartOverride(ctx, priority.StartOverrideRequest{
			StationID:  req.StationID,
			MountID:    req.MountID,
			SourceType: models.SourceTypeLive,
			SourceID:   req.SessionID,
			Metadata: map[string]any{
				"session_id": req.SessionID,
				"user_id":    req.UserID,
				"username":   session.Username,
				"handover_at": startTime,
			},
		})

	case models.PriorityLiveScheduled:
		// Scheduled live show (priority 2)
		priorityResult, transitionErr = s.prioritySvc.StartScheduledLive(ctx, priority.StartScheduledLiveRequest{
			StationID:  req.StationID,
			MountID:    req.MountID,
			SourceID:   req.SessionID,
			Metadata: map[string]any{
				"session_id": req.SessionID,
				"user_id":    req.UserID,
				"username":   session.Username,
				"handover_at": startTime,
			},
		})

	default:
		return &HandoverResult{
			SessionID: req.SessionID,
			Success:   false,
			Error:     "invalid priority level",
		}, fmt.Errorf("invalid priority %d for live session", req.Priority)
	}

	// Handle priority transition errors
	if transitionErr != nil {
		s.logger.Error().Err(transitionErr).Msg("priority transition failed")

		return &HandoverResult{
			SessionID:      req.SessionID,
			Success:        false,
			HandoverAt:     startTime,
			PreviousSource: previousSource,
			Error:          fmt.Sprintf("priority transition failed: %v", transitionErr),
		}, fmt.Errorf("priority transition: %w", transitionErr)
	}

	// Determine transition type based on priority result
	transitionType := "faded"
	if req.Immediate || priorityResult.TransitionType == priority.TransitionPreempt {
		transitionType = "immediate"
	} else if priorityResult.TransitionType == priority.TransitionNone {
		transitionType = "delayed" // Will transition at next track boundary
	}

	// Update session metadata with handover info
	if err := s.updateSessionMetadata(ctx, req.SessionID, map[string]any{
		"handover_completed": true,
		"handover_at":        startTime,
		"priority":           req.Priority,
		"transition_type":    transitionType,
	}); err != nil {
		s.logger.Warn().Err(err).Msg("failed to update session metadata")
		// Don't fail handover on metadata update failure
	}

	// Publish handover event
	s.bus.Publish(events.EventLiveHandover, events.Payload{
		"station_id":      req.StationID,
		"session_id":      req.SessionID,
		"user_id":         req.UserID,
		"username":        session.Username,
		"priority":        req.Priority,
		"transition_type": transitionType,
		"handover_at":     startTime,
	})

	result := &HandoverResult{
		SessionID:   req.SessionID,
		Success:     true,
		HandoverAt:  startTime,
		PreviousSource: previousSource,
		NewSource: &PrioritySourceInfo{
			Priority:   req.Priority,
			SourceType: models.SourceTypeLive,
			SourceID:   req.SessionID,
			Metadata: map[string]any{
				"user_id":  req.UserID,
				"username": session.Username,
			},
		},
		TransitionType: transitionType,
	}

	s.logger.Info().
		Str("session_id", req.SessionID).
		Str("transition_type", transitionType).
		Dur("duration_ms", time.Since(startTime)).
		Msg("live handover completed successfully")

	return result, nil
}

// ReleaseHandover releases a live session's priority hold and transitions back to automation.
func (s *Service) ReleaseHandover(ctx context.Context, sessionID string) error {
	s.logger.Info().
		Str("session_id", sessionID).
		Msg("releasing live handover")

	// Get session
	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	// Release priority source
	_, err = s.prioritySvc.Release(ctx, session.StationID, sessionID)
	if err != nil {
		s.logger.Error().Err(err).Msg("priority release failed")
		return fmt.Errorf("release priority: %w", err)
	}

	// Publish release event
	s.bus.Publish(events.EventLiveReleased, events.Payload{
		"station_id":   session.StationID,
		"session_id":   sessionID,
		"user_id":      session.UserID,
		"username":     session.Username,
		"released_at":  time.Now(),
	})

	s.logger.Info().
		Str("session_id", sessionID).
		Str("station_id", session.StationID).
		Msg("live handover released")

	return nil
}

// getCurrentPrioritySource retrieves the current active priority source for a station.
func (s *Service) getCurrentPrioritySource(ctx context.Context, stationID string) (*PrioritySourceInfo, error) {
	current, err := s.prioritySvc.GetCurrent(ctx, stationID)
	if err != nil {
		return nil, fmt.Errorf("get current priority: %w", err)
	}

	if current == nil {
		return nil, nil // No active priority source
	}

	return &PrioritySourceInfo{
		Priority:   current.Priority,
		SourceType: current.SourceType,
		SourceID:   current.SourceID,
		Metadata:   current.Metadata,
	}, nil
}

// updateSessionMetadata updates live session metadata.
func (s *Service) updateSessionMetadata(ctx context.Context, sessionID string, metadata map[string]any) error {
	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

	// Merge new metadata with existing
	if session.Metadata == nil {
		session.Metadata = make(map[string]any)
	}
	for k, v := range metadata {
		session.Metadata[k] = v
	}

	// Update in database using Save to ensure proper JSON serialization
	return s.db.WithContext(ctx).Save(&session).Error
}
