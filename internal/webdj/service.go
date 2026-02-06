/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webdj

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/live"
	"github.com/friendsincode/grimnir_radio/internal/media"
	"github.com/friendsincode/grimnir_radio/internal/mediaengine/client"
	"github.com/friendsincode/grimnir_radio/internal/models"
	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

var (
	// ErrSessionNotFound indicates the WebDJ session was not found.
	ErrSessionNotFound = errors.New("webdj session not found")

	// ErrSessionNotActive indicates the session is no longer active.
	ErrSessionNotActive = errors.New("webdj session not active")

	// ErrInvalidDeck indicates an invalid deck identifier.
	ErrInvalidDeck = errors.New("invalid deck identifier")

	// ErrNoTrackLoaded indicates no track is loaded on the deck.
	ErrNoTrackLoaded = errors.New("no track loaded on deck")

	// ErrMediaNotFound indicates the media item was not found.
	ErrMediaNotFound = errors.New("media not found")

	// ErrUnauthorized indicates the user is not authorized.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrAlreadyLive indicates the session is already broadcasting live.
	ErrAlreadyLive = errors.New("session already broadcasting live")

	// ErrNotLive indicates the session is not broadcasting live.
	ErrNotLive = errors.New("session not broadcasting live")

	// ErrMediaEngineUnavailable indicates the media engine is not connected.
	ErrMediaEngineUnavailable = errors.New("media engine unavailable")
)

// Service handles WebDJ console sessions and deck control.
type Service struct {
	db       *gorm.DB
	liveSvc  *live.Service
	mediaSvc *media.Service
	meClient *client.Client
	bus      *events.Bus
	logger   zerolog.Logger

	// In-memory session tracking for real-time updates
	mu       sync.RWMutex
	sessions map[string]*Session
}

// Session represents an active WebDJ session with real-time state.
type Session struct {
	*models.WebDJSession
	mu          sync.RWMutex
	subscribers []chan *StateUpdate
	stopChan    chan struct{}
	lastUpdate  time.Time

	// Live broadcast state
	isLive  bool   // Whether session is currently broadcasting live
	liveID  string // Media engine live routing ID
	mountID string // Mount being broadcast to
}

