/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package broadcast provides a simple audio broadcast server that receives
// audio from GStreamer and serves it to multiple HTTP clients.
package broadcast

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/telemetry"
	"github.com/rs/zerolog"
)

// writeTimeout bounds how long a single write or flush to a client may block
// before we disconnect it. A live stream pushes audio continuously, so a
// healthy client's kernel send buffer drains well within this window. The value
// is set for the clients that stall legitimately: OBS restream VMs whose read
// pauses while they re-encode toward YouTube, and cellular listeners (a T-Mobile
// handoff or coverage dip stalls the flow for 10-30s, then recovers). At the old
// 10s a normal cellular hiccup dropped a listener who would have come back, so
// this sits at 30s. A truly dead or half-open socket is still reaped here rather
// than lingering until the kernel TCP retransmit timeout (~15 min), and the
// establishment counter (establishSeconds) keeps such sockets out of the listener
// count regardless of this value. Must stay below the keepalive interval (45s),
// which in turn stays below the server IdleTimeout (60s).
// It is a var, not a const, only so tests can shorten it.
var writeTimeout = 30 * time.Second

// Mount represents a single audio stream mount point.
type Mount struct {
	Name        string
	ContentType string
	Bitrate     int

	mu         sync.RWMutex
	clients    map[*client]struct{}
	buffer     *ringBuffer
	logger     zerolog.Logger
	inputDone  chan struct{}
	inputCount int         // tracks active input feeds
	lastFedAt  int64       // unix nano; updated atomically each time bytes are written
	bus        *events.Bus // for publishing listener stats

	// establishedCount tracks clients that have proven themselves by draining
	// establishSeconds of audio. Only established clients roll into
	// ClientCount/TotalListeners/listener stats; a connection that grabs the
	// pre-roll and parks (recycled browser <audio> elements, proxy-held
	// sockets) never counts. Guarded by mu.
	establishedCount int

	// silentFrame is one CBR MP3 frame of digital silence sized to this mount's
	// bitrate, or nil for a codec/bitrate the silence bridge can't encode.
	// runSilenceBridge emits it across input gaps (a track/program transition
	// stops one pipeline before the next attaches) so a downstream puller (OBS
	// restream to YouTube) never sees a stall long enough to drop — but only
	// while the mount is behind its wall-clock realtime budget; see bytesOut.
	silentFrame []byte
	bridgeStop  chan struct{}
	bridgeStart sync.Once // starts the bridge goroutine on first real feed
	bridgeEnd   sync.Once // closes bridgeStop exactly once

	// Bridge tuning, captured from the package vars at NewMount so the bridge
	// goroutine never reads the mutable globals (tests shorten those, and a
	// goroutine from an earlier test still draining would race the write).
	bridgeGraceD time.Duration
	bridgePollD  time.Duration
	bridgeSlackD time.Duration

	// bytesOut counts every byte handed to Broadcast (real feed and bridge
	// silence alike). The bridge compares it against a wall-clock realtime
	// budget: silence flows only while delivery is BEHIND realtime. This is
	// what the v1.40.19 bridge lacked — it filled every gap at full bitrate,
	// which removed the drain windows paced clients (OBS restreams) used to
	// catch up after per-boundary bursts, so their backlog compounded until the
	// 256-chunk send channel overflowed and the connection reset. Atomic.
	bytesOut int64
}

// Silence-bridge tuning. A gap shorter than bridgeGrace is left alone: client
// kernel/app buffers absorb it, and inserting silence on every fast track
// change would be needless. bridgePoll is how often the bridge advances its
// budget and checks for a gap. bridgeMaxSlack bounds how far the budget
// accountant may drift from bytesOut in either direction, so hours of small
// encoder clock drift can't build unbounded credit or debt. All three are
// vars, not consts, only so tests can shorten them.
var (
	bridgeGrace    = 300 * time.Millisecond
	bridgePoll     = 50 * time.Millisecond
	bridgeMaxSlack = 4 * time.Second
)

