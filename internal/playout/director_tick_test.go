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

// ── tick with schedule entries ─────────────────────────────────────────────

func TestTick_WithActiveMediaEntry_CallsHandleEntry(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Tick Track",
		Path:          "/tmp/tick.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	now := time.Now().UTC()
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "media",
		SourceID:   mediaID,
		StartsAt:   now.Add(-1 * time.Second),
		EndsAt:     now.Add(5 * time.Minute),
	}
	if err := d.db.Create(&entry).Error; err != nil {
		t.Fatalf("seed schedule entry: %v", err)
	}

	d.scheduleCache.dirty = true

	if err := d.tick(ctx); err != nil {
		t.Errorf("tick with media entry returned error: %v", err)
	}

	// After tick, the entry should be in active state.
	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after tick with media entry")
	}
	if state.MediaID != mediaID {
		t.Errorf("MediaID = %q, want %q", state.MediaID, mediaID)
	}
}

func TestTick_AlreadyPlayedEntry_SkipsIt(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Already Played",
		Path:          "/tmp/played.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	now := time.Now().UTC()
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "media",
		SourceID:   mediaID,
		StartsAt:   now.Add(-1 * time.Second),
		EndsAt:     now.Add(5 * time.Minute),
	}
	if err := d.db.Create(&entry).Error; err != nil {
		t.Fatalf("seed schedule entry: %v", err)
	}

	d.scheduleCache.dirty = true

	// First tick: plays the entry.
	if err := d.tick(ctx); err != nil {
		t.Errorf("first tick returned error: %v", err)
	}

	// Mark the entry as not active so we can detect if second tick re-plays it.
	d.mu.Lock()
	delete(d.active, mountID)
	d.mu.Unlock()

	// Second tick: entry already played, should skip.
	if err := d.tick(ctx); err != nil {
		t.Errorf("second tick returned error: %v", err)
	}

	d.mu.Lock()
	_, active := d.active[mountID]
	d.mu.Unlock()
	if active {
		t.Error("expected entry to be skipped on second tick (already played)")
	}
}

// ── scheduleStop ─────────────────────────────────────────────────────────

func TestScheduleStop_PastEndTime_StopsImmediately(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	mountID := uuid.NewString()
	stationID := uuid.NewString()
	entryID := uuid.NewString()

	// Set up an active state.
	endsAt := time.Now().UTC().Add(-1 * time.Second) // already ended
	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:   entryID,
		StationID: stationID,
		Ends:      endsAt,
	}
	d.mu.Unlock()

	d.scheduleStop(ctx, stationID, mountID, endsAt)

	// Wait for the goroutine to fire (delay is max 200ms after stopAt).
	// Since stopAt is in the past, delay=0, so 300ms should be enough.
	time.Sleep(400 * time.Millisecond)

	d.mu.Lock()
	_, stillActive := d.active[mountID]
	d.mu.Unlock()
	if stillActive {
		t.Error("expected active state to be cleared by scheduleStop")
	}
}

func TestScheduleStop_ActiveEntryChanged_DoesNotStop(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	mountID := uuid.NewString()
	stationID := uuid.NewString()

	// Set state with a future end time (well past the original endsAt).
	futureEnd := time.Now().UTC().Add(10 * time.Minute)
	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:   uuid.NewString(),
		StationID: stationID,
		Ends:      futureEnd,
	}
	d.mu.Unlock()

	// Schedule stop for a past time — but active state has a far-future Ends.
	// The goroutine should detect state.Ends.After(expected+500ms) and bail.
	pastEnd := time.Now().UTC().Add(-2 * time.Second)
	d.scheduleStop(ctx, stationID, mountID, pastEnd)

	time.Sleep(400 * time.Millisecond)

	d.mu.Lock()
	_, stillActive := d.active[mountID]
	d.mu.Unlock()
	if !stillActive {
		t.Error("expected active state to remain when entry has been superseded")
	}
}

// ── getScheduleSnapshot: instance-suppresses-parent ──────────────────────

