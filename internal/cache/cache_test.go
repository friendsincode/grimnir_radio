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

func newTestCache(t *testing.T) (*Cache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	cfg := DefaultConfig()
	cfg.RedisAddr = mr.Addr()
	c, err := New(cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, mr
}

func TestCache_UnavailableRedisDegradesGracefully(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RedisAddr = "127.0.0.1:1" // nothing listens
	c, err := New(cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("New must not error without redis (cache is optional): %v", err)
	}
	defer func() { _ = c.Close() }()

	if c.IsAvailable() {
		t.Fatal("cache claims availability without redis")
	}
	// Every operation is a silent no-op, never an error surfaced to callers.
	ctx := context.Background()
	if err := c.SetStationList(ctx, []CachedStation{{ID: "s1"}}); err != nil {
		t.Errorf("set on disabled cache errored: %v", err)
	}
	if _, ok := c.GetStationList(ctx); ok {
		t.Error("get on disabled cache reported a hit")
	}
	if err := c.InvalidateStationList(ctx); err != nil {
		t.Errorf("invalidate on disabled cache errored: %v", err)
	}
}

func TestCache_StationListRoundTripAndInvalidate(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	if _, ok := c.GetStationList(ctx); ok {
		t.Fatal("hit on empty cache")
	}

	stations := []CachedStation{{ID: "s1", Name: "One"}, {ID: "s2", Name: "Two"}}
	if err := c.SetStationList(ctx, stations); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, ok := c.GetStationList(ctx)
	if !ok || len(got) != 2 || got[0].Name != "One" {
		t.Fatalf("round trip = %v ok=%v", got, ok)
	}

	if err := c.InvalidateStationList(ctx); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if _, ok := c.GetStationList(ctx); ok {
		t.Fatal("hit after invalidation")
	}
}

func TestCache_MountScopedInvalidation(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	m1 := &CachedMount{ID: "m1", StationID: "s1", Name: "main"}
	m2 := &CachedMount{ID: "m2", StationID: "s2", Name: "other"}
	if err := c.SetMount(ctx, m1); err != nil {
		t.Fatalf("set m1: %v", err)
	}
	if err := c.SetMount(ctx, m2); err != nil {
		t.Fatalf("set m2: %v", err)
	}
	if err := c.SetDefaultMount(ctx, "s1", m1); err != nil {
		t.Fatalf("set default: %v", err)
	}

	// Invalidate a single mount: the sibling survives.
	if err := c.InvalidateMount(ctx, "m1", "s1"); err != nil {
		t.Fatalf("invalidate mount: %v", err)
	}
	if _, ok := c.GetMount(ctx, "m1"); ok {
		t.Error("m1 survived its own invalidation")
	}
	if _, ok := c.GetMount(ctx, "m2"); !ok {
		t.Error("m2 was collateral damage of m1's invalidation")
	}
	// The station's default-mount entry is invalidated with it.
	if _, ok := c.GetDefaultMount(ctx, "s1"); ok {
		t.Error("s1 default mount survived mount invalidation")
	}
}

func TestCache_SmartBlockMediaClockRoundTrips(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	sb := &CachedSmartBlock{ID: "sb1", StationID: "s1", Name: "Block"}
	if err := c.SetSmartBlock(ctx, sb); err != nil {
		t.Fatalf("set sb: %v", err)
	}
	if got, ok := c.GetSmartBlock(ctx, "sb1"); !ok || got.Name != "Block" {
		t.Errorf("smart block round trip: %v %v", got, ok)
	}
	if err := c.InvalidateSmartBlock(ctx, "sb1"); err != nil {
		t.Fatalf("invalidate sb: %v", err)
	}
	if _, ok := c.GetSmartBlock(ctx, "sb1"); ok {
		t.Error("sb1 survived invalidation")
	}

	mi := &CachedMediaItem{ID: "mi1", StationID: "s1", Title: "Song"}
	if err := c.SetMediaItem(ctx, mi); err != nil {
		t.Fatalf("set media: %v", err)
	}
	if got, ok := c.GetMediaItem(ctx, "mi1"); !ok || got.Title != "Song" {
		t.Errorf("media round trip: %v %v", got, ok)
	}
	if err := c.InvalidateMediaItem(ctx, "mi1"); err != nil {
		t.Fatalf("invalidate media: %v", err)
	}

	ck := &CachedClock{ID: "c1", StationID: "s1", Name: "Morning"}
	if err := c.SetClock(ctx, ck); err != nil {
		t.Fatalf("set clock: %v", err)
	}
	if got, ok := c.GetClock(ctx, "c1"); !ok || got.Name != "Morning" {
		t.Errorf("clock round trip: %v %v", got, ok)
	}
	hours := []CachedClockHour{{ID: "h1", StationID: "s1"}}
	if err := c.SetClockHours(ctx, "s1", hours); err != nil {
		t.Fatalf("set hours: %v", err)
	}
	if got, ok := c.GetClockHours(ctx, "s1"); !ok || len(got) != 1 {
		t.Errorf("hours round trip: %v %v", got, ok)
	}
	if err := c.InvalidateClockHours(ctx, "s1"); err != nil {
		t.Fatalf("invalidate hours: %v", err)
	}
	if _, ok := c.GetClockHours(ctx, "s1"); ok {
		t.Error("hours survived invalidation")
	}
}

func TestCache_InvalidateStationSweepsItsKeys(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	_ = c.SetDefaultMount(ctx, "s1", &CachedMount{ID: "m1", StationID: "s1"})
	_ = c.SetClockHours(ctx, "s1", []CachedClockHour{{ID: "h1", StationID: "s1"}})
	_ = c.SetStationList(ctx, []CachedStation{{ID: "s1"}})
	_ = c.SetDefaultMount(ctx, "s2", &CachedMount{ID: "m2", StationID: "s2"})

	if err := c.InvalidateStation(ctx, "s1"); err != nil {
		t.Fatalf("invalidate station: %v", err)
	}
	if _, ok := c.GetDefaultMount(ctx, "s1"); ok {
		t.Error("s1 default mount survived station invalidation")
	}
	if _, ok := c.GetClockHours(ctx, "s1"); ok {
		t.Error("s1 clock hours survived station invalidation")
	}
	if _, ok := c.GetDefaultMount(ctx, "s2"); !ok {
		t.Error("s2 was collateral damage of s1's invalidation")
	}
}

func TestCache_FlushAllClearsOnlyGrimnirKeys(t *testing.T) {
	c, mr := newTestCache(t)
	ctx := context.Background()

	_ = c.SetStationList(ctx, []CachedStation{{ID: "s1"}})
	if err := mr.Set("unrelated:key", "keepme"); err != nil {
		t.Fatalf("seed foreign key: %v", err)
	}

	if err := c.FlushAll(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if _, ok := c.GetStationList(ctx); ok {
		t.Error("grimnir key survived FlushAll")
	}
	if v, _ := mr.Get("unrelated:key"); v != "keepme" {
		t.Error("FlushAll deleted keys outside the grimnir namespace")
	}
}

func TestCache_CircuitBreakerDisablesOnError(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := DefaultConfig()
	cfg.RedisAddr = mr.Addr()
	cfg.DisableOnError = true
	c, err := New(cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	_ = c.SetStationList(ctx, []CachedStation{{ID: "s1"}})
	if !c.IsAvailable() {
		t.Fatal("cache should be up")
	}

	// Kill redis mid-flight: the next operation errors & trips the breaker.
	mr.Close()
	_ = c.SetStationList(ctx, []CachedStation{{ID: "s2"}})
	if c.IsAvailable() {
		t.Fatal("breaker did not open after a redis error with DisableOnError")
	}
	// Subsequent operations are silent no-ops.
	if _, ok := c.GetStationList(ctx); ok {
		t.Error("hit reported with the breaker open")
	}
}

func TestCache_CorruptEntryIsAMiss(t *testing.T) {
	c, mr := newTestCache(t)
	ctx := context.Background()

	_ = c.SetStationList(ctx, []CachedStation{{ID: "s1"}})
	// Find the key & corrupt it.
	keys := mr.Keys()
	if len(keys) == 0 {
		t.Fatal("no keys written")
	}
	if err := mr.Set(keys[0], "{not json"); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	if _, ok := c.GetStationList(ctx); ok {
		t.Error("corrupt cache entry reported as a hit")
	}
}
