/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package priority

import (
	"context"
	"errors"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newResolverTestDB(t *testing.T) (*Resolver, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.PrioritySource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewResolver(db, zerolog.Nop()), db
}

func TestEmergencyPreemptsAll(t *testing.T) {
	levels := []struct {
		name     string
		priority models.PriorityLevel
	}{
		{"live_override", models.PriorityLiveOverride},
		{"live_scheduled", models.PriorityLiveScheduled},
		{"automation", models.PriorityAutomation},
		{"fallback", models.PriorityFallback},
	}

	for _, tt := range levels {
		t.Run(tt.name, func(t *testing.T) {
			resolver, _ := newResolverTestDB(t)
			ctx := context.Background()

			// Activate current source
			_, err := resolver.Transition(ctx, TransitionRequest{
				StationID:   "s1",
				NewPriority: tt.priority,
				SourceType:  models.SourceTypeMedia,
				SourceID:    "src-current",
			})
			if err != nil {
				t.Fatalf("activate current: %v", err)
			}

			// Insert emergency
			result, err := resolver.InsertEmergency(ctx, "s1", "src-emergency", nil)
			if err != nil {
				t.Fatalf("insert emergency: %v", err)
			}
			if result.TransitionType != TransitionEmergency {
				t.Errorf("TransitionType = %q, want %q", result.TransitionType, TransitionEmergency)
			}
			if result.RequiresFade {
				t.Error("expected RequiresFade=false for emergency")
			}
			if !result.Preempted {
				t.Error("expected Preempted=true")
			}
		})
	}
}

func TestLiveOverridePreemptsAutomation(t *testing.T) {
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	// Activate automation
	_, err := resolver.Transition(ctx, TransitionRequest{
		StationID:   "s1",
		NewPriority: models.PriorityAutomation,
		SourceType:  models.SourceTypeMedia,
		SourceID:    "auto-1",
	})
	if err != nil {
		t.Fatalf("activate automation: %v", err)
	}

	// Transition to live override
	result, err := resolver.Transition(ctx, TransitionRequest{
		StationID:   "s1",
		NewPriority: models.PriorityLiveOverride,
		SourceType:  models.SourceTypeLive,
		SourceID:    "live-1",
	})
	if err != nil {
		t.Fatalf("transition live override: %v", err)
	}
	if result.TransitionType != TransitionPreempt {
		t.Errorf("TransitionType = %q, want %q", result.TransitionType, TransitionPreempt)
	}
	if result.OldSource == nil {
		t.Fatal("expected OldSource to be set")
	}
}

func TestScheduledLiveCannotPreemptOverride(t *testing.T) {
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	// Activate live override
	_, err := resolver.Transition(ctx, TransitionRequest{
		StationID:   "s1",
		NewPriority: models.PriorityLiveOverride,
		SourceType:  models.SourceTypeLive,
		SourceID:    "override-1",
	})
	if err != nil {
		t.Fatalf("activate override: %v", err)
	}

	// Attempt scheduled live (should fail to preempt)
	result, err := resolver.Transition(ctx, TransitionRequest{
		StationID:   "s1",
		NewPriority: models.PriorityLiveScheduled,
		SourceType:  models.SourceTypeLive,
		SourceID:    "scheduled-1",
	})
	if err != nil {
		t.Fatalf("transition: %v", err)
	}
	if result.TransitionType != TransitionNone {
		t.Errorf("TransitionType = %q, want %q", result.TransitionType, TransitionNone)
	}
	if result.Preempted {
		t.Error("expected Preempted=false")
	}
}

func TestScheduledLivePreemptsAutomation(t *testing.T) {
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	_, err := resolver.Transition(ctx, TransitionRequest{
		StationID:   "s1",
		NewPriority: models.PriorityAutomation,
		SourceType:  models.SourceTypeMedia,
		SourceID:    "auto-1",
	})
	if err != nil {
		t.Fatalf("activate automation: %v", err)
	}

	result, err := resolver.Transition(ctx, TransitionRequest{
		StationID:   "s1",
		NewPriority: models.PriorityLiveScheduled,
		SourceType:  models.SourceTypeLive,
		SourceID:    "scheduled-1",
	})
	if err != nil {
		t.Fatalf("transition: %v", err)
	}
	if result.TransitionType != TransitionPreempt {
		t.Errorf("TransitionType = %q, want %q", result.TransitionType, TransitionPreempt)
	}
}

func TestReleaseReturnsToNextHighest(t *testing.T) {
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	// Activate automation
	_, err := resolver.Transition(ctx, TransitionRequest{
		StationID:   "s1",
		NewPriority: models.PriorityAutomation,
		SourceType:  models.SourceTypeMedia,
		SourceID:    "auto-1",
	})
	if err != nil {
		t.Fatalf("activate automation: %v", err)
	}

	// Activate live override (preempts automation)
	_, err = resolver.Transition(ctx, TransitionRequest{
		StationID:   "s1",
		NewPriority: models.PriorityLiveOverride,
		SourceType:  models.SourceTypeLive,
		SourceID:    "live-1",
	})
	if err != nil {
		t.Fatalf("activate live: %v", err)
	}

	// Release live override → should return to automation... but automation was deactivated by preemption.
	// Release finds no remaining active source → fallback.
	result, err := resolver.Release(ctx, "s1", "live-1")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if result.TransitionType != TransitionFallback {
		t.Errorf("TransitionType = %q, want %q", result.TransitionType, TransitionFallback)
	}
}

func TestReleaseWithNoFallback(t *testing.T) {
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	// Activate a single source
	_, err := resolver.Transition(ctx, TransitionRequest{
		StationID:   "s1",
		NewPriority: models.PriorityAutomation,
		SourceType:  models.SourceTypeMedia,
		SourceID:    "auto-1",
	})
	if err != nil {
		t.Fatalf("activate: %v", err)
	}

	result, err := resolver.Release(ctx, "s1", "auto-1")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if result.TransitionType != TransitionFallback {
		t.Errorf("TransitionType = %q, want %q", result.TransitionType, TransitionFallback)
	}
	if result.NewSource != nil {
		t.Errorf("expected NewSource=nil, got %+v", result.NewSource)
	}
}

func TestMultipleActiveSourcesOrdering(t *testing.T) {
	resolver, db := newResolverTestDB(t)
	ctx := context.Background()

	// Directly insert active sources at different priorities
	sources := []models.PrioritySource{
		{ID: "ps-1", StationID: "s1", Priority: models.PriorityFallback, SourceType: models.SourceTypeMedia, SourceID: "fb", Active: true},
		{ID: "ps-2", StationID: "s1", Priority: models.PriorityAutomation, SourceType: models.SourceTypeMedia, SourceID: "auto", Active: true},
		{ID: "ps-3", StationID: "s1", Priority: models.PriorityLiveScheduled, SourceType: models.SourceTypeLive, SourceID: "sched", Active: true},
	}
	for _, s := range sources {
		if err := db.Create(&s).Error; err != nil {
			t.Fatalf("create source: %v", err)
		}
	}

	result, err := resolver.GetActiveSourcesByPriority(ctx, "s1")
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("got %d sources, want 3", len(result))
	}
	if result[0].Priority != models.PriorityLiveScheduled {
		t.Errorf("first priority = %d, want %d", result[0].Priority, models.PriorityLiveScheduled)
	}
	if result[1].Priority != models.PriorityAutomation {
		t.Errorf("second priority = %d, want %d", result[1].Priority, models.PriorityAutomation)
	}
	if result[2].Priority != models.PriorityFallback {
		t.Errorf("third priority = %d, want %d", result[2].Priority, models.PriorityFallback)
	}
}

