/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newReplRedis builds a miniredis-backed *redis.Client for use in
// replication tests. The cleanup func tears both down.
func newReplRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, mr, func() {
		_ = rdb.Close()
		mr.Close()
	}
}

func TestSessionReplicator_WriteOnCreate(t *testing.T) {
	rdb, mr, cleanup := newReplRedis(t)
	defer cleanup()

	rep := NewSessionReplicator(rdb)
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	s := newSessionWithDeps("sess-write-1", ProtocolHarbor, now)

	if err := rep.Replicate(context.Background(), s); err != nil {
		t.Fatalf("Replicate: %v", err)
	}

	key := "dj:session:sess-write-1"
	if !mr.Exists(key) {
		t.Fatalf("expected key %s to exist", key)
	}
	got, err := rdb.HGetAll(context.Background(), key).Result()
	if err != nil {
		t.Fatalf("HGetAll: %v", err)
	}
	if got["id"] != "sess-write-1" {
		t.Errorf("hash[id] = %q, want sess-write-1", got["id"])
	}
	if got["protocol"] != "harbor" {
		t.Errorf("hash[protocol] = %q, want harbor", got["protocol"])
	}
	if got["state"] != "idle" {
		t.Errorf("hash[state] = %q, want idle", got["state"])
	}
	if got["started_at"] == "" {
		t.Error("hash[started_at] empty, want RFC3339 timestamp")
	}
	if got["last_active_at"] == "" {
		t.Error("hash[last_active_at] empty, want RFC3339 timestamp")
	}
}

func TestSessionReplicator_TTL60s(t *testing.T) {
	rdb, mr, cleanup := newReplRedis(t)
	defer cleanup()

	rep := NewSessionReplicator(rdb)
	s := newSessionWithDeps("sess-ttl", ProtocolRTP, time.Now())
	if err := rep.Replicate(context.Background(), s); err != nil {
		t.Fatalf("Replicate: %v", err)
	}

	ttl := mr.TTL("dj:session:sess-ttl")
	if ttl <= 0 {
		t.Fatalf("TTL = %v, want > 0", ttl)
	}
	if ttl != 60*time.Second {
		t.Errorf("TTL = %v, want 60s", ttl)
	}
}

func TestSessionReplicator_RefreshExtendsTTL(t *testing.T) {
	rdb, mr, cleanup := newReplRedis(t)
	defer cleanup()

	rep := NewSessionReplicator(rdb)
	s := newSessionWithDeps("sess-refresh", ProtocolSRT, time.Now())
	if err := rep.Replicate(context.Background(), s); err != nil {
		t.Fatalf("Replicate (first): %v", err)
	}
	// Fast-forward 30 seconds; key should still be alive.
	mr.FastForward(30 * time.Second)
	if !mr.Exists("dj:session:sess-refresh") {
		t.Fatalf("key expired after 30s, want still alive (TTL 60s)")
	}
	// Refresh.
	if err := rep.Replicate(context.Background(), s); err != nil {
		t.Fatalf("Replicate (refresh): %v", err)
	}
	// Another 45s; without refresh would have expired, but refresh reset
	// the TTL to 60s so the key must still be alive.
	mr.FastForward(45 * time.Second)
	if !mr.Exists("dj:session:sess-refresh") {
		t.Error("key expired after refresh; expected TTL to be reset to 60s")
	}
}

func TestSessionReplicator_ExpiresWithoutRefresh(t *testing.T) {
	rdb, mr, cleanup := newReplRedis(t)
	defer cleanup()

	rep := NewSessionReplicator(rdb)
	s := newSessionWithDeps("sess-expire", ProtocolWebRTC, time.Now())
	if err := rep.Replicate(context.Background(), s); err != nil {
		t.Fatalf("Replicate: %v", err)
	}
	mr.FastForward(61 * time.Second)
	if mr.Exists("dj:session:sess-expire") {
		t.Error("key still exists after 61s, want expired (TTL 60s)")
	}
}

func TestSessionReplicator_ReflectsStateChange(t *testing.T) {
	rdb, _, cleanup := newReplRedis(t)
	defer cleanup()

	rep := NewSessionReplicator(rdb)
	s := newSessionWithDeps("sess-state", ProtocolHarbor, time.Now())
	if err := rep.Replicate(context.Background(), s); err != nil {
		t.Fatalf("first replicate: %v", err)
	}
	if err := s.transitionTo(SessionAuthenticating); err != nil {
		t.Fatalf("transitionTo: %v", err)
	}
	if err := rep.Replicate(context.Background(), s); err != nil {
		t.Fatalf("second replicate: %v", err)
	}
	got, err := rdb.HGet(context.Background(), "dj:session:sess-state", "state").Result()
	if err != nil {
		t.Fatalf("HGet state: %v", err)
	}
	if got != "authenticating" {
		t.Errorf("hash[state] = %q, want authenticating", got)
	}
}

