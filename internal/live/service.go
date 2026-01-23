package live

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

var (
	// ErrInvalidToken indicates the live authorization token is invalid or expired.
	ErrInvalidToken = errors.New("invalid or expired token")

	// ErrTokenAlreadyUsed indicates the token has already been used.
	ErrTokenAlreadyUsed = errors.New("token already used")

	// ErrSessionNotFound indicates the live session was not found.
	ErrSessionNotFound = errors.New("live session not found")

	// ErrUnauthorized indicates the user is not authorized for live access.
	ErrUnauthorized = errors.New("unauthorized for live access")
)

// Service handles live input authorization and session management.
type Service struct {
	db          *gorm.DB
	prioritySvc *priority.Service
	bus         *events.Bus
	logger      zerolog.Logger
}

// NewService creates a new live input service.
func NewService(db *gorm.DB, prioritySvc *priority.Service, bus *events.Bus, logger zerolog.Logger) *Service {
	return &Service{
		db:          db,
		prioritySvc: prioritySvc,
		bus:         bus,
		logger:      logger.With().Str("component", "live").Logger(),
	}
}

// GenerateTokenRequest contains parameters for generating a live token.
type GenerateTokenRequest struct {
	StationID string
	MountID   string
	UserID    string
	Username  string
	Priority  models.PriorityLevel // PriorityLiveOverride (1) or PriorityLiveScheduled (2)
	ExpiresIn time.Duration        // How long the token is valid
}

// GenerateToken creates a one-time use token for live DJ access.
func (s *Service) GenerateToken(ctx context.Context, req GenerateTokenRequest) (string, error) {
	// Validate priority is a live priority
	if req.Priority != models.PriorityLiveOverride && req.Priority != models.PriorityLiveScheduled {
		return "", fmt.Errorf("invalid priority for live session: %d", req.Priority)
	}

	// Generate random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	// Create session record
	session := &models.LiveSession{
		ID:          uuid.New().String(),
		StationID:   req.StationID,
		MountID:     req.MountID,
		UserID:      req.UserID,
		Username:    req.Username,
		Priority:    req.Priority,
		Token:       token,
		TokenUsed:   false,
		Active:      false,
		ConnectedAt: time.Now(), // Will be updated on actual connect
		Metadata:    make(map[string]any),
	}

	if err := s.db.WithContext(ctx).Create(session).Error; err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	s.logger.Info().
		Str("session_id", session.ID).
		Str("station_id", req.StationID).
		Str("user_id", req.UserID).
		Str("username", req.Username).
		Int("priority", int(req.Priority)).
		Msg("generated live token")

	return token, nil
}

// AuthorizeSource validates live source credentials and returns session info.
func (s *Service) AuthorizeSource(ctx context.Context, stationID, mountID, token string) (bool, error) {
	var session models.LiveSession
	err := s.db.WithContext(ctx).
		Where("station_id = ? AND mount_id = ? AND token = ?", stationID, mountID, token).
		First(&session).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.logger.Warn().
				Str("station_id", stationID).
				Str("mount_id", mountID).
				Msg("authorization failed: invalid token")
			return false, ErrInvalidToken
		}
		return false, fmt.Errorf("query session: %w", err)
	}

	// Check if token already used
	if session.TokenUsed {
		s.logger.Warn().
			Str("session_id", session.ID).
			Str("station_id", stationID).
			Msg("authorization failed: token already used")
		return false, ErrTokenAlreadyUsed
	}

	// Mark token as used
	if err := s.db.WithContext(ctx).
		Model(&session).
		Update("token_used", true).Error; err != nil {
		return false, fmt.Errorf("mark token used: %w", err)
	}

	s.logger.Info().
		Str("session_id", session.ID).
		Str("station_id", stationID).
		Str("user_id", session.UserID).
		Str("username", session.Username).
		Msg("live source authorized")

	return true, nil
}

// HandleConnect processes a live DJ connect event and initiates priority handover.
type ConnectRequest struct {
	StationID  string
	MountID    string
	Token      string
	SourceIP   string
	SourcePort int
	UserAgent  string
}

