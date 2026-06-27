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

	if !waitFor(t, 2*time.Second, func() bool { return m.ClientCount() == 1 }) {
		t.Fatalf("client never registered; ClientCount=%d", m.ClientCount())
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

	if !waitFor(t, 5*time.Second, func() bool { return m.ClientCount() == 0 }) {
		t.Fatalf("stalled client was not disconnected; ClientCount=%d", m.ClientCount())
	}
}