// StateUpdate contains a real-time state update for WebSocket clients.
type StateUpdate struct {
	Type      string      `json:"type"`
	SessionID string      `json:"session_id"`
	Deck      string      `json:"deck,omitempty"` // "a" or "b"
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// NewService creates a new WebDJ service.
func NewService(db *gorm.DB, liveSvc *live.Service, mediaSvc *media.Service, meClient *client.Client, bus *events.Bus, logger zerolog.Logger) *Service {
	return &Service{
		db:       db,
		liveSvc:  liveSvc,
		mediaSvc: mediaSvc,
		meClient: meClient,
		bus:      bus,
		logger:   logger.With().Str("component", "webdj").Logger(),
		sessions: make(map[string]*Session),
	}
}

// StartSessionRequest contains parameters for starting a WebDJ session.
type StartSessionRequest struct {
	StationID string
	UserID    string
	Username  string
}

// StartSession creates a new WebDJ session.
func (s *Service) StartSession(ctx context.Context, req StartSessionRequest) (*models.WebDJSession, error) {
	// Check for existing active session for this user/station
	var existing models.WebDJSession
	err := s.db.WithContext(ctx).
		Where("station_id = ? AND user_id = ? AND active = ?", req.StationID, req.UserID, true).
		First(&existing).Error
	if err == nil {
		// Return existing session
		s.logger.Info().
			Str("session_id", existing.ID).
			Str("station_id", req.StationID).
			Str("user_id", req.UserID).
			Msg("returning existing webdj session")
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("query existing session: %w", err)
	}

	// Generate live session token for priority handover
	liveToken, err := s.liveSvc.GenerateToken(ctx, live.GenerateTokenRequest{
		StationID: req.StationID,
		MountID:   "", // Will be set when going live
		UserID:    req.UserID,
		Username:  req.Username,
		Priority:  models.PriorityLiveOverride,
		ExpiresIn: 24 * time.Hour,
	})
	if err != nil {
		s.logger.Warn().Err(err).Msg("failed to generate live token, continuing without")
	}

	// Create new session
	session := &models.WebDJSession{
		ID:              uuid.New().String(),
		StationID:       req.StationID,
		UserID:          req.UserID,
		DeckAState:      models.NewDeckState(),
		DeckBState:      models.NewDeckState(),
		MixerState:      models.NewMixerState(),
		CrossfaderCurve: string(models.CrossfaderLinear),
		Active:          true,
	}

	// Store the live token in metadata if generated
	if liveToken != "" {
		session.LiveSessionID = liveToken
	}

	if err := s.db.WithContext(ctx).Create(session).Error; err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	// Create in-memory session
	s.mu.Lock()
	s.sessions[session.ID] = &Session{
		WebDJSession: session,
		subscribers:  make([]chan *StateUpdate, 0),
		stopChan:     make(chan struct{}),
		lastUpdate:   time.Now(),
	}
	s.mu.Unlock()

	s.logger.Info().
		Str("session_id", session.ID).
		Str("station_id", req.StationID).
		Str("user_id", req.UserID).
		Msg("created webdj session")

	// Publish event
	s.bus.Publish(events.EventType("webdj.session_start"), events.Payload{
		"session_id": session.ID,
		"station_id": req.StationID,
		"user_id":    req.UserID,
	})

	return session, nil
}

// EndSession terminates a WebDJ session.
func (s *Service) EndSession(ctx context.Context, sessionID string) error {
	// Get session
	session, err := s.getSession(ctx, sessionID)
	if err != nil {
		return err
	}

	// Go off air if currently live
	s.mu.RLock()
	sess, exists := s.sessions[sessionID]
	isLive := exists && sess.isLive
	s.mu.RUnlock()

	if isLive {
		if err := s.GoOffAir(ctx, sessionID); err != nil {
			s.logger.Warn().Err(err).Str("session_id", sessionID).Msg("failed to go off air during session end")
		}
	}

	// Mark session as inactive
	if err := s.db.WithContext(ctx).
		Model(&models.WebDJSession{}).
		Where("id = ?", sessionID).
		Update("active", false).Error; err != nil {
		return fmt.Errorf("update session: %w", err)
	}

	// Clean up in-memory session
	s.mu.Lock()
	if sess, ok := s.sessions[sessionID]; ok {
		close(sess.stopChan)
		// Close all subscriber channels
		for _, ch := range sess.subscribers {
			close(ch)
		}
		delete(s.sessions, sessionID)
	}
	s.mu.Unlock()

	s.logger.Info().
		Str("session_id", sessionID).
		Str("station_id", session.StationID).
		Msg("ended webdj session")

	// Publish event
	s.bus.Publish(events.EventType("webdj.session_end"), events.Payload{
		"session_id": sessionID,
		"station_id": session.StationID,
	})

	return nil
}

// GoLiveRequest contains parameters for going live.
type GoLiveRequest struct {
	SessionID string
	MountID   string
	InputType string // "webrtc", "rtp", or "icecast"
	InputURL  string // Connection URL for RTP/Icecast inputs
}

// GoLive activates live broadcast for a WebDJ session.
// This routes the DJ's audio input to the specified mount.
func (s *Service) GoLive(ctx context.Context, req GoLiveRequest) error {
	session, err := s.getActiveSession(ctx, req.SessionID)
	if err != nil {
		return err
	}

	// Check if media engine is available
	if s.meClient == nil || !s.meClient.IsConnected() {
		return ErrMediaEngineUnavailable
	}

	// Check if already live
	s.mu.RLock()
	sess, exists := s.sessions[req.SessionID]
	if exists && sess.isLive {
		s.mu.RUnlock()
		return ErrAlreadyLive
	}
	s.mu.RUnlock()

	// Determine input type
	inputType := pb.LiveInputType_LIVE_INPUT_TYPE_WEBRTC
	switch req.InputType {
	case "rtp":
		inputType = pb.LiveInputType_LIVE_INPUT_TYPE_RTP
	case "icecast":
		inputType = pb.LiveInputType_LIVE_INPUT_TYPE_ICECAST
	case "srt":
		inputType = pb.LiveInputType_LIVE_INPUT_TYPE_SRT
	}

	// Route live input via media engine
	liveID, err := s.meClient.RouteLive(ctx, &client.RouteLiveRequest{
		StationID: session.StationID,
		MountID:   req.MountID,
		SessionID: req.SessionID,
		InputType: inputType,
		InputURL:  req.InputURL,
		FadeInMs:  500, // 500ms fade in for smooth transition
	})
	if err != nil {
		return fmt.Errorf("route live input: %w", err)
	}

	// Update session state
	s.mu.Lock()
	if sess, ok := s.sessions[req.SessionID]; ok {
		sess.mu.Lock()
		sess.isLive = true
		sess.liveID = liveID
		sess.mountID = req.MountID
		sess.mu.Unlock()
	}
	s.mu.Unlock()

	s.logger.Info().
		Str("session_id", req.SessionID).
		Str("station_id", session.StationID).
		Str("mount_id", req.MountID).
		Str("live_id", liveID).
		Str("input_type", req.InputType).
		Msg("webdj session went live")

	// Broadcast update
	s.broadcastUpdate(req.SessionID, &StateUpdate{
		Type:      "live_started",
		SessionID: req.SessionID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"mount_id":   req.MountID,
			"live_id":    liveID,
			"input_type": req.InputType,
		},
	})

	// Publish event
	s.bus.Publish(events.EventType("webdj.live_start"), events.Payload{
		"session_id": req.SessionID,
		"station_id": session.StationID,
		"mount_id":   req.MountID,
		"live_id":    liveID,
	})

	return nil
}

