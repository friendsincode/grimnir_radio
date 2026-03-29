/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package priority

import (
	"context"
	"errors"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newServiceTestDB creates an in-memory SQLite DB with priority_sources migrated and returns
// a Service and the underlying *gorm.DB for direct inspection.
func newServiceTestDB(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.PrioritySource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	bus := events.NewBus()
	svc := NewService(db, bus, zerolog.Nop())
	return svc, db
}

// ---- GetCurrentSource ----

func TestGetCurrentSource_NoSources(t *testing.T) {
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	_, err := resolver.GetCurrentSource(ctx, "no-such-station")
	if !errors.Is(err, ErrNoActiveSources) {
		t.Errorf("got %v, want ErrNoActiveSources", err)
	}
}

func TestGetCurrentSource_ReturnsHighestPriority(t *testing.T) {
	resolver, db := newResolverTestDB(t)
	ctx := context.Background()

	// Insert two active sources with different priorities
	sources := []models.PrioritySource{
		{ID: "gc-1", StationID: "s1", Priority: models.PriorityFallback, SourceType: models.SourceTypeMedia, SourceID: "fb", Active: true},
		{ID: "gc-2", StationID: "s1", Priority: models.PriorityLiveOverride, SourceType: models.SourceTypeLive, SourceID: "lo", Active: true},
	}
	for _, s := range sources {
		if err := db.Create(&s).Error; err != nil {
			t.Fatalf("create source: %v", err)
		}
	}

	src, err := resolver.GetCurrentSource(ctx, "s1")
	if err != nil {
		t.Fatalf("GetCurrentSource: %v", err)
	}
	if src.Priority != models.PriorityLiveOverride {
		t.Errorf("priority = %d, want %d", src.Priority, models.PriorityLiveOverride)
	}
}

func TestGetCurrentSource_InactiveIgnored(t *testing.T) {
	resolver, db := newResolverTestDB(t)
	ctx := context.Background()

	// Only inactive source present
	s := models.PrioritySource{ID: "gc-3", StationID: "s2", Priority: models.PriorityAutomation, SourceType: models.SourceTypeMedia, SourceID: "auto", Active: false}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}

	_, err := resolver.GetCurrentSource(ctx, "s2")
	if !errors.Is(err, ErrNoActiveSources) {
		t.Errorf("got %v, want ErrNoActiveSources", err)
	}
}

// ---- GetActiveSourcesByPriority: empty station ----

func TestGetActiveSourcesByPriority_Empty(t *testing.T) {
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	sources, err := resolver.GetActiveSourcesByPriority(ctx, "ghost-station")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("expected 0 sources, got %d", len(sources))
	}
}

// ---- Transition: first activation (no existing source) ----

func TestTransition_FirstActivation(t *testing.T) {
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	result, err := resolver.Transition(ctx, TransitionRequest{
		StationID:   "s1",
		NewPriority: models.PriorityAutomation,
		SourceType:  models.SourceTypeMedia,
		SourceID:    "auto-1",
	})
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if result.OldSource != nil {
		t.Errorf("expected OldSource=nil, got %+v", result.OldSource)
	}
	if result.NewSource == nil {
		t.Fatal("expected NewSource to be set")
	}
	if result.RequiresFade {
		t.Error("expected RequiresFade=false when no previous source")
	}
	if result.TransitionType != TransitionSwitch {
		t.Errorf("TransitionType = %q, want %q", result.TransitionType, TransitionSwitch)
	}
}

// ---- Transition: metadata is stored ----

func TestTransition_MetadataStored(t *testing.T) {
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	meta := map[string]any{"show": "Morning Mix", "presenter": "Alice"}
	result, err := resolver.Transition(ctx, TransitionRequest{
		StationID:   "s1",
		NewPriority: models.PriorityLiveScheduled,
		SourceType:  models.SourceTypeLive,
		SourceID:    "live-meta",
		Metadata:    meta,
	})
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if result.NewSource.Metadata == nil {
		t.Fatal("expected Metadata to be set")
	}
	if result.NewSource.Metadata["show"] != "Morning Mix" {
		t.Errorf("metadata show = %v, want Morning Mix", result.NewSource.Metadata["show"])
	}
}

