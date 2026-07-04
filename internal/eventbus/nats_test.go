/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package eventbus

import (
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/events"
)

// The JetStream wire-up needs a running NATS server & stays untested until an
// embedded nats-server test dependency is worth adding; these tests cover the
// fallback-mode delivery contract (shared with RedisBus & previously carrying
// the same same-node black hole & double-close bugs) plus the envelope codec.

func TestNATSBus_FallbackModeDeliversLocally(t *testing.T) {
	cfg := NATSConfig{URL: "nats://127.0.0.1:1"} // nothing listens; fast refuse
	nb, err := NewNATSBus(cfg, "node-a", zerolog.Nop())
	if err != nil {
		t.Fatalf("NewNATSBus fallback path errored: %v", err)
	}
	defer func() { _ = nb.Close() }()

	if !nb.useFallback {
		t.Fatal("expected fallback mode against unreachable NATS")
	}

	sub := nb.Subscribe(testEvent)
	nb.Publish(testEvent, events.Payload{"local": "only"})

	select {
	case got := <-sub:
		if got["local"] != "only" {
			t.Errorf("payload = %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("fallback-mode publish never reached the subscriber (same-node black hole)")
	}

	// Panicked (double close) before the fix; twice proves no-op semantics.
	nb.Unsubscribe(testEvent, sub)
	nb.Unsubscribe(testEvent, sub)
}

func TestNATSMessage_MarshalRoundTrip(t *testing.T) {
	data, err := marshalNATSMessage(testEvent, events.Payload{"a": "b"}, "node-x")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	msg, err := unmarshalNATSMessage(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.NodeID != "node-x" || msg.EventType != testEvent || msg.Payload["a"] != "b" {
		t.Errorf("round trip = %+v", msg)
	}
}
