/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package priority

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

var (
	// ErrNoActiveSources indicates no active priority sources exist for the station.
	ErrNoActiveSources = errors.New("no active priority sources")

	// ErrInvalidPriority indicates an invalid priority level was provided.
	ErrInvalidPriority = errors.New("invalid priority level")

	// ErrSourceNotFound indicates the specified source was not found.
	ErrSourceNotFound = errors.New("priority source not found")
)

// Resolver manages priority source selection and state transitions.
type Resolver struct {
	db     *gorm.DB
	logger zerolog.Logger
}

// NewResolver creates a priority resolver instance.
func NewResolver(db *gorm.DB, logger zerolog.Logger) *Resolver {
	return &Resolver{
		db:     db,
		logger: logger,
	}
}

// TransitionRequest describes a request to change the active priority source.
type TransitionRequest struct {
	StationID    string
	NewPriority  models.PriorityLevel
	SourceType   models.SourceType
	SourceID     string
	MountID      string
	Metadata     map[string]any
	ForcePreempt bool // If true, preempt even at same priority level
}

// TransitionResult contains the outcome of a priority transition.
type TransitionResult struct {
	Preempted       bool                   // Was a higher/equal priority source preempted?
	OldSource       *models.PrioritySource // Previous active source (if any)
	NewSource       *models.PrioritySource // Newly activated source
	RequiresFade    bool                   // Should audio fade between sources?
	TransitionType  TransitionType         // Type of transition that occurred
}

// TransitionType enumerates the kinds of priority transitions.
type TransitionType string

const (
	TransitionNone         TransitionType = "none"          // No change needed
	TransitionPreempt      TransitionType = "preempt"       // Higher priority preempts lower
	TransitionRelease      TransitionType = "release"       // Source released, return to lower priority
	TransitionSwitch       TransitionType = "switch"        // Switch within same priority level
	TransitionEmergency    TransitionType = "emergency"     // Emergency activation (immediate)
	TransitionFallback     TransitionType = "fallback"      // Fall back to safety content
)

// GetCurrentSource returns the highest priority active source for a station.
func (r *Resolver) GetCurrentSource(ctx context.Context, stationID string) (*models.PrioritySource, error) {
	var source models.PrioritySource

	err := r.db.WithContext(ctx).
		Where("station_id = ?", stationID).
		Where("active = ?", true).
		Order("priority ASC"). // Lower number = higher priority
		First(&source).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNoActiveSources
	}
	if err != nil {
		return nil, fmt.Errorf("query current source: %w", err)
	}

	return &source, nil
}

// GetActiveSourcesByPriority returns all active sources for a station, ordered by priority.
func (r *Resolver) GetActiveSourcesByPriority(ctx context.Context, stationID string) ([]models.PrioritySource, error) {
	var sources []models.PrioritySource

	err := r.db.WithContext(ctx).
		Where("station_id = ?", stationID).
		Where("active = ?", true).
		Order("priority ASC").
		Find(&sources).Error

	if err != nil {
		return nil, fmt.Errorf("query active sources: %w", err)
	}

	return sources, nil
}

// CanPreempt checks if a new priority can preempt the current active source.
func (r *Resolver) CanPreempt(current *models.PrioritySource, newPriority models.PriorityLevel) bool {
	if current == nil {
		return true // No active source, always can activate
	}

	// Lower number = higher priority
	return newPriority < current.Priority
}