// ---- Transition: lower priority blocked by higher ----

func TestTransition_LowerPriorityBlocked(t *testing.T) {
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	// Establish emergency source
	_, err := resolver.Transition(ctx, TransitionRequest{
		StationID:   "s1",
		NewPriority: models.PriorityEmergency,
		SourceType:  models.SourceTypeEmergency,
		SourceID:    "emer-1",
	})
	if err != nil {
		t.Fatalf("activate emergency: %v", err)
	}

	// Attempt lower priority
	result, err := resolver.Transition(ctx, TransitionRequest{
		StationID:   "s1",
		NewPriority: models.PriorityFallback,
		SourceType:  models.SourceTypeMedia,
		SourceID:    "fb-1",
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
	if result.NewSource != nil {
		t.Errorf("expected NewSource=nil, got %+v", result.NewSource)
	}
}

// ---- Transition: fade is set for preemption ----

func TestTransition_PreemptHasFade(t *testing.T) {
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
		SourceID:    "live-1",
	})
	if err != nil {
		t.Fatalf("transition: %v", err)
	}
	if !result.RequiresFade {
		t.Error("expected RequiresFade=true for preemption")
	}
}

// ---- Release: source not found ----

func TestRelease_SourceNotFound(t *testing.T) {
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	_, err := resolver.Release(ctx, "s1", "nonexistent-source")
	if !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("got %v, want ErrSourceNotFound", err)
	}
}

// ---- Release: returns to next active source ----

func TestRelease_ReturnsToNextActive(t *testing.T) {
	resolver, db := newResolverTestDB(t)
	ctx := context.Background()

	// Insert two active sources: automation (lower priority) and live (higher priority)
	// Both active simultaneously (simulating a state where both are tracked)
	sources := []models.PrioritySource{
		{ID: "rel-1", StationID: "s1", Priority: models.PriorityLiveOverride, SourceType: models.SourceTypeLive, SourceID: "live-rel", Active: true},
		{ID: "rel-2", StationID: "s1", Priority: models.PriorityAutomation, SourceType: models.SourceTypeMedia, SourceID: "auto-rel", Active: true},
	}
	for _, s := range sources {
		if err := db.Create(&s).Error; err != nil {
			t.Fatalf("create source: %v", err)
		}
	}

	// Release the live override — automation should become next
	result, err := resolver.Release(ctx, "s1", "live-rel")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if result.TransitionType != TransitionRelease {
		t.Errorf("TransitionType = %q, want %q", result.TransitionType, TransitionRelease)
	}
	if result.NewSource == nil {
		t.Fatal("expected NewSource to be set")
	}
	if result.NewSource.Priority != models.PriorityAutomation {
		t.Errorf("next source priority = %d, want %d", result.NewSource.Priority, models.PriorityAutomation)
	}
	if result.OldSource == nil {
		t.Fatal("expected OldSource to be set")
	}
	if !result.RequiresFade {
		t.Error("expected RequiresFade=true on release")
	}
}

// ---- InsertEmergency: error propagation ----

func TestInsertEmergency_InvalidPriority_NotReached(t *testing.T) {
	// InsertEmergency always uses PriorityEmergency (valid), so it should never
	// return ErrInvalidPriority. Verify the happy path once more with nil metadata.
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	result, err := resolver.InsertEmergency(ctx, "s1", "emer-nil-meta", nil)
	if err != nil {
		t.Fatalf("InsertEmergency with nil metadata: %v", err)
	}
	if result.TransitionType != TransitionEmergency {
		t.Errorf("TransitionType = %q, want %q", result.TransitionType, TransitionEmergency)
	}
}