// mpeg1L3BitrateIndex maps a CBR MP3 bitrate (kbps) to its MPEG-1 Layer III
// bitrate index. Only standard MPEG-1 values are listed; anything else
// disables the bridge for that mount.
var mpeg1L3BitrateIndex = map[int]byte{
	32: 1, 40: 2, 48: 3, 56: 4, 64: 5, 80: 6, 96: 7, 112: 8,
	128: 9, 160: 10, 192: 11, 224: 12, 256: 13, 320: 14,
}

// mp3SilenceFrame builds one CBR MPEG-1 Layer III frame whose payload is all
// zeroes. A zeroed main_data section decodes to digital silence in lame,
// ffmpeg, and mpg123, and CBR silent frames concatenate seamlessly, so a
// stream of these is valid, silent audio. It assumes 44.1 kHz, which every
// station mount runs (director sets SampleRate: 44100). Returns nil for any
// content type or bitrate it can't encode (AAC/OGG mounts, non-standard
// bitrates), which leaves those mounts on the prior behaviour with no bridge.
func mp3SilenceFrame(contentType string, bitrateKbps int) []byte {
	if !strings.Contains(contentType, "mpeg") {
		return nil
	}
	brIndex, ok := mpeg1L3BitrateIndex[bitrateKbps]
	if !ok {
		return nil
	}
	const sampleRate = 44100 // MPEG-1 sample-rate index 0
	// MPEG-1 Layer III carries 1152 samples per frame; CBR length, no padding.
	frameLen := 144 * bitrateKbps * 1000 / sampleRate
	if frameLen < 4 {
		return nil
	}
	frame := make([]byte, frameLen)
	frame[0] = 0xFF              // frame sync (8 bits)
	frame[1] = 0xFB              // sync (3) + MPEG-1 (11) + Layer III (01) + no CRC (1)
	frame[2] = brIndex<<4 | 0x00 // bitrate index, sample-rate index 0 (44.1k), no padding/private
	frame[3] = 0x00              // stereo, no mode ext, not copyright/original, no emphasis
	return frame
}

// establishSeconds of continuously delivered audio marks a connection as an
// actual listener. The threshold sits well past what a socket's kernel send
// buffer plus the initial pre-roll (max 64KB, ~4s at 128kbps) can absorb
// without anyone draining the far end, so a connection that counts has been
// pulling real audio for real time.
const establishSeconds = 10

// establishThresholdBytes converts establishSeconds to a byte count at this
// mount's bitrate (kbps). Zero/unset bitrate assumes 128.
func (m *Mount) establishThresholdBytes() int {
	br := m.Bitrate
	if br <= 0 {
		br = 128
	}
	return (br * 1000 / 8) * establishSeconds
}

// BytesReceivedAt returns the time bytes were last written to this mount by FeedFrom.
// Returns zero time if no bytes have been written yet.
func (m *Mount) BytesReceivedAt() time.Time {
	ns := atomic.LoadInt64(&m.lastFedAt)
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

type client struct {
	ch     chan []byte
	done   chan struct{}
	closed bool
	mu     sync.Mutex

	// skipped counts chunks dropped for this client because ch was full (it was
	// reading below realtime). Incremented by Broadcast under c.mu, read at
	// disconnect. A high value on a long-lived connection is the fingerprint of a
	// slow pull client (OBS restream, cellular) getting gapped audio.
	skipped int64

	// remoteAddr and userAgent identify the connection in the connect/disconnect
	// logs so a specific client (an OBS VM's IP, say) can be picked out of the churn.
	remoteAddr string
	userAgent  string

	// established flips once the serve loop has delivered the establishment
	// threshold to this connection. Written by the serve goroutine and read by
	// the disconnect path, both under the mount's mu.
	established bool
}

// ringBuffer holds recent audio data for new clients to start with.
type ringBuffer struct {
	data []byte
	size int
	pos  int
	mu   sync.RWMutex
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{
		data: make([]byte, size),
		size: size,
	}
}

func (rb *ringBuffer) Write(p []byte) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	for _, b := range p {
		rb.data[rb.pos] = b
		rb.pos = (rb.pos + 1) % rb.size
	}
}

