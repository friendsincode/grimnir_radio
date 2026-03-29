/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package state

import (
	"sync"
	"testing"
	"time"
)

func TestNewStore(t *testing.T) {
	s := NewStore()
	if s == nil {
		t.Fatal("NewStore returned nil")
	}
	if s.recent == nil {
		t.Fatal("NewStore: recent slice is nil")
	}
	if len(s.recent) != 0 {
		t.Fatalf("NewStore: expected empty slice, got len=%d", len(s.recent))
	}
}

func TestAdd(t *testing.T) {
	s := NewStore()
	now := time.Now()

	s.Add(RecentPlay{
		MediaID:   "m1",
		Artist:    "Artist A",
		PlayedAt:  now,
		StationID: "st1",
		MountID:   "mo1",
	})

	recent := s.Recent()
	if len(recent) != 1 {
		t.Fatalf("expected 1 play, got %d", len(recent))
	}
	if recent[0].MediaID != "m1" {
		t.Errorf("MediaID = %q, want %q", recent[0].MediaID, "m1")
	}
	if recent[0].Artist != "Artist A" {
		t.Errorf("Artist = %q, want %q", recent[0].Artist, "Artist A")
	}
}

func TestAddMultiple(t *testing.T) {
	s := NewStore()
	now := time.Now()

	for i := 0; i < 5; i++ {
		s.Add(RecentPlay{
			MediaID:  "m" + string(rune('0'+i)),
			PlayedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}

	recent := s.Recent()
	if len(recent) != 5 {
		t.Fatalf("expected 5 plays, got %d", len(recent))
	}
}

func TestRecent_ReturnsCopy(t *testing.T) {
	s := NewStore()
	now := time.Now()
	s.Add(RecentPlay{MediaID: "m1", PlayedAt: now})

	r1 := s.Recent()
	r1[0].MediaID = "mutated"

	r2 := s.Recent()
	if r2[0].MediaID == "mutated" {
		t.Fatal("Recent() should return a copy, not a reference")
	}
}

func TestPrune_RemovesOldEntries(t *testing.T) {
	s := NewStore()
	now := time.Now()

	s.Add(RecentPlay{MediaID: "old", PlayedAt: now.Add(-2 * time.Hour)})
	s.Add(RecentPlay{MediaID: "new", PlayedAt: now.Add(time.Minute)})
	s.Add(RecentPlay{MediaID: "recent", PlayedAt: now.Add(-30 * time.Minute)})

	cutoff := now.Add(-time.Hour)
	s.Prune(cutoff)

	recent := s.Recent()
	if len(recent) != 2 {
		t.Fatalf("after prune: expected 2 entries, got %d", len(recent))
	}
	for _, rp := range recent {
		if rp.MediaID == "old" {
			t.Error("old entry should have been pruned")
		}
	}
}

func TestPrune_EmptyStore(t *testing.T) {
	s := NewStore()
	// Should not panic
	s.Prune(time.Now())
	if len(s.Recent()) != 0 {
		t.Fatal("expected empty store after pruning empty store")
	}
}

func TestPrune_AllOld(t *testing.T) {
	s := NewStore()
	now := time.Now()

	for i := 0; i < 3; i++ {
		s.Add(RecentPlay{
			MediaID:  "old" + string(rune('0'+i)),
			PlayedAt: now.Add(-24 * time.Hour),
		})
	}

	s.Prune(now)
	if len(s.Recent()) != 0 {
		t.Fatalf("expected 0 entries after pruning all old, got %d", len(s.Recent()))
	}
}

func TestPrune_AllNew(t *testing.T) {
	s := NewStore()
	now := time.Now()

	for i := 0; i < 3; i++ {
		s.Add(RecentPlay{
			MediaID:  "new" + string(rune('0'+i)),
			PlayedAt: now.Add(time.Hour),
		})
	}

	s.Prune(now)
	if len(s.Recent()) != 3 {
		t.Fatalf("expected 3 entries (all new), got %d", len(s.Recent()))
	}
}

func TestConcurrentAddAndRecent(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	now := time.Now()

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Add(RecentPlay{
				MediaID:  "m" + string(rune('a'+i%26)),
				PlayedAt: now,
			})
		}(i)
	}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Recent()
		}()
	}

	wg.Wait()
}