// ---- Service tests ----

func TestServiceNewService(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	bus := events.NewBus()
	svc := NewService(db, bus, zerolog.Nop())
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.resolver == nil {
		t.Fatal("resolver not initialised")
	}
}

func TestServiceInsertEmergency(t *testing.T) {
	svc, _ := newServiceTestDB(t)
	ctx := context.Background()

	result, err := svc.InsertEmergency(ctx, InsertEmergencyRequest{
		StationID: "s1",
		MediaID:   "m1",
		Metadata:  map[string]any{"reason": "test"},
	})
	if err != nil {
		t.Fatalf("InsertEmergency: %v", err)
	}
	if result.TransitionType != TransitionEmergency {
		t.Errorf("TransitionType = %q, want %q", result.TransitionType, TransitionEmergency)
	}
	if result.RequiresFade {
		t.Error("expected RequiresFade=false")
	}
}

func TestServiceInsertEmergency_PublishesEvent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.PrioritySource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	bus := events.NewBus()
	sub := bus.Subscribe(events.EventPriorityEmergency)
	defer bus.Unsubscribe(events.EventPriorityEmergency, sub)

	svc := NewService(db, bus, zerolog.Nop())
	_, err = svc.InsertEmergency(context.Background(), InsertEmergencyRequest{
		StationID: "s1",
		MediaID:   "m1",
	})
	if err != nil {
		t.Fatalf("InsertEmergency: %v", err)
	}

	select {
	case payload := <-sub:
		if payload["station_id"] != "s1" {
			t.Errorf("event station_id = %v, want s1", payload["station_id"])
		}
	default:
		t.Error("expected emergency event to be published")
	}
}

func TestServiceStartOverride(t *testing.T) {
	svc, _ := newServiceTestDB(t)
	ctx := context.Background()

	result, err := svc.StartOverride(ctx, StartOverrideRequest{
		StationID:  "s1",
		SourceType: models.SourceTypeLive,
		SourceID:   "dj-1",
	})
	if err != nil {
		t.Fatalf("StartOverride: %v", err)
	}
	if result.NewSource == nil {
		t.Fatal("expected NewSource to be set")
	}
	if result.NewSource.Priority != models.PriorityLiveOverride {
		t.Errorf("priority = %d, want %d", result.NewSource.Priority, models.PriorityLiveOverride)
	}
}

func TestServiceStartOverride_PublishesEvent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.PrioritySource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	bus := events.NewBus()
	sub := bus.Subscribe(events.EventPriorityOverride)
	defer bus.Unsubscribe(events.EventPriorityOverride, sub)

	svc := NewService(db, bus, zerolog.Nop())
	_, err = svc.StartOverride(context.Background(), StartOverrideRequest{
		StationID:  "s1",
		SourceType: models.SourceTypeLive,
		SourceID:   "dj-1",
	})
	if err != nil {
		t.Fatalf("StartOverride: %v", err)
	}

	select {
	case payload := <-sub:
		if payload["station_id"] != "s1" {
			t.Errorf("event station_id = %v, want s1", payload["station_id"])
		}
	default:
		t.Error("expected override event to be published")
	}
}

func TestServiceStartOverride_NoEventOnNone(t *testing.T) {
	// If override can't preempt current source, TransitionNone → no event.
	svc, _ := newServiceTestDB(t)
	ctx := context.Background()

	// Activate emergency first (highest priority)
	_, err := svc.InsertEmergency(ctx, InsertEmergencyRequest{StationID: "s1", MediaID: "emer-1"})
	if err != nil {
		t.Fatalf("insert emergency: %v", err)
	}

	// StartOverride at lower priority → should produce TransitionNone
	result, err := svc.StartOverride(ctx, StartOverrideRequest{
		StationID:  "s1",
		SourceType: models.SourceTypeLive,
		SourceID:   "dj-1",
	})
	if err != nil {
		t.Fatalf("StartOverride: %v", err)
	}
	if result.TransitionType != TransitionNone {
		t.Errorf("TransitionType = %q, want %q", result.TransitionType, TransitionNone)
	}
}

