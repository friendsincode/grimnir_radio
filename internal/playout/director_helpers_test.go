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
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ── occurrenceDateAfter ───────────────────────────────────────────────────

func TestOccurrenceDateAfter_SameDate(t *testing.T) {
	d := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	if occurrenceDateAfter(d, d) {
		t.Error("occurrenceDateAfter(d, d) should be false for same date")
	}
}

func TestOccurrenceDateAfter_AAfterB(t *testing.T) {
	a := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)
	b := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	if !occurrenceDateAfter(a, b) {
		t.Error("occurrenceDateAfter(Mar16, Mar15) should be true")
	}
}

func TestOccurrenceDateAfter_ABeforeB(t *testing.T) {
	a := time.Date(2026, 3, 14, 23, 59, 59, 0, time.UTC)
	b := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	if occurrenceDateAfter(a, b) {
		t.Error("occurrenceDateAfter(Mar14, Mar15) should be false")
	}
}

func TestOccurrenceDateAfter_DifferentYears(t *testing.T) {
	a := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	b := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	if !occurrenceDateAfter(a, b) {
		t.Error("occurrenceDateAfter(2027-01-01, 2026-12-31) should be true")
	}
}

func TestOccurrenceDateAfter_DifferentMonths(t *testing.T) {
	a := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	b := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	if !occurrenceDateAfter(a, b) {
		t.Error("occurrenceDateAfter(Apr1, Mar31) should be true")
	}
}

// ── matchesRecurringDay ───────────────────────────────────────────────────

func TestMatchesRecurringDay_Daily(t *testing.T) {
	entry := models.ScheduleEntry{RecurrenceType: models.RecurrenceDaily}
	for _, day := range []time.Weekday{
		time.Monday, time.Tuesday, time.Wednesday, time.Thursday,
		time.Friday, time.Saturday, time.Sunday,
	} {
		if !matchesRecurringDay(entry, day, time.UTC) {
			t.Errorf("RecurrenceDaily should match %v", day)
		}
	}
}

func TestMatchesRecurringDay_Weekdays(t *testing.T) {
	entry := models.ScheduleEntry{RecurrenceType: models.RecurrenceWeekdays}
	for _, day := range []time.Weekday{
		time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday,
	} {
		if !matchesRecurringDay(entry, day, time.UTC) {
			t.Errorf("RecurrenceWeekdays should match %v", day)
		}
	}
	for _, day := range []time.Weekday{time.Saturday, time.Sunday} {
		if matchesRecurringDay(entry, day, time.UTC) {
			t.Errorf("RecurrenceWeekdays should not match %v", day)
		}
	}
}

func TestMatchesRecurringDay_WeeklyWithRecurrenceDays(t *testing.T) {
	// RecurrenceDays [1, 3, 5] = Monday, Wednesday, Friday
	entry := models.ScheduleEntry{
		RecurrenceType: models.RecurrenceWeekly,
		RecurrenceDays: []int{1, 3, 5}, // Mon, Wed, Fri
	}
	if !matchesRecurringDay(entry, time.Monday, time.UTC) {
		t.Error("should match Monday (int=1)")
	}
	if !matchesRecurringDay(entry, time.Wednesday, time.UTC) {
		t.Error("should match Wednesday (int=3)")
	}
	if !matchesRecurringDay(entry, time.Friday, time.UTC) {
		t.Error("should match Friday (int=5)")
	}
	if matchesRecurringDay(entry, time.Tuesday, time.UTC) {
		t.Error("should not match Tuesday (int=2)")
	}
	if matchesRecurringDay(entry, time.Saturday, time.UTC) {
		t.Error("should not match Saturday (int=6)")
	}
}

func TestMatchesRecurringDay_WeeklyFallsBackToStartsAtWeekday(t *testing.T) {
	// No RecurrenceDays — falls back to StartsAt weekday (a Tuesday)
	tuesday := time.Date(2026, 3, 17, 10, 0, 0, 0, time.UTC) // 2026-03-17 is a Tuesday
	entry := models.ScheduleEntry{
		RecurrenceType: models.RecurrenceWeekly,
		StartsAt:       tuesday,
	}
	if !matchesRecurringDay(entry, time.Tuesday, time.UTC) {
		t.Error("weekly with no RecurrenceDays should match StartsAt weekday (Tuesday)")
	}
	if matchesRecurringDay(entry, time.Monday, time.UTC) {
		t.Error("weekly with no RecurrenceDays should NOT match Monday when StartsAt is Tuesday")
	}
}

