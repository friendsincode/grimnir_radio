/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package broadcast

import (
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
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

// When a live feed ends with the stream behind its realtime budget, the bridge
// must keep the outgoing stream flowing so a connected puller is never starved
// into dropping.
func TestMount_SilenceBridge_FillsGap(t *testing.T) {
	origGrace, origPoll := bridgeGrace, bridgePoll
	bridgeGrace, bridgePoll = 40*time.Millisecond, 10*time.Millisecond
	defer func() { bridgeGrace, bridgePoll = origGrace, origPoll }()

	bus := events.NewBus()
	m := NewMount("test", "audio/mpeg", 128, zerolog.Nop(), bus)
	defer m.Close()

	// One real feed that ends immediately (EOF), which sets lastFedAt and then
	// drops inputCount to 0: the exact seam between two tracks. This also starts
	// the bridge goroutine (first FeedFrom). The feed is one frame, far below
	// wall-clock realtime, so the mount is behind budget and the bridge must run.
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

// The bridge must pace against the realtime budget: over a window it may not
// emit meaningfully more than bitrate. The v1.40.19 bridge had no such bound
// tied to a wall-clock budget, which let stream delivery run ahead of realtime
// and accumulate client backlog (the prod regression).
func TestMount_SilenceBridge_PacesAtRealtime(t *testing.T) {
	origGrace, origPoll := bridgeGrace, bridgePoll
	bridgeGrace, bridgePoll = 20*time.Millisecond, 10*time.Millisecond
	defer func() { bridgeGrace, bridgePoll = origGrace, origPoll }()

	bus := events.NewBus()
	m := NewMount("test", "audio/mpeg", 128, zerolog.Nop(), bus)
	defer m.Close()

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

	if !waitFor(t, 2*time.Second, func() bool { return atomic.LoadInt64(&got) > 0 }) {
		t.Fatal("bridge never started emitting")
	}
	start := atomic.LoadInt64(&got)
	const window = 500 * time.Millisecond
	time.Sleep(window)
	emitted := atomic.LoadInt64(&got) - start
	// 128 kbps = 16000 B/s -> 8000 B in 500ms. Allow 2x for poll granularity,
	// slack catch-up, and scheduling jitter; the broken behaviour (unbounded
	// ahead-of-realtime emission) blows well past this.
	if maxBytes := int64(2 * 16000 * window.Seconds()); emitted > maxBytes {
		t.Fatalf("bridge emitted %d bytes in %v, want <= %d (realtime-ish)", emitted, window, maxBytes)
	}
}

// A mount that is AHEAD of its realtime budget (bytes were delivered faster
// than wall clock, e.g. an encoder burst at a track boundary) must NOT be
// bridged: the gap is the drain window where paced clients catch up. Silence
// starts only once the budget overtakes what was already sent.
func TestMount_SilenceBridge_HoldsWhileAheadThenDrains(t *testing.T) {
	origGrace, origPoll := bridgeGrace, bridgePoll
	bridgeGrace, bridgePoll = 20*time.Millisecond, 10*time.Millisecond
	defer func() { bridgeGrace, bridgePoll = origGrace, origPoll }()

	bus := events.NewBus()
	m := NewMount("test", "audio/mpeg", 128, zerolog.Nop(), bus)
	defer m.Close()

	// Register a bare client directly on the mount instead of going through
	// ServeHTTP: the HTTP path's connect pre-roll can double-deliver a chunk
	// that lands between client registration and the ring-buffer read, which
	// would contaminate the byte accounting this test depends on. Reading
	// c.ch observes exactly what Broadcast delivered, nothing else.
	c := &client{ch: make(chan []byte, 256), done: make(chan struct{})}
	m.mu.Lock()
	m.clients[c] = struct{}{}
	m.mu.Unlock()

	drain := func() int64 {
		var n int64
		for {
			select {
			case data := <-c.ch:
				n += int64(len(data))
			default:
				return n
			}
		}
	}

	// Burst ~700ms worth of audio (11200 B at 16000 B/s) through a real feed in
	// one go: the mount is now ~700ms ahead of realtime, like a boundary burst.
	burst := make([]byte, 11200)
	if err := m.FeedFrom(newByteReader(burst)); err != io.EOF {
		t.Fatalf("FeedFrom returned %v, want io.EOF", err)
	}
	if got := drain(); got != int64(len(burst)) {
		t.Fatalf("client received %d bytes of the burst, want %d", got, len(burst))
	}

	// For the next ~200ms the mount is still ahead of budget; the bridge must
	// hold off even though inputCount is 0, a client is attached, and the grace
	// (20ms) has long expired. Allow one frame of slop for poll-edge timing.
	time.Sleep(200 * time.Millisecond)
	if over := drain(); over > int64(len(m.silentFrame)) {
		t.Fatalf("bridge emitted %d bytes while mount was ahead of realtime budget", over)
	}

	// Once wall clock catches up with the burst (~700ms in), the deficit
	// returns and the bridge must start filling again.
	var resumed int64
	if !waitFor(t, 3*time.Second, func() bool {
		resumed += drain()
		return resumed > int64(len(m.silentFrame))
	}) {
		t.Fatal("bridge never resumed after the surplus drained")
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

// A client connecting while chunks are broadcasting must receive a clean
// splice: every chunk lands either wholly in the ring-buffer preroll or wholly
// in the live channel, never both (double delivery) and never neither (a lost
// chunk). The old connect seam registered the client and THEN snapshotted the
// ring outside any common lock, so a chunk arriving in between was delivered
// twice: once in the preroll, once through the channel.
func TestMount_AttachClient_NoDoubleDeliveryAtConnectSeam(t *testing.T) {
	bus := events.NewBus()
	m := NewMount("test", "audio/mpeg", 128, zerolog.Nop(), bus)
	defer m.Close()

	// Broadcast 4-byte big-endian sequence numbers in a tight loop. Chunk size
	// divides the ring size (80000 at 128kbps), so chunk boundaries survive the
	// ring wrap and the preroll tail is always a whole chunk.
	var seq uint32
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4)
		for {
			select {
			case <-stop:
				return
			default:
			}
			binary.BigEndian.PutUint32(buf, atomic.AddUint32(&seq, 1))
			m.Broadcast(append([]byte(nil), buf...))
			time.Sleep(50 * time.Microsecond)
		}
	}()
	defer func() { close(stop); wg.Wait() }()

	// Let the ring accumulate more than the preroll we'll ask for, so the
	// snapshot holds only real numbered chunks.
	const prerollBytes = 400
	if !waitFor(t, 2*time.Second, func() bool { return atomic.LoadUint32(&seq) > prerollBytes }) {
		t.Fatal("broadcaster never filled the ring")
	}

	for i := 0; i < 300; i++ {
		c := &client{ch: make(chan []byte, 4096), done: make(chan struct{})}
		preroll, _ := m.attachClient(c, prerollBytes)
		if len(preroll) < 4 {
			t.Fatalf("iteration %d: preroll too short: %d bytes", i, len(preroll))
		}
		tail := binary.BigEndian.Uint32(preroll[len(preroll)-4:])

		var first uint32
		select {
		case data := <-c.ch:
			first = binary.BigEndian.Uint32(data)
		case <-time.After(time.Second):
			t.Fatalf("iteration %d: no live chunk after attach", i)
		}

		if first <= tail {
			t.Fatalf("iteration %d: connect seam double-delivered: preroll tail seq %d, first live seq %d", i, tail, first)
		}
		if first != tail+1 {
			t.Fatalf("iteration %d: chunk lost at connect seam: preroll tail seq %d, first live seq %d", i, tail, first)
		}

		m.mu.Lock()
		delete(m.clients, c)
		m.mu.Unlock()
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