// GoOffAir stops live broadcast for a WebDJ session.
func (s *Service) GoOffAir(ctx context.Context, sessionID string) error {
	session, err := s.getActiveSession(ctx, sessionID)
	if err != nil {
		return err
	}

	// Get live state
	s.mu.RLock()
	sess, exists := s.sessions[sessionID]
	if !exists || !sess.isLive {
		s.mu.RUnlock()
		return ErrNotLive
	}
	mountID := sess.mountID
	s.mu.RUnlock()

	// Stop live routing via media engine
	if s.meClient != nil && s.meClient.IsConnected() {
		if err := s.meClient.Stop(ctx, session.StationID, mountID, false); err != nil {
			s.logger.Warn().Err(err).
				Str("session_id", sessionID).
				Str("mount_id", mountID).
				Msg("failed to stop live routing in media engine")
		}
	}

	// Update session state
	s.mu.Lock()
	if sess, ok := s.sessions[sessionID]; ok {
		sess.mu.Lock()
		sess.isLive = false
		sess.liveID = ""
		sess.mountID = ""
		sess.mu.Unlock()
	}
	s.mu.Unlock()

	s.logger.Info().
		Str("session_id", sessionID).
		Str("station_id", session.StationID).
		Msg("webdj session went off air")

	// Broadcast update
	s.broadcastUpdate(sessionID, &StateUpdate{
		Type:      "live_stopped",
		SessionID: sessionID,
		Timestamp: time.Now(),
		Data:      nil,
	})

	// Publish event
	s.bus.Publish(events.EventType("webdj.live_stop"), events.Payload{
		"session_id": sessionID,
		"station_id": session.StationID,
	})

	return nil
}

// IsLive returns whether a session is currently broadcasting live.
func (s *Service) IsLive(sessionID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if sess, ok := s.sessions[sessionID]; ok {
		sess.mu.RLock()
		defer sess.mu.RUnlock()
		return sess.isLive
	}
	return false
}

// GetSession retrieves a WebDJ session by ID.
func (s *Service) GetSession(ctx context.Context, sessionID string) (*models.WebDJSession, error) {
	return s.getSession(ctx, sessionID)
}

func (s *Service) getSession(ctx context.Context, sessionID string) (*models.WebDJSession, error) {
	// Check in-memory first
	s.mu.RLock()
	if sess, ok := s.sessions[sessionID]; ok {
		s.mu.RUnlock()
		return sess.WebDJSession, nil
	}
	s.mu.RUnlock()

	// Fall back to database
	var session models.WebDJSession
	if err := s.db.WithContext(ctx).First(&session, "id = ?", sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("query session: %w", err)
	}

	return &session, nil
}

