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

// Circuit-breaker & malformed-message paths for the Redis bus.

func TestRedisBus_CircuitBreakerOpensAfterMaxFailures(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := DefaultRedisConfig()
	cfg.Addr = mr.Addr()
	cfg.MaxFailures = 3
	rb, err := NewRedisBus(cfg, "node-a", zerolog.Nop())
	if err != nil {
		t.Fatalf("NewRedisBus: %v", err)
	}
	defer func() { _ = rb.Close() }()

	for i := 0; i < 3; i++ {
		if rb.useFallback {
			t.Fatalf("circuit opened after %d failures, threshold is 3", i)
		}
		rb.handleFailure()
	}
	if !rb.useFallback {
		t.Fatal("circuit did not open at the failure threshold")
	}

	// Local delivery keeps working with the circuit open.
	sub := rb.Subscribe(testEvent)
	rb.Publish(testEvent, events.Payload{"circuit": "open"})
	if got := recvOne(t, sub, time.Second); got["circuit"] != "open" {
		t.Errorf("payload = %v", got)
	}
}

func TestRedisBus_TryReconnectClosesCircuit(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := DefaultRedisConfig()
	cfg.Addr = mr.Addr()
	cfg.MaxFailures = 1
	rb, err := NewRedisBus(cfg, "node-a", zerolog.Nop())
	if err != nil {
		t.Fatalf("NewRedisBus: %v", err)
	}
	defer func() { _ = rb.Close() }()

	rb.handleFailure() // opens the circuit at threshold 1, closes the client

	// Too soon: the retry interval gates reconnection attempts.
	if err := rb.tryReconnect(); err == nil {
		t.Fatal("tryReconnect succeeded inside the retry interval")
	}

	// Age the last check past the interval; handleFailure closed the client,
	// so reconnect must re-ping through a fresh connection state. miniredis
	// is still up, so this should close the circuit.
	rb.mu.Lock()
	rb.lastCheck = time.Now().Add(-time.Minute)
	rb.mu.Unlock()
	if err := rb.tryReconnect(); err != nil {
		t.Fatalf("tryReconnect with live redis: %v", err)
	}
	if rb.useFallback {
		t.Fatal("circuit still open after successful reconnect")
	}
}

func TestRedisBus_TryReconnectNoopWhenHealthy(t *testing.T) {
	mr := miniredis.RunT(t)
	rb := newTestRedisBus(t, mr, "node-a")
	if err := rb.tryReconnect(); err != nil {
		t.Fatalf("tryReconnect on a healthy bus should be a no-op, got %v", err)
	}
}

func TestRedisBus_MalformedRemoteMessageIsSkipped(t *testing.T) {
	mr := miniredis.RunT(t)
	rb := newTestRedisBus(t, mr, "node-a")
	b := newTestRedisBus(t, mr, "node-b")

	sub := rb.Subscribe(testEvent)
	time.Sleep(50 * time.Millisecond)

	// Garbage straight onto the raw channel: the receiver must log & continue,
	// not die — the next well-formed message still arrives.
	if err := b.client.Publish(b.ctx, string(testEvent), "not json at all").Err(); err != nil {
		t.Fatalf("raw publish: %v", err)
	}
	b.Publish(testEvent, events.Payload{"after": "garbage"})

	if got := recvOne(t, sub, 2*time.Second); got["after"] != "garbage" {
		t.Errorf("payload = %v (receiver died on malformed message?)", got)
	}
}

func TestUnmarshalMessage_Malformed(t *testing.T) {
	if _, err := unmarshalMessage([]byte("{broken")); err == nil {
		t.Fatal("unmarshalMessage accepted malformed JSON")
	}
	if _, err := unmarshalNATSMessage([]byte("{broken")); err == nil {
		t.Fatal("unmarshalNATSMessage accepted malformed JSON")
	}
}

func TestSanitizeConsumerName(t *testing.T) {
	cases := map[string]string{
		"node-a-schedule.update": "node-a-schedule_update",
		"node-a-listener.stats":  "node-a-listener_stats",
		"plain":                  "plain",
		"wild*card>and space":    "wild_card_and_space",
	}
	for in, want := range cases {
		if got := sanitizeConsumerName(in); got != want {
			t.Errorf("sanitizeConsumerName(%q) = %q, want %q", in, got, want)
		}
	}
}
