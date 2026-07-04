/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package events

import (
	"sync"
	"testing"
	"time"
)

// Every service in the process publishes through this bus; it had zero tests
// (#255 campaign). Writing them exposed a real defect, now fixed: Unsubscribe
// closed the channel unconditionally, so a repeated unsubscribe — or one with
// the wrong event type — panicked the whole process.

const busTestEvent = EventType("bus.test")

func TestBus_PublishSubscribeRoundTrip(t *testing.T) {
	b := NewBus()
	sub := b.Subscribe(busTestEvent)

	b.Publish(busTestEvent, Payload{"k": "v"})
	select {
	case p := <-sub:
		if p["k"] != "v" {
			t.Errorf("payload = %v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("no delivery")
	}

	// Different event type: not delivered.
	b.Publish(EventType("other.event"), Payload{"x": 1})
	select {
	case p := <-sub:
		t.Fatalf("cross-type delivery: %v", p)
	default:
	}
}

func TestBus_MultiSubscriberFanOut(t *testing.T) {
	b := NewBus()
	sub1 := b.Subscribe(busTestEvent)
	sub2 := b.Subscribe(busTestEvent)

	b.Publish(busTestEvent, Payload{"n": 1})
	for i, sub := range []Subscriber{sub1, sub2} {
		select {
		case <-sub:
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d missed the event", i+1)
		}
	}
}

func TestBus_SlowSubscriberDropsInsteadOfBlocking(t *testing.T) {
	b := NewBus()
	sub := b.Subscribe(busTestEvent) // capacity 8, never drained

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			b.Publish(busTestEvent, Payload{"i": i})
		}
		close(done)
	}()
	select {
	case <-done:
		// Publish never blocked despite a full channel.
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
	if n := len(sub); n != 8 {
		t.Errorf("buffered = %d, want exactly the channel capacity 8", n)
	}
}

func TestBus_UnsubscribeStopsDeliveryAndClosesOnce(t *testing.T) {
	b := NewBus()
	sub1 := b.Subscribe(busTestEvent)
	sub2 := b.Subscribe(busTestEvent)

	b.Unsubscribe(busTestEvent, sub1)

	// sub1's channel is closed exactly once...
	if _, open := <-sub1; open {
		t.Error("unsubscribed channel not closed")
	}
	// ...and a second unsubscribe is a no-op, not a panic (the old behavior
	// closed unconditionally & crashed the process).
	b.Unsubscribe(busTestEvent, sub1)
	// Wrong event type: also a no-op, & must not close sub2.
	b.Unsubscribe(EventType("wrong.type"), sub2)

	b.Publish(busTestEvent, Payload{"still": "works"})
	select {
	case p, open := <-sub2:
		if !open {
			t.Fatal("survivor channel was closed by a wrong-type unsubscribe")
		}
		if p["still"] != "works" {
			t.Errorf("payload = %v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("survivor missed the event")
	}
}

func TestBus_ConcurrentPublishSubscribeUnsubscribe(t *testing.T) {
	b := NewBus()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Publishers.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					b.Publish(busTestEvent, Payload{"x": 1})
				}
			}
		}()
	}
	// Subscriber churn.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				sub := b.Subscribe(busTestEvent)
				b.Unsubscribe(busTestEvent, sub)
			}
		}()
	}

	deadline := time.After(3 * time.Second)
	churnDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(churnDone)
	}()
	// Stop publishers once churners finish; -race validates the whole dance.
	go func() {
		time.Sleep(500 * time.Millisecond)
		close(stop)
	}()
	select {
	case <-churnDone:
	case <-deadline:
		t.Fatal("concurrent churn deadlocked")
	}
}