// LoadTrackRequest contains parameters for loading a track.
type LoadTrackRequest struct {
	SessionID string
	Deck      models.DeckID
	MediaID   string
}

// LoadTrack loads a media item onto a deck.
func (s *Service) LoadTrack(ctx context.Context, req LoadTrackRequest) (*models.DeckState, error) {
	session, err := s.getActiveSession(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}

	// Get media item
	var mediaItem models.MediaItem
	if err := s.db.WithContext(ctx).First(&mediaItem, "id = ?", req.MediaID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMediaNotFound
		}
		return nil, fmt.Errorf("query media: %w", err)
	}

	// Check media belongs to same station
	if mediaItem.StationID != session.StationID {
		return nil, ErrUnauthorized
	}

	// Create new deck state
	deckState := models.DeckState{
		MediaID:    mediaItem.ID,
		Title:      mediaItem.Title,
		Artist:     mediaItem.Artist,
		DurationMS: mediaItem.Duration.Milliseconds(),
		PositionMS: 0,
		State:      string(models.DeckStateCued),
		BPM:        mediaItem.BPM,
		Pitch:      0,
		Volume:     1.0,
		HotCues:    make([]models.CuePoint, 0),
		EQHigh:     0,
		EQMid:      0,
		EQLow:      0,
	}

	// Pre-populate cue points from media analysis markers if available
	if mediaItem.CuePoints.IntroEnd > 0 {
		deckState.HotCues = append(deckState.HotCues, models.CuePoint{
			ID:         1,
			PositionMS: int64(mediaItem.CuePoints.IntroEnd * 1000),
			Label:      "Intro End",
			Color:      "#00ff00",
		})
	}
	if mediaItem.CuePoints.OutroIn > 0 {
		deckState.HotCues = append(deckState.HotCues, models.CuePoint{
			ID:         2,
			PositionMS: int64(mediaItem.CuePoints.OutroIn * 1000),
			Label:      "Outro Start",
			Color:      "#ff0000",
		})
	}

	// Update session in database and memory
	if err := s.updateDeckState(ctx, req.SessionID, req.Deck, deckState); err != nil {
		return nil, err
	}

	s.logger.Info().
		Str("session_id", req.SessionID).
		Str("deck", string(req.Deck)).
		Str("media_id", req.MediaID).
		Str("title", mediaItem.Title).
		Msg("loaded track on deck")

	// Broadcast update
	s.broadcastUpdate(req.SessionID, &StateUpdate{
		Type:      "deck_loaded",
		SessionID: req.SessionID,
		Deck:      string(req.Deck),
		Timestamp: time.Now(),
		Data:      deckState,
	})

	return &deckState, nil
}

// Play starts playback on a deck.
func (s *Service) Play(ctx context.Context, sessionID string, deck models.DeckID) error {
	deckState, err := s.getDeckState(ctx, sessionID, deck)
	if err != nil {
		return err
	}

	if deckState.MediaID == "" {
		return ErrNoTrackLoaded
	}

	deckState.State = string(models.DeckStatePlaying)

	if err := s.updateDeckState(ctx, sessionID, deck, *deckState); err != nil {
		return err
	}

	s.logger.Debug().
		Str("session_id", sessionID).
		Str("deck", string(deck)).
		Msg("deck playing")

	s.broadcastUpdate(sessionID, &StateUpdate{
		Type:      "deck_state",
		SessionID: sessionID,
		Deck:      string(deck),
		Timestamp: time.Now(),
		Data:      deckState,
	})

	return nil
}

// Pause pauses playback on a deck.
func (s *Service) Pause(ctx context.Context, sessionID string, deck models.DeckID) error {
	deckState, err := s.getDeckState(ctx, sessionID, deck)
	if err != nil {
		return err
	}

	if deckState.MediaID == "" {
		return ErrNoTrackLoaded
	}

	deckState.State = string(models.DeckStatePaused)

	if err := s.updateDeckState(ctx, sessionID, deck, *deckState); err != nil {
		return err
	}

	s.logger.Debug().
		Str("session_id", sessionID).
		Str("deck", string(deck)).
		Msg("deck paused")

	s.broadcastUpdate(sessionID, &StateUpdate{
		Type:      "deck_state",
		SessionID: sessionID,
		Deck:      string(deck),
		Timestamp: time.Now(),
		Data:      deckState,
	})

	return nil
}