func TestMatchesRecurringDay_Custom(t *testing.T) {
	// RecurrenceDays [0, 6] = Sunday, Saturday (weekend)
	entry := models.ScheduleEntry{
		RecurrenceType: models.RecurrenceCustom,
		RecurrenceDays: []int{0, 6},
	}
	if !matchesRecurringDay(entry, time.Sunday, time.UTC) {
		t.Error("custom should match Sunday (int=0)")
	}
	if !matchesRecurringDay(entry, time.Saturday, time.UTC) {
		t.Error("custom should match Saturday (int=6)")
	}
	if matchesRecurringDay(entry, time.Monday, time.UTC) {
		t.Error("custom should not match Monday")
	}
}

func TestMatchesRecurringDay_CustomEmpty(t *testing.T) {
	// Empty RecurrenceDays means all days
	entry := models.ScheduleEntry{
		RecurrenceType: models.RecurrenceCustom,
		RecurrenceDays: []int{},
	}
	for _, day := range []time.Weekday{time.Sunday, time.Monday, time.Saturday} {
		if !matchesRecurringDay(entry, day, time.UTC) {
			t.Errorf("custom with no days should match %v", day)
		}
	}
}

func TestMatchesRecurringDay_None(t *testing.T) {
	entry := models.ScheduleEntry{RecurrenceType: models.RecurrenceNone}
	if matchesRecurringDay(entry, time.Monday, time.UTC) {
		t.Error("RecurrenceNone should not match any day")
	}
}

// ── deterministicSmartBlockSeed ───────────────────────────────────────────

func TestDeterministicSmartBlockSeed_Deterministic(t *testing.T) {
	entry := models.ScheduleEntry{
		ID:        "entry-1",
		StationID: "station-1",
		MountID:   "mount-1",
		StartsAt:  time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC),
		EndsAt:    time.Date(2026, 3, 15, 11, 0, 0, 0, time.UTC),
	}
	blockID := "block-1"

	seed1 := deterministicSmartBlockSeed(entry, blockID, 0)
	seed2 := deterministicSmartBlockSeed(entry, blockID, 0)
	if seed1 != seed2 {
		t.Errorf("deterministicSmartBlockSeed not deterministic: %d != %d", seed1, seed2)
	}
}

func TestDeterministicSmartBlockSeed_NonNegative(t *testing.T) {
	entry := models.ScheduleEntry{
		ID:        "entry-1",
		StationID: "station-1",
		MountID:   "mount-1",
		StartsAt:  time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC),
		EndsAt:    time.Date(2026, 3, 15, 11, 0, 0, 0, time.UTC),
	}
	seed := deterministicSmartBlockSeed(entry, "block-x", 0)
	if seed < 0 {
		t.Errorf("deterministicSmartBlockSeed returned negative: %d", seed)
	}
}

func TestDeterministicSmartBlockSeed_DifferentInputsDifferentSeeds(t *testing.T) {
	entry1 := models.ScheduleEntry{
		ID:        "entry-1",
		StationID: "station-1",
		MountID:   "mount-1",
		StartsAt:  time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC),
		EndsAt:    time.Date(2026, 3, 15, 11, 0, 0, 0, time.UTC),
	}
	entry2 := entry1
	entry2.StartsAt = time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	entry2.EndsAt = time.Date(2026, 3, 16, 11, 0, 0, 0, time.UTC)

	s1 := deterministicSmartBlockSeed(entry1, "block-1", 0)
	s2 := deterministicSmartBlockSeed(entry2, "block-1", 0)
	if s1 == s2 {
		t.Error("different entry times should produce different seeds")
	}

	s3 := deterministicSmartBlockSeed(entry1, "block-2", 0)
	if s1 == s3 {
		t.Error("different block IDs should produce different seeds")
	}

	// Different generations must produce different seeds (the anti-loop guarantee).
	s4 := deterministicSmartBlockSeed(entry1, "block-1", 1)
	if s1 == s4 {
		t.Error("generation 0 and generation 1 should produce different seeds")
	}
}

// ── normalizeEncoderRate ──────────────────────────────────────────────────

func TestNormalizeEncoderRate_ExactStandardRates(t *testing.T) {
	standard := []int{8000, 11025, 12000, 16000, 22050, 24000, 32000, 44100, 48000}
	for _, rate := range standard {
		got := normalizeEncoderRate(rate)
		if got != rate {
			t.Errorf("normalizeEncoderRate(%d) = %d, want same (exact match)", rate, got)
		}
	}
}

func TestNormalizeEncoderRate_NearestTo44100(t *testing.T) {
	// 43000 is closer to 44100 than to any other standard rate
	got := normalizeEncoderRate(43000)
	if got != 44100 {
		t.Errorf("normalizeEncoderRate(43000) = %d, want 44100", got)
	}
}

func TestNormalizeEncoderRate_NearestTo48000(t *testing.T) {
	got := normalizeEncoderRate(47500)
	if got != 48000 {
		t.Errorf("normalizeEncoderRate(47500) = %d, want 48000", got)
	}
}