func (rb *ringBuffer) GetRecent(n int) []byte {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if n > rb.size {
		n = rb.size
	}

	result := make([]byte, n)
	start := (rb.pos - n + rb.size) % rb.size

	for i := 0; i < n; i++ {
		result[i] = rb.data[(start+i)%rb.size]
	}
	return result
}

func (rb *ringBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	// Zero out the buffer and reset position
	for i := range rb.data {
		rb.data[i] = 0
	}
	rb.pos = 0
}

// NewMount creates a new broadcast mount point.
func NewMount(name, contentType string, bitrate int, logger zerolog.Logger, bus *events.Bus) *Mount {
	// Buffer 5 seconds of audio for new clients at the stream's bitrate
	// Larger buffer helps with connection stability and quick starts
	// At 128kbps: 128000 bits/sec = 16000 bytes/sec, so 80000 bytes = 5 seconds
	// At 64kbps: 64000 bits/sec = 8000 bytes/sec, so 40000 bytes = 5 seconds
	bufferSize := (bitrate * 1000 / 8) * 5
	if bufferSize < 20000 {
		bufferSize = 20000 // Minimum ~2.5 seconds at 64kbps
	}

	return &Mount{
		Name:        name,
		ContentType: contentType,
		Bitrate:     bitrate,
		clients:     make(map[*client]struct{}),
		buffer:      newRingBuffer(bufferSize),
		logger:      logger.With().Str("mount", name).Logger(),
		inputDone:   make(chan struct{}),
		bus:         bus,
		silentFrame:  mp3SilenceFrame(contentType, bitrate),
		bridgeStop:   make(chan struct{}),
		bridgeGraceD: bridgeGrace,
		bridgePollD:  bridgePoll,
		bridgeSlackD: bridgeMaxSlack,
	}
}

// clientAddr returns the best client identifier for logging. Behind the edge
// nginx the TCP peer is the proxy, so prefer the forwarded headers (first hop of
// X-Forwarded-For, then X-Real-IP) and fall back to RemoteAddr.
func clientAddr(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return xr
	}
	return r.RemoteAddr
}

// Broadcast sends audio data to all connected clients.
func (m *Mount) Broadcast(data []byte) {
	if len(data) == 0 {
		return
	}

	// Account every delivered byte (real feed and bridge silence alike) so the
	// silence bridge can hold its output to a wall-clock realtime budget.
	atomic.AddInt64(&m.bytesOut, int64(len(data)))

	// Store in ring buffer for new clients
	m.buffer.Write(data)

	m.mu.RLock()
	defer m.mu.RUnlock()

	skipped := false
	for c := range m.clients {
		c.mu.Lock()
		if !c.closed {
			select {
			case c.ch <- data:
			default:
				// Client is slow, skip this chunk. Count it so the disconnect log
				// and the per-mount metric reveal gapped-audio clients that would
				// otherwise leave no trace. Atomic because the disconnect path in
				// the serve goroutine reads it without holding c.mu.
				atomic.AddInt64(&c.skipped, 1)
				skipped = true
			}
		}
		c.mu.Unlock()
	}
	if skipped {
		telemetry.BroadcastSkippedChunksTotal.WithLabelValues(m.Name).Inc()
	}
}