// Seek moves playback position on a deck.
func (s *Service) Seek(ctx context.Context, sessionID string, deck models.DeckID, positionMS int64) error {
	deckState, err := s.getDeckState(ctx, sessionID, deck)
	if err != nil {
		return err
	}

	if deckState.MediaID == "" {
		return ErrNoTrackLoaded
	}

	// Clamp position
	if positionMS < 0 {
		positionMS = 0
	}
	if positionMS > deckState.DurationMS {
		positionMS = deckState.DurationMS
	}

	deckState.PositionMS = positionMS

	if err := s.updateDeckState(ctx, sessionID, deck, *deckState); err != nil {
		return err
	}

	s.broadcastUpdate(sessionID, &StateUpdate{
		Type:      "deck_position",
		SessionID: sessionID,
		Deck:      string(deck),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"position_ms": positionMS,
		},
	})

	return nil
}

// SetCue sets a hot cue point on a deck.
func (s *Service) SetCue(ctx context.Context, sessionID string, deck models.DeckID, cueID int, positionMS int64) error {
	if cueID < 1 || cueID > 8 {
		return fmt.Errorf("cue ID must be between 1 and 8")
	}

	deckState, err := s.getDeckState(ctx, sessionID, deck)
	if err != nil {
		return err
	}

	if deckState.MediaID == "" {
		return ErrNoTrackLoaded
	}

	// Find or create cue point
	found := false
	for i := range deckState.HotCues {
		if deckState.HotCues[i].ID == cueID {
			deckState.HotCues[i].PositionMS = positionMS
			found = true
			break
		}
	}
	if !found {
		deckState.HotCues = append(deckState.HotCues, models.CuePoint{
			ID:         cueID,
			PositionMS: positionMS,
		})
	}

	if err := s.updateDeckState(ctx, sessionID, deck, *deckState); err != nil {
		return err
	}

	s.broadcastUpdate(sessionID, &StateUpdate{
		Type:      "cue_set",
		SessionID: sessionID,
		Deck:      string(deck),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"cue_id":      cueID,
			"position_ms": positionMS,
		},
	})

	return nil
}

// DeleteCue removes a hot cue point from a deck.
func (s *Service) DeleteCue(ctx context.Context, sessionID string, deck models.DeckID, cueID int) error {
	deckState, err := s.getDeckState(ctx, sessionID, deck)
	if err != nil {
		return err
	}

	// Remove cue point
	newCues := make([]models.CuePoint, 0, len(deckState.HotCues))
	for _, cue := range deckState.HotCues {
		if cue.ID != cueID {
			newCues = append(newCues, cue)
		}
	}
	deckState.HotCues = newCues

	if err := s.updateDeckState(ctx, sessionID, deck, *deckState); err != nil {
		return err
	}

	s.broadcastUpdate(sessionID, &StateUpdate{
		Type:      "cue_deleted",
		SessionID: sessionID,
		Deck:      string(deck),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"cue_id": cueID,
		},
	})

	return nil
}

// EjectTrack removes the track from a deck.
func (s *Service) EjectTrack(ctx context.Context, sessionID string, deck models.DeckID) error {
	deckState := models.NewDeckState()

	if err := s.updateDeckState(ctx, sessionID, deck, deckState); err != nil {
		return err
	}

	s.logger.Debug().
		Str("session_id", sessionID).
		Str("deck", string(deck)).
		Msg("deck ejected")

	s.broadcastUpdate(sessionID, &StateUpdate{
		Type:      "deck_ejected",
		SessionID: sessionID,
		Deck:      string(deck),
		Timestamp: time.Now(),
		Data:      deckState,
	})

	return nil
}