func (s *Service) HandleConnect(ctx context.Context, req ConnectRequest) (*models.LiveSession, error) {
	// Find session by token
	var session models.LiveSession
	err := s.db.WithContext(ctx).
		Where("station_id = ? AND mount_id = ? AND token = ?", req.StationID, req.MountID, req.Token).
		First(&session).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("query session: %w", err)
	}

	// Update session with connection details
	now := time.Now()
	updates := map[string]any{
		"active":       true,
		"connected_at": now,
		"source_ip":    req.SourceIP,
		"source_port":  req.SourcePort,
		"user_agent":   req.UserAgent,
	}

	if err := s.db.WithContext(ctx).
		Model(&session).
		Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("update session: %w", err)
	}

	// Reload to get updated values
	if err := s.db.WithContext(ctx).First(&session, "id = ?", session.ID).Error; err != nil {
		return nil, fmt.Errorf("reload session: %w", err)
	}

	// Start priority handover based on session priority
	metadata := map[string]any{
		"user_id":    session.UserID,
		"username":   session.Username,
		"session_id": session.ID,
	}

	var result *priority.TransitionResult
	if session.Priority == models.PriorityLiveOverride {
		overrideReq := priority.StartOverrideRequest{
			StationID:  session.StationID,
			MountID:    session.MountID,
			SourceType: models.SourceTypeLive,
			SourceID:   session.ID,
			Metadata:   metadata,
		}
		result, err = s.prioritySvc.StartOverride(ctx, overrideReq)
	} else if session.Priority == models.PriorityLiveScheduled {
		// For scheduled live, use StartScheduledLive method
		scheduledReq := priority.StartScheduledLiveRequest{
			StationID: session.StationID,
			MountID:   session.MountID,
			SourceID:  session.ID,
			Metadata:  metadata,
		}
		result, err = s.prioritySvc.StartScheduledLive(ctx, scheduledReq)
	}

	if err != nil {
		s.logger.Error().Err(err).
			Str("session_id", session.ID).
			Msg("failed to start priority handover")
		return nil, fmt.Errorf("priority handover: %w", err)
	}

	s.logger.Info().
		Str("session_id", session.ID).
		Str("station_id", session.StationID).
		Str("user_id", session.UserID).
		Str("username", session.Username).
		Int("priority", int(session.Priority)).
		Bool("preempted", result.Preempted).
		Msg("live DJ connected")

	// Publish connect event
	s.bus.Publish(events.EventDJConnect, events.Payload{
		"session_id": session.ID,
		"station_id": session.StationID,
		"mount_id":   session.MountID,
		"user_id":    session.UserID,
		"username":   session.Username,
		"priority":   session.Priority,
		"preempted":  result.Preempted,
	})

	return &session, nil
}

// HandleDisconnect processes a live DJ disconnect event.
func (s *Service) HandleDisconnect(ctx context.Context, sessionID string) error {
	var session models.LiveSession
	if err := s.db.WithContext(ctx).First(&session, "id = ?", sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionNotFound
		}
		return fmt.Errorf("query session: %w", err)
	}

	// Mark session as disconnected
	now := time.Now()
	if err := s.db.WithContext(ctx).
		Model(&session).
		Updates(map[string]any{
			"active":          false,
			"disconnected_at": now,
		}).Error; err != nil {
		return fmt.Errorf("update session: %w", err)
	}

	// Release priority (failback to automation)
	if _, err := s.prioritySvc.Release(ctx, session.StationID, session.ID); err != nil {
		s.logger.Error().Err(err).
			Str("session_id", sessionID).
			Msg("failed to release priority on disconnect")
		// Don't return error, log and continue
	}

	duration := now.Sub(session.ConnectedAt)
	s.logger.Info().
		Str("session_id", sessionID).
		Str("station_id", session.StationID).
		Str("user_id", session.UserID).
		Str("username", session.Username).
		Dur("duration", duration).
		Msg("live DJ disconnected")

	// Publish disconnect event
	s.bus.Publish(events.EventDJDisconnect, events.Payload{
		"session_id": sessionID,
		"station_id": session.StationID,
		"mount_id":   session.MountID,
		"user_id":    session.UserID,
		"username":   session.Username,
		"duration_seconds": duration.Seconds(),
	})

	return nil
}

// GetActiveSessions returns all active live sessions.
func (s *Service) GetActiveSessions(ctx context.Context, stationID string) ([]models.LiveSession, error) {
	var sessions []models.LiveSession
	query := s.db.WithContext(ctx).Where("active = ?", true)

	if stationID != "" {
		query = query.Where("station_id = ?", stationID)
	}

	if err := query.Order("connected_at DESC").Find(&sessions).Error; err != nil {
		return nil, fmt.Errorf("query active sessions: %w", err)
	}

	return sessions, nil
}

// GetSession retrieves a live session by ID.
func (s *Service) GetSession(ctx context.Context, sessionID string) (*models.LiveSession, error) {
	var session models.LiveSession
	if err := s.db.WithContext(ctx).First(&session, "id = ?", sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("query session: %w", err)
	}

	return &session, nil
}
