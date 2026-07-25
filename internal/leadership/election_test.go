/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package leadership

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/rs/zerolog"
)

func newElection(t *testing.T, addr, instanceID string) *Election {
	t.Helper()
	cfg := DefaultConfig()
	cfg.RedisAddr = addr
	cfg.InstanceID = instanceID
	e, err := NewElection(cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("new election %s: %v", instanceID, err)
	}
	return e
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.InstanceID == "" || cfg.ElectionKey == "" || cfg.LeaseDuration == 0 {
		t.Fatalf("default config unset: %+v", cfg)
	}
}

func TestNewElection_UnreachableRedisErrors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RedisAddr = "127.0.0.1:1"
	if _, err := NewElection(cfg, zerolog.Nop()); err == nil {
		t.Fatal("expected an error connecting to unreachable Redis")
	}
}

func TestElection_SingleLeaderAndHandover(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	ctx := context.Background()
	a := newElection(t, mr.Addr(), "node-A")
	b := newElection(t, mr.Addr(), "node-B")

	// A campaigns first and wins.
	a.attemptLeadership(ctx)
	if !a.IsLeader() {
		t.Fatal("A should have acquired leadership")
	}
	select {
	case v := <-a.LeaderCh():
		if !v {
			t.Fatal("A's leader channel should report true")
		}
	default:
		t.Fatal("A should have emitted a leadership change")
	}

	leader, err := a.GetLeader(ctx)
	if err != nil || leader != "node-A" {
		t.Fatalf("GetLeader = %q (err %v), want node-A", leader, err)
	}

	// B cannot acquire while A holds the lease.
	acquired, err := b.acquireLock(ctx)
	if err != nil {
		t.Fatalf("B acquireLock: %v", err)
	}
	if acquired {
		t.Fatal("B must not acquire the lock while A holds it")
	}
	b.attemptLeadership(ctx)
	if b.IsLeader() {
		t.Fatal("B should not be leader")
	}

	// A renews (it already owns the lock).
	renewed, err := a.acquireLock(ctx)
	if err != nil || !renewed {
		t.Fatalf("A renew: acquired=%v err=%v", renewed, err)
	}

	// A steps down; the key is freed.
	if err := a.releaseLock(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}
	if l, _ := a.GetLeader(ctx); l != "" {
		t.Fatalf("after release, leader = %q, want empty", l)
	}

	// Now B can take over.
	b.attemptLeadership(ctx)
	if !b.IsLeader() {
		t.Fatal("B should acquire leadership after A releases")
	}
	if l, _ := b.GetLeader(ctx); l != "node-B" {
		t.Fatalf("new leader = %q, want node-B", l)
	}
}

func TestReleaseLock_NotOwnerLeavesKey(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()
	ctx := context.Background()

	a := newElection(t, mr.Addr(), "node-A")
	b := newElection(t, mr.Addr(), "node-B")

	a.attemptLeadership(ctx) // A is leader
	// B tries to release a lock it does not own: the Lua guard is a no-op.
	if err := b.releaseLock(ctx); err != nil {
		t.Fatalf("B release: %v", err)
	}
	if l, _ := a.GetLeader(ctx); l != "node-A" {
		t.Fatalf("A should still hold the lock, leader = %q", l)
	}
}