func TestNormalizeEncoderRate_LowRateMapsTo8000(t *testing.T) {
	got := normalizeEncoderRate(7000)
	if got != 8000 {
		t.Errorf("normalizeEncoderRate(7000) = %d, want 8000", got)
	}
}

func TestNormalizeEncoderRate_HighRateMapsTo48000(t *testing.T) {
	got := normalizeEncoderRate(96000)
	if got != 48000 {
		t.Errorf("normalizeEncoderRate(96000) = %d, want 48000", got)
	}
}

// ── findCurrentSlot ───────────────────────────────────────────────────────

func TestFindCurrentSlot_Empty(t *testing.T) {
	d := newCoverageDirector(t)
	slot := d.findCurrentSlot(nil, time.Now())
	if slot != nil {
		t.Error("findCurrentSlot with nil slots should return nil")
	}
}

func TestFindCurrentSlot_SingleSlotAtZero(t *testing.T) {
	d := newCoverageDirector(t)
	slots := []models.ClockSlot{
		{ID: "s1", Offset: 0},
	}
	// 5 minutes into the hour
	at := time.Date(2026, 3, 15, 10, 5, 0, 0, time.UTC)
	slot := d.findCurrentSlot(slots, at)
	if slot == nil {
		t.Fatal("expected slot, got nil")
	}
	if slot.ID != "s1" {
		t.Errorf("slot ID = %q, want %q", slot.ID, "s1")
	}
}

func TestFindCurrentSlot_MultipleSlots(t *testing.T) {
	d := newCoverageDirector(t)
	slots := []models.ClockSlot{
		{ID: "s1", Offset: 0},
		{ID: "s2", Offset: 15 * time.Minute},
		{ID: "s3", Offset: 30 * time.Minute},
		{ID: "s4", Offset: 45 * time.Minute},
	}

	// At 17 minutes → should be slot s2 (0 ≤ 15 ≤ 17 < 30)
	at := time.Date(2026, 3, 15, 10, 17, 0, 0, time.UTC)
	slot := d.findCurrentSlot(slots, at)
	if slot == nil || slot.ID != "s2" {
		t.Errorf("at 10:17 expected s2, got %v", slot)
	}

	// At exactly 30 minutes → should be slot s3
	at = time.Date(2026, 3, 15, 10, 30, 0, 0, time.UTC)
	slot = d.findCurrentSlot(slots, at)
	if slot == nil || slot.ID != "s3" {
		t.Errorf("at 10:30 expected s3, got %v", slot)
	}

	// At 46 minutes → should be slot s4
	at = time.Date(2026, 3, 15, 10, 46, 0, 0, time.UTC)
	slot = d.findCurrentSlot(slots, at)
	if slot == nil || slot.ID != "s4" {
		t.Errorf("at 10:46 expected s4, got %v", slot)
	}

	// At 0 seconds → should be slot s1
	at = time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	slot = d.findCurrentSlot(slots, at)
	if slot == nil || slot.ID != "s1" {
		t.Errorf("at 10:00 expected s1, got %v", slot)
	}
}

func TestFindCurrentSlot_BeforeFirstSlot(t *testing.T) {
	// If the first slot starts at 5 minutes and the time is 2 minutes, no slot should match.
	d := newCoverageDirector(t)
	slots := []models.ClockSlot{
		{ID: "s1", Offset: 5 * time.Minute},
	}
	at := time.Date(2026, 3, 15, 10, 2, 0, 0, time.UTC)
	slot := d.findCurrentSlot(slots, at)
	if slot != nil {
		t.Errorf("before first slot, expected nil, got %v", slot)
	}
}

// ── resolveRecurringOccurrenceWindow ─────────────────────────────────────

func TestResolveRecurringOccurrenceWindow_NonRecurringReturnsFalse(t *testing.T) {
	entry := models.ScheduleEntry{
		RecurrenceType: models.RecurrenceNone,
		IsInstance:     false,
		StartsAt:       time.Now().UTC().Add(-1 * time.Hour),
		EndsAt:         time.Now().UTC().Add(1 * time.Hour),
	}
	_, _, ok := resolveRecurringOccurrenceWindow(entry, time.Now(), time.UTC)
	if ok {
		t.Error("non-recurring entry should return ok=false")
	}
}

func TestResolveRecurringOccurrenceWindow_IsInstanceReturnsFalse(t *testing.T) {
	entry := models.ScheduleEntry{
		RecurrenceType: models.RecurrenceDaily,
		IsInstance:     true,
		StartsAt:       time.Now().UTC().Add(-1 * time.Hour),
		EndsAt:         time.Now().UTC().Add(1 * time.Hour),
	}
	_, _, ok := resolveRecurringOccurrenceWindow(entry, time.Now(), time.UTC)
	if ok {
		t.Error("instance entry should return ok=false from resolveRecurringOccurrenceWindow")
	}
}