func TestSessionReplicator_DeleteOnEnd(t *testing.T) {
	rdb, mr, cleanup := newReplRedis(t)
	defer cleanup()

	rep := NewSessionReplicator(rdb)
	s := newSessionWithDeps("sess-end", ProtocolRTP, time.Now())
	if err := rep.Replicate(context.Background(), s); err != nil {
		t.Fatalf("Replicate: %v", err)
	}
	if err := rep.Delete(context.Background(), s.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if mr.Exists("dj:session:sess-end") {
		t.Error("key still exists after Delete")
	}
}

func TestSessionReplicator_EncodesAuthClaimsJSON(t *testing.T) {
	rdb, _, cleanup := newReplRedis(t)
	defer cleanup()

	rep := NewSessionReplicator(rdb)
	s := newSessionWithDeps("sess-claims", ProtocolHarbor, time.Now())
	s.AuthClaims = map[string]any{
		"dj_id":   "dj-42",
		"mount":   "/live",
		"role":    "dj",
		"granted": true,
	}
	if err := rep.Replicate(context.Background(), s); err != nil {
		t.Fatalf("Replicate: %v", err)
	}
	raw, err := rdb.HGet(context.Background(), "dj:session:sess-claims", "auth_claims").Result()
	if err != nil {
		t.Fatalf("HGet auth_claims: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("decode auth_claims: %v (raw=%q)", err, raw)
	}
	if decoded["dj_id"] != "dj-42" {
		t.Errorf("decoded[dj_id] = %v, want dj-42", decoded["dj_id"])
	}
	if decoded["mount"] != "/live" {
		t.Errorf("decoded[mount] = %v, want /live", decoded["mount"])
	}
}

func TestSessionReplicator_HydrateWarmSessions(t *testing.T) {
	rdb, _, cleanup := newReplRedis(t)
	defer cleanup()

	// Simulate a peer node that wrote two sessions, one fresh and one
	// stale, before this node started up.
	ctx := context.Background()
	now := time.Now()

	rdb.HSet(ctx, "dj:session:warm-fresh", map[string]string{
		"id":             "warm-fresh",
		"protocol":       "harbor",
		"state":          "active",
		"started_at":     now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
		"last_active_at": now.Add(-3 * time.Second).Format(time.RFC3339Nano),
	})
	rdb.HSet(ctx, "dj:session:warm-stale", map[string]string{
		"id":             "warm-stale",
		"protocol":       "rtp",
		"state":          "active",
		"started_at":     now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
		"last_active_at": now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
	})

	rep := NewSessionReplicator(rdb)
	mgr := NewSessionMgr()
	hydrated, err := rep.Hydrate(ctx, mgr, 30*time.Second)
	if err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	if hydrated != 1 {
		t.Errorf("hydrated = %d, want 1 (only the fresh one)", hydrated)
	}
	if mgr.Count() != 1 {
		t.Errorf("mgr.Count = %d, want 1", mgr.Count())
	}
	got, ok := mgr.Get("warm-fresh")
	if !ok {
		t.Fatal("expected warm-fresh session in mgr, missing")
	}
	if got.Protocol != ProtocolHarbor {
		t.Errorf("Protocol = %v, want ProtocolHarbor", got.Protocol)
	}
	if !got.IsRecovering() {
		t.Error("hydrated session should be marked IsRecovering")
	}
	if _, ok := mgr.Get("warm-stale"); ok {
		t.Error("stale session should NOT have been hydrated")
	}
}

func TestSessionReplicator_HydrateSkipsLocalSessions(t *testing.T) {
	rdb, _, cleanup := newReplRedis(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	rdb.HSet(ctx, "dj:session:already-local", map[string]string{
		"id":             "already-local",
		"protocol":       "harbor",
		"state":          "active",
		"started_at":     now.Add(-time.Minute).Format(time.RFC3339Nano),
		"last_active_at": now.Format(time.RFC3339Nano),
	})

	mgr := NewSessionMgr()
	existing := newSessionWithDeps("already-local", ProtocolHarbor, now)
	mgr.Add(existing)

	rep := NewSessionReplicator(rdb)
	hydrated, err := rep.Hydrate(ctx, mgr, 30*time.Second)
	if err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	if hydrated != 0 {
		t.Errorf("hydrated = %d, want 0 (already local)", hydrated)
	}
	if existing.IsRecovering() {
		t.Error("existing local session should NOT be flipped to recovering")
	}
}