// SetVolume sets the volume for a deck.
func (s *Service) SetVolume(ctx context.Context, sessionID string, deck models.DeckID, volume float64) error {
	deckState, err := s.getDeckState(ctx, sessionID, deck)
	if err != nil {
		return err
	}

	// Clamp volume
	if volume < 0 {
		volume = 0
	}
	if volume > 1 {
		volume = 1
	}

	deckState.Volume = volume

	if err := s.updateDeckState(ctx, sessionID, deck, *deckState); err != nil {
		return err
	}

	s.broadcastUpdate(sessionID, &StateUpdate{
		Type:      "deck_volume",
		SessionID: sessionID,
		Deck:      string(deck),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"volume": volume,
		},
	})

	return nil
}

// SetEQ sets the EQ values for a deck.
func (s *Service) SetEQ(ctx context.Context, sessionID string, deck models.DeckID, high, mid, low float64) error {
	deckState, err := s.getDeckState(ctx, sessionID, deck)
	if err != nil {
		return err
	}

	// Clamp EQ values to -12 to +12 dB
	clamp := func(v float64) float64 {
		if v < -12 {
			return -12
		}
		if v > 12 {
			return 12
		}
		return v
	}

	deckState.EQHigh = clamp(high)
	deckState.EQMid = clamp(mid)
	deckState.EQLow = clamp(low)

	if err := s.updateDeckState(ctx, sessionID, deck, *deckState); err != nil {
		return err
	}

	s.broadcastUpdate(sessionID, &StateUpdate{
		Type:      "deck_eq",
		SessionID: sessionID,
		Deck:      string(deck),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"high": deckState.EQHigh,
			"mid":  deckState.EQMid,
			"low":  deckState.EQLow,
		},
	})

	return nil
}

// SetPitch sets the pitch/tempo adjustment for a deck.
func (s *Service) SetPitch(ctx context.Context, sessionID string, deck models.DeckID, pitch float64) error {
	deckState, err := s.getDeckState(ctx, sessionID, deck)
	if err != nil {
		return err
	}

	// Clamp pitch to -8% to +8%
	if pitch < -8 {
		pitch = -8
	}
	if pitch > 8 {
		pitch = 8
	}

	deckState.Pitch = pitch

	if err := s.updateDeckState(ctx, sessionID, deck, *deckState); err != nil {
		return err
	}

	s.broadcastUpdate(sessionID, &StateUpdate{
		Type:      "deck_pitch",
		SessionID: sessionID,
		Deck:      string(deck),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"pitch": pitch,
		},
	})

	return nil
}

// SetCrossfader sets the crossfader position.
func (s *Service) SetCrossfader(ctx context.Context, sessionID string, position float64) error {
	session, err := s.getActiveSession(ctx, sessionID)
	if err != nil {
		return err
	}

	// Clamp position to 0-1
	if position < 0 {
		position = 0
	}
	if position > 1 {
		position = 1
	}

	session.MixerState.Crossfader = position

	if err := s.updateMixerState(ctx, sessionID, session.MixerState); err != nil {
		return err
	}

	s.broadcastUpdate(sessionID, &StateUpdate{
		Type:      "mixer_crossfader",
		SessionID: sessionID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"position": position,
		},
	})

	return nil
}

// SetMasterVolume sets the master output volume.
func (s *Service) SetMasterVolume(ctx context.Context, sessionID string, volume float64) error {
	session, err := s.getActiveSession(ctx, sessionID)
	if err != nil {
		return err
	}

	// Clamp volume
	if volume < 0 {
		volume = 0
	}
	if volume > 1 {
		volume = 1
	}

	session.MixerState.MasterVolume = volume

	if err := s.updateMixerState(ctx, sessionID, session.MixerState); err != nil {
		return err
	}

	s.broadcastUpdate(sessionID, &StateUpdate{
		Type:      "mixer_master_volume",
		SessionID: sessionID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"volume": volume,
		},
	})

	return nil
}

// Subscribe creates a channel for receiving state updates.
func (s *Service) Subscribe(sessionID string) (<-chan *StateUpdate, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return nil, nil, ErrSessionNotFound
	}

	ch := make(chan *StateUpdate, 16)
	sess.subscribers = append(sess.subscribers, ch)

	unsubscribe := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if sess, ok := s.sessions[sessionID]; ok {
			for i, sub := range sess.subscribers {
				if sub == ch {
					sess.subscribers = append(sess.subscribers[:i], sess.subscribers[i+1:]...)
					break
				}
			}
		}
		// Don't close channel here - it may already be closed
	}

	return ch, unsubscribe, nil
}