// TestGetScheduleSnapshot_InstanceSuppressesRecurringParent verifies that when
// pre-materialized media instances are active for a mount, the recurring
// smart_block parent for that mount is excluded from the snapshot.  Without
// this filter the director would start a live-generated sequence that a
// subsequent tick immediately overrides with the first instance, causing a
// brief wrong-track flash.
func TestGetScheduleSnapshot_InstanceSuppressesRecurringParent(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()
	blockID := uuid.NewString()

	// Seed a media item so the instance entry is valid.
	if err := d.db.Create(&models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Jingle",
		Path:          "/tmp/jingle.mp3",
		Duration:      45 * time.Second,
		AnalysisState: models.AnalysisComplete,
	}).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	now := time.Now().UTC()

	// Recurring smart_block parent (first occurrence in the past, recurring daily).
	parent := models.ScheduleEntry{
		ID:             uuid.NewString(),
		StationID:      stationID,
		MountID:        mountID,
		SourceType:     "smart_block",
		SourceID:       blockID,
		StartsAt:       now.Add(-7 * 24 * time.Hour).Truncate(time.Minute), // first occurrence a week ago
		EndsAt:         now.Add(-7 * 24 * time.Hour).Truncate(time.Minute).Add(2 * time.Hour),
		RecurrenceType: models.RecurrenceDaily,
		IsInstance:     false,
	}
	if err := d.db.Create(&parent).Error; err != nil {
		t.Fatalf("seed recurring parent: %v", err)
	}

	// Pre-materialized media instance currently active on the same mount.
	instance := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "media",
		SourceID:   mediaID,
		StartsAt:   now.Add(-10 * time.Second), // started 10s ago
		EndsAt:     now.Add(35 * time.Second),  // ends in 35s
		IsInstance: true,
	}
	if err := d.db.Create(&instance).Error; err != nil {
		t.Fatalf("seed instance: %v", err)
	}

	d.scheduleCache.dirty = true
	entries, err := d.getScheduleSnapshot(ctx, now)
	if err != nil {
		t.Fatalf("getScheduleSnapshot: %v", err)
	}

	for _, e := range entries {
		if e.ID == parent.ID {
			t.Errorf("recurring smart_block parent should be suppressed when instances are active, but it appeared in snapshot")
		}
	}

	found := false
	for _, e := range entries {
		if e.ID == instance.ID {
			found = true
		}
	}
	if !found {
		t.Error("pre-materialized instance should still appear in snapshot")
	}
}

// TestGetScheduleSnapshot_ParentKeptWhenNoInstances verifies that the recurring
// smart_block parent is NOT suppressed when no pre-materialized instances are
// active for the mount (fall-through to live generation).
func TestGetScheduleSnapshot_ParentKeptWhenNoInstances(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	blockID := uuid.NewString()

	now := time.Now().UTC()

	parent := models.ScheduleEntry{
		ID:             uuid.NewString(),
		StationID:      stationID,
		MountID:        mountID,
		SourceType:     "smart_block",
		SourceID:       blockID,
		StartsAt:       now.Add(-7 * 24 * time.Hour).Truncate(time.Minute),
		EndsAt:         now.Add(-7 * 24 * time.Hour).Truncate(time.Minute).Add(2 * time.Hour),
		RecurrenceType: models.RecurrenceDaily,
		IsInstance:     false,
	}
	if err := d.db.Create(&parent).Error; err != nil {
		t.Fatalf("seed recurring parent: %v", err)
	}

	d.scheduleCache.dirty = true
	entries, err := d.getScheduleSnapshot(ctx, now)
	if err != nil {
		t.Fatalf("getScheduleSnapshot: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.ID == parent.ID {
			found = true
		}
	}
	if !found {
		t.Error("recurring parent should be kept in snapshot when no instances exist")
	}
}

// ── playNextFromState ─────────────────────────────────────────────────────

