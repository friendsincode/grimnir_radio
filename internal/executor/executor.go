package executor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

var (
	// ErrInvalidTransition indicates an invalid state transition was attempted.
	ErrInvalidTransition = errors.New("invalid state transition")

	// ErrExecutorNotRunning indicates the executor is not running for the station.
	ErrExecutorNotRunning = errors.New("executor not running")
)

// Executor manages per-station playout execution.
type Executor struct {
	stationID     string
	db            *gorm.DB
	stateManager  *StateManager
	prioritySvc   *priority.Service
	bus           *events.Bus
	mediaCtrl     *MediaController
	logger        zerolog.Logger

	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	running       bool
}

// New creates a new executor for a station.
func New(stationID string, db *gorm.DB, stateManager *StateManager, prioritySvc *priority.Service, bus *events.Bus, mediaCtrl *MediaController, logger zerolog.Logger) *Executor {
	return &Executor{
		stationID:    stationID,
		db:           db,
		stateManager: stateManager,
		prioritySvc:  prioritySvc,
		bus:          bus,
		mediaCtrl:    mediaCtrl,
		logger:       logger.With().Str("station_id", stationID).Logger(),
	}
}

// Start begins the executor lifecycle for this station.
func (e *Executor) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return errors.New("executor already running")
	}

	e.ctx, e.cancel = context.WithCancel(ctx)
	e.running = true

	// Initialize state - GetState will create the record if it doesn't exist
	if _, err := e.stateManager.GetState(e.ctx, e.stationID); err != nil {
		return fmt.Errorf("initialize state: %w", err)
	}

	// Set state to idle
	if err := e.stateManager.SetState(e.ctx, e.stationID, models.ExecutorStateIdle); err != nil {
		return fmt.Errorf("set idle state: %w", err)
	}

	// Start heartbeat goroutine
	go e.heartbeatLoop()

	// Start priority listener goroutine
	go e.priorityEventLoop()

	// Start telemetry streaming if media controller is available
	if e.mediaCtrl != nil && e.mediaCtrl.IsConnected() {
		go e.telemetryStreamLoop()
	}

	e.logger.Info().Msg("executor started")
	return nil
}

// Stop halts the executor.
func (e *Executor) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return ErrExecutorNotRunning
	}

	e.cancel()
	e.running = false

	// Set state to idle
	ctx := context.Background()
	if err := e.stateManager.SetState(ctx, e.stationID, models.ExecutorStateIdle); err != nil {
		e.logger.Error().Err(err).Msg("failed to set idle state on stop")
	}

	e.logger.Info().Msg("executor stopped")
	return nil
}

// IsRunning checks if the executor is running.
func (e *Executor) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// TransitionTo attempts to transition to a new state.
func (e *Executor) TransitionTo(ctx context.Context, newState models.ExecutorStateEnum) error {
	state, err := e.stateManager.GetState(ctx, e.stationID)
	if err != nil {
		return fmt.Errorf("get current state: %w", err)
	}

	// Validate transition
	if !e.isValidTransition(state.State, newState) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, state.State, newState)
	}

	// Apply transition
	if err := e.stateManager.SetState(ctx, e.stationID, newState); err != nil {
		return fmt.Errorf("set state: %w", err)
	}

	e.logger.Info().
		Str("from", string(state.State)).
		Str("to", string(newState)).
		Msg("state transition")

	return nil
}

// Preload prepares the next track for playback.
func (e *Executor) Preload(ctx context.Context, sourceID string) error {
	if err := e.TransitionTo(ctx, models.ExecutorStatePreloading); err != nil {
		return err
	}

	if err := e.stateManager.SetNextSource(ctx, e.stationID, sourceID); err != nil {
		return fmt.Errorf("set next source: %w", err)
	}

	e.logger.Debug().Str("source_id", sourceID).Msg("preloaded next source")
	return nil
}

