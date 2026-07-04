/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package leadership

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/rs/zerolog"
)

// Leader election gates executor distribution & the scheduler's leader-aware
// path; a regression here means split-brain (two schedulers driving one
// station) or a healthy leader stepping down (nothing driving it). These
// tests run against miniredis; note miniredis TTLs advance only via
// FastForward, never wall clock, so lease expiry is always explicit.

func testConfig(mr *miniredis.Miniredis, id string) ElectionConfig {
	return ElectionConfig{
		RedisAddr:       mr.Addr(),
		ElectionKey:     "test:leader",
		LeaseDuration:   300 * time.Millisecond,
		RenewalInterval: 20 * time.Millisecond,
		RetryInterval:   20 * time.Millisecond,
		InstanceID:      id,
	}
}

func waitUntil(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func TestElection_AcquireRenewRelease(t *testing.T) {
	mr := miniredis.RunT(t)
	e, err := NewElection(testConfig(mr, "node-a"), zerolog.Nop())
	if err != nil {
		t.Fatalf("NewElection: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !waitUntil(t, 2*time.Second, e.IsLeader) {
		t.Fatal("never acquired leadership")
	}

	// The transition was announced on LeaderCh.
	select {
	case got := <-e.LeaderCh():
		if !got {
			t.Error("first LeaderCh value = false, want true")
		}
	default:
		t.Error("no leadership transition on LeaderCh")
	}

	// GetLeader reports us.
	leader, err := e.GetLeader(context.Background())
	if err != nil {
		t.Fatalf("GetLeader: %v", err)
	}
	if leader != "node-a" {
		t.Errorf("GetLeader = %q, want node-a", leader)
	}

	// Renewal keeps the lease alive across many renewal intervals of real
	// time (the key would only die via FastForward, but a broken renewal
	// path would also stop reporting leadership on foreign-owner checks).
	time.Sleep(100 * time.Millisecond)
	if !e.IsLeader() {
		t.Error("lost leadership while renewing unopposed")
	}

	// Stop releases the lock: the key is gone, not left to expire.
	if err := e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if mr.Exists("test:leader") {
		t.Error("leadership key still present after Stop; release-on-shutdown failed")
	}
}

func TestElection_ExactlyOneHolderAndFailover(t *testing.T) {
	mr := miniredis.RunT(t)
	a, err := NewElection(testConfig(mr, "node-a"), zerolog.Nop())
	if err != nil {
		t.Fatalf("NewElection a: %v", err)
	}
	b, err := NewElection(testConfig(mr, "node-b"), zerolog.Nop())
	if err != nil {
		t.Fatalf("NewElection b: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = a.Start(ctx)
	_ = b.Start(ctx)

	if !waitUntil(t, 2*time.Second, func() bool { return a.IsLeader() || b.IsLeader() }) {
		t.Fatal("no contender ever acquired leadership")
	}

	// Sample for a while: never two leaders at once.
	for i := 0; i < 40; i++ {
		if a.IsLeader() && b.IsLeader() {
			t.Fatal("split-brain: both contenders report leadership")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Stop the current leader; the survivor must take over (graceful release
	// deletes the key, so takeover needs only the next retry tick).
	leader, survivor := a, b
	if b.IsLeader() {
		leader, survivor = b, a
	}
	if err := leader.Stop(); err != nil {
		t.Fatalf("stop leader: %v", err)
	}
	if !waitUntil(t, 2*time.Second, survivor.IsLeader) {
		t.Fatal("survivor never took over after graceful release")
	}
	_ = survivor.Stop()
}

func TestElection_LeaseLapseDemotes(t *testing.T) {
	mr := miniredis.RunT(t)
	e, err := NewElection(testConfig(mr, "node-a"), zerolog.Nop())
	if err != nil {
		t.Fatalf("NewElection: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = e.Start(ctx)

	if !waitUntil(t, 2*time.Second, e.IsLeader) {
		t.Fatal("never acquired leadership")
	}

	// Simulate a lease lapse another node capitalized on: expire the lease,
	// then a rival claims the key before our next renewal tick lands.
	mr.FastForward(400 * time.Millisecond)
	if err := mr.Set("test:leader", "node-rival"); err != nil {
		t.Fatalf("rival set: %v", err)
	}
	mr.SetTTL("test:leader", time.Hour)

	// The next attempt sees a foreign owner & must demote — no lingering
	// double-leader window.
	if !waitUntil(t, 2*time.Second, func() bool { return !e.IsLeader() }) {
		t.Fatal("still claiming leadership while a rival holds the key")
	}

	// And it must NOT steal the key back while the rival's lease is valid.
	time.Sleep(100 * time.Millisecond)
	if got, _ := mr.Get("test:leader"); got != "node-rival" {
		t.Errorf("key owner = %q, want node-rival (demoted node must not usurp)", got)
	}
	_ = e.Stop()
}

func TestNewElection_UnreachableRedis(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RedisAddr = "127.0.0.1:1" // nothing listens here; refuse fast
	if _, err := NewElection(cfg, zerolog.Nop()); err == nil {
		t.Fatal("NewElection succeeded against an unreachable Redis")
	}
}