// runSilenceBridge keeps the outgoing byte stream alive across input gaps,
// without ever pushing delivery ahead of realtime. It maintains a wall-clock
// byte budget that grows at the mount's bitrate; each poll it advances the
// budget, clamps it to within bridgeMaxSlack of bytesOut (so slow drift can't
// bank unbounded credit or debt), and — only when the mount is in a gap
// (inGap) AND delivery is behind budget — emits silent frames until the two
// meet again. A mount that ran ahead of realtime (boundary burst, connect
// pre-roll) is deliberately left quiet through the gap: that is the drain
// window where paced clients (OBS restreams re-encoding toward YouTube) work
// off their backlog, and filling it is exactly what regressed v1.40.19.
// It never touches lastFedAt, so the director's dead-air detection still
// fires and still restarts the real pipeline; the bridge only covers the seam.
// baseline and start anchor the budget: bytes delivered before the bridge
// existed are history, not debt. Both are captured synchronously by the
// caller (FeedFrom's bridgeStart.Do), BEFORE the first feed's bytes flow, so
// a fast first feed can't slip under the baseline while this goroutine is
// still being scheduled.
func (m *Mount) runSilenceBridge(baseline int64, start time.Time) {
	byteRate := float64(m.Bitrate * 1000 / 8)
	if byteRate <= 0 {
		byteRate = 16000 // 128 kbps
	}
	frameLen := int64(len(m.silentFrame))

	poll := time.NewTicker(m.bridgePollD)
	defer poll.Stop()

	budget := float64(baseline)
	last := start
	filling := false

	for {
		select {
		case <-m.bridgeStop:
			return
		case now := <-poll.C:
			budget += now.Sub(last).Seconds() * byteRate
			last = now

			out := atomic.LoadInt64(&m.bytesOut)
			slack := m.bridgeSlackD.Seconds() * byteRate
			if budget > float64(out)+slack {
				budget = float64(out) + slack
			} else if budget < float64(out)-slack {
				budget = float64(out) - slack
			}

			if !m.inGap() {
				if filling {
					m.logger.Info().Msg("silence bridge released: live feed resumed")
					filling = false
				}
				continue
			}

			// Emit only up to the budget; the next poll grows it by one tick's
			// worth, so sustained output averages exactly realtime.
			emitted := false
			for m.inGap() && atomic.LoadInt64(&m.bytesOut)+frameLen <= int64(budget) {
				m.Broadcast(m.silentFrame)
				telemetry.BroadcastBridgedChunksTotal.WithLabelValues(m.Name).Inc()
				emitted = true
			}
			if emitted && !filling {
				m.logger.Info().Msg("silence bridge filling input gap")
				filling = true
			}
		}
	}
}

// inGap reports whether the mount is in an input gap the silence bridge may
// fill: a client is connected, no live feed is attached, and real audio last
// arrived longer ago than bridgeGrace. lastFedAt is advanced only by FeedFrom
// (real audio), never by the bridge itself, so the time since it is exactly
// the current gap length, and the grace test keeps working while the bridge
// runs. Whether silence actually flows is additionally gated on the realtime
// budget in runSilenceBridge.
func (m *Mount) inGap() bool {
	fedNs := atomic.LoadInt64(&m.lastFedAt)
	if fedNs == 0 {
		return false // never fed real audio: don't prepend silence at startup
	}
	m.mu.RLock()
	noInput := m.inputCount == 0
	hasClients := len(m.clients) > 0
	m.mu.RUnlock()
	if !noInput || !hasClients {
		return false
	}
	return time.Since(time.Unix(0, fedNs)) > m.bridgeGraceD
}