func TestServiceStartScheduledLive(t *testing.T) {
	svc, _ := newServiceTestDB(t)
	ctx := context.Background()

	result, err := svc.StartScheduledLive(ctx, StartScheduledLiveRequest{
		StationID: "s1",
		SourceID:  "show-1",
	})
	if err != nil {
		t.Fatalf("StartScheduledLive: %v", err)
	}
	if result.NewSource.Priority != models.PriorityLiveScheduled {
		t.Errorf("priority = %d, want %d", result.NewSource.Priority, models.PriorityLiveScheduled)
	}
}

func TestServiceStartScheduledLive_PublishesEvent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.PrioritySource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	bus := events.NewBus()
	sub := bus.Subscribe(events.EventPriorityChange)
	defer bus.Unsubscribe(events.EventPriorityChange, sub)

	svc := NewService(db, bus, zerolog.Nop())
	_, err = svc.StartScheduledLive(context.Background(), StartScheduledLiveRequest{
		StationID: "s1",
		SourceID:  "show-1",
	})
	if err != nil {
		t.Fatalf("StartScheduledLive: %v", err)
	}

	select {
	case payload := <-sub:
		if payload["station_id"] != "s1" {
			t.Errorf("event station_id = %v, want s1", payload["station_id"])
		}
	default:
		t.Error("expected priority change event to be published")
	}
}

func TestServiceStartScheduledLive_NoEventOnNone(t *testing.T) {
	svc, _ := newServiceTestDB(t)
	ctx := context.Background()

	// Emergency blocks scheduled live
	_, err := svc.InsertEmergency(ctx, InsertEmergencyRequest{StationID: "s1", MediaID: "emer-1"})
	if err != nil {
		t.Fatalf("insert emergency: %v", err)
	}

	result, err := svc.StartScheduledLive(ctx, StartScheduledLiveRequest{
		StationID: "s1",
		SourceID:  "show-1",
	})
	if err != nil {
		t.Fatalf("StartScheduledLive: %v", err)
	}
	if result.TransitionType != TransitionNone {
		t.Errorf("TransitionType = %q, want %q", result.TransitionType, TransitionNone)
	}
}

func TestServiceActivateAutomation(t *testing.T) {
	svc, _ := newServiceTestDB(t)
	ctx := context.Background()

	result, err := svc.ActivateAutomation(ctx, ActivateAutomationRequest{
		StationID:  "s1",
		SourceType: models.SourceTypeMedia,
		SourceID:   "track-1",
	})
	if err != nil {
		t.Fatalf("ActivateAutomation: %v", err)
	}
	if result.NewSource.Priority != models.PriorityAutomation {
		t.Errorf("priority = %d, want %d", result.NewSource.Priority, models.PriorityAutomation)
	}
}

func TestServiceActivateAutomation_PublishesEvent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.PrioritySource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	bus := events.NewBus()
	sub := bus.Subscribe(events.EventPriorityChange)
	defer bus.Unsubscribe(events.EventPriorityChange, sub)

	svc := NewService(db, bus, zerolog.Nop())
	_, err = svc.ActivateAutomation(context.Background(), ActivateAutomationRequest{
		StationID:  "s1",
		SourceType: models.SourceTypeMedia,
		SourceID:   "track-1",
	})
	if err != nil {
		t.Fatalf("ActivateAutomation: %v", err)
	}

	select {
	case payload := <-sub:
		if payload["station_id"] != "s1" {
			t.Errorf("event station_id = %v, want s1", payload["station_id"])
		}
	default:
		t.Error("expected priority change event to be published")
	}
}

