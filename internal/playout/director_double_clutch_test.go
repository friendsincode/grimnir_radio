/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
)

// TestTick_NoDoubleClutchOnEqualCreatedAtOverlap reproduces the on-air
// "double-clutch": two overlapping media instances on one mount whose created_at
// the overlap-winner check can't separate (they're equal) both reached
// handleEntry in a single tick, building the broadcast pipeline twice back-to-back
// (observed on mount rlmradioxyz / RLMradio.xyz-M). The tick must launch the mount
// exactly once per pass.
func TestTick_NoDoubleClutchOnEqualCreatedAtOverlap(t *testing.T) {
	now := time.Now().UTC()
	d, mgr := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "double-clutch-" + mountID[:8],
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	mediaA := uuid.NewString()
	mediaB := uuid.NewString()
	for _, m := range []models.MediaItem{
		{ID: mediaA, StationID: stationID, Title: "Track A", Path: "/tmp/a.mp3", Duration: 4 * time.Minute, AnalysisState: models.AnalysisComplete},
		{ID: mediaB, StationID: stationID, Title: "Track B", Path: "/tmp/b.mp3", Duration: 4 * time.Minute, AnalysisState: models.AnalysisComplete},
	} {
		if err := d.db.Create(&m).Error; err != nil {
			t.Fatalf("seed media: %v", err)
		}
	}

	// Two instances on the SAME mount, both covering now, with the SAME created_at
	// so the overlap-winner check (skips only a STRICTLY newer instance) can't
	// drop either. Before the fix, both launch -> two pipeline builds.
	created := now.Add(-1 * time.Hour)
	for _, e := range []models.ScheduleEntry{
		{ID: uuid.NewString(), StationID: stationID, MountID: mountID, SourceType: "media", SourceID: mediaA, IsInstance: true, StartsAt: now.Add(-2 * time.Minute), EndsAt: now.Add(5 * time.Minute), CreatedAt: created},
		{ID: uuid.NewString(), StationID: stationID, MountID: mountID, SourceType: "media", SourceID: mediaB, IsInstance: true, StartsAt: now.Add(-2 * time.Minute), EndsAt: now.Add(5 * time.Minute), CreatedAt: created},
	} {
		if err := d.db.Create(&e).Error; err != nil {
			t.Fatalf("seed entry: %v", err)
		}
	}

	d.markScheduleDirty()
	if err := d.tick(ctx); err != nil {
		t.Fatalf("tick returned error: %v", err)
	}

	if got := mgr.ensureCalls[mountID]; got != 1 {
		t.Errorf("pipeline builds for mount = %d, want exactly 1 (double-clutch)", got)
	}
	d.mu.Lock()
	_, active := d.active[mountID]
	d.mu.Unlock()
	if !active {
		t.Error("expected the mount to be active after tick")
	}
}

// TestTick_EndedEntryDoesNotBuildOverSuccessor reproduces the double-clutch that
// survived #280, taken from prod mount d4f41798 at the top of every hour.
//
// resolveEntryForNow keeps an entry resolvable for 2s past its own EndsAt. At a
// hard boundary that means an entry ending exactly now and its successor both
// resolve in the same pass. #280's per-tick guard deliberately does not let the
// ended entry claim the launch (so a valid successor can still preempt), but
// nothing stopped the ended entry running handleEntry FIRST and building a whole
// pipeline that the successor then immediately replaced. On air that was two
// builds 0.81s apart on one mount: a recurring "custom" template ending at
// 00:00:00 launching Behind The Woodshed, then the 00:00 instance rebuilding the
// same upstream.
//
// An entry that has already ended must not build when something still current
// covers the same mount.
func TestTick_EndedEntryDoesNotBuildOverSuccessor(t *testing.T) {
	now := time.Now().UTC()
	d, mgr := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	if err := d.db.Create(&models.Mount{
		ID: mountID, StationID: stationID, Name: "boundary-" + mountID[:8],
		Format: "mp3", Bitrate: 128, SampleRate: 44100, Channels: 2,
	}).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	outgoing := uuid.NewString()
	incoming := uuid.NewString()
	for _, m := range []models.MediaItem{
		{ID: outgoing, StationID: stationID, Title: "Outgoing", Path: "/tmp/out.mp3", Duration: 4 * time.Minute, AnalysisState: models.AnalysisComplete},
		{ID: incoming, StationID: stationID, Title: "Incoming", Path: "/tmp/in.mp3", Duration: 4 * time.Minute, AnalysisState: models.AnalysisComplete},
	} {
		if err := d.db.Create(&m).Error; err != nil {
			t.Fatalf("seed media: %v", err)
		}
	}

	// The outgoing entry ended 1s ago: still inside the 2s resolve grace, so it
	// resolves, but it is over. The incoming entry started at that same instant.
	boundary := now.Add(-1 * time.Second)
	outgoingEntry := models.ScheduleEntry{
		ID: uuid.NewString(), StationID: stationID, MountID: mountID,
		SourceType: "media", SourceID: outgoing, IsInstance: true,
		StartsAt: boundary.Add(-1 * time.Hour), EndsAt: boundary,
		CreatedAt: now.Add(-48 * time.Hour),
	}
	incomingEntry := models.ScheduleEntry{
		ID: uuid.NewString(), StationID: stationID, MountID: mountID,
		SourceType: "media", SourceID: incoming, IsInstance: true,
		StartsAt: boundary, EndsAt: boundary.Add(2 * time.Hour),
		CreatedAt: now.Add(-24 * time.Hour),
	}
	// Seed the ended entry first so it is the one the loop reaches first.
	for _, e := range []models.ScheduleEntry{outgoingEntry, incomingEntry} {
		if err := d.db.Create(&e).Error; err != nil {
			t.Fatalf("seed entry: %v", err)
		}
	}

	d.markScheduleDirty()
	if err := d.tick(ctx); err != nil {
		t.Fatalf("tick returned error: %v", err)
	}

	if got := mgr.ensureCalls[mountID]; got != 1 {
		t.Errorf("pipeline builds for mount = %d, want exactly 1; the ended entry built a throwaway pipeline over its successor", got)
	}

	// The mount must be left on the incoming entry, not the one that ended.
	d.mu.Lock()
	state, active := d.active[mountID]
	d.mu.Unlock()
	if !active {
		t.Fatal("expected the mount to be active after tick")
	}
	if state.EntryID != incomingEntry.ID {
		t.Errorf("mount is on entry %s, want the incoming entry %s", state.EntryID, incomingEntry.ID)
	}
}

