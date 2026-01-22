package executor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// StateManager manages executor state for all stations.
type StateManager struct {
	db     *gorm.DB
	logger zerolog.Logger
	mu     sync.RWMutex
	states map[string]*models.ExecutorState // stationID -> state
}

// NewStateManager creates an executor state manager.
func NewStateManager(db *gorm.DB, logger zerolog.Logger) *StateManager {
	return &StateManager{
		db:     db,
		logger: logger,
		states: make(map[string]*models.ExecutorState),
	}
}

// GetState retrieves the current state for a station.
func (sm *StateManager) GetState(ctx context.Context, stationID string) (*models.ExecutorState, error) {
	sm.mu.RLock()
	cached, ok := sm.states[stationID]
	sm.mu.RUnlock()

	if ok {
		return cached, nil
	}

	// Load from database
	var state models.ExecutorState
	err := sm.db.WithContext(ctx).Where("station_id = ?", stationID).First(&state).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("query executor state: %w", err)
	}

	// Create new state if not found
	if err == gorm.ErrRecordNotFound {
		state = models.ExecutorState{
			ID:            uuid.NewString(),
			StationID:     stationID,
			State:         models.ExecutorStateIdle,
			LastHeartbeat: time.Now(),
			Metadata:      make(map[string]any),
		}
		if err := sm.db.WithContext(ctx).Create(&state).Error; err != nil {
			return nil, fmt.Errorf("create executor state: %w", err)
		}
	}

	// Cache it
	sm.mu.Lock()
	sm.states[stationID] = &state
	sm.mu.Unlock()

	return &state, nil
}

// UpdateState updates the executor state and persists to database.
func (sm *StateManager) UpdateState(ctx context.Context, stationID string, update func(*models.ExecutorState)) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state, ok := sm.states[stationID]
	if !ok {
		// Load from DB first
		var dbState models.ExecutorState
		err := sm.db.WithContext(ctx).Where("station_id = ?", stationID).First(&dbState).Error
		if err != nil {
			return fmt.Errorf("load state for update: %w", err)
		}
		state = &dbState
		sm.states[stationID] = state
	}

	// Apply update
	update(state)
	state.UpdatedAt = time.Now()

	// Persist to database
	if err := sm.db.WithContext(ctx).Save(state).Error; err != nil {
		return fmt.Errorf("save executor state: %w", err)
	}

	sm.logger.Debug().
		Str("station_id", stationID).
		Str("state", string(state.State)).
		Msg("executor state updated")

	return nil
}

// SetState transitions the executor to a new state.
func (sm *StateManager) SetState(ctx context.Context, stationID string, newState models.ExecutorStateEnum) error {
	return sm.UpdateState(ctx, stationID, func(state *models.ExecutorState) {
		state.State = newState
	})
}

// SetCurrentSource updates the currently playing source.
func (sm *StateManager) SetCurrentSource(ctx context.Context, stationID, sourceID string, priority models.PriorityLevel) error {
	return sm.UpdateState(ctx, stationID, func(state *models.ExecutorState) {
		state.CurrentSourceID = sourceID
		state.CurrentPriority = priority
	})
}

// SetNextSource updates the preloaded next source.
func (sm *StateManager) SetNextSource(ctx context.Context, stationID, sourceID string) error {
	return sm.UpdateState(ctx, stationID, func(state *models.ExecutorState) {
		state.NextSourceID = sourceID
	})
}

// UpdateTelemetry updates audio telemetry data.
func (sm *StateManager) UpdateTelemetry(ctx context.Context, stationID string, telemetry Telemetry) error {
	return sm.UpdateState(ctx, stationID, func(state *models.ExecutorState) {
		state.AudioLevelL = telemetry.AudioLevelL
		state.AudioLevelR = telemetry.AudioLevelR
		state.LoudnessLUFS = telemetry.LoudnessLUFS
		state.BufferDepthMS = telemetry.BufferDepthMS
		state.LastHeartbeat = time.Now()
	})
}

// IncrementUnderrun increments the underrun counter.
func (sm *StateManager) IncrementUnderrun(ctx context.Context, stationID string) error {
	return sm.UpdateState(ctx, stationID, func(state *models.ExecutorState) {
		state.UnderrunCount++
	})
}

// Heartbeat updates the last heartbeat timestamp.
func (sm *StateManager) Heartbeat(ctx context.Context, stationID string) error {
	return sm.UpdateState(ctx, stationID, func(state *models.ExecutorState) {
		state.LastHeartbeat = time.Now()
	})
}

// ListStates returns all executor states.
func (sm *StateManager) ListStates(ctx context.Context) ([]*models.ExecutorState, error) {
	var states []*models.ExecutorState
	err := sm.db.WithContext(ctx).Find(&states).Error
	if err != nil {
		return nil, fmt.Errorf("list executor states: %w", err)
	}
	return states, nil
}

// ClearCache removes a station from the in-memory cache.
func (sm *StateManager) ClearCache(stationID string) {
	sm.mu.Lock()
	delete(sm.states, stationID)
	sm.mu.Unlock()
}

// Telemetry contains real-time executor telemetry data.
type Telemetry struct {
	AudioLevelL   float64 // Left channel RMS level (-60 to 0 dBFS)
	AudioLevelR   float64 // Right channel RMS level
	LoudnessLUFS  float64 // Current LUFS measurement
	BufferDepthMS int64   // Milliseconds of buffered audio
}
