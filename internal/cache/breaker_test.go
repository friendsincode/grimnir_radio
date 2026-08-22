/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/rs/zerolog"
)

// TestCache_RuntimeCircuitBreaker guards #86: only the startup-unreachable path
// was tested. The mid-flight transition — cache connects, Redis then dies, an op
// fails, DisableOnError flips the breaker, and subsequent ops become safe no-ops
// — is the package's core resilience contract and had no test.
func TestCache_RuntimeCircuitBreaker(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	cfg := DefaultConfig()
	cfg.RedisAddr = mr.Addr()
	c, err := New(cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	defer c.Close()
	ctx := context.Background()

	if !c.IsAvailable() {
		t.Fatal("cache should be available against a live miniredis")
	}

	// Redis dies mid-flight.
	mr.Close()

	// The next op errors, which trips the breaker.
	if err := c.set(ctx, "k", "v", time.Minute); err == nil {
		t.Fatal("set should return an error once Redis is gone")
	}
	if c.IsAvailable() {
		t.Fatal("circuit breaker should have disabled the cache after the runtime error")
	}

	// A disabled cache degrades to safe no-ops, never erroring.
	if err := c.set(ctx, "k2", "v", time.Minute); err != nil {
		t.Fatalf("disabled set should be a no-op, got %v", err)
	}
	var dst string
	if ok, err := c.get(ctx, "k2", &dst); ok || err != nil {
		t.Fatalf("disabled get should miss without error, got ok=%v err=%v", ok, err)
	}
}
