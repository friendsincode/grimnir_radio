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
)

// TestElection_FailoverOnLeaseExpiry guards #86: acquire, renew and explicit
// release were tested, but not the real failover path — the leader dies without
// releasing, its lease TTL expires, and a follower takes over. Without this, a
// dead leader's schedule would never be picked up.
func TestElection_FailoverOnLeaseExpiry(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	ctx := context.Background()
	a := newElection(t, mr.Addr(), "node-A")
	b := newElection(t, mr.Addr(), "node-B")

	a.attemptLeadership(ctx)
	if !a.IsLeader() {
		t.Fatal("A should acquire leadership")
	}
	if ok, _ := b.acquireLock(ctx); ok {
		t.Fatal("B must not acquire the lock while A's lease is live")
	}

	// A dies without releasing. Advance past the lease so miniredis expires the
	// key, then B campaigns and takes over.
	mr.FastForward(DefaultConfig().LeaseDuration + time.Second)

	b.attemptLeadership(ctx)
	if !b.IsLeader() {
		t.Fatal("B should take over after A's lease expires")
	}
	if l, _ := b.GetLeader(ctx); l != "node-B" {
		t.Fatalf("leader after failover = %q, want node-B", l)
	}
}
