/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package eventbus

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/events"
)

// The Redis bus carries WebSocket fan-out & cross-instance coordination once
// HA mode wires it in; a delivery bug here is silent & high impact (#252).
// These tests pinned two real defects on first writing: same-node publishes
// black-holed (subscribers only ever saw remote messages), & every
// Unsubscribe panicked on a double channel close.

const testEvent = events.EventType("test.event")

func newTestRedisBus(t *testing.T, mr *miniredis.Miniredis, nodeID string) *RedisBus {
	t.Helper()
	cfg := DefaultRedisConfig()
	cfg.Addr = mr.Addr()
	rb, err := NewRedisBus(cfg, nodeID, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewRedisBus: %v", err)
	}
	t.Cleanup(func() { _ = rb.Close() })
	return rb
}

func recvOne(t *testing.T, sub events.Subscriber, within time.Duration) events.Payload {
	t.Helper()
	select {
	case p := <-sub:
		return p
	case <-time.After(within):
		t.Fatal("no event delivered within timeout")
		return nil
	}
}

func assertSilent(t *testing.T, sub events.Subscriber, during time.Duration, msg string) {
	t.Helper()
	select {
	case p := <-sub:
		t.Fatalf("%s: got %v", msg, p)
	case <-time.After(during):
	}
}

func TestRedisBus_SameNodeDeliveryExactlyOnce(t *testing.T) {
	mr := miniredis.RunT(t)
	rb := newTestRedisBus(t, mr, "node-a")

	sub := rb.Subscribe(testEvent)
	rb.Publish(testEvent, events.Payload{"k": "v"})

	got := recvOne(t, sub, 2*time.Second)
	if got["k"] != "v" {
		t.Errorf("payload = %v, want k=v", got)
	}
	// The Redis echo of our own publish must be skipped: exactly one copy.
	assertSilent(t, sub, 300*time.Millisecond, "duplicate delivery (own Redis echo not skipped)")
}

func TestRedisBus_CrossNodeDelivery(t *testing.T) {
	mr := miniredis.RunT(t)
	a := newTestRedisBus(t, mr, "node-a")
	b := newTestRedisBus(t, mr, "node-b")

	subB := b.Subscribe(testEvent)
	// Give the pub/sub receiver a beat to attach before publishing.
	time.Sleep(50 * time.Millisecond)

	a.Publish(testEvent, events.Payload{"n": "cross"})

	got := recvOne(t, subB, 2*time.Second)
	if got["n"] != "cross" {
		t.Errorf("payload = %v, want n=cross (JSON round trip)", got)
	}
}

func TestRedisBus_MultiSubscriberDelivery(t *testing.T) {
	mr := miniredis.RunT(t)
	rb := newTestRedisBus(t, mr, "node-a")

	sub1 := rb.Subscribe(testEvent)
	sub2 := rb.Subscribe(testEvent)
	rb.Publish(testEvent, events.Payload{"x": "y"})

	if got := recvOne(t, sub1, 2*time.Second); got["x"] != "y" {
		t.Errorf("sub1 payload = %v", got)
	}
	if got := recvOne(t, sub2, 2*time.Second); got["x"] != "y" {
		t.Errorf("sub2 payload = %v", got)
	}
}

func TestRedisBus_UnsubscribeIsSafeAndClean(t *testing.T) {
	mr := miniredis.RunT(t)
	rb := newTestRedisBus(t, mr, "node-a")

	sub1 := rb.Subscribe(testEvent)
	sub2 := rb.Subscribe(testEvent)

	// This panicked (double close) before the fix.
	rb.Unsubscribe(testEvent, sub1)

	// The survivor still receives.
	rb.Publish(testEvent, events.Payload{"still": "alive"})
	if got := recvOne(t, sub2, 2*time.Second); got["still"] != "alive" {
		t.Errorf("survivor payload = %v", got)
	}

	// Unsubscribing an already-removed subscriber is a no-op, not a panic.
	rb.Unsubscribe(testEvent, sub1)

	// Last one out closes the Redis subscription without blowing up delivery
	// bookkeeping for future subscribers.
	rb.Unsubscribe(testEvent, sub2)
	sub3 := rb.Subscribe(testEvent)
	rb.Publish(testEvent, events.Payload{"re": "sub"})
	if got := recvOne(t, sub3, 2*time.Second); got["re"] != "sub" {
		t.Errorf("post-cleanup subscriber payload = %v", got)
	}
}

func TestRedisBus_FallbackModeStillDeliversLocally(t *testing.T) {
	cfg := DefaultRedisConfig()
	cfg.Addr = "127.0.0.1:1" // nothing listens; constructor flips to fallback
	cfg.DialTimeout = 200 * time.Millisecond
	rb, err := NewRedisBus(cfg, "node-a", zerolog.Nop())
	if err != nil {
		t.Fatalf("NewRedisBus fallback path errored: %v", err)
	}
	defer func() { _ = rb.Close() }()

	if !rb.useFallback {
		t.Fatal("expected fallback mode against unreachable Redis")
	}

	sub := rb.Subscribe(testEvent)
	rb.Publish(testEvent, events.Payload{"local": "only"})
	if got := recvOne(t, sub, time.Second); got["local"] != "only" {
		t.Errorf("fallback payload = %v", got)
	}
	rb.Unsubscribe(testEvent, sub)
}

func TestRedisBus_CloseTerminatesReceivers(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := DefaultRedisConfig()
	cfg.Addr = mr.Addr()
	rb, err := NewRedisBus(cfg, "node-a", zerolog.Nop())
	if err != nil {
		t.Fatalf("NewRedisBus: %v", err)
	}
	_ = rb.Subscribe(testEvent)

	done := make(chan error, 1)
	go func() { done <- rb.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close hung waiting for receiver goroutines")
	}
}

func TestRedisMessage_MarshalRoundTrip(t *testing.T) {
	data, err := marshalMessage(testEvent, events.Payload{"a": "b", "n": float64(3)}, "node-x")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	msg, err := unmarshalMessage(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.NodeID != "node-x" || msg.EventType != testEvent {
		t.Errorf("envelope = %+v", msg)
	}
	if msg.Payload["a"] != "b" || msg.Payload["n"] != float64(3) {
		t.Errorf("payload = %v", msg.Payload)
	}
}