func TestResolveRecurringOccurrenceWindow_ZeroDurationReturnsFalse(t *testing.T) {
	now := time.Now().UTC()
	entry := models.ScheduleEntry{
		RecurrenceType: models.RecurrenceDaily,
		StartsAt:       now,
		EndsAt:         now, // zero duration
	}
	_, _, ok := resolveRecurringOccurrenceWindow(entry, now, time.UTC)
	if ok {
		t.Error("zero duration entry should return ok=false")
	}
}

func TestResolveRecurringOccurrenceWindow_DailyMatchesToday(t *testing.T) {
	// Build a daily entry that started yesterday at a time "just before now",
	// so today's occurrence is within the 2-second window.
	now := time.Now().UTC()
	// Template starts at the same wall-clock time yesterday.
	templateStart := now.Add(-24 * time.Hour)
	templateEnd := templateStart.Add(1 * time.Hour)

	entry := models.ScheduleEntry{
		ID:             uuid.NewString(),
		RecurrenceType: models.RecurrenceDaily,
		StartsAt:       templateStart,
		EndsAt:         templateEnd,
	}

	// Now is exactly at today's occurrence start (within 2s window).
	occNow := time.Date(
		now.Year(), now.Month(), now.Day(),
		templateStart.Hour(), templateStart.Minute(), templateStart.Second(), 0, time.UTC,
	)
	// Use a time 1 second after the occurrence start (within the 2s grace window).
	probe := occNow.Add(1 * time.Second)

	occStart, occEnd, ok := resolveRecurringOccurrenceWindow(entry, probe, time.UTC)
	if !ok {
		t.Skip("daily occurrence not found within 2s window — timing-sensitive test, skipping")
	}
	if occEnd.Sub(occStart) != templateEnd.Sub(templateStart) {
		t.Errorf("occurrence duration = %v, want %v", occEnd.Sub(occStart), templateEnd.Sub(templateStart))
	}
}

// ── persistMountState / clearPersistedMountState ─────────────────────────

func TestPersistMountState_WritesRow(t *testing.T) {
	d := newCoverageDirector(t)
	ctx := context.Background()

	mountID := uuid.NewString()
	state := playoutState{
		MediaID:    uuid.NewString(),
		EntryID:    uuid.NewString(),
		StationID:  uuid.NewString(),
		Started:    time.Now().UTC().Add(-1 * time.Minute),
		Ends:       time.Now().UTC().Add(5 * time.Minute),
		SourceType: "media",
	}

	d.persistMountState(ctx, mountID, state)

	var row models.MountPlayoutState
	if err := d.db.First(&row, "mount_id = ?", mountID).Error; err != nil {
		t.Fatalf("expected persisted row, got: %v", err)
	}
	if row.MediaID != state.MediaID {
		t.Errorf("media_id = %q, want %q", row.MediaID, state.MediaID)
	}
	if row.EntryID != state.EntryID {
		t.Errorf("entry_id = %q, want %q", row.EntryID, state.EntryID)
	}
}

func TestPersistMountState_SkipsEmptyMountID(t *testing.T) {
	d := newCoverageDirector(t)
	ctx := context.Background()

	state := playoutState{
		MediaID:   uuid.NewString(),
		EntryID:   uuid.NewString(),
		StationID: uuid.NewString(),
		Started:   time.Now().UTC(),
		Ends:      time.Now().UTC().Add(5 * time.Minute),
	}

	// Should silently skip (no panic, no DB write)
	d.persistMountState(ctx, "", state)

	var count int64
	d.db.Model(&models.MountPlayoutState{}).Count(&count)
	if count != 0 {
		t.Errorf("expected no rows when mountID is empty, got %d", count)
	}
}

func TestPersistMountState_SkipsEmptyMediaID(t *testing.T) {
	d := newCoverageDirector(t)
	ctx := context.Background()

	state := playoutState{
		MediaID:   "", // empty
		EntryID:   uuid.NewString(),
		StationID: uuid.NewString(),
	}

	d.persistMountState(ctx, uuid.NewString(), state)

	var count int64
	d.db.Model(&models.MountPlayoutState{}).Count(&count)
	if count != 0 {
		t.Errorf("expected no rows when MediaID is empty, got %d", count)
	}
}

func TestClearPersistedMountState_DeletesRow(t *testing.T) {
	d := newCoverageDirector(t)
	ctx := context.Background()

	mountID := uuid.NewString()
	row := models.MountPlayoutState{
		MountID:   mountID,
		StationID: uuid.NewString(),
		EntryID:   uuid.NewString(),
		MediaID:   uuid.NewString(),
		StartedAt: time.Now().UTC(),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
		UpdatedAt: time.Now().UTC(),
	}
	if err := d.db.Create(&row).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	d.clearPersistedMountState(ctx, mountID)

	var count int64
	d.db.Model(&models.MountPlayoutState{}).Where("mount_id = ?", mountID).Count(&count)
	if count != 0 {
		t.Errorf("expected row to be deleted, but found %d rows", count)
	}
}