// FeedFrom reads from an io.Reader and broadcasts the data.
// This is typically connected to GStreamer's stdout.
// Note: Call ClearBuffer() on all related mounts BEFORE calling FeedFrom
// to ensure synchronized buffer clearing across HQ/LQ mount pairs.
func (m *Mount) FeedFrom(r io.Reader) error {
	// Start the silence bridge on the first real feed. Starting it here (not in
	// NewMount) keeps it inert for mounts that are only ever driven directly via
	// Broadcast, which is every unit test, so the bridge never perturbs them.
	if m.silentFrame != nil {
		m.bridgeStart.Do(func() {
			// Snapshot the budget anchor here, not in the goroutine: this runs
			// before the feed below delivers its first byte, so those bytes
			// count against the budget rather than vanishing into the baseline.
			go m.runSilenceBridge(atomic.LoadInt64(&m.bytesOut), time.Now())
		})
	}

	// Register this feed
	m.mu.Lock()
	m.inputCount++
	// Reset inputDone channel if this is the first active feed
	if m.inputCount == 1 {
		m.inputDone = make(chan struct{})
	}
	count := m.inputCount
	m.mu.Unlock()

	m.logger.Info().Str("mount", m.Name).Int("input_count", count).Msg("feed started")

	defer func() {
		m.mu.Lock()
		m.inputCount--
		// Only signal done when no more active feeds
		if m.inputCount == 0 {
			select {
			case <-m.inputDone:
				// Already closed
			default:
				close(m.inputDone)
			}
		}
		m.mu.Unlock()
	}()

	buf := make([]byte, 4096)
	totalBytes := 0
	lastLog := time.Now()
	for {
		n, err := r.Read(buf)
		if n > 0 {
			totalBytes += n
			// Log every 10 seconds to show data is flowing
			if time.Since(lastLog) > 10*time.Second {
				m.logger.Info().
					Str("mount", m.Name).
					Int("bytes_last_10s", totalBytes).
					Int("clients", m.ClientCount()).
					Msg("feed active")
				totalBytes = 0
				lastLog = time.Now()
			}
			// Make a copy since we're broadcasting asynchronously
			data := make([]byte, n)
			copy(data, buf[:n])
			m.Broadcast(data)
			atomic.StoreInt64(&m.lastFedAt, time.Now().UnixNano())
		}
		if err != nil {
			if err == io.EOF {
				m.logger.Info().Str("mount", m.Name).Msg("input stream ended (EOF)")
			} else {
				m.logger.Error().Str("mount", m.Name).Err(err).Msg("input read error")
			}
			return err
		}
	}
}

