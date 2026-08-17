/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package analytics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

type fakeCounter struct {
	counts map[string]int
	errFor map[string]bool
}

func (f *fakeCounter) ListenerCount(ctx context.Context, stationID string) (int, error) {
	if f.errFor[stationID] {
		return 0, errors.New("counter unavailable")
	}
	return f.counts[stationID], nil
}

func newListenerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}, &models.ListenerSample{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func sampleCount(t *testing.T, db *gorm.DB, stationID string) int64 {
	t.Helper()
	var n int64
	db.Model(&models.ListenerSample{}).Where("station_id = ?", stationID).Count(&n)
	return n
}

func TestCaptureSnapshot_RecordsActiveStationsOnly(t *testing.T) {
	db := newListenerDB(t)
	db.Create(&models.Station{ID: "s1", Active: true})
	db.Create(&models.Station{ID: "s2", Active: true})
	db.Create(&models.Station{ID: "s3", Active: false})

	svc := NewListenerAnalyticsService(db, &fakeCounter{counts: map[string]int{"s1": 10, "s2": 5}}, zerolog.Nop())
	svc.captureSnapshot(context.Background(), time.Now())

	if got := sampleCount(t, db, "s1"); got != 1 {
		t.Fatalf("s1 samples = %d, want 1", got)
	}
	if got := sampleCount(t, db, "s3"); got != 0 {
		t.Fatalf("inactive station should not be sampled, got %d", got)
	}
	var s1 models.ListenerSample
	db.Where("station_id = ?", "s1").First(&s1)
	if s1.Listeners != 10 {
		t.Fatalf("s1 listeners = %d, want 10", s1.Listeners)
	}
}

func TestCaptureSnapshot_CounterErrorSkipsStation(t *testing.T) {
	db := newListenerDB(t)
	db.Create(&models.Station{ID: "s1", Active: true})
	db.Create(&models.Station{ID: "s2", Active: true})

	svc := NewListenerAnalyticsService(db, &fakeCounter{
		counts: map[string]int{"s1": 3},
		errFor: map[string]bool{"s2": true},
	}, zerolog.Nop())
	svc.captureSnapshot(context.Background(), time.Now())

	if got := sampleCount(t, db, "s1"); got != 1 {
		t.Fatalf("s1 samples = %d, want 1", got)
	}
	if got := sampleCount(t, db, "s2"); got != 0 {
		t.Fatalf("s2 (counter error) samples = %d, want 0", got)
	}
}

func TestPruneOldSamples_DeletesBeyondRetention(t *testing.T) {
	db := newListenerDB(t)
	now := time.Now().UTC()
	db.Create(&models.ListenerSample{ID: "old", StationID: "s1", CapturedAt: now.Add(-40 * 24 * time.Hour)})
	db.Create(&models.ListenerSample{ID: "recent", StationID: "s1", CapturedAt: now.Add(-1 * time.Hour)})

	svc := NewListenerAnalyticsService(db, &fakeCounter{}, zerolog.Nop())
	svc.pruneOldSamples(context.Background(), now) // retention is 30 days

	if got := sampleCount(t, db, "s1"); got != 1 {
		t.Fatalf("after prune, s1 samples = %d, want 1 (recent only)", got)
	}
	var remaining models.ListenerSample
	db.First(&remaining)
	if remaining.ID != "recent" {
		t.Fatalf("wrong sample survived prune: %q", remaining.ID)
	}
}

func TestStart_NoCounter_ReturnsImmediately(t *testing.T) {
	db := newListenerDB(t)
	svc := NewListenerAnalyticsService(db, nil, zerolog.Nop())

	done := make(chan struct{})
	go func() { svc.Start(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Start with nil counter did not return")
	}
}

func TestStart_CapturesImmediatelyAndOnTick(t *testing.T) {
	db := newListenerDB(t)
	// Start runs concurrently; ":memory:" sqlite is per-connection, so pin to one.
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	db.Create(&models.Station{ID: "s1", Active: true})

	svc := NewListenerAnalyticsService(db, &fakeCounter{counts: map[string]int{"s1": 7}}, zerolog.Nop())
	svc.interval = 15 * time.Millisecond // exercise the ticker branch quickly

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.Start(ctx); close(done) }()

	// Immediate capture + at least one tick.
	deadline := time.Now().Add(2 * time.Second)
	for sampleCount(t, db, "s1") < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("expected >=2 samples from immediate + tick, got %d", sampleCount(t, db, "s1"))
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not stop on cancel")
	}
}