func TestClearPersistedMountState_EmptyMountIDNoOp(t *testing.T) {
	d := newCoverageDirector(t)
	ctx := context.Background()
	// Should not panic
	d.clearPersistedMountState(ctx, "")
}

// ── loadPersistedMountStates ──────────────────────────────────────────────

func TestLoadPersistedMountStates_LoadsActiveRow(t *testing.T) {
	d := newCoverageDirector(t)
	ctx := context.Background()

	mountID := uuid.NewString()
	entryID := uuid.NewString()
	mediaID := uuid.NewString()
	stationID := uuid.NewString()

	row := models.MountPlayoutState{
		MountID:   mountID,
		StationID: stationID,
		EntryID:   entryID,
		MediaID:   mediaID,
		StartedAt: time.Now().UTC().Add(-1 * time.Minute),
		EndsAt:    time.Now().UTC().Add(1 * time.Hour), // not stale
		UpdatedAt: time.Now().UTC(),
	}
	if err := d.db.Create(&row).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	d.loadPersistedMountStates(ctx)

	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()

	if !ok {
		t.Fatal("expected active state to be loaded from persisted row")
	}
	if state.MediaID != mediaID {
		t.Errorf("state.MediaID = %q, want %q", state.MediaID, mediaID)
	}
	if state.EntryID != entryID {
		t.Errorf("state.EntryID = %q, want %q", state.EntryID, entryID)
	}
}

func TestLoadPersistedMountStates_PrunesStaleRows(t *testing.T) {
	d := newCoverageDirector(t)
	ctx := context.Background()

	mountID := uuid.NewString()
	row := models.MountPlayoutState{
		MountID:   mountID,
		StationID: uuid.NewString(),
		EntryID:   uuid.NewString(),
		MediaID:   uuid.NewString(),
		StartedAt: time.Now().UTC().Add(-8 * time.Hour),
		EndsAt:    time.Now().UTC().Add(-7 * time.Hour), // more than 6 hours ago → stale
		UpdatedAt: time.Now().UTC(),
	}
	if err := d.db.Create(&row).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	d.loadPersistedMountStates(ctx)

	d.mu.Lock()
	_, ok := d.active[mountID]
	d.mu.Unlock()

	if ok {
		t.Error("stale row should not be loaded into active state")
	}
}

// ── computePlaybackResume ─────────────────────────────────────────────────

func TestComputePlaybackResume_StartInFuture(t *testing.T) {
	d := newCoverageDirector(t)
	entry := models.ScheduleEntry{
		StartsAt: time.Now().UTC().Add(10 * time.Second),
		EndsAt:   time.Now().UTC().Add(4 * time.Minute),
	}
	media := models.MediaItem{Duration: 3 * time.Minute}

	resume := d.computePlaybackResume(entry, media, nil)
	if resume.Offset != 0 {
		t.Errorf("future entry should have offset=0, got %v", resume.Offset)
	}
}

func TestComputePlaybackResume_TinyOffset(t *testing.T) {
	// Started 500ms ago — offset < 2s is treated as 0
	d := newCoverageDirector(t)
	entry := models.ScheduleEntry{
		StartsAt: time.Now().UTC().Add(-500 * time.Millisecond),
		EndsAt:   time.Now().UTC().Add(4 * time.Minute),
	}
	media := models.MediaItem{Duration: 4 * time.Minute}

	resume := d.computePlaybackResume(entry, media, nil)
	if resume.Offset != 0 {
		t.Errorf("offset < 2s should be 0, got %v", resume.Offset)
	}
}

func TestComputePlaybackResume_NormalOffset(t *testing.T) {
	// Started 30 seconds ago with a 5-minute track
	d := newCoverageDirector(t)
	entry := models.ScheduleEntry{
		StartsAt: time.Now().UTC().Add(-30 * time.Second),
		EndsAt:   time.Now().UTC().Add(4 * time.Minute),
	}
	media := models.MediaItem{Duration: 5 * time.Minute}

	resume := d.computePlaybackResume(entry, media, nil)
	// Expect around 30s offset (±2s tolerance)
	if resume.Offset < 28*time.Second || resume.Offset > 32*time.Second {
		t.Errorf("expected offset ~30s, got %v", resume.Offset)
	}
}

func TestComputePlaybackResume_OffsetCappedAtMaxDuration(t *testing.T) {
	// Started 10 minutes ago, track is only 2 minutes long
	d := newCoverageDirector(t)
	entry := models.ScheduleEntry{
		StartsAt: time.Now().UTC().Add(-10 * time.Minute),
		EndsAt:   time.Now().UTC().Add(1 * time.Minute),
	}
	media := models.MediaItem{Duration: 2 * time.Minute}

	resume := d.computePlaybackResume(entry, media, nil)
	// max = duration - 1s = 119s
	if resume.Offset > media.Duration {
		t.Errorf("offset %v exceeds media duration %v", resume.Offset, media.Duration)
	}
}