// ServeHTTP handles HTTP client connections for streaming.
func (m *Mount) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set headers for streaming audio
	// NOTE: Do NOT set Transfer-Encoding manually - Go handles it automatically
	// when Content-Length is not set
	w.Header().Set("Content-Type", m.ContentType)
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no")
	// Explicitly delete Content-Length to force chunked transfer
	w.Header().Del("Content-Length")

	// ICY metadata headers
	w.Header().Set("icy-br", itoa(m.Bitrate))
	w.Header().Set("icy-name", m.Name)

	// Check if client wants to skip buffer (used when switching quality)
	// This prevents audio "jumping" when switching between HQ and LQ
	skipBuffer := r.URL.Query().Get("nobuffer") == "1"

	// Log buffer state on connect for debugging sync issues
	m.buffer.mu.RLock()
	bufPos := m.buffer.pos
	m.buffer.mu.RUnlock()
	m.logger.Debug().Int("buffer_pos", bufPos).Bool("skip_buffer", skipBuffer).Msg("client connecting, buffer state")

	// ResponseController drives both flushing & per-write deadlines. A write
	// deadline is what bounds a half-open client's lifetime: without it the
	// serve goroutine parks inside w.Write on a dead socket until the kernel
	// gives up (~15 min) & the client stays in the count the whole time.
	rc := http.NewResponseController(w)

	// writeAndFlush writes a chunk under a fresh write deadline. Any error
	// (a closed socket, or a deadline exceeded on a client that stopped
	// draining) returns so the caller disconnects & the defer drops the client
	// from the count. A writer that doesn't support deadlines or flushing
	// (errors.ErrUnsupported) degrades to the prior best-effort behavior.
	writeAndFlush := func(data []byte) error {
		if err := rc.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil && !errors.Is(err, errors.ErrUnsupported) {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if err := rc.Flush(); err != nil && !errors.Is(err, errors.ErrUnsupported) {
			return err
		}
		return nil
	}

	// Create client with larger buffer for stability
	// 256 chunks * ~4KB = ~1MB buffer, helps prevent drops during network hiccups
	c := &client{
		ch:         make(chan []byte, 256),
		done:       make(chan struct{}),
		remoteAddr: clientAddr(r),
		userAgent:  r.UserAgent(),
	}

	// Register client for delivery. It does NOT count as a listener yet:
	// the counters roll only after the connection is fully established
	// (establishThresholdBytes delivered), so recycled/parked connections
	// never inflate the listener numbers they used to (issue #18: ~1,415
	// reported against ~15 real).
	m.mu.Lock()
	m.clients[c] = struct{}{}
	connectionCount := len(m.clients)
	m.mu.Unlock()

	m.logger.Info().Str("mount", m.Name).Int("connections", connectionCount).Bool("quality_switch", skipBuffer).
		Str("remote", c.remoteAddr).Str("ua", c.userAgent).Msg("client connected (unestablished)")

	// Establishment bookkeeping: delivered() accumulates successful live-loop
	// writes and rolls the client into the listener count exactly once when
	// the threshold is crossed. The initial pre-roll deliberately does NOT
	// count: it is a single burst that socket buffers absorb whether or not a
	// listener exists, so only sustained live delivery proves the connection.
	bytesOut := 0
	establishAt := m.establishThresholdBytes()
	localEstablished := false
	delivered := func(n int) {
		bytesOut += n
		if localEstablished || bytesOut < establishAt {
			return
		}
		localEstablished = true
		m.mu.Lock()
		c.established = true
		m.establishedCount++
		listeners := m.establishedCount
		m.mu.Unlock()
		m.logger.Info().Str("mount", m.Name).Int("listeners", listeners).Int("bytes", bytesOut).Msg("client established")
		m.publishListenerStats(listeners, "connect")
	}

	// Send buffered data to help client start faster
	// For quality switches (nobuffer=1), send minimal data to prime connection
	// This prevents browser timeout while waiting for live data
	if skipBuffer {
		// Send ~200ms of audio to prime the connection without affecting sync much
		// At 128kbps: 200ms = ~3KB, At 64kbps: 200ms = ~1.5KB
		primeBytes := (m.Bitrate * 1000 / 8) / 5 // 200ms of audio
		if primeBytes < 1000 {
			primeBytes = 1000
		}
		if recent := m.buffer.GetRecent(primeBytes); len(recent) > 0 {
			if err := writeAndFlush(recent); err != nil {
				m.logger.Info().Err(err).Msg("initial buffer write failed (skipBuffer)")
				return
			}
		}
	} else {
		// Send 2 seconds of audio for quick start and stable playback
		// At 128kbps: 2s = 32KB, At 64kbps: 2s = 16KB
		bufferBytes := (m.Bitrate * 1000 / 8) * 2 // 2 seconds of audio
		if bufferBytes > 64000 {
			bufferBytes = 64000 // Cap at 64KB (~4s at 128kbps)
		}
		if bufferBytes < 8000 {
			bufferBytes = 8000 // Minimum 8KB
		}
		if recent := m.buffer.GetRecent(bufferBytes); len(recent) > 0 {
			if err := writeAndFlush(recent); err != nil {
				m.logger.Info().Err(err).Int("bytes", len(recent)).Msg("initial buffer write failed")
				return
			}
			m.logger.Info().Int("bytes", len(recent)).Msg("initial buffer sent, entering main loop")
		}
	}

	// Cleanup on disconnect. Only an established client rolls the counters
	// back and emits a disconnect event — a connection that never qualified
	// was never counted, so it has nothing to undo.
	defer func() {
		c.mu.Lock()
		c.closed = true
		close(c.done)
		c.mu.Unlock()

		m.mu.Lock()
		_, present := m.clients[c]
		delete(m.clients, c)
		// Only decrement for clients still registered: Mount.Close() empties
		// the map and zeroes establishedCount itself, so a serve goroutine
		// unwinding after Close must not double-decrement.
		wasEstablished := present && c.established
		if wasEstablished {
			m.establishedCount--
		}
		listeners := m.establishedCount
		connections := len(m.clients)
		m.mu.Unlock()

		m.logger.Info().Str("mount", m.Name).Int("connections", connections).Int("listeners", listeners).Bool("established", wasEstablished).Msg("client disconnected")

		if wasEstablished {
			m.publishListenerStats(listeners, "disconnect")
		}
	}()

	// Create a single timer for keepalive - reused instead of creating new ones.
	// Stays above writeTimeout (30s) and below the server IdleTimeout (60s).
	keepalive := time.NewTimer(45 * time.Second)
	defer keepalive.Stop()

	m.logger.Info().Int("channel_len", len(c.ch)).Msg("entering main streaming loop")

	// Stream data to client - keep connected through track transitions
	writeCount := 0
	for {
		select {
		case <-r.Context().Done():
			m.logger.Info().Int("writes", writeCount).Int64("skipped", atomic.LoadInt64(&c.skipped)).
				Str("remote", c.remoteAddr).Str("ua", c.userAgent).Err(r.Context().Err()).Msg("client context cancelled")
			return
		case data := <-c.ch:
			if err := writeAndFlush(data); err != nil {
				m.logger.Info().Err(err).Int("writes", writeCount).Int64("skipped", atomic.LoadInt64(&c.skipped)).
					Str("remote", c.remoteAddr).Str("ua", c.userAgent).Msg("write failed, disconnecting client")
				return
			}
			delivered(len(data))
			writeCount++
			// Log first few writes to debug streaming issues
			if writeCount <= 5 || writeCount%100 == 0 {
				m.logger.Debug().
					Int("writes", writeCount).
					Int("bytes", len(data)).
					Msg("stream chunk delivered to client")
			}
			// Reset keepalive timer after successful write
			if !keepalive.Stop() {
				select {
				case <-keepalive.C:
				default:
				}
			}
			keepalive.Reset(45 * time.Second)
		case <-keepalive.C:
			// No data for 45 seconds - flush under a write deadline so a dead
			// or half-open client is detected during an idle gap between tracks
			// instead of lingering in the count. A real flush error means the
			// connection is gone; disconnect.
			if err := rc.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil && !errors.Is(err, errors.ErrUnsupported) {
				m.logger.Info().Err(err).Int("writes", writeCount).Int64("skipped", atomic.LoadInt64(&c.skipped)).
					Str("remote", c.remoteAddr).Str("ua", c.userAgent).Msg("keepalive deadline failed, disconnecting client")
				return
			}
			if err := rc.Flush(); err != nil && !errors.Is(err, errors.ErrUnsupported) {
				m.logger.Info().Err(err).Int("writes", writeCount).Int64("skipped", atomic.LoadInt64(&c.skipped)).
					Str("remote", c.remoteAddr).Str("ua", c.userAgent).Msg("keepalive flush failed, disconnecting client")
				return
			}
			m.logger.Debug().Int("writes", writeCount).Msg("keepalive flush")
			keepalive.Reset(45 * time.Second)
		}
	}
}