// Transition activates a new priority source and handles preemption logic.
func (r *Resolver) Transition(ctx context.Context, req TransitionRequest) (*TransitionResult, error) {
	// Validate priority level
	if req.NewPriority < models.PriorityEmergency || req.NewPriority > models.PriorityFallback {
		return nil, ErrInvalidPriority
	}

	// Start transaction
	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Get current active source
	currentSource, err := r.getCurrentSourceTx(tx, req.StationID)
	if err != nil && !errors.Is(err, ErrNoActiveSources) {
		tx.Rollback()
		return nil, fmt.Errorf("get current source: %w", err)
	}

	// Check if preemption is allowed
	if currentSource != nil && !req.ForcePreempt {
		if !r.CanPreempt(currentSource, req.NewPriority) {
			tx.Rollback()
			return &TransitionResult{
				Preempted:      false,
				OldSource:      currentSource,
				NewSource:      nil,
				TransitionType: TransitionNone,
			}, nil
		}
	}

	// Deactivate current source if being preempted
	var oldSource *models.PrioritySource
	if currentSource != nil {
		currentSource.Deactivate()
		if err := tx.Save(currentSource).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("deactivate current source: %w", err)
		}
		oldSource = currentSource
	}

	// Create and activate new source
	newSource := &models.PrioritySource{
		ID:          generateID(),
		StationID:   req.StationID,
		MountID:     req.MountID,
		Priority:    req.NewPriority,
		SourceType:  req.SourceType,
		SourceID:    req.SourceID,
		Metadata:    req.Metadata,
		Active:      true,
		ActivatedAt: time.Now(),
	}

	if err := tx.Create(newSource).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("create new source: %w", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// Determine transition type and fade requirement
	transitionType := r.determineTransitionType(oldSource, newSource)
	requiresFade := r.requiresFade(oldSource, newSource, transitionType)

	r.logger.Info().
		Str("station_id", req.StationID).
		Str("transition_type", string(transitionType)).
		Int("new_priority", int(req.NewPriority)).
		Bool("preempted", oldSource != nil).
		Bool("requires_fade", requiresFade).
		Msg("priority transition completed")

	return &TransitionResult{
		Preempted:      oldSource != nil,
		OldSource:      oldSource,
		NewSource:      newSource,
		RequiresFade:   requiresFade,
		TransitionType: transitionType,
	}, nil
}

// Release deactivates a priority source and returns to the next highest priority.
func (r *Resolver) Release(ctx context.Context, stationID, sourceID string) (*TransitionResult, error) {
	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Find and deactivate the specified source
	var source models.PrioritySource
	err := tx.Where("source_id = ?", sourceID).Where("station_id = ?", stationID).First(&source).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		tx.Rollback()
		return nil, ErrSourceNotFound
	}
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("query source: %w", err)
	}

	source.Deactivate()
	if err := tx.Save(&source).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("deactivate source: %w", err)
	}

	// Find the next highest priority active source
	var nextSource models.PrioritySource
	err = tx.
		Where("station_id = ?", stationID).
		Where("active = ?", true).
		Where("id != ?", sourceID).
		Order("priority ASC").
		First(&nextSource).Error

	hasNext := err == nil

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	var transitionType TransitionType
	var newSourcePtr *models.PrioritySource

	if hasNext {
		transitionType = TransitionRelease
		newSourcePtr = &nextSource
	} else {
		transitionType = TransitionFallback
		newSourcePtr = nil
	}

	r.logger.Info().
		Str("station_id", stationID).
		Str("released_source_id", sourceID).
		Str("transition_type", string(transitionType)).
		Bool("has_next_source", hasNext).
		Msg("priority source released")

	return &TransitionResult{
		Preempted:      false,
		OldSource:      &source,
		NewSource:      newSourcePtr,
		RequiresFade:   true,
		TransitionType: transitionType,
	}, nil
}

// InsertEmergency immediately activates an emergency source, preempting everything.
func (r *Resolver) InsertEmergency(ctx context.Context, stationID, sourceID string, metadata map[string]any) (*TransitionResult, error) {
	req := TransitionRequest{
		StationID:    stationID,
		NewPriority:  models.PriorityEmergency,
		SourceType:   models.SourceTypeEmergency,
		SourceID:     sourceID,
		Metadata:     metadata,
		ForcePreempt: true,
	}

	result, err := r.Transition(ctx, req)
	if err != nil {
		return nil, err
	}

	result.TransitionType = TransitionEmergency
	result.RequiresFade = false // Emergency cuts immediately, no fade

	return result, nil
}

// Helper functions

func (r *Resolver) getCurrentSourceTx(tx *gorm.DB, stationID string) (*models.PrioritySource, error) {
	var source models.PrioritySource

	err := tx.
		Where("station_id = ?", stationID).
		Where("active = ?", true).
		Order("priority ASC").
		First(&source).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNoActiveSources
	}
	if err != nil {
		return nil, err
	}

	return &source, nil
}

func (r *Resolver) determineTransitionType(old, new *models.PrioritySource) TransitionType {
	if old == nil {
		return TransitionSwitch
	}

	if new.Priority == models.PriorityEmergency {
		return TransitionEmergency
	}

	if new.Priority < old.Priority {
		return TransitionPreempt
	}

	if new.Priority == old.Priority {
		return TransitionSwitch
	}

	return TransitionRelease
}

func (r *Resolver) requiresFade(old, new *models.PrioritySource, transitionType TransitionType) bool {
	// Emergency transitions don't fade
	if transitionType == TransitionEmergency {
		return false
	}

	// No fade if no previous source
	if old == nil {
		return false
	}

	// Fade for all other transitions
	return true
}

func generateID() string {
	return uuid.NewString()
}