func TestPlayNextFromState_WithMountAndMedia_SetsActiveStateAtPosition(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID1 := uuid.NewString()
	mediaID2 := uuid.NewString()
	entryID := uuid.NewString()

	for _, id := range []string{mediaID1, mediaID2} {
		m := models.MediaItem{
			ID:            id,
			StationID:     stationID,
			Title:         "Track " + id[:8],
			Path:          "/tmp/" + id + ".mp3",
			Duration:      3 * time.Minute,
			AnalysisState: models.AnalysisComplete,
		}
		if err := d.db.Create(&m).Error; err != nil {
			t.Fatalf("seed media: %v", err)
		}
	}

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "nextfromstate",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	d.broadcast.CreateMount("nextfromstate", "audio/mpeg", 128)
	d.broadcast.CreateMount("nextfromstate-lq", "audio/mpeg", 64)

	entry := models.ScheduleEntry{
		ID:        entryID,
		StationID: stationID,
		MountID:   mountID,
		StartsAt:  time.Now().UTC().Add(-1 * time.Second),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
	}

	state := playoutState{
		EntryID:    entryID,
		StationID:  stationID,
		SourceType: "playlist",
		SourceID:   uuid.NewString(),
		Position:   0,
		TotalItems: 2,
		Items:      []string{mediaID1, mediaID2},
		Ends:       entry.EndsAt,
	}

	d.playNextFromState(entry, state, 1, "nextfromstate")

	d.mu.Lock()
	active, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after playNextFromState")
	}
	if active.MediaID != mediaID2 {
		t.Errorf("MediaID = %q, want %q", active.MediaID, mediaID2)
	}
	if active.Position != 1 {
		t.Errorf("Position = %d, want 1", active.Position)
	}
}

func TestTick_HardBoundaryPreemption_ClearsOldActiveBeforeStartingNew(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{}, &models.Playlist{}, &models.PlaylistItem{}, &models.SmartBlock{})

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "hb-preempt-" + mountID[:8],
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	media1 := models.MediaItem{
		ID:            uuid.NewString(),
		StationID:     stationID,
		Path:          "/tmp/m1.mp3",
		Duration:      5 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	media2 := models.MediaItem{
		ID:            uuid.NewString(),
		StationID:     stationID,
		Path:          "/tmp/m2.mp3",
		Duration:      5 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media1).Error; err != nil {
		t.Fatalf("seed media1: %v", err)
	}
	if err := d.db.Create(&media2).Error; err != nil {
		t.Fatalf("seed media2: %v", err)
	}

	now := time.Now().UTC()
	entry1 := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "media",
		SourceID:   media1.ID,
		StartsAt:   now.Add(-10 * time.Minute),
		EndsAt:     now.Add(-1 * time.Second), // PAST its end
	}
	entry2 := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "media",
		SourceID:   media2.ID,
		StartsAt:   now.Add(-1 * time.Second),
		EndsAt:     now.Add(5 * time.Minute),
	}
	if err := d.db.Create(&entry1).Error; err != nil {
		t.Fatalf("seed entry1: %v", err)
	}
	if err := d.db.Create(&entry2).Error; err != nil {
		t.Fatalf("seed entry2: %v", err)
	}

	// Prime d.active with entry1 still listed as active, but its Ends is in the past.
	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    entry1.ID,
		StationID:  stationID,
		MediaID:    media1.ID,
		Ends:       entry1.EndsAt,
		SourceType: "media",
	}
	d.mu.Unlock()

	d.broadcast.CreateMount(mount.Name, "audio/mpeg", 128)
	d.broadcast.CreateMount(mount.Name+"-lq", "audio/mpeg", 64)

	if err := d.tick(context.Background()); err != nil {
		t.Errorf("tick returned error: %v", err)
	}

	d.mu.Lock()
	activeState, ok := d.active[mountID]
	d.mu.Unlock()

	if !ok {
		t.Fatal("expected active state on mount after tick with preempting entry")
	}
	if activeState.EntryID != entry2.ID {
		t.Errorf("EntryID = %q, want %q (new entry should own mount after preemption)", activeState.EntryID, entry2.ID)
	}
}