// ClientCount returns the number of established listeners: connections that
// have drained at least establishSeconds of audio. This is what every public
// counter (TotalListeners, GetListenerStats, the analytics samples) reads, so
// unproven connections never roll into the reported numbers. Raw connection
// count (including unestablished) is ConnectionCount.
func (m *Mount) ClientCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.establishedCount
}

// ConnectionCount returns every registered connection, established or not.
// Diagnostic view: the gap between this and ClientCount is the population of
// connections that grabbed the stream but haven't proven a listener is
// draining it.
func (m *Mount) ConnectionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}

// publishListenerStats publishes a listener_stats event via the event bus
func (m *Mount) publishListenerStats(clientCount int, eventType string) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(events.EventListenerStats, events.Payload{
		"mount":        m.Name,
		"bitrate":      m.Bitrate,
		"listeners":    clientCount,
		"event":        eventType, // "connect" or "disconnect"
		"content_type": m.ContentType,
	})
}

// ClearBuffer clears the ring buffer to prepare for a new track.
// This should be called on all related mounts (e.g., HQ and LQ) together
// before starting new feeds to ensure synchronized playback.
func (m *Mount) ClearBuffer() {
	m.logger.Debug().Msg("clearing buffer for new track")
	m.buffer.Clear()
}

// Close disconnects all clients.
func (m *Mount) Close() {
	// Stop the silence bridge goroutine if it ever started.
	m.bridgeEnd.Do(func() { close(m.bridgeStop) })

	m.mu.Lock()
	defer m.mu.Unlock()

	for c := range m.clients {
		c.mu.Lock()
		c.closed = true
		close(c.done)
		c.mu.Unlock()
	}
	m.clients = make(map[*client]struct{})
	m.establishedCount = 0
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

// Server manages multiple broadcast mounts.
type Server struct {
	mounts map[string]*Mount
	mu     sync.RWMutex
	logger zerolog.Logger
	bus    *events.Bus

	// extraListeners, when set, contributes listeners delivered outside the HTTP
	// mount path (WebRTC peers reach audio over the RTP branch, never through a
	// mount's serve loop, so they'd otherwise be invisible to the count). Wired
	// at startup; nil until then. Guarded by mu.
	extraListeners func() int
}

// NewServer creates a new broadcast server.
func NewServer(logger zerolog.Logger, bus *events.Bus) *Server {
	return &Server{
		mounts: make(map[string]*Mount),
		logger: logger,
		bus:    bus,
	}
}

// CreateMount creates a new mount point.
func (s *Server) CreateMount(name, contentType string, bitrate int) *Mount {
	s.mu.Lock()
	defer s.mu.Unlock()

	mount := NewMount(name, contentType, bitrate, s.logger, s.bus)
	s.mounts[name] = mount
	return mount
}

// GetMount returns a mount by name.
func (s *Server) GetMount(name string) *Mount {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mounts[name]
}

// RemoveMount removes and closes a mount.
func (s *Server) RemoveMount(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if mount, ok := s.mounts[name]; ok {
		mount.Close()
		delete(s.mounts, name)
	}
}

// MountStats contains listener statistics for a mount.
type MountStats struct {
	Name        string `json:"name"`
	Bitrate     int    `json:"bitrate"`
	ContentType string `json:"content_type"`
	Listeners   int    `json:"listeners"`
}

// GetListenerStats returns listener counts for all mounts.
func (s *Server) GetListenerStats() []MountStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make([]MountStats, 0, len(s.mounts))
	for _, mount := range s.mounts {
		stats = append(stats, MountStats{
			Name:        mount.Name,
			Bitrate:     mount.Bitrate,
			ContentType: mount.ContentType,
			Listeners:   mount.ClientCount(),
		})
	}
	return stats
}

// SetExtraListenerSource registers a source of listeners delivered outside the
// HTTP mount path (WebRTC peers) so TotalListeners reflects them. Call once at
// startup, before serving.
func (s *Server) SetExtraListenerSource(fn func() int) {
	s.mu.Lock()
	s.extraListeners = fn
	s.mu.Unlock()
}

// TotalListeners returns the total number of listeners across all mounts, plus
// any WebRTC peers reported by the extra-listener source.
func (s *Server) TotalListeners() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := 0
	for _, mount := range s.mounts {
		total += mount.ClientCount()
	}
	if s.extraListeners != nil {
		total += s.extraListeners()
	}
	return total
}

// ServeHTTP routes requests to the appropriate mount.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract mount name from path (e.g., /broadcast/main -> main)
	name := r.URL.Path
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}

	mount := s.GetMount(name)
	if mount == nil {
		http.Error(w, "Mount not found", http.StatusNotFound)
		return
	}

	mount.ServeHTTP(w, r)
}