// Helper methods

func (s *Service) getActiveSession(ctx context.Context, sessionID string) (*models.WebDJSession, error) {
	session, err := s.getSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if !session.Active {
		return nil, ErrSessionNotActive
	}

	return session, nil
}

func (s *Service) getDeckState(ctx context.Context, sessionID string, deck models.DeckID) (*models.DeckState, error) {
	session, err := s.getActiveSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	switch deck {
	case models.DeckA:
		return &session.DeckAState, nil
	case models.DeckB:
		return &session.DeckBState, nil
	default:
		return nil, ErrInvalidDeck
	}
}

func (s *Service) updateDeckState(ctx context.Context, sessionID string, deck models.DeckID, state models.DeckState) error {
	var column string
	switch deck {
	case models.DeckA:
		column = "deck_a_state"
	case models.DeckB:
		column = "deck_b_state"
	default:
		return ErrInvalidDeck
	}

	// Update database
	if err := s.db.WithContext(ctx).
		Model(&models.WebDJSession{}).
		Where("id = ?", sessionID).
		Update(column, state).Error; err != nil {
		return fmt.Errorf("update deck state: %w", err)
	}

	// Update in-memory
	s.mu.Lock()
	if sess, ok := s.sessions[sessionID]; ok {
		sess.mu.Lock()
		switch deck {
		case models.DeckA:
			sess.DeckAState = state
		case models.DeckB:
			sess.DeckBState = state
		}
		sess.lastUpdate = time.Now()
		sess.mu.Unlock()
	}
	s.mu.Unlock()

	return nil
}

func (s *Service) updateMixerState(ctx context.Context, sessionID string, state models.MixerState) error {
	// Update database
	if err := s.db.WithContext(ctx).
		Model(&models.WebDJSession{}).
		Where("id = ?", sessionID).
		Update("mixer_state", state).Error; err != nil {
		return fmt.Errorf("update mixer state: %w", err)
	}

	// Update in-memory
	s.mu.Lock()
	if sess, ok := s.sessions[sessionID]; ok {
		sess.mu.Lock()
		sess.MixerState = state
		sess.lastUpdate = time.Now()
		sess.mu.Unlock()
	}
	s.mu.Unlock()

	return nil
}

func (s *Service) broadcastUpdate(sessionID string, update *StateUpdate) {
	s.mu.RLock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		s.mu.RUnlock()
		return
	}
	subscribers := append([]chan *StateUpdate(nil), sess.subscribers...)
	s.mu.RUnlock()

	for _, ch := range subscribers {
		select {
		case ch <- update:
		default:
			// Channel full, skip
		}
	}
}

// GetActiveSessions returns all active WebDJ sessions for a station.
func (s *Service) GetActiveSessions(ctx context.Context, stationID string) ([]models.WebDJSession, error) {
	var sessions []models.WebDJSession
	query := s.db.WithContext(ctx).Where("active = ?", true)

	if stationID != "" {
		query = query.Where("station_id = ?", stationID)
	}

	if err := query.Order("created_at DESC").Find(&sessions).Error; err != nil {
		return nil, fmt.Errorf("query active sessions: %w", err)
	}

	return sessions, nil
}

// LoadSessionFromDB loads a persisted session into memory.
func (s *Service) LoadSessionFromDB(ctx context.Context, sessionID string) error {
	var session models.WebDJSession
	if err := s.db.WithContext(ctx).First(&session, "id = ?", sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionNotFound
		}
		return fmt.Errorf("query session: %w", err)
	}

	if !session.Active {
		return ErrSessionNotActive
	}

	s.mu.Lock()
	if _, exists := s.sessions[sessionID]; !exists {
		s.sessions[sessionID] = &Session{
			WebDJSession: &session,
			subscribers:  make([]chan *StateUpdate, 0),
			stopChan:     make(chan struct{}),
			lastUpdate:   time.Now(),
		}
	}
	s.mu.Unlock()

	return nil
}
