/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package events

import "testing"

func TestBus_PublishDeliversToSubscribers(t *testing.T) {
	b := NewBus()
	s1 := b.Subscribe(EventNowPlaying)
	s2 := b.Subscribe(EventNowPlaying)

	b.Publish(EventNowPlaying, Payload{"track": "Highway Star"})

	for i, sub := range []Subscriber{s1, s2} {
		select {
		case p := <-sub:
			if p["track"] != "Highway Star" {
				t.Fatalf("subscriber %d got %v", i, p)
			}
		default:
			t.Fatalf("subscriber %d received nothing", i)
		}
	}
}

func TestBus_PublishToOtherTypeNotDelivered(t *testing.T) {
	b := NewBus()
	sub := b.Subscribe(EventDJConnect)
	b.Publish(EventDJDisconnect, Payload{"x": 1}) // different type
	select {
	case <-sub:
		t.Fatal("subscriber should not receive an unrelated event type")
	default:
	}
}

func TestBus_PublishDropsWhenBufferFull(t *testing.T) {
	b := NewBus()
	sub := b.Subscribe(EventHealth)
	// Buffer is 8; publishing 20 must not block and must drop the overflow.
	for i := 0; i < 20; i++ {
		b.Publish(EventHealth, Payload{"i": i})
	}
	if got := len(sub); got != 8 {
		t.Fatalf("buffered = %d, want 8 (overflow dropped)", got)
	}
}

func TestBus_Unsubscribe(t *testing.T) {
	b := NewBus()
	sub := b.Subscribe(EventShowStart)
	b.Unsubscribe(EventShowStart, sub)

	// Channel is closed on unsubscribe.
	if _, open := <-sub; open {
		t.Fatal("unsubscribed channel should be closed")
	}
	// Publishing after unsubscribe reaches nobody (no panic on closed chan).
	b.Publish(EventShowStart, Payload{"x": 1})
}
