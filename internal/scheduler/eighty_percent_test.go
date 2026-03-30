/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/clock"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// TestNewLeaderAware_WithNilElection covers the NewLeaderAware constructor
// without needing Redis — passing a nil election pointer is valid since the
// pointer is only stored, not dereferenced, during construction.
func TestNewLeaderAware_WithNilElection(t *testing.T) {
	svc, _, _ := newServiceForAPITests(t)
	las := NewLeaderAware(svc, nil, zerolog.Nop())
	if las == nil {
		t.Fatal("NewLeaderAware returned nil")
	}
	if las.scheduler != svc {
		t.Error("scheduler field not set")
	}
	if las.schedulerRunning {
		t.Error("schedulerRunning should be false")
	}
}

// TestStartScheduler_DeadlineExceededLogsError covers the error-log branch in
// startScheduler's goroutine (line 126).  Run returns context.DeadlineExceeded
// when its context expires; since DeadlineExceeded != context.Canceled the
// error branch fires.
func TestStartScheduler_DeadlineExceededLogsError(t *testing.T) {
	las := newTestLeaderAwareScheduler(t)

	// Use a very short deadline so Run exits quickly with DeadlineExceeded.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	las.ctx = ctx

	las.startScheduler()

	// Wait for the goroutine to finish (deadline + small buffer).
	time.Sleep(150 * time.Millisecond)

	las.mu.Lock()
	running := las.schedulerRunning
	las.mu.Unlock()
	if running {
		t.Error("expected schedulerRunning false after goroutine exit")
	}
}

// TestCreateStopsetEntry_Idempotent covers the count>0 early-return path
// (line 875-876) by inserting the same stopset slot twice.
func TestCreateStopsetEntry_Idempotent(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	createTestMount(t, db, stationID, mountID)

	plan := clock.SlotPlan{
		SlotID:   "slot-idempotent-stopset",
		StartsAt: time.Now().UTC().Add(2 * time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(4 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypeStopset),
		Payload:  map[string]any{"mount_id": mountID},
	}

	// First call — creates the entry.
	if err := svc.createStopsetEntry(ctx, stationID, plan); err != nil {
		t.Fatalf("first createStopsetEntry: %v", err)
	}
	// Second call — idempotent, should return nil without creating a duplicate.
	if err := svc.createStopsetEntry(ctx, stationID, plan); err != nil {
		t.Fatalf("second createStopsetEntry (idempotent): %v", err)
	}

	var count int64
	db.Model(&models.ScheduleEntry{}).Where("station_id = ?", stationID).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 entry after idempotent insert, got %d", count)
	}
}

// TestCreateHardItemEntry_MissingMediaID covers the warnOnce path (lines 817-821)
// when media_id is absent from the plan payload.
func TestCreateHardItemEntry_MissingMediaID(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	createTestMount(t, db, stationID, mountID)

	plan := clock.SlotPlan{
		SlotID:   "slot-missing-media",
		StartsAt: time.Now().UTC().Add(2 * time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypeHardItem),
		Payload:  map[string]any{"mount_id": mountID}, // no media_id
	}

	// Should return nil (warn and skip) without creating an entry.
	if err := svc.createHardItemEntry(ctx, stationID, plan); err != nil {
		t.Fatalf("createHardItemEntry with no media_id: %v", err)
	}

	var count int64
	db.Model(&models.ScheduleEntry{}).Where("station_id = ?", stationID).Count(&count)
	if count != 0 {
		t.Errorf("expected 0 entries when media_id missing, got %d", count)
	}
}

// TestCreateHardItemEntry_Idempotent covers the count>0 early-return path
// (line 833-834) by inserting the same hard-item slot twice.
func TestCreateHardItemEntry_Idempotent(t *testing.T) {
	svc, db := newFullTestService(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	createTestMount(t, db, stationID, mountID)

	mediaID := uuid.NewString()
	if err := db.Create(&models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Idempotent Track",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	plan := clock.SlotPlan{
		SlotID:   "slot-idempotent-harditem",
		StartsAt: time.Now().UTC().Add(2 * time.Minute).Truncate(time.Second),
		EndsAt:   time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second),
		SlotType: string(models.SlotTypeHardItem),
		Payload:  map[string]any{"mount_id": mountID, "media_id": mediaID},
	}

	// First call — creates the entry.
	if err := svc.createHardItemEntry(ctx, stationID, plan); err != nil {
		t.Fatalf("first createHardItemEntry: %v", err)
	}
	// Second call — idempotent, should return nil without creating a duplicate.
	if err := svc.createHardItemEntry(ctx, stationID, plan); err != nil {
		t.Fatalf("second createHardItemEntry (idempotent): %v", err)
	}

	var count int64
	db.Model(&models.ScheduleEntry{}).Where("station_id = ?", stationID).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 entry after idempotent insert, got %d", count)
	}
}
