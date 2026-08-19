/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package eventbus

import (
	"strings"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/events"
)

func TestDefaultNATSConfig(t *testing.T) {
	c := DefaultNATSConfig()
	if c.URL != "nats://localhost:4222" {
		t.Errorf("URL = %q", c.URL)
	}
	if c.StreamName != "GRIMNIR_EVENTS" {
		t.Errorf("StreamName = %q", c.StreamName)
	}
	if c.MaxReconnects != -1 {
		t.Errorf("MaxReconnects = %d, want -1 (unlimited)", c.MaxReconnects)
	}
	if c.MaxFailures != 5 {
		t.Errorf("MaxFailures = %d, want 5", c.MaxFailures)
	}
}

// TestNATSMessageRoundTrip guards the wire format: a marshalled message must
// unmarshal back with its event type, payload, and node id intact and carry a
// message id, since this is how events cross the NATS boundary between nodes.
func TestNATSMessageRoundTrip(t *testing.T) {
	payload := events.Payload{"station_id": "s1", "count": float64(3)}
	data, err := marshalNATSMessage(events.EventType("test.event"), payload, "node-a")
	if err != nil {
		t.Fatalf("marshalNATSMessage: %v", err)
	}

	msg, err := unmarshalNATSMessage(data)
	if err != nil {
		t.Fatalf("unmarshalNATSMessage: %v", err)
	}
	if msg.EventType != events.EventType("test.event") {
		t.Errorf("EventType = %q", msg.EventType)
	}
	if msg.NodeID != "node-a" {
		t.Errorf("NodeID = %q", msg.NodeID)
	}
	if msg.Payload["station_id"] != "s1" || msg.Payload["count"] != float64(3) {
		t.Errorf("Payload = %v", msg.Payload)
	}
	if msg.MessageID == "" {
		t.Error("MessageID is empty")
	}
	if msg.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
}

func TestUnmarshalNATSMessage_Invalid(t *testing.T) {
	if _, err := unmarshalNATSMessage([]byte("not json")); err == nil {
		t.Error("expected error for invalid json, got nil")
	}
}

func TestGenerateNodeID(t *testing.T) {
	id := generateNodeID()
	if id == "" || !strings.Contains(id, "-") {
		t.Errorf("generateNodeID = %q, want non-empty host-suffix form", id)
	}
	if generateNodeID() == id {
		t.Error("generateNodeID should produce unique ids")
	}
}