// An ended entry inside the grace must still launch when nothing else covers the
// mount: the grace is dead-air cover, and the fix must not turn a boundary with
// no successor into silence.
func TestTick_EndedEntryStillLaunchesWithNoSuccessor(t *testing.T) {
	now := time.Now().UTC()
	d, mgr := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	if err := d.db.Create(&models.Mount{
		ID: mountID, StationID: stationID, Name: "grace-" + mountID[:8],
		Format: "mp3", Bitrate: 128, SampleRate: 44100, Channels: 2,
	}).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	mediaID := uuid.NewString()
	if err := d.db.Create(&models.MediaItem{
		ID: mediaID, StationID: stationID, Title: "Only", Path: "/tmp/only.mp3",
		Duration: 4 * time.Minute, AnalysisState: models.AnalysisComplete,
	}).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	boundary := now.Add(-1 * time.Second)
	if err := d.db.Create(&models.ScheduleEntry{
		ID: uuid.NewString(), StationID: stationID, MountID: mountID,
		SourceType: "media", SourceID: mediaID, IsInstance: true,
		StartsAt: boundary.Add(-1 * time.Hour), EndsAt: boundary,
		CreatedAt: now.Add(-48 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	d.markScheduleDirty()
	if err := d.tick(ctx); err != nil {
		t.Fatalf("tick returned error: %v", err)
	}

	if got := mgr.ensureCalls[mountID]; got != 1 {
		t.Errorf("pipeline builds = %d, want 1; the grace launch was lost and the mount goes dark", got)
	}
}

// TestTick_RecurringParentAndItsInstanceLaunchOnce reproduces the double-clutch
// still heard on 1.40.34, taken from prod mount f58c7e4a at 19:00.
//
// The scheduler materializes a weekly slot into a concrete instance, but the
// recurring parent keeps resolving for the same slot. Both then cover now on the
// same mount with the same source. Their entry IDs differ, so playbackKey
// differs and isPlayed never deduped them, and launchedThisTick only collapses
// entries inside a single pass — the first launch blocks ~1.35s in
// startWebstreamEntry's synchronous ICY fetch, so the second lands on a later
// tick and rebuilds the identical source.
//
// The instance is the concrete plan for today, so it must win and the parent
// must not build at all.
func TestTick_RecurringParentAndItsInstanceLaunchOnce(t *testing.T) {
	now := time.Now().UTC()
	d, mgr := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	if err := d.db.Create(&models.Mount{
		ID: mountID, StationID: stationID, Name: "dup-" + mountID[:8],
		Format: "mp3", Bitrate: 128, SampleRate: 44100, Channels: 2,
	}).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	mediaID := uuid.NewString()
	if err := d.db.Create(&models.MediaItem{
		ID: mediaID, StationID: stationID, Title: "Shared Source", Path: "/tmp/shared.mp3",
		Duration: 4 * time.Minute, AnalysisState: models.AnalysisComplete,
	}).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	slotStart := now.Add(-90 * time.Second)
	slotEnd := now.Add(2 * time.Hour)

	// The recurring parent: template dated months back, resolves to today's slot.
	parent := models.ScheduleEntry{
		ID: uuid.NewString(), StationID: stationID, MountID: mountID,
		SourceType: "media", SourceID: mediaID,
		IsInstance: false, RecurrenceType: models.RecurrenceDaily,
		StartsAt: slotStart.AddDate(0, -4, 0), EndsAt: slotEnd.AddDate(0, -4, 0),
		CreatedAt: now.AddDate(0, -4, 0),
	}
	// The materialized instance for today, same mount, same source, same window.
	instance := models.ScheduleEntry{
		ID: uuid.NewString(), StationID: stationID, MountID: mountID,
		SourceType: "media", SourceID: mediaID, IsInstance: true,
		StartsAt: slotStart, EndsAt: slotEnd,
		CreatedAt: now.AddDate(0, 0, -7),
	}
	// Parent first so the loop would reach it first without the winner check.
	for _, e := range []models.ScheduleEntry{parent, instance} {
		if err := d.db.Create(&e).Error; err != nil {
			t.Fatalf("seed entry: %v", err)
		}
	}

	d.markScheduleDirty()
	if err := d.tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	// A second tick must not rebuild either: this is where the on-air duplicate
	// actually landed, one tick after the first launch.
	if err := d.tick(ctx); err != nil {
		t.Fatalf("second tick: %v", err)
	}

	if got := mgr.ensureCalls[mountID]; got != 1 {
		t.Errorf("pipeline builds = %d, want exactly 1; the recurring parent and its own instance both built", got)
	}

	d.mu.Lock()
	state, active := d.active[mountID]
	d.mu.Unlock()
	if !active {
		t.Fatal("mount not active after tick")
	}
	if state.EntryID != instance.ID {
		t.Errorf("mount is on entry %s, want the materialized instance %s", state.EntryID, instance.ID)
	}
}
