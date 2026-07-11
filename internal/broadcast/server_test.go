/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package broadcast

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/rs/zerolog"
)

// waitFor polls cond until it is true or the deadline passes.
func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

func TestMount_BytesReceivedAt_ZeroBeforeFeed(t *testing.T) {
	bus := events.NewBus()
	m := NewMount("test", "audio/mpeg", 128, zerolog.Nop(), bus)
	got := m.BytesReceivedAt()
	if !got.IsZero() {
		t.Errorf("BytesReceivedAt() = %v, want zero time before any feed", got)
	}
}

func TestMount_BytesReceivedAt_UpdatedAfterFeed(t *testing.T) {
	bus := events.NewBus()
	m := NewMount("test", "audio/mpeg", 128, zerolog.Nop(), bus)

	before := time.Now()
	// FeedFrom reads until EOF; give it a small payload
	r := bytes.NewReader(make([]byte, 1024))
	_ = m.FeedFrom(r) // returns io.EOF

	got := m.BytesReceivedAt()
	if got.IsZero() {
		t.Fatal("BytesReceivedAt() is zero after FeedFrom wrote bytes")
	}
	if got.Before(before) {
		t.Errorf("BytesReceivedAt() = %v, want after %v", got, before)
	}
}

// TestMount_StalledClientIsDisconnected proves the zombie fix: a client that
// stops reading (half-open) is dropped from the count within the write-deadline
// window, instead of lingering until the kernel TCP timeout (~15 min). See #18.
func TestMount_StalledClientIsDisconnected(t *testing.T) {
	orig := writeTimeout
	writeTimeout = 200 * time.Millisecond
	defer func() { writeTimeout = orig }()

	bus := events.NewBus()
	m := NewMount("test", "audio/mpeg", 128, zerolog.Nop(), bus)

	srv := httptest.NewServer(http.HandlerFunc(m.ServeHTTP))
	defer srv.Close()

	// Dial raw so we can read the response start, then stop reading to simulate
	// a stalled listener whose socket buffers fill while audio keeps flowing.
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetReadBuffer(2048) // small receive window so writes block sooner
	}

	if _, err := conn.Write([]byte("GET /live HTTP/1.1\r\nHost: x\r\n\r\n")); err != nil {
		t.Fatalf("request: %v", err)
	}
	// Read the headers + a little body, then never read again.
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = conn.Read(make([]byte, 1024))

	// Registration is the raw connection view: the stalled client never
	// establishes, so ClientCount (established listeners) stays 0 for it.
	if !waitFor(t, 2*time.Second, func() bool { return m.ConnectionCount() == 1 }) {
		t.Fatalf("client never registered; ConnectionCount=%d", m.ConnectionCount())
	}

	// Keep feeding audio so the serve loop keeps writing into the stalled pipe.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		chunk := make([]byte, 64*1024)
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				m.Broadcast(chunk)
			}
		}
	}()

	if !waitFor(t, 5*time.Second, func() bool { return m.ConnectionCount() == 0 }) {
		t.Fatalf("stalled client was not disconnected; ConnectionCount=%d", m.ConnectionCount())
	}
}

// A connection only rolls into the listener count after draining the
// establishment threshold, and rolls back out on disconnect. A connection
// that grabs the stream and parks never counts at all (issue #18).
func TestMount_ClientCountsOnlyAfterEstablishment(t *testing.T) {
	bus := events.NewBus()
	// bitrate 1 kbps -> threshold = 1*1000/8*10 = 1250 bytes, so the test
	// crosses it with a couple of small broadcasts.
	m := NewMount("test", "audio/mpeg", 1, zerolog.Nop(), bus)

	srv := httptest.NewServer(http.HandlerFunc(m.ServeHTTP))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/live")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	if !waitFor(t, 2*time.Second, func() bool { return m.ConnectionCount() == 1 }) {
		t.Fatalf("client never registered; ConnectionCount=%d", m.ConnectionCount())
	}
	if got := m.ClientCount(); got != 0 {
		t.Fatalf("unestablished connection already counted: ClientCount=%d", got)
	}

	// Drive audio through and drain it client-side until the threshold trips.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		chunk := make([]byte, 512)
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				m.Broadcast(chunk)
			}
		}
	}()
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := resp.Body.Read(buf); err != nil {
				return
			}
		}
	}()

	if !waitFor(t, 5*time.Second, func() bool { return m.ClientCount() == 1 }) {
		t.Fatalf("client never established; ClientCount=%d ConnectionCount=%d", m.ClientCount(), m.ConnectionCount())
	}

	// Disconnect rolls the counter back.
	resp.Body.Close()
	if !waitFor(t, 5*time.Second, func() bool { return m.ClientCount() == 0 && m.ConnectionCount() == 0 }) {
		t.Fatalf("counts did not roll back; ClientCount=%d ConnectionCount=%d", m.ClientCount(), m.ConnectionCount())
	}
}

// Listener stats events fire on establishment and established-disconnect only.
// An unestablished connect/disconnect cycle must be silent: before this gate,
// every recycled browser connection published a connect event and inflated
// grimnir_listeners_current.
func TestMount_ListenerStatsOnlyForEstablished(t *testing.T) {
	bus := events.NewBus()
	sub := bus.Subscribe(events.EventListenerStats)
	defer bus.Unsubscribe(events.EventListenerStats, sub)

	m := NewMount("test", "audio/mpeg", 1, zerolog.Nop(), bus)
	srv := httptest.NewServer(http.HandlerFunc(m.ServeHTTP))
	defer srv.Close()

	// Cycle 1: connect and immediately drop, never establishing.
	resp, err := http.Get(srv.URL + "/live")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return m.ConnectionCount() == 1 }) {
		t.Fatal("client never registered")
	}
	resp.Body.Close()
	// Unregister happens when the serve goroutine sees r.Context() cancel. With
	// no audio flowing this cycle, there's no write to fail against, so detection
	// rides on server-goroutine scheduling and runs slow under CI load. Match the
	// 5s rollback bound the other unestablished-close checks use (line 117).
	if !waitFor(t, 5*time.Second, func() bool { return m.ConnectionCount() == 0 }) {
		t.Fatal("client never unregistered")
	}
	select {
	case ev := <-sub:
		t.Fatalf("unestablished connection published a stats event: %+v", ev)
	case <-time.After(200 * time.Millisecond):
		// Correct: silence.
	}

	// Cycle 2: connect, drain past the threshold, then drop.
	resp2, err := http.Get(srv.URL + "/live")
	if err != nil {
		t.Fatalf("connect 2: %v", err)
	}
	defer resp2.Body.Close()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		chunk := make([]byte, 512)
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				m.Broadcast(chunk)
			}
		}
	}()
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := resp2.Body.Read(buf); err != nil {
				return
			}
		}
	}()

	var got events.Payload
	select {
	case ev := <-sub:
		got = ev
	case <-time.After(5 * time.Second):
		t.Fatal("no connect stats event after establishment")
	}
	if got["event"] != "connect" {
		t.Fatalf("first stats event = %v, want connect", got["event"])
	}
	if got["listeners"] != 1 {
		t.Fatalf("connect event listeners = %v, want 1", got["listeners"])
	}
}