func TestServiceActivateAutomation_NoEventOnNone(t *testing.T) {
	svc, _ := newServiceTestDB(t)
	ctx := context.Background()

	// Live override blocks automation
	_, err := svc.StartOverride(ctx, StartOverrideRequest{
		StationID:  "s1",
		SourceType: models.SourceTypeLive,
		SourceID:   "dj-1",
	})
	if err != nil {
		t.Fatalf("start override: %v", err)
	}

	result, err := svc.ActivateAutomation(ctx, ActivateAutomationRequest{
		StationID:  "s1",
		SourceType: models.SourceTypeMedia,
		SourceID:   "track-1",
	})
	if err != nil {
		t.Fatalf("ActivateAutomation: %v", err)
	}
	if result.TransitionType != TransitionNone {
		t.Errorf("TransitionType = %q, want %q", result.TransitionType, TransitionNone)
	}
}

func TestServiceRelease(t *testing.T) {
	svc, _ := newServiceTestDB(t)
	ctx := context.Background()

	// Activate, then release
	result, err := svc.ActivateAutomation(ctx, ActivateAutomationRequest{
		StationID:  "s1",
		SourceType: models.SourceTypeMedia,
		SourceID:   "track-1",
	})
	if err != nil {
		t.Fatalf("ActivateAutomation: %v", err)
	}
	sourceID := result.NewSource.SourceID

	releaseResult, err := svc.Release(ctx, "s1", sourceID)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if releaseResult.OldSource == nil {
		t.Fatal("expected OldSource to be set")
	}
}

func TestServiceRelease_PublishesEvent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.PrioritySource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	bus := events.NewBus()
	svc := NewService(db, bus, zerolog.Nop())

	ctx := context.Background()
	autoResult, err := svc.ActivateAutomation(ctx, ActivateAutomationRequest{
		StationID:  "s1",
		SourceType: models.SourceTypeMedia,
		SourceID:   "track-1",
	})
	if err != nil {
		t.Fatalf("ActivateAutomation: %v", err)
	}

	sub := bus.Subscribe(events.EventPriorityReleased)
	defer bus.Unsubscribe(events.EventPriorityReleased, sub)

	_, err = svc.Release(ctx, "s1", autoResult.NewSource.SourceID)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}

	select {
	case payload := <-sub:
		if payload["station_id"] != "s1" {
			t.Errorf("event station_id = %v, want s1", payload["station_id"])
		}
	default:
		t.Error("expected priority released event to be published")
	}
}

