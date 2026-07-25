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

func TestDefaultRedisConfig(t *testing.T) {
	cfg := DefaultRedisConfig()
	if cfg.Addr == "" || cfg.MaxFailures == 0 || cfg.DialTimeout == 0 {
		t.Fatalf("default config looks unset: %+v", cfg)
	}
}

func TestMarshalUnmarshalMessage(t *testing.T) {
	data, err := marshalMessage(events.EventNowPlaying, events.Payload{"track": "Highway Star"}, "nodeA")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	msg, err := unmarshalMessage(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.EventType != events.EventNowPlaying || msg.NodeID != "nodeA" || msg.Payload["track"] != "Highway Star" {
		t.Fatalf("round-trip mismatch: %+v", msg)
	}
	if _, err := unmarshalMessage([]byte("not json")); err == nil {
		t.Fatal("expected error on malformed message")
	}
}

func TestNewRedisBus_FallbackDelivery(t *testing.T) {
	// Unreachable Redis => circuit-breaker fallback to the in-memory bus.
	cfg := DefaultRedisConfig()
	cfg.Addr = "127.0.0.1:1"
	bus, err := NewRedisBus(cfg, "node1", zerolog.Nop())
	if err != nil {
		t.Fatalf("NewRedisBus should not error on unreachable Redis: %v", err)
	}
	defer bus.Close()

	// In fallback mode, same-node publish/subscribe delivers via the in-memory bus.
	sub := bus.Subscribe(events.EventNowPlaying)
	bus.Publish(events.EventNowPlaying, events.Payload{"n": 1})
	select {
	case p := <-sub:
		if p["n"] != 1 {
			t.Fatalf("payload = %v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("fallback delivery timed out")
	}
}

func TestRedisBus_CrossNodeDelivery(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	cfg := DefaultRedisConfig()
	cfg.Addr = mr.Addr()

	busA, err := NewRedisBus(cfg, "nodeA", zerolog.Nop())
	if err != nil {
		t.Fatalf("busA: %v", err)
	}
	defer busA.Close()
	busB, err := NewRedisBus(cfg, "nodeB", zerolog.Nop())
	if err != nil {
		t.Fatalf("busB: %v", err)
	}
	defer busB.Close()

	sub := busA.Subscribe(events.EventScheduleUpdate)

	// Retry-publish from the other node until the subscriber receives it or we
	// time out; this rides over pub/sub subscription setup latency without a
	// fixed sleep, so it doesn't flake.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		busB.Publish(events.EventScheduleUpdate, events.Payload{"station_id": "st1"})
		select {
		case p := <-sub:
			if p["station_id"] != "st1" {
				t.Fatalf("cross-node payload = %v", p)
			}
			return // delivered
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatal("cross-node delivery never arrived")
}