// TestDirector_HardCutOnEntryEnds reproduces issue #227: a media/smart_block
// state whose Ends time has elapsed must be hard-cut even when no new schedule
// entry has arrived to displace it. The pre-fix director only preempted when
// a DIFFERENT entry.ID was due, so a 5h file inside a 2h smart-block slot
// (constant entry.ID) would play to its natural file end, eating subsequent
// slots.
func TestDirector_HardCutOnEntryEnds(t *testing.T) {
	d, mgr := newMockDirector(t, &models.ScheduleEntry{}, &models.Playlist{}, &models.PlaylistItem{}, &models.SmartBlock{})

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	entryID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "hc-overrun-" + mountID[:8],
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Path:          "/tmp/5h-file.mp3",
		Duration:      5 * time.Hour,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	now := time.Now().UTC()
	// The entry's slot ended 5 minutes ago, but no successor entry is queued.
	// Pre-fix: tick sees no new entry and never preempts; the 5h file plays on.
	// Post-fix: the hard-cut sweep stops the pipeline and clears active state.
	entry := models.ScheduleEntry{
		ID:         entryID,
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "smart_block",
		SourceID:   uuid.NewString(),
		StartsAt:   now.Add(-2 * time.Hour),
		EndsAt:     now.Add(-5 * time.Minute),
	}
	if err := d.db.Create(&entry).Error; err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	// Prime an active pipeline for this entry; Ends is 5 minutes in the past.
	// TrackEndsAt is far in the future (matching the 5h file's natural end) to
	// prove that the hard-cut path fires off state.Ends rather than the stuck-track
	// watchdog at TrackEndsAt.
	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:     entryID,
		StationID:   stationID,
		MediaID:     mediaID,
		Started:     now.Add(-2 * time.Hour),
		Ends:        entry.EndsAt,
		SourceType:  "smart_block",
		TrackEndsAt: now.Add(3 * time.Hour),
		MountName:   mount.Name,
	}
	d.mu.Unlock()

	d.broadcast.CreateMount(mount.Name, "audio/mpeg", 128)
	d.broadcast.CreateMount(mount.Name+"-lq", "audio/mpeg", 64)
	// Seed a pipeline so we can observe its removal by StopPipeline.
	if err := mgr.EnsurePipeline(context.Background(), mountID, "fakesrc ! fakesink"); err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}
	if mgr.pipelines[mountID] == nil {
		t.Fatalf("precondition: expected pipeline registered for mount")
	}

	if err := d.tick(context.Background()); err != nil {
		t.Errorf("tick returned error: %v", err)
	}

	// After tick: pipeline should be stopped and active state cleared so that
	// dead-air recovery or the next entry can take over cleanly.
	if _, stillRunning := mgr.pipelines[mountID]; stillRunning {
		t.Errorf("expected pipeline stopped after overrun; still in pipelines map")
	}
	d.mu.Lock()
	state, stillActive := d.active[mountID]
	d.mu.Unlock()
	if stillActive {
		t.Errorf("expected active state cleared after hard-cut; got %+v", state)
	}
}

func TestCheckDeadAir_RestartsIdleMount(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "dead-air-" + mountID[:8],
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	media := models.MediaItem{
		ID:            uuid.NewString(),
		StationID:     stationID,
		Path:          "/tmp/da.mp3",
		Duration:      5 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	now := time.Now().UTC()
	// Entry started 20s ago — past 15s grace period, still within its window.
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "media",
		SourceID:   media.ID,
		StartsAt:   now.Add(-20 * time.Second),
		EndsAt:     now.Add(5 * time.Minute),
	}
	if err := d.db.Create(&entry).Error; err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	d.broadcast.CreateMount(mount.Name, "audio/mpeg", 128)
	d.broadcast.CreateMount(mount.Name+"-lq", "audio/mpeg", 64)

	// d.active is empty — no pipeline running. Call checkDeadAir directly so we
	// isolate watchdog behavior from the normal tick dispatch path.
	d.checkDeadAir(context.Background(), now)

	d.mu.Lock()
	_, active := d.active[mountID]
	d.mu.Unlock()
	if !active {
		t.Error("expected dead air watchdog to restart the mount")
	}
}

func TestCheckDeadAir_RespectsGracePeriod(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "grace-" + mountID[:8],
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	media := models.MediaItem{
		ID:            uuid.NewString(),
		StationID:     stationID,
		Path:          "/tmp/grace.mp3",
		Duration:      5 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	now := time.Now().UTC()
	// Entry started only 5s ago — within 15s grace period.
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "media",
		SourceID:   media.ID,
		StartsAt:   now.Add(-5 * time.Second),
		EndsAt:     now.Add(5 * time.Minute),
	}
	if err := d.db.Create(&entry).Error; err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	// Call checkDeadAir directly to isolate watchdog from normal tick dispatch.
	d.checkDeadAir(context.Background(), now)

	d.mu.Lock()
	_, active := d.active[mountID]
	d.mu.Unlock()
	if active {
		t.Error("expected watchdog NOT to trigger during grace period")
	}
}