// Play starts playback of the current or preloaded source.
func (e *Executor) Play(ctx context.Context, sourceID string, priority models.PriorityLevel) error {
	// Determine target state based on priority
	var targetState models.ExecutorStateEnum
	switch priority {
	case models.PriorityEmergency:
		targetState = models.ExecutorStateEmergency
	case models.PriorityLiveOverride, models.PriorityLiveScheduled:
		targetState = models.ExecutorStateLive
	default:
		targetState = models.ExecutorStatePlaying
	}

	if err := e.TransitionTo(ctx, targetState); err != nil {
		return err
	}

	if err := e.stateManager.SetCurrentSource(ctx, e.stationID, sourceID, priority); err != nil {
		return fmt.Errorf("set current source: %w", err)
	}

	e.logger.Info().
		Str("source_id", sourceID).
		Int("priority", int(priority)).
		Str("state", string(targetState)).
		Msg("playback started")

	return nil
}

// Fade initiates a crossfade to the next track.
func (e *Executor) Fade(ctx context.Context, nextSourceID string, nextPriority models.PriorityLevel) error {
	if err := e.TransitionTo(ctx, models.ExecutorStateFading); err != nil {
		return err
	}

	// Set next source
	if err := e.stateManager.SetNextSource(ctx, e.stationID, nextSourceID); err != nil {
		return fmt.Errorf("set next source for fade: %w", err)
	}

	e.logger.Info().
		Str("next_source_id", nextSourceID).
		Int("next_priority", int(nextPriority)).
		Msg("crossfade started")

	return nil
}

// CompleteFade finishes a crossfade and makes the next source current.
func (e *Executor) CompleteFade(ctx context.Context) error {
	state, err := e.stateManager.GetState(ctx, e.stationID)
	if err != nil {
		return err
	}

	if state.State != models.ExecutorStateFading {
		return fmt.Errorf("not in fading state")
	}

	// Get priority source to determine target state
	prioritySource, err := e.prioritySvc.GetCurrent(ctx, e.stationID)
	if err != nil {
		return fmt.Errorf("get priority source: %w", err)
	}

	// Determine new state based on priority
	var newState models.ExecutorStateEnum
	switch prioritySource.Priority {
	case models.PriorityEmergency:
		newState = models.ExecutorStateEmergency
	case models.PriorityLiveOverride, models.PriorityLiveScheduled:
		newState = models.ExecutorStateLive
	default:
		newState = models.ExecutorStatePlaying
	}

	// Transition and swap sources
	if err := e.stateManager.UpdateState(ctx, e.stationID, func(s *models.ExecutorState) {
		s.State = newState
		s.CurrentSourceID = s.NextSourceID
		s.CurrentPriority = prioritySource.Priority
		s.NextSourceID = ""
	}); err != nil {
		return fmt.Errorf("complete fade: %w", err)
	}

	e.logger.Info().Msg("crossfade completed")
	return nil
}

// UpdateTelemetry updates real-time telemetry data.
func (e *Executor) UpdateTelemetry(ctx context.Context, telemetry Telemetry) error {
	return e.stateManager.UpdateTelemetry(ctx, e.stationID, telemetry)
}

// State machine validation

func (e *Executor) isValidTransition(from, to models.ExecutorStateEnum) bool {
	// Define valid transitions
	validTransitions := map[models.ExecutorStateEnum][]models.ExecutorStateEnum{
		models.ExecutorStateIdle: {
			models.ExecutorStatePreloading,
			models.ExecutorStatePlaying,
			models.ExecutorStateLive,
			models.ExecutorStateEmergency,
		},
		models.ExecutorStatePreloading: {
			models.ExecutorStateIdle,
			models.ExecutorStatePlaying,
			models.ExecutorStateLive,
			models.ExecutorStateEmergency,
		},
		models.ExecutorStatePlaying: {
			models.ExecutorStateIdle,
			models.ExecutorStatePreloading,
			models.ExecutorStateFading,
			models.ExecutorStateLive,
			models.ExecutorStateEmergency,
		},
		models.ExecutorStateFading: {
			models.ExecutorStatePlaying,
			models.ExecutorStateLive,
			models.ExecutorStateEmergency,
		},
		models.ExecutorStateLive: {
			models.ExecutorStateIdle,
			models.ExecutorStateFading,
			models.ExecutorStatePlaying,
			models.ExecutorStateEmergency,
		},
		models.ExecutorStateEmergency: {
			models.ExecutorStateIdle,
			models.ExecutorStatePlaying,
			models.ExecutorStateLive,
		},
	}

	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}

	for _, allowedState := range allowed {
		if allowedState == to {
			return true
		}
	}

	return false
}

