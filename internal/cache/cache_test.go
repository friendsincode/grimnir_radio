/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package cache

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/rs/zerolog"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	cfg := DefaultConfig()
	cfg.RedisAddr = mr.Addr()
	c, err := New(cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if !c.IsAvailable() {
		t.Fatal("cache should be available against miniredis")
	}
	return c
}

func bg() context.Context { return context.Background() }

func TestNew_UnreachableRedis_DisablesGracefully(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RedisAddr = "127.0.0.1:1" // nothing listening
	c, err := New(cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("New should not error on unreachable Redis: %v", err)
	}
	if c.IsAvailable() {
		t.Fatal("cache should be disabled when Redis is unreachable")
	}
	// Operations on a disabled cache are safe no-ops.
	if err := c.SetStationList(bg(), []CachedStation{{ID: "s1"}}); err != nil {
		t.Fatalf("disabled Set should be a no-op, got %v", err)
	}
	if _, ok := c.GetStationList(bg()); ok {
		t.Fatal("disabled Get should miss")
	}
}

func TestStationListRoundTrip(t *testing.T) {
	c := newTestCache(t)
	if _, ok := c.GetStationList(bg()); ok {
		t.Fatal("expected miss before set")
	}
	want := []CachedStation{{ID: "s1", Name: "Rock"}, {ID: "s2", Name: "Jazz"}}
	if err := c.SetStationList(bg(), want); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, ok := c.GetStationList(bg())
	if !ok || len(got) != 2 || got[0].Name != "Rock" {
		t.Fatalf("station list round-trip: ok=%v got=%+v", ok, got)
	}
	if err := c.InvalidateStationList(bg()); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if _, ok := c.GetStationList(bg()); ok {
		t.Fatal("expected miss after invalidate")
	}
}

func TestMountCaching(t *testing.T) {
	c := newTestCache(t)
	mount := &CachedMount{ID: "m1", StationID: "st1", Name: "main", URL: "/live/main"}

	c.SetMount(bg(), mount)
	if got, ok := c.GetMount(bg(), "m1"); !ok || got.URL != "/live/main" {
		t.Fatalf("mount round-trip: ok=%v got=%+v", ok, got)
	}

	c.SetDefaultMount(bg(), "st1", mount)
	if got, ok := c.GetDefaultMount(bg(), "st1"); !ok || got.ID != "m1" {
		t.Fatalf("default mount: ok=%v got=%+v", ok, got)
	}

	// InvalidateMount clears the mount plus the station-level default.
	if err := c.InvalidateMount(bg(), "m1", "st1"); err != nil {
		t.Fatalf("invalidate mount: %v", err)
	}
	if _, ok := c.GetMount(bg(), "m1"); ok {
		t.Fatal("mount should be gone")
	}
	if _, ok := c.GetDefaultMount(bg(), "st1"); ok {
		t.Fatal("default mount should be gone")
	}
}

func TestSmartBlockAndMediaAndClock(t *testing.T) {
	c := newTestCache(t)

	c.SetSmartBlock(bg(), &CachedSmartBlock{ID: "sb1", Name: "Drive", Rules: map[string]any{"k": "v"}})
	if got, ok := c.GetSmartBlock(bg(), "sb1"); !ok || got.Name != "Drive" {
		t.Fatalf("smartblock: ok=%v got=%+v", ok, got)
	}
	c.InvalidateSmartBlock(bg(), "sb1")
	if _, ok := c.GetSmartBlock(bg(), "sb1"); ok {
		t.Fatal("smartblock should be invalidated")
	}

	c.SetMediaItem(bg(), &CachedMediaItem{ID: "md1", Title: "Song", Artist: "Band"})
	if got, ok := c.GetMediaItem(bg(), "md1"); !ok || got.Artist != "Band" {
		t.Fatalf("media: ok=%v got=%+v", ok, got)
	}
	c.InvalidateMediaItem(bg(), "md1")
	if _, ok := c.GetMediaItem(bg(), "md1"); ok {
		t.Fatal("media should be invalidated")
	}

	c.SetClock(bg(), &CachedClock{ID: "ck1", Name: "Weekday"})
	if _, ok := c.GetClock(bg(), "ck1"); !ok {
		t.Fatal("clock miss after set")
	}
	c.SetClockHours(bg(), "st1", []CachedClockHour{{ID: "h1", Hour: 9}})
	if got, ok := c.GetClockHours(bg(), "st1"); !ok || len(got) != 1 {
		t.Fatalf("clock hours: ok=%v got=%+v", ok, got)
	}
}

func TestInvalidateStation_ClearsRelatedKeys(t *testing.T) {
	c := newTestCache(t)
	c.SetStationList(bg(), []CachedStation{{ID: "st1"}})
	c.SetDefaultMount(bg(), "st1", &CachedMount{ID: "m1", StationID: "st1"})
	c.SetClockHours(bg(), "st1", []CachedClockHour{{ID: "h1"}})

	if err := c.InvalidateStation(bg(), "st1"); err != nil {
		t.Fatalf("invalidate station: %v", err)
	}
	if _, ok := c.GetStationList(bg()); ok {
		t.Fatal("station list should be cleared")
	}
	if _, ok := c.GetDefaultMount(bg(), "st1"); ok {
		t.Fatal("default mount should be cleared")
	}
	if _, ok := c.GetClockHours(bg(), "st1"); ok {
		t.Fatal("clock hours should be cleared")
	}
}

func TestFlushAll(t *testing.T) {
	c := newTestCache(t)
	c.SetStationList(bg(), []CachedStation{{ID: "s1"}})
	c.SetMount(bg(), &CachedMount{ID: "m1"})
	if err := c.FlushAll(bg()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if _, ok := c.GetStationList(bg()); ok {
		t.Fatal("station list should be gone after flush")
	}
	if _, ok := c.GetMount(bg(), "m1"); ok {
		t.Fatal("mount should be gone after flush")
	}
}
