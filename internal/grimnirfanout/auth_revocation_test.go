/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Task 7.2 — When the control plane publishes a DJAuthRevoke event with
// {mount, token}, the subscriber evicts that entry from the DJAuthClient's
// cache. The next Validate must re-dial.
func TestAuthRevocationSubscriber_EvictsOnEvent(t *testing.T) {
	srv := &fakeDJAuthServer{}
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()

	c, err := NewDJAuthClient(DJAuthClientConfig{
		Addr:        addr,
		Timeout:     2 * time.Second,
		MaxTTL:      5 * time.Minute,
		DialOptions: []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	})
	if err != nil {
		t.Fatalf("NewDJAuthClient: %v", err)
	}
	defer c.Close()

	bus := events.NewBus()
	sub := NewAuthRevocationSubscriber(c, bus)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)

	// Prime the cache.
	if _, err := c.Validate(context.Background(), "/live", "tok-r", "harbor"); err != nil {
		t.Fatalf("first Validate: %v", err)
	}
	if srv.callCount() != 1 {
		t.Fatalf("after prime, server calls = %d, want 1", srv.callCount())
	}

	// Publish a revocation. The subscriber goroutine should evict the entry.
	bus.Publish(EventDJAuthRevoke, events.Payload{
		"mount": "/live",
		"token": "tok-r",
	})

	// Wait until subscriber processes the event (bounded poll, not sleep loop).
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := c.cache.get(cacheKey("/live", "tok-r"), time.Now()); !ok {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if _, ok := c.cache.get(cacheKey("/live", "tok-r"), time.Now()); ok {
		t.Fatal("cache entry still present after revoke event")
	}

	// Next Validate must miss & re-dial.
	if _, err := c.Validate(context.Background(), "/live", "tok-r", "harbor"); err != nil {
		t.Fatalf("post-revoke Validate: %v", err)
	}
	if srv.callCount() != 2 {
		t.Errorf("server calls = %d, want 2 (revoke event should have evicted)", srv.callCount())
	}
}

// "revoke_all" payload purges the entire cache.
func TestAuthRevocationSubscriber_RevokeAll(t *testing.T) {
	srv := &fakeDJAuthServer{}
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()

	c, err := NewDJAuthClient(DJAuthClientConfig{
		Addr:        addr,
		Timeout:     2 * time.Second,
		MaxTTL:      5 * time.Minute,
		DialOptions: []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	})
	if err != nil {
		t.Fatalf("NewDJAuthClient: %v", err)
	}
	defer c.Close()

	bus := events.NewBus()
	sub := NewAuthRevocationSubscriber(c, bus)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)

	_, _ = c.Validate(context.Background(), "/live", "a", "harbor")
	_, _ = c.Validate(context.Background(), "/live", "b", "harbor")
	if srv.callCount() != 2 {
		t.Fatalf("after prime, server calls = %d, want 2", srv.callCount())
	}

	bus.Publish(EventDJAuthRevoke, events.Payload{"all": true})

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		_, aLive := c.cache.get(cacheKey("/live", "a"), time.Now())
		_, bLive := c.cache.get(cacheKey("/live", "b"), time.Now())
		if !aLive && !bLive {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	_, _ = c.Validate(context.Background(), "/live", "a", "harbor")
	_, _ = c.Validate(context.Background(), "/live", "b", "harbor")
	if srv.callCount() != 4 {
		t.Errorf("server calls = %d, want 4 (revoke_all should have purged both)", srv.callCount())
	}
}

// Malformed payload (no mount or token) is ignored.
func TestAuthRevocationSubscriber_IgnoresMalformed(t *testing.T) {
	srv := &fakeDJAuthServer{}
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()
	c, err := NewDJAuthClient(DJAuthClientConfig{
		Addr:        addr,
		Timeout:     2 * time.Second,
		MaxTTL:      5 * time.Minute,
		DialOptions: []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	})
	if err != nil {
		t.Fatalf("NewDJAuthClient: %v", err)
	}
	defer c.Close()

	bus := events.NewBus()
	sub := NewAuthRevocationSubscriber(c, bus)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)

	_, _ = c.Validate(context.Background(), "/live", "k", "harbor")
	bus.Publish(EventDJAuthRevoke, events.Payload{"garbage": "yes"})
	// Give the subscriber a chance to drain.
	time.Sleep(20 * time.Millisecond)
	if _, ok := c.cache.get(cacheKey("/live", "k"), time.Now()); !ok {
		t.Error("malformed event evicted a valid cache entry")
	}
}
