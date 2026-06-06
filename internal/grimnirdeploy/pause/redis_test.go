/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package pause

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newMini(t *testing.T) (*Client, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return NewClient(rdb), mr, rdb
}

func TestSetReadClear_DefaultRegion(t *testing.T) {
	c, _, _ := newMini(t)
	ctx := context.Background()

	got, err := c.Read(ctx, "default")
	if err != nil {
		t.Fatalf("Read empty: %v", err)
	}
	if got != nil {
		t.Errorf("Read empty should return nil; got %+v", got)
	}

	if err := c.Set(ctx, "default", "fixing #999", "alice", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err = c.Read(ctx, "default")
	if err != nil {
		t.Fatalf("Read after Set: %v", err)
	}
	if got == nil {
		t.Fatal("Read after Set returned nil")
	}
	if got.Reason != "fixing #999" {
		t.Errorf("Reason = %q, want fixing #999", got.Reason)
	}
	if got.Operator != "alice" {
		t.Errorf("Operator = %q, want alice", got.Operator)
	}
	if got.Region != "default" {
		t.Errorf("Region = %q, want default", got.Region)
	}
	if time.Since(got.TS) > time.Second {
		t.Errorf("TS too old: %v", got.TS)
	}

	if err := c.Clear(ctx, "default"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	got, _ = c.Read(ctx, "default")
	if got != nil {
		t.Errorf("Read after Clear should be nil; got %+v", got)
	}
}

func TestSet_RequiresReasonAndOperator(t *testing.T) {
	c, _, _ := newMini(t)
	ctx := context.Background()

	if err := c.Set(ctx, "default", "", "alice", 0); err == nil {
		t.Error("Set with empty reason should fail")
	}
	if err := c.Set(ctx, "default", "reason", "", 0); err == nil {
		t.Error("Set with empty operator should fail")
	}
}

func TestSet_PerRegionIsolation(t *testing.T) {
	c, _, _ := newMini(t)
	ctx := context.Background()

	if err := c.Set(ctx, "us-east", "us-east issue", "alice", 0); err != nil {
		t.Fatalf("Set us-east: %v", err)
	}

	// Other region must not see the pause.
	got, err := c.Read(ctx, "eu-west")
	if err != nil {
		t.Fatalf("Read eu-west: %v", err)
	}
	if got != nil {
		t.Errorf("eu-west should be empty; got %+v", got)
	}

	// Original region still has it.
	got, err = c.Read(ctx, "us-east")
	if err != nil {
		t.Fatalf("Read us-east: %v", err)
	}
	if got == nil || got.Reason != "us-east issue" {
		t.Errorf("us-east lost the pause: %+v", got)
	}
}

func TestSet_KeyNameFormat(t *testing.T) {
	c, mr, _ := newMini(t)
	ctx := context.Background()
	if err := c.Set(ctx, "us-east", "reason", "alice", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// Key must be the documented name so the scheduler in grimnirradio can read it.
	if _, err := mr.Get("grimnir-deploy:emergency-pause:us-east"); err != nil {
		t.Errorf("expected key grimnir-deploy:emergency-pause:us-east to exist: %v", err)
	}
}

func TestSet_WithTTL(t *testing.T) {
	c, mr, _ := newMini(t)
	ctx := context.Background()
	if err := c.Set(ctx, "default", "reason", "alice", 30*time.Second); err != nil {
		t.Fatalf("Set with TTL: %v", err)
	}
	ttl := mr.TTL("grimnir-deploy:emergency-pause:default")
	if ttl <= 0 || ttl > 30*time.Second {
		t.Errorf("TTL = %v, want >0 and <=30s", ttl)
	}
}

func TestSet_NoTTL_DefaultsToSticky(t *testing.T) {
	c, mr, _ := newMini(t)
	ctx := context.Background()
	if err := c.Set(ctx, "default", "reason", "alice", 0); err != nil {
		t.Fatalf("Set without TTL: %v", err)
	}
	if mr.TTL("grimnir-deploy:emergency-pause:default") != 0 {
		t.Errorf("Expected no TTL for sticky pause; got %v", mr.TTL("grimnir-deploy:emergency-pause:default"))
	}
}

func TestClear_IsIdempotent(t *testing.T) {
	c, _, _ := newMini(t)
	ctx := context.Background()
	if err := c.Clear(ctx, "default"); err != nil {
		t.Errorf("Clear on empty should be idempotent: %v", err)
	}
}

func TestCheck_Helper(t *testing.T) {
	_, _, rdb := newMini(t)
	ctx := context.Background()

	paused, reason, err := Check(ctx, rdb, "default")
	if err != nil {
		t.Fatalf("Check empty: %v", err)
	}
	if paused {
		t.Errorf("Empty Check should return paused=false; got paused=%v reason=%q", paused, reason)
	}

	c := NewClient(rdb)
	if err := c.Set(ctx, "default", "incident #1", "bob", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	paused, reason, err = Check(ctx, rdb, "default")
	if err != nil {
		t.Fatalf("Check after Set: %v", err)
	}
	if !paused {
		t.Errorf("paused should be true; got %v", paused)
	}
	if reason == "" || reason == "incident #1" {
		// reason should be a human-readable string that includes the reason,
		// operator, and timestamp so callers can show it in an abort message.
		if reason != "incident #1" {
			// The minimal contract is the reason text appears somewhere.
		}
	}
	// At minimum, the reason text must appear in the returned reason.
	if !containsAll(reason, "incident #1", "bob") {
		t.Errorf("reason should include reason + operator; got %q", reason)
	}
}

func TestCheck_RedisDown(t *testing.T) {
	_, mr, rdb := newMini(t)
	mr.Close() // simulate Redis going away
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, _, err := Check(ctx, rdb, "default")
	if err == nil {
		t.Error("Check with dead Redis should return an error")
	}
	if errors.Is(err, ErrNotPaused) {
		t.Error("Check error must not be ErrNotPaused on transport failure")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
