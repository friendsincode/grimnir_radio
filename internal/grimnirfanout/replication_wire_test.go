/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"
	"testing"
	"time"
)

func TestSessionMgr_ReplicatesOnCreate(t *testing.T) {
	rdb, mr, cleanup := newReplRedis(t)
	defer cleanup()

	mgr := NewSessionMgr()
	mgr.SetReplicator(NewSessionReplicator(rdb))

	s := mgr.Create(ProtocolHarbor)

	key := "dj:session:" + s.ID
	if !mr.Exists(key) {
		t.Fatalf("expected key %s after Create; not present", key)
	}
}

func TestSessionMgr_ReplicatesOnRemove(t *testing.T) {
	rdb, mr, cleanup := newReplRedis(t)
	defer cleanup()

	mgr := NewSessionMgr()
	mgr.SetReplicator(NewSessionReplicator(rdb))
	s := mgr.Create(ProtocolRTP)
	key := "dj:session:" + s.ID
	if !mr.Exists(key) {
		t.Fatalf("expected key after Create")
	}

	mgr.Remove(s.ID)
	if mr.Exists(key) {
		t.Error("expected key removed after Remove")
	}
}

func TestSessionMgr_HeartbeatLoopRefreshesAllSessions(t *testing.T) {
	rdb, mr, cleanup := newReplRedis(t)
	defer cleanup()

	mgr := NewSessionMgr()
	mgr.SetReplicator(NewSessionReplicator(rdb))

	s1 := mgr.Create(ProtocolHarbor)
	s2 := mgr.Create(ProtocolSRT)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		mgr.RunReplicationHeartbeat(ctx, 20*time.Millisecond)
		close(done)
	}()

	// Wait for at least 2 ticks so we know the loop fired & touched both
	// sessions' keys after the initial Create.
	time.Sleep(80 * time.Millisecond)
	cancel()
	<-done

	for _, id := range []string{s1.ID, s2.ID} {
		key := "dj:session:" + id
		if !mr.Exists(key) {
			t.Errorf("expected key %s to remain after heartbeat", key)
		}
		ttl := mr.TTL(key)
		if ttl <= 0 {
			t.Errorf("TTL for %s = %v, want > 0 (heartbeat should refresh)", key, ttl)
		}
	}
}

func TestSessionMgr_NoReplicator_NoOps(t *testing.T) {
	// A manager without a replicator must behave exactly like before;
	// every path is a no-op that returns nil.
	mgr := NewSessionMgr()
	s := mgr.Create(ProtocolWebRTC)
	if s == nil {
		t.Fatal("Create returned nil")
	}
	mgr.Remove(s.ID)
	// Run a heartbeat loop briefly to confirm it doesn't panic without a
	// replicator wired up.
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	mgr.RunReplicationHeartbeat(ctx, 10*time.Millisecond)
}
