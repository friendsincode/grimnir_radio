/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package broadcast

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/rs/zerolog"
)

// A silent MP3 frame must carry a valid MPEG-1 Layer III header and the CBR
// length for its bitrate, and be all zeroes past the 4-byte header so it decodes
// to digital silence.
func TestMP3SilenceFrame(t *testing.T) {
	cases := []struct {
		bitrate   int
		wantLen   int
		wantByte2 byte
	}{
		{128, 417, 0x90}, // 144*128000/44100 = 417, bitrate index 9
		{64, 208, 0x50},  // 144*64000/44100 = 208, bitrate index 5
	}
	for _, tc := range cases {
		f := mp3SilenceFrame("audio/mpeg", tc.bitrate)
		if len(f) != tc.wantLen {
			t.Fatalf("bitrate %d: frame len = %d, want %d", tc.bitrate, len(f), tc.wantLen)
		}
		if f[0] != 0xFF || f[1] != 0xFB {
			t.Fatalf("bitrate %d: bad sync/header %02X %02X", tc.bitrate, f[0], f[1])
		}
		if f[2] != tc.wantByte2 {
			t.Fatalf("bitrate %d: byte2 = %02X, want %02X", tc.bitrate, f[2], tc.wantByte2)
		}
		for i := 4; i < len(f); i++ {
			if f[i] != 0 {
				t.Fatalf("bitrate %d: payload byte %d = %02X, want 0 (silence)", tc.bitrate, i, f[i])
			}
		}
	}
}

// Non-MP3 codecs and non-standard bitrates get no frame, which disables the
// bridge for those mounts rather than emitting bytes a decoder can't parse.
func TestMP3SilenceFrame_Unsupported(t *testing.T) {
	for _, tc := range []struct {
		ct      string
		bitrate int
	}{
		{"audio/aac", 128},
		{"audio/ogg", 128},
		{"audio/mpeg", 130}, // not a standard MP3 bitrate
	} {
		if f := mp3SilenceFrame(tc.ct, tc.bitrate); f != nil {
			t.Fatalf("mp3SilenceFrame(%q, %d) = %d bytes, want nil", tc.ct, tc.bitrate, len(f))
		}
	}
}

// When a live feed ends and no new feed attaches, the bridge must keep the
// outgoing stream flowing so a connected puller is never starved into dropping.
func TestMount_SilenceBridge_FillsGap(t *testing.T) {
	origGrace, origPoll := bridgeGrace, bridgePoll
	bridgeGrace, bridgePoll = 40*time.Millisecond, 10*time.Millisecond
	defer func() { bridgeGrace, bridgePoll = origGrace, origPoll }()

	bus := events.NewBus()
	m := NewMount("test", "audio/mpeg", 128, zerolog.Nop(), bus)
	defer m.Close()

	// One real feed that ends immediately (EOF), which sets lastFedAt and then
	// drops inputCount to 0: the exact seam between two tracks. This also starts
	// the bridge goroutine (first FeedFrom).
	if err := m.FeedFrom(newByteReader(m.silentFrame)); err != io.EOF {
		t.Fatalf("FeedFrom returned %v, want io.EOF", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(m.ServeHTTP))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/live")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	var got int64
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			atomic.AddInt64(&got, int64(n))
			if err != nil {
				return
			}
		}
	}()

	// After the grace, the bridge should be feeding silence. Sample the byte
	// count across a window that has no live feed at all, and require it to keep
	// climbing: that only happens if the bridge is emitting.
	if !waitFor(t, 2*time.Second, func() bool { return atomic.LoadInt64(&got) > 0 }) {
		t.Fatal("client received no data during input gap; bridge did not fill it")
	}
	mid := atomic.LoadInt64(&got)
	time.Sleep(200 * time.Millisecond)
	end := atomic.LoadInt64(&got)
	if end <= mid {
		t.Fatalf("byte count did not advance during gap (mid=%d end=%d); bridge stalled", mid, end)
	}
}

// While a live feed is attached the bridge must stay silent: inputCount > 0
// means real audio is flowing and injecting frames would double it.
func TestMount_SilenceBridge_IdleWhileFeeding(t *testing.T) {
	origGrace, origPoll := bridgeGrace, bridgePoll
	bridgeGrace, bridgePoll = 20*time.Millisecond, 10*time.Millisecond
	defer func() { bridgeGrace, bridgePoll = origGrace, origPoll }()

	bus := events.NewBus()
	m := NewMount("test", "audio/mpeg", 128, zerolog.Nop(), bus)
	defer m.Close()

	// Hold a feed open (pipe never closed) so inputCount stays 1, and register a
	// client so the only gate left is inputCount.
	pr, pw := io.Pipe()
	defer pw.Close()
	go func() { _ = m.FeedFrom(pr) }()
	if !waitFor(t, time.Second, func() bool { return m.inputActive() }) {
		t.Fatal("feed never registered")
	}
	if _, err := pw.Write(m.silentFrame); err != nil {
		t.Fatalf("prime feed: %v", err)
	}

	m.mu.Lock()
	c := &client{ch: make(chan []byte, 256), done: make(chan struct{})}
	m.clients[c] = struct{}{}
	m.mu.Unlock()

	// inGap must report false the whole time a feed is attached.
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		if m.inGap() {
			t.Fatal("inGap true while a live feed is attached")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// inputActive reports whether any feed is currently attached. Test helper.
func (m *Mount) inputActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.inputCount > 0
}

// newByteReader returns a reader that yields p once then io.EOF.
func newByteReader(p []byte) io.Reader {
	return &sliceReader{data: p}
}

type sliceReader struct {
	data []byte
	pos  int
}

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
