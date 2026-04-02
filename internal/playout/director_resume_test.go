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

// TestStartSmartBlockEntry_ResumesFromActiveState covers the resumeEntryIfPossible=true
// path in startSmartBlockEntry, exercising the early "resume and return nil" branch.
func TestStartSmartBlockEntry_ResumesFromActiveState(t *testing.T) {
	d, _ := newMockDirector(t, &models.SmartBlock{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	entryID := uuid.NewString()
	blockID := uuid.NewString()
	mediaID := uuid.NewString()

	block := models.SmartBlock{
		ID:        blockID,
		StationID: stationID,
		Name:      "Resume Block",
	}
	if err := d.db.Create(&block).Error; err != nil {
		t.Fatalf("seed smart block: %v", err)
	}

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "resume-sb-" + mountID[:8],
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
		Title:         "Resume Track",
		Path:          "/tmp/resume-track.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    entryID,
		StationID:  stationID,
		MediaID:    mediaID,
		SourceType: "smart_block",
		SourceID:   blockID,
		Position:   0,
		Items:      []string{mediaID},
		TotalItems: 1,
	}
	d.mu.Unlock()

	entry := models.ScheduleEntry{
		ID:         entryID,
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "smart_block",
		SourceID:   blockID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	if err := d.startSmartBlockEntry(ctx, entry); err != nil {
		t.Errorf("startSmartBlockEntry (resume) returned error: %v", err)
	}
}

// TestResumeEntryIfPossible_ClockTemplatePath covers the clock_template source type path.
func TestResumeEntryIfPossible_ClockTemplatePath(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	entryID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID: mountID, StationID: stationID,
		Name: "resume-ct-" + mountID[:8], Format: "mp3",
		Bitrate: 128, SampleRate: 44100, Channels: 2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "CT Track",
		Path:          "/tmp/ct-resume.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    entryID,
		StationID:  stationID,
		MediaID:    mediaID,
		SourceType: "clock_template",
		SourceID:   "ct-123",
		Position:   0,
		Items:      []string{mediaID},
		TotalItems: 1,
	}
	d.mu.Unlock()

	entry := models.ScheduleEntry{
		ID:         entryID,
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "clock_template",
		SourceID:   "ct-123",
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	result := d.resumeEntryIfPossible(ctx, entry)
	if !result {
		t.Error("expected resumeEntryIfPossible to return true for clock_template")
	}
}

// TestResumeEntryIfPossible_EntryExpired covers the early return when entry is in the past.
func TestResumeEntryIfPossible_EntryExpired(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "playlist",
		StartsAt:   time.Now().UTC().Add(-10 * time.Minute),
		EndsAt:     time.Now().UTC().Add(-1 * time.Second), // already ended
	}

	// Should return false because entry has ended.
	if d.resumeEntryIfPossible(ctx, entry) {
		t.Error("expected false for expired entry")
	}
}

// TestResumeEntryIfPossible_EmptyMediaAtPosition covers the fallback to state.MediaID.
func TestResumeEntryIfPossible_EmptyMediaAtPosition(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	entryID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID: mountID, StationID: stationID,
		Name: "emi-" + mountID[:8], Format: "mp3",
		Bitrate: 128, SampleRate: 44100, Channels: 2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	media := models.MediaItem{
		ID: mediaID, StationID: stationID, Title: "EMI Track",
		Path: "/tmp/emi.mp3", Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	// Set Items[0] = "" so code falls back to state.MediaID.
	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    entryID,
		StationID:  stationID,
		MediaID:    mediaID, // fallback MediaID
		SourceType: "playlist",
		Position:   0,
		Items:      []string{""}, // empty string at position 0
		TotalItems: 1,
	}
	d.mu.Unlock()

	entry := models.ScheduleEntry{
		ID:         entryID,
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "playlist",
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	// Should return true after loading media via state.MediaID fallback.
	result := d.resumeEntryIfPossible(ctx, entry)
	if !result {
		t.Error("expected resumeEntryIfPossible to return true with empty Items[0] fallback to MediaID")
	}
}

// TestResumeEntryIfPossible_PositionOutOfBounds covers the bounds check early return.
func TestResumeEntryIfPossible_PositionOutOfBounds(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	entryID := uuid.NewString()

	d.mu.Lock()
	d.active[mountID] = playoutState{
		EntryID:    entryID,
		StationID:  stationID,
		MediaID:    uuid.NewString(),
		SourceType: "smart_block",
		Position:   5, // out of bounds
		Items:      []string{"id-1", "id-2"},
		TotalItems: 2,
	}
	d.mu.Unlock()

	entry := models.ScheduleEntry{
		ID: entryID, StationID: stationID, MountID: mountID,
		SourceType: "smart_block",
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	if d.resumeEntryIfPossible(ctx, entry) {
		t.Error("expected false when position out of bounds")
	}
}

// TestGetScheduleBoundaryPolicy_SoftMode covers the soft boundary mode branch.
func TestGetScheduleBoundaryPolicy_SoftMode(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	station := models.Station{
		ID:                         stationID,
		Name:                       "Soft Boundary Station",
		ScheduleBoundaryMode:       "soft",
		ScheduleSoftOverrunSeconds: 30,
	}
	if err := d.db.Create(&station).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}

	policy := d.getScheduleBoundaryPolicy(ctx, stationID)
	if policy.Mode != "soft" {
		t.Errorf("expected mode=soft, got %q", policy.Mode)
	}
	if policy.SoftOverrun == 0 {
		t.Error("expected non-zero SoftOverrun")
	}
}