func TestSamePriorityNoPreempt(t *testing.T) {
	resolver := NewResolver(nil, zerolog.Nop())
	current := &models.PrioritySource{Priority: models.PriorityAutomation}
	if resolver.CanPreempt(current, models.PriorityAutomation) {
		t.Error("expected CanPreempt=false at same priority")
	}
}

func TestSamePriorityForcePreempt(t *testing.T) {
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	// Activate automation
	_, err := resolver.Transition(ctx, TransitionRequest{
		StationID:   "s1",
		NewPriority: models.PriorityAutomation,
		SourceType:  models.SourceTypeMedia,
		SourceID:    "auto-1",
	})
	if err != nil {
		t.Fatalf("activate: %v", err)
	}

	// Force preempt at same level
	result, err := resolver.Transition(ctx, TransitionRequest{
		StationID:    "s1",
		NewPriority:  models.PriorityAutomation,
		SourceType:   models.SourceTypeMedia,
		SourceID:     "auto-2",
		ForcePreempt: true,
	})
	if err != nil {
		t.Fatalf("force preempt: %v", err)
	}
	if result.TransitionType != TransitionSwitch {
		t.Errorf("TransitionType = %q, want %q", result.TransitionType, TransitionSwitch)
	}
	if result.OldSource == nil {
		t.Fatal("expected OldSource to be set")
	}
}

func TestInsertEmergency(t *testing.T) {
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	result, err := resolver.InsertEmergency(ctx, "s1", "emer-1", map[string]any{"reason": "test"})
	if err != nil {
		t.Fatalf("insert emergency: %v", err)
	}
	if result.TransitionType != TransitionEmergency {
		t.Errorf("TransitionType = %q, want %q", result.TransitionType, TransitionEmergency)
	}
	if result.RequiresFade {
		t.Error("expected RequiresFade=false")
	}
	if result.NewSource == nil {
		t.Fatal("expected NewSource to be set")
	}
	if result.NewSource.Priority != models.PriorityEmergency {
		t.Errorf("priority = %d, want %d", result.NewSource.Priority, models.PriorityEmergency)
	}
}

func TestTransitionInvalidPriority(t *testing.T) {
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		priority models.PriorityLevel
	}{
		{"negative", models.PriorityLevel(-1)},
		{"too high", models.PriorityLevel(99)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolver.Transition(ctx, TransitionRequest{
				StationID:   "s1",
				NewPriority: tt.priority,
				SourceType:  models.SourceTypeMedia,
				SourceID:    "src-1",
			})
			if !errors.Is(err, ErrInvalidPriority) {
				t.Errorf("got err=%v, want ErrInvalidPriority", err)
			}
		})
	}
}
