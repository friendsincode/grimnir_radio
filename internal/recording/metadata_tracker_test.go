/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package recording

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestMetadataTracker_AddsChapterOnNowPlaying(t *testing.T) {
	svc, db := newSvc(t)
	seedRecording(t, db, dbtest.UUID("r1"), dbtest.UUID("st1"), models.RecordingStatusActive, 0)

	bus := events.NewBus()
	mt := NewMetadataTracker(db, svc, bus, zerolog.Nop())

	ctx, cancel := context.WithCancel(bg())
	defer cancel()
	mt.Start(ctx)
	defer mt.Stop()

	time.Sleep(20 * time.Millisecond)
	bus.Publish(events.EventNowPlaying, events.Payload{
		"station_id": dbtest.UUID("st1"), "title": "Song A", "artist": "Artist A",
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		var n int64
		db.Model(&models.RecordingChapter{}).Where("recording_id = ?", dbtest.UUID("r1")).Count(&n)
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("now-playing event did not add a chapter")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHandleNowPlaying_NoActiveRecording_NoOp(t *testing.T) {
	svc, db := newSvc(t)
	// A completed (non-active) recording must not receive chapters.
	seedRecording(t, db, dbtest.UUID("r1"), dbtest.UUID("st1"), models.RecordingStatusComplete, 0)
	mt := NewMetadataTracker(db, svc, events.NewBus(), zerolog.Nop())

	mt.handleNowPlaying(bg(), events.Payload{"station_id": dbtest.UUID("st1"), "title": "X", "artist": "Y"})

	var n int64
	db.Model(&models.RecordingChapter{}).Count(&n)
	if n != 0 {
		t.Fatalf("expected no chapters without an active recording, got %d", n)
	}
}

func TestHandleNowPlaying_DedupsIdenticalMetadata(t *testing.T) {
	svc, db := newSvc(t)
	seedRecording(t, db, dbtest.UUID("r1"), dbtest.UUID("st1"), models.RecordingStatusActive, 0)
	mt := NewMetadataTracker(db, svc, events.NewBus(), zerolog.Nop())

	meta := events.Payload{"station_id": dbtest.UUID("st1"), "title": "Same", "artist": "Same"}
	mt.handleNowPlaying(bg(), meta)
	mt.handleNowPlaying(bg(), meta) // identical → deduped, no second chapter

	var n int64
	db.Model(&models.RecordingChapter{}).Where("recording_id = ?", dbtest.UUID("r1")).Count(&n)
	if n != 1 {
		t.Fatalf("expected 1 chapter after duplicate metadata, got %d", n)
	}
}

// TestHandleNowPlaying_ChangedTrackAddsChapter guards #86: the dedup is by
// content, not "seen this station before." When the now-playing title changes,
// a second chapter must be written — this is the complement of the dedup test,
// and without it a stuck lastMeta comparison would suppress every later track.
func TestHandleNowPlaying_ChangedTrackAddsChapter(t *testing.T) {
	svc, db := newSvc(t)
	seedRecording(t, db, dbtest.UUID("r1"), dbtest.UUID("st1"), models.RecordingStatusActive, 0)
	mt := NewMetadataTracker(db, svc, events.NewBus(), zerolog.Nop())

	station := dbtest.UUID("st1")
	mt.handleNowPlaying(bg(), events.Payload{"station_id": station, "title": "Song A", "artist": "Artist A"})
	mt.handleNowPlaying(bg(), events.Payload{"station_id": station, "title": "Song B", "artist": "Artist A"})

	var n int64
	db.Model(&models.RecordingChapter{}).Where("recording_id = ?", dbtest.UUID("r1")).Count(&n)
	if n != 2 {
		t.Fatalf("a changed track should add a second chapter, got %d", n)
	}
}