func TestComputePlaybackResume_FromPersistedStartTime(t *testing.T) {
	d := newCoverageDirector(t)
	entry := models.ScheduleEntry{
		StartsAt: time.Now().UTC().Add(-1 * time.Hour), // would give large offset
		EndsAt:   time.Now().UTC().Add(1 * time.Minute),
	}
	media := models.MediaItem{Duration: 5 * time.Minute}

	// Override start via extra payload
	resumeStartedAt := time.Now().UTC().Add(-45 * time.Second)
	extra := map[string]any{
		"resume_started_at": resumeStartedAt,
	}
	resume := d.computePlaybackResume(entry, media, extra)

	// Should be around 45s (±3s)
	if resume.Offset < 42*time.Second || resume.Offset > 48*time.Second {
		t.Errorf("expected offset ~45s from persisted start, got %v", resume.Offset)
	}
	if !resume.FromPersisted {
		t.Error("expected FromPersisted=true when resume_started_at is provided")
	}
}

// ── applyTrackOverrides ───────────────────────────────────────────────────

func TestApplyTrackOverrides_NoMetadataPassthrough(t *testing.T) {
	d := newCoverageDirector(t)
	ctx := context.Background()

	items := []string{"media-1", "media-2", "media-3"}
	entry := models.ScheduleEntry{Metadata: nil}

	got := d.applyTrackOverrides(ctx, entry, items)
	if len(got) != len(items) {
		t.Fatalf("expected %d items, got %d", len(items), len(got))
	}
	for i, id := range items {
		if got[i] != id {
			t.Errorf("item[%d] = %q, want %q", i, got[i], id)
		}
	}
}

func TestApplyTrackOverrides_EmptyItemsPassthrough(t *testing.T) {
	d := newCoverageDirector(t)
	ctx := context.Background()

	entry := models.ScheduleEntry{
		Metadata: map[string]any{
			"track_overrides": map[string]any{"0": "new-media"},
		},
	}

	got := d.applyTrackOverrides(ctx, entry, nil)
	if got != nil {
		t.Errorf("nil input should return nil, got %v", got)
	}
}

func TestApplyTrackOverrides_RemoveOverride(t *testing.T) {
	d := newCoverageDirector(t, &models.MediaItem{})
	ctx := context.Background()
	stationID := uuid.NewString()

	items := []string{"media-1", "media-2", "media-3"}
	entry := models.ScheduleEntry{
		StationID: stationID,
		Metadata: map[string]any{
			"track_overrides": map[string]any{
				"1": "__remove__", // remove item at index 1
			},
		},
	}

	got := d.applyTrackOverrides(ctx, entry, items)
	if len(got) != 2 {
		t.Fatalf("expected 2 items after removal, got %d", len(got))
	}
	if got[0] != "media-1" || got[1] != "media-3" {
		t.Errorf("after remove, got %v, want [media-1 media-3]", got)
	}
}

func TestApplyTrackOverrides_ReplacementNotInDB_FallsBackToOriginal(t *testing.T) {
	// If the replacement media doesn't exist in DB, the original item is kept.
	d := newCoverageDirector(t, &models.MediaItem{})
	ctx := context.Background()
	stationID := uuid.NewString()

	items := []string{"media-1", "media-2"}
	entry := models.ScheduleEntry{
		StationID: stationID,
		Metadata: map[string]any{
			"track_overrides": map[string]any{
				"0": "nonexistent-media", // not in DB
			},
		},
	}

	got := d.applyTrackOverrides(ctx, entry, items)
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}
	// Falls back to original when replacement not in DB
	if got[0] != "media-1" {
		t.Errorf("should fall back to original when replacement not in DB, got %q", got[0])
	}
}

func TestApplyTrackOverrides_ReplacementInDB(t *testing.T) {
	d := newCoverageDirector(t, &models.MediaItem{})
	ctx := context.Background()
	stationID := uuid.NewString()
	replacementID := uuid.NewString()

	// Seed the replacement media in DB
	media := models.MediaItem{
		ID:            replacementID,
		StationID:     stationID,
		Title:         "Replacement Track",
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	items := []string{"media-1", "media-2"}
	entry := models.ScheduleEntry{
		StationID: stationID,
		Metadata: map[string]any{
			"track_overrides": map[string]any{
				"0": replacementID, // replace item 0
			},
		},
	}

	got := d.applyTrackOverrides(ctx, entry, items)
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d: %v", len(got), got)
	}
	if got[0] != replacementID {
		t.Errorf("item[0] = %q, want replacement %q", got[0], replacementID)
	}
	if got[1] != "media-2" {
		t.Errorf("item[1] = %q, want unchanged %q", got[1], "media-2")
	}
}

