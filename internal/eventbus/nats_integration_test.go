/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package eventbus

import (
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/events"
)

// JetStream integration tests against an embedded nats-server (the dep the
// earlier fallback-only tests deferred). These exercise the real wire-up:
// stream creation, per-event consumers, cross-node fan-out, echo skip, &
// teardown.

func startNATSServer(t *testing.T) *server.Server {
	t.Helper()
	opts := &server.Options{
		Port:      -1, // random free port
		JetStream: true,
		StoreDir:  t.TempDir(),
	}
	ns, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("nats server: %v", err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server never became ready")
	}
	t.Cleanup(ns.Shutdown)
	return ns
}

func newTestNATSBus(t *testing.T, ns *server.Server, nodeID string) *NATSBus {
	t.Helper()
	cfg := DefaultNATSConfig()
	cfg.URL = ns.ClientURL()
	nb, err := NewNATSBus(cfg, nodeID, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewNATSBus: %v", err)
	}
	if nb.useFallback {
		t.Fatal("bus fell back with a live server")
	}
	t.Cleanup(func() { _ = nb.Close() })
	return nb
}

func TestNATSBus_SameNodeDeliveryExactlyOnce(t *testing.T) {
	ns := startNATSServer(t)
	nb := newTestNATSBus(t, ns, "node-a")

	sub := nb.Subscribe(testEvent)
	nb.Publish(testEvent, events.Payload{"k": "v"})

	got := recvOne(t, sub, 3*time.Second)
	if got["k"] != "v" {
		t.Errorf("payload = %v", got)
	}
	// The JetStream echo of our own publish must be skipped.
	assertSilent(t, sub, 400*time.Millisecond, "duplicate delivery (own JetStream echo not skipped)")
}

func TestNATSBus_CrossNodeDelivery(t *testing.T) {
	ns := startNATSServer(t)
	a := newTestNATSBus(t, ns, "node-a")
	b := newTestNATSBus(t, ns, "node-b")

	subB := b.Subscribe(testEvent)
	time.Sleep(200 * time.Millisecond) // let the consumer attach

	a.Publish(testEvent, events.Payload{"n": "cross"})

	got := recvOne(t, subB, 5*time.Second)
	if got["n"] != "cross" {
		t.Errorf("payload = %v", got)
	}
}

func TestNATSBus_UnsubscribeSafeAndResubscribe(t *testing.T) {
	ns := startNATSServer(t)
	nb := newTestNATSBus(t, ns, "node-a")

	sub1 := nb.Subscribe(testEvent)
	sub2 := nb.Subscribe(testEvent)
	nb.Unsubscribe(testEvent, sub1) // double-close panic before the fix
	nb.Unsubscribe(testEvent, sub1) // repeated: no-op

	nb.Publish(testEvent, events.Payload{"still": "alive"})
	if got := recvOne(t, sub2, 3*time.Second); got["still"] != "alive" {
		t.Errorf("survivor payload = %v", got)
	}

	nb.Unsubscribe(testEvent, sub2)
	sub3 := nb.Subscribe(testEvent)
	nb.Publish(testEvent, events.Payload{"re": "sub"})
	if got := recvOne(t, sub3, 3*time.Second); got["re"] != "sub" {
		t.Errorf("post-cleanup payload = %v", got)
	}
}

func TestNATSBus_CloseTerminates(t *testing.T) {
	ns := startNATSServer(t)
	cfg := DefaultNATSConfig()
	cfg.URL = ns.ClientURL()
	nb, err := NewNATSBus(cfg, "node-a", zerolog.Nop())
	if err != nil {
		t.Fatalf("NewNATSBus: %v", err)
	}
	_ = nb.Subscribe(testEvent)

	done := make(chan error, 1)
	go func() { done <- nb.Close() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close hung")
	}
}

func TestNATSBus_CustomStreamName(t *testing.T) {
	ns := startNATSServer(t)
	cfg := DefaultNATSConfig()
	cfg.URL = ns.ClientURL()
	cfg.StreamName = "CUSTOM_EVENTS"
	nb, err := NewNATSBus(cfg, "node-a", zerolog.Nop())
	if err != nil {
		t.Fatalf("NewNATSBus: %v", err)
	}
	defer func() { _ = nb.Close() }()
	if nb.useFallback {
		t.Fatal("fell back with a live server")
	}

	// Before the fix, Subscribe created consumers against the hardcoded
	// GRIMNIR_EVENTS stream while the configured stream was CUSTOM_EVENTS —
	// every subscribe failed consumer creation.
	sub := nb.Subscribe(testEvent)
	nb.mu.RLock()
	_, consumerCreated := nb.natsSubs[testEvent]
	nb.mu.RUnlock()
	if !consumerCreated {
		t.Fatal("consumer not created on the configured stream")
	}
	nb.Publish(testEvent, events.Payload{"custom": "stream"})
	if got := recvOne(t, sub, 3*time.Second); got["custom"] != "stream" {
		t.Errorf("payload = %v", got)
	}
}