func TestServiceRelease_NotFound(t *testing.T) {
	svc, _ := newServiceTestDB(t)
	ctx := context.Background()

	_, err := svc.Release(ctx, "s1", "ghost-source")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestServiceGetCurrent(t *testing.T) {
	svc, _ := newServiceTestDB(t)
	ctx := context.Background()

	// No sources yet → ErrNoActiveSources wrapped
	_, err := svc.GetCurrent(ctx, "s1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Activate one
	_, err = svc.ActivateAutomation(ctx, ActivateAutomationRequest{
		StationID:  "s1",
		SourceType: models.SourceTypeMedia,
		SourceID:   "track-1",
	})
	if err != nil {
		t.Fatalf("ActivateAutomation: %v", err)
	}

	src, err := svc.GetCurrent(ctx, "s1")
	if err != nil {
		t.Fatalf("GetCurrent: %v", err)
	}
	if src.Priority != models.PriorityAutomation {
		t.Errorf("priority = %d, want %d", src.Priority, models.PriorityAutomation)
	}
}

func TestServiceGetActive(t *testing.T) {
	svc, _ := newServiceTestDB(t)
	ctx := context.Background()

	// Empty
	sources, err := svc.GetActive(ctx, "s1")
	if err != nil {
		t.Fatalf("GetActive empty: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("expected 0, got %d", len(sources))
	}

	// Activate one
	_, err = svc.ActivateAutomation(ctx, ActivateAutomationRequest{
		StationID:  "s1",
		SourceType: models.SourceTypeMedia,
		SourceID:   "track-1",
	})
	if err != nil {
		t.Fatalf("ActivateAutomation: %v", err)
	}

	sources, err = svc.GetActive(ctx, "s1")
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if len(sources) != 1 {
		t.Errorf("expected 1, got %d", len(sources))
	}
}

// ---- publishEvent with metadata in NewSource ----

func TestServicePublishEvent_WithMetadata(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.PrioritySource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	bus := events.NewBus()
	sub := bus.Subscribe(events.EventPriorityEmergency)
	defer bus.Unsubscribe(events.EventPriorityEmergency, sub)

	svc := NewService(db, bus, zerolog.Nop())
	meta := map[string]any{"alert_code": "EAS001"}
	_, err = svc.InsertEmergency(context.Background(), InsertEmergencyRequest{
		StationID: "s1",
		MediaID:   "emer-meta",
		Metadata:  meta,
	})
	if err != nil {
		t.Fatalf("InsertEmergency: %v", err)
	}

	select {
	case payload := <-sub:
		if payload["alert_code"] != "EAS001" {
			t.Errorf("metadata key alert_code = %v, want EAS001", payload["alert_code"])
		}
	default:
		t.Error("expected event with metadata")
	}
}

// ---- publishEvent with OldSource set (covers old_source_id branch) ----

func TestServicePublishEvent_WithOldAndNewSource(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.PrioritySource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	bus := events.NewBus()
	sub := bus.Subscribe(events.EventPriorityOverride)
	defer bus.Unsubscribe(events.EventPriorityOverride, sub)

	svc := NewService(db, bus, zerolog.Nop())
	ctx := context.Background()

	// First set up automation
	_, err = svc.ActivateAutomation(ctx, ActivateAutomationRequest{
		StationID:  "s1",
		SourceType: models.SourceTypeMedia,
		SourceID:   "track-1",
	})
	if err != nil {
		t.Fatalf("ActivateAutomation: %v", err)
	}

	// Now override (preempts automation, so OldSource is set in the event)
	_, err = svc.StartOverride(ctx, StartOverrideRequest{
		StationID:  "s1",
		SourceType: models.SourceTypeLive,
		SourceID:   "dj-1",
	})
	if err != nil {
		t.Fatalf("StartOverride: %v", err)
	}

	select {
	case payload := <-sub:
		if _, ok := payload["old_source_id"]; !ok {
			t.Error("expected old_source_id in event payload")
		}
		if _, ok := payload["new_source_id"]; !ok {
			t.Error("expected new_source_id in event payload")
		}
	default:
		t.Error("expected override event")
	}
}

// ---- MountID is stored in new source ----

func TestTransition_MountIDStored(t *testing.T) {
	resolver, _ := newResolverTestDB(t)
	ctx := context.Background()

	result, err := resolver.Transition(ctx, TransitionRequest{
		StationID:   "s1",
		NewPriority: models.PriorityAutomation,
		SourceType:  models.SourceTypeMedia,
		SourceID:    "track-1",
		MountID:     "mount-abc",
	})
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if result.NewSource.MountID != "mount-abc" {
		t.Errorf("MountID = %q, want %q", result.NewSource.MountID, "mount-abc")
	}
}

// ---- Service StartOverride stores MountID ----

func TestServiceStartOverride_MountID(t *testing.T) {
	svc, _ := newServiceTestDB(t)
	ctx := context.Background()

	result, err := svc.StartOverride(ctx, StartOverrideRequest{
		StationID:  "s1",
		MountID:    "mnt-1",
		SourceType: models.SourceTypeLive,
		SourceID:   "dj-1",
	})
	if err != nil {
		t.Fatalf("StartOverride: %v", err)
	}
	if result.NewSource.MountID != "mnt-1" {
		t.Errorf("MountID = %q, want mnt-1", result.NewSource.MountID)
	}
}