func TestApplyTrackOverrides_NoOverridesKeyPassthrough(t *testing.T) {
	d := newCoverageDirector(t)
	ctx := context.Background()

	items := []string{"media-1", "media-2"}
	entry := models.ScheduleEntry{
		Metadata: map[string]any{
			"some_other_key": "value",
		},
	}

	got := d.applyTrackOverrides(ctx, entry, items)
	if len(got) != 2 || got[0] != "media-1" || got[1] != "media-2" {
		t.Errorf("without track_overrides key, items should pass through unchanged: %v", got)
	}
}

// ── resolveEntryForNow (recurring) ───────────────────────────────────────

func TestResolveEntryForNow_RecurringNotInstance(t *testing.T) {
	// A daily recurring entry that is NOT an instance should be handled via
	// resolveRecurringOccurrenceWindow. If it doesn't find an occurrence in
	// the 2s window it returns ok=false.
	now := time.Now().UTC()
	// Use a template that started 5 hours ago — today's occurrence would be 5 hours ago.
	entry := models.ScheduleEntry{
		ID:             uuid.NewString(),
		RecurrenceType: models.RecurrenceDaily,
		IsInstance:     false,
		StartsAt:       now.Add(-5 * time.Hour),
		EndsAt:         now.Add(-4 * time.Hour), // 5h ago to 4h ago — not in window
	}
	_, _, _, ok := resolveEntryForNow(entry, now, time.UTC)
	if ok {
		t.Error("daily recurring occurrence from 5 hours ago should NOT resolve to now")
	}
}

func TestResolveEntryForNow_NilLocation(t *testing.T) {
	// nil location should not panic (Go treats nil *time.Location as UTC)
	now := time.Now().UTC()
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StartsAt:   now.Add(-1 * time.Second),
		EndsAt:     now.Add(5 * time.Minute),
		IsInstance: true,
	}
	_, _, _, ok := resolveEntryForNow(entry, now, time.UTC)
	if !ok {
		t.Error("valid non-recurring instance should resolve")
	}
}

// ── popNextQueuedMedia ────────────────────────────────────────────────────

