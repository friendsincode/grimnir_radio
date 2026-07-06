/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package broadcast

import (
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
)

// clientAddr feeds the connect/disconnect logs, so an OBS VM's real IP can be
// picked out of the churn even though the TCP peer is the edge nginx.
func TestClientAddr(t *testing.T) {
	cases := []struct{ name, xff, xreal, remote, want string }{
		{"xff single", "203.0.113.7", "", "10.0.0.1:5000", "203.0.113.7"},
		{"xff chain uses first hop", "203.0.113.7, 70.1.2.3", "", "10.0.0.1:5000", "203.0.113.7"},
		{"x-real-ip when no xff", "", "198.51.100.9", "10.0.0.1:5000", "198.51.100.9"},
		{"remoteaddr fallback", "", "", "10.0.0.1:5000", "10.0.0.1:5000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/live", nil)
			r.RemoteAddr = tc.remote
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xreal != "" {
				r.Header.Set("X-Real-IP", tc.xreal)
			}
			if got := clientAddr(r); got != tc.want {
				t.Fatalf("clientAddr = %q, want %q", got, tc.want)
			}
		})
	}
}

// A client that never drains its channel takes the skip path. Both the
// per-client counter (surfaced in the disconnect log) and the per-mount metric
// must climb, so a gapped-audio client stops being invisible.
func TestBroadcast_CountsSkippedChunksForSlowClient(t *testing.T) {
	bus := events.NewBus()
	m := NewMount("skip-metric-mount", "audio/mpeg", 128, zerolog.Nop(), bus)

	// cap 1 so the channel fills on the first chunk; nothing drains it.
	c := &client{ch: make(chan []byte, 1), done: make(chan struct{})}
	m.mu.Lock()
	m.clients[c] = struct{}{}
	m.mu.Unlock()

	before := testutil.ToFloat64(telemetry.BroadcastSkippedChunksTotal.WithLabelValues("skip-metric-mount"))

	const n = 6
	for i := 0; i < n; i++ {
		m.Broadcast([]byte("audio"))
	}

	// The first chunk fills the 1-slot channel; the remaining n-1 are skipped.
	if got := atomic.LoadInt64(&c.skipped); got != n-1 {
		t.Fatalf("client skipped = %d, want %d", got, n-1)
	}
	if delta := testutil.ToFloat64(telemetry.BroadcastSkippedChunksTotal.WithLabelValues("skip-metric-mount")) - before; delta != n-1 {
		t.Fatalf("skipped-chunks metric delta = %v, want %d", delta, n-1)
	}
}