// Background goroutines

func (e *Executor) heartbeatLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			if err := e.stateManager.Heartbeat(e.ctx, e.stationID); err != nil {
				e.logger.Error().Err(err).Msg("heartbeat failed")
			}
		}
	}
}

func (e *Executor) priorityEventLoop() {
	// Subscribe to priority events
	prioritySub := e.bus.Subscribe(events.EventPriorityChange)
	defer e.bus.Unsubscribe(events.EventPriorityChange, prioritySub)

	emergencySub := e.bus.Subscribe(events.EventPriorityEmergency)
	defer e.bus.Unsubscribe(events.EventPriorityEmergency, emergencySub)

	for {
		select {
		case <-e.ctx.Done():
			return
		case payload := <-prioritySub:
			e.handlePriorityEvent(payload)
		case payload := <-emergencySub:
			e.handleEmergencyEvent(payload)
		}
	}
}

func (e *Executor) handlePriorityEvent(payload events.Payload) {
	stationID, ok := payload["station_id"].(string)
	if !ok || stationID != e.stationID {
		return
	}

	e.logger.Info().
		Interface("payload", payload).
		Msg("priority event received")

	// Get source information from payload
	sourceID, _ := payload["source_id"].(string)
	priority, _ := payload["priority"].(models.PriorityLevel)

	if sourceID == "" {
		e.logger.Warn().Msg("priority event missing source_id")
		return
	}

	// Handle priority change through media engine
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start playback with new priority
	if err := e.Play(ctx, sourceID, priority); err != nil {
		e.logger.Error().Err(err).Msg("failed to handle priority change")
	}
}

func (e *Executor) handleEmergencyEvent(payload events.Payload) {
	stationID, ok := payload["station_id"].(string)
	if !ok || stationID != e.stationID {
		return
	}

	e.logger.Warn().
		Interface("payload", payload).
		Msg("emergency event received")

	// Get emergency source information
	sourceID, _ := payload["source_id"].(string)
	path, _ := payload["path"].(string)

	if sourceID == "" || path == "" {
		e.logger.Error().Msg("emergency event missing source_id or path")
		return
	}

	// Insert emergency content through media engine
	if e.mediaCtrl != nil && e.mediaCtrl.IsConnected() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if _, err := e.mediaCtrl.InsertEmergency(ctx, sourceID, path); err != nil {
			e.logger.Error().Err(err).Msg("failed to insert emergency content")
			return
		}
	}

	// Update executor state
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := e.TransitionTo(ctx, models.ExecutorStateEmergency); err != nil {
		e.logger.Error().Err(err).Msg("failed to transition to emergency state")
	}
}

func (e *Executor) telemetryStreamLoop() {
	e.logger.Info().Msg("starting telemetry stream")

	callback := func(telemetry *pb.TelemetryData) error {
		// Update executor state with telemetry
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := e.UpdateTelemetry(ctx, Telemetry{
			AudioLevelL:   float64(telemetry.AudioLevelL),
			AudioLevelR:   float64(telemetry.AudioLevelR),
			LoudnessLUFS:  float64(telemetry.LoudnessLufs),
			BufferDepthMS: telemetry.BufferDepthMs,
		})

		if err != nil {
			e.logger.Error().Err(err).Msg("failed to update telemetry")
		}

		// Log additional telemetry data for debugging
		e.logger.Debug().
			Int64("position_ms", telemetry.PositionMs).
			Int64("duration_ms", telemetry.DurationMs).
			Int64("underrun_count", telemetry.UnderrunCount).
			Msg("telemetry received")

		return nil
	}

	// Stream telemetry with 1-second intervals
	if err := e.mediaCtrl.StreamTelemetry(e.ctx, 1000, callback); err != nil {
		if e.ctx.Err() == nil {
			// Only log error if context wasn't cancelled (normal shutdown)
			e.logger.Error().Err(err).Msg("telemetry stream error")
		}
	}

	e.logger.Info().Msg("telemetry stream ended")
}