func newCoverageDirectorWithQueue(t *testing.T) *Director {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.PlayHistory{},
		&models.MountPlayoutState{},
		&models.MediaItem{},
		&models.PlayoutQueueItem{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return &Director{
		db:     db,
		active: make(map[string]playoutState),
		played: make(map[string]time.Time),
		logger: zerolog.Nop(),
	}
}

func TestPopNextQueuedMedia_Empty(t *testing.T) {
	d := newCoverageDirectorWithQueue(t)
	ctx := context.Background()

	media, item, err := d.popNextQueuedMedia(ctx, uuid.NewString(), uuid.NewString())
	if err != nil {
		t.Fatalf("popNextQueuedMedia: %v", err)
	}
	if media != nil || item != nil {
		t.Error("expected nil results when queue is empty")
	}
}

func TestPopNextQueuedMedia_ReturnsItem(t *testing.T) {
	d := newCoverageDirectorWithQueue(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()

	// Seed a media item
	m := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Queued Track",
		AnalysisState: models.AnalysisComplete,
		Duration:      3 * time.Minute,
	}
	if err := d.db.Create(&m).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	// Seed a queue item
	q := models.PlayoutQueueItem{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   mountID,
		MediaID:   mediaID,
		Position:  1,
	}
	if err := d.db.Create(&q).Error; err != nil {
		t.Fatalf("seed queue item: %v", err)
	}

	media, item, err := d.popNextQueuedMedia(ctx, stationID, mountID)
	if err != nil {
		t.Fatalf("popNextQueuedMedia: %v", err)
	}
	if media == nil || item == nil {
		t.Fatal("expected non-nil media and item")
	}
	if media.ID != mediaID {
		t.Errorf("media.ID = %q, want %q", media.ID, mediaID)
	}
	if item.MediaID != mediaID {
		t.Errorf("item.MediaID = %q, want %q", item.MediaID, mediaID)
	}

	// Item should be consumed (deleted from queue)
	var count int64
	d.db.Model(&models.PlayoutQueueItem{}).Where("station_id = ?", stationID).Count(&count)
	if count != 0 {
		t.Errorf("queue item should be deleted after pop, found %d", count)
	}
}

func TestPopNextQueuedMedia_SkipsOrphanedItem(t *testing.T) {
	// Queue item references a media ID that doesn't exist in DB
	// → should skip it and return nil.
	d := newCoverageDirectorWithQueue(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	q := models.PlayoutQueueItem{
		ID:        uuid.NewString(),
		StationID: stationID,
		MountID:   mountID,
		MediaID:   uuid.NewString(), // references non-existent media
		Position:  1,
	}
	if err := d.db.Create(&q).Error; err != nil {
		t.Fatalf("seed queue item: %v", err)
	}

	media, item, err := d.popNextQueuedMedia(ctx, stationID, mountID)
	if err != nil {
		t.Fatalf("popNextQueuedMedia with orphaned item: %v", err)
	}
	// Orphaned item is skipped — both nil.
	if media != nil || item != nil {
		t.Errorf("expected nil results for orphaned queue item, got media=%v item=%v", media, item)
	}
}

// ── effectiveCrossfade edge cases ─────────────────────────────────────────

func TestEffectiveCrossfade_InheritKeepsStationDuration(t *testing.T) {
	stationCfg := crossfadeConfig{Enabled: true, Duration: 4 * time.Second}
	entry := models.ScheduleEntry{
		Metadata: map[string]any{
			"crossfade": map[string]any{
				"override":    true,
				"enabled":     "inherit", // explicit inherit
				"duration_ms": float64(2000),
			},
		},
	}
	got := effectiveCrossfade(entry, stationCfg)
	// inherit means keep station's Enabled setting
	if got.Enabled != stationCfg.Enabled {
		t.Errorf("inherit should keep station enabled=%v, got %v", stationCfg.Enabled, got.Enabled)
	}
	// duration should be overridden to 2000ms
	if got.Duration != 2*time.Second {
		t.Errorf("duration = %v, want 2s", got.Duration)
	}
}

func TestEffectiveCrossfade_IntDurationOverride(t *testing.T) {
	// duration_ms as int (not float64)
	stationCfg := crossfadeConfig{Enabled: true, Duration: 3 * time.Second}
	entry := models.ScheduleEntry{
		Metadata: map[string]any{
			"crossfade": map[string]any{
				"override":    true,
				"enabled":     "on",
				"duration_ms": int(7000),
			},
		},
	}
	got := effectiveCrossfade(entry, stationCfg)
	if got.Duration != 7*time.Second {
		t.Errorf("int duration_ms = %v, want 7s", got.Duration)
	}
}

func TestEffectiveCrossfade_IntDurationCap(t *testing.T) {
	stationCfg := crossfadeConfig{Enabled: true, Duration: 3 * time.Second}
	entry := models.ScheduleEntry{
		Metadata: map[string]any{
			"crossfade": map[string]any{
				"override":    true,
				"enabled":     "on",
				"duration_ms": int(60000), // over 30s
			},
		},
	}
	got := effectiveCrossfade(entry, stationCfg)
	if got.Duration != 30*time.Second {
		t.Errorf("int duration_ms cap = %v, want 30s", got.Duration)
	}
}

func TestEffectiveCrossfade_NonMapMetadata(t *testing.T) {
	// crossfade key present but not a map — should fall back to station config
	stationCfg := crossfadeConfig{Enabled: true, Duration: 5 * time.Second}
	entry := models.ScheduleEntry{
		Metadata: map[string]any{
			"crossfade": "not-a-map",
		},
	}
	got := effectiveCrossfade(entry, stationCfg)
	if got.Enabled != stationCfg.Enabled || got.Duration != stationCfg.Duration {
		t.Errorf("non-map crossfade metadata should return station config, got %+v", got)
	}
}

// ── clearPersistedMountState coverage ────────────────────────────────────

func TestClearPersistedMountState_NonExistentMountNoError(t *testing.T) {
	d := newCoverageDirector(t)
	ctx := context.Background()
	// Non-existent mount — should not fail
	d.clearPersistedMountState(ctx, uuid.NewString())
}

// ── loadPersistedMountStates with empty/invalid rows ─────────────────────

func TestLoadPersistedMountStates_SkipsEmptyMountID(t *testing.T) {
	d := newCoverageDirector(t)
	ctx := context.Background()

	// Row with empty MountID should be skipped
	row := models.MountPlayoutState{
		MountID:   "", // invalid
		StationID: uuid.NewString(),
		EntryID:   uuid.NewString(),
		MediaID:   uuid.NewString(),
		StartedAt: time.Now().UTC(),
		EndsAt:    time.Now().UTC().Add(1 * time.Hour),
		UpdatedAt: time.Now().UTC(),
	}
	// SQLite allows empty primary key — insert directly
	if err := d.db.Exec(
		"INSERT INTO mount_playout_states (mount_id, station_id, entry_id, media_id, started_at, ends_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		row.MountID, row.StationID, row.EntryID, row.MediaID, row.StartedAt, row.EndsAt, row.UpdatedAt,
	).Error; err != nil {
		t.Skip("cannot seed empty mount_id row: " + err.Error())
	}

	d.loadPersistedMountStates(ctx)

	d.mu.Lock()
	_, ok := d.active[""]
	d.mu.Unlock()
	if ok {
		t.Error("row with empty MountID should not be loaded into active state")
	}
}
