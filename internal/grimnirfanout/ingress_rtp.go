/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
)

// rtpReadBufferSize bounds a single UDP read. RTP packets fit in an MTU; 2 KiB
// is comfortably more than required & keeps allocation small. Matches the size
// used by pion/webrtc internally.
const rtpReadBufferSize = 2048

// RTPSessionTimeout is the per-session inactivity window. A session whose last
// packet is older than this gets torn down by the watchdog. Configurable in
// tests via the RTPListener.SessionTimeout field; production uses the default.
const RTPSessionTimeout = 30 * time.Second

// rtpSessionBuilder is the hook the listener calls when it sees an SSRC it
// hasn't seen before. Production wires this to SessionMgr.Create + NewPipeline;
// tests inject a fake so they can assert per-SSRC pipeline creation without
// spinning a real GStreamer pipeline. Returning an error means the listener
// drops the packet & logs.
type rtpSessionBuilder func(ssrc uint32, payloadType uint8, remote *net.UDPAddr) (*rtpSession, error)

// rtpSession is the per-SSRC state the listener tracks. Pipeline is the
// real Pipeline in production; tests use a fake closer + counter.
type rtpSession struct {
	id        string
	ssrc      uint32
	createdAt time.Time

	mu         sync.Mutex
	lastPacket time.Time
	bytesIn    uint64

	// push handles one RTP payload. Production wires this to
	// Pipeline.PushPCM(packetBytes); the rtpL16depay element in the
	// per-session pipeline strips the RTP header & emits PCM downstream.
	// Tests record what got pushed so they can assert per-SSRC routing.
	push func(rtpPacket []byte) error

	// close releases the underlying Pipeline + removes the session from the
	// SessionMgr. Idempotent. Called by the listener's watchdog on timeout
	// and by the listener's Stop on shutdown.
	close func() error
}

// RTPListener is the UDP ingress for raw RTP push from external broadcast
// tools (FFmpeg, hardware encoders, OBS). It binds one UDP socket, demuxes
// inbound packets by RTP SSRC into per-session pipelines, and drops sessions
// whose last packet is older than SessionTimeout.
//
// Phase 1 supports RTP/L16 only (payload type 10, 44.1 kHz stereo) — the
// payload format the rest of the fan-out path already speaks (matches the
// caps the edge encoder & media engine RTP ingress use). Opus/AAC come in
// a later chunk; payloads of any other PT get logged & dropped at session
// creation.
type RTPListener struct {
	conn           *net.UDPConn
	builder        rtpSessionBuilder
	sessionTimeout time.Duration

	mu       sync.Mutex
	sessions map[uint32]*rtpSession

	stopCh   chan struct{}
	stopOnce sync.Once
	doneCh   chan struct{}
	// serveEntered flips to 1 the first time Serve runs. Stop reads this to
	// decide whether to wait on doneCh (Serve will close it) or close it
	// itself (Serve never ran, e.g. a test that only checks LocalAddr).
	serveEntered atomic.Bool
}

// NewRTPListener binds on the supplied UDP address & returns a listener.
// Caller must invoke Serve to start the read loop. builder is called the
// first time a packet for a previously-unseen SSRC arrives.
//
// Address example: "0.0.0.0:5006". Returns an error if the bind fails.
func NewRTPListener(addr string, builder rtpSessionBuilder) (*RTPListener, error) {
	if builder == nil {
		return nil, fmt.Errorf("rtp listener: builder is required")
	}
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("rtp listener: resolve %q: %w", addr, err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("rtp listener: bind %q: %w", addr, err)
	}
	return &RTPListener{
		conn:           conn,
		builder:        builder,
		sessionTimeout: RTPSessionTimeout,
		sessions:       make(map[uint32]*rtpSession),
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
	}, nil
}

// LocalAddr returns the actual bound UDP address. Useful in tests that pass
// ":0" so the OS picks a free port.
func (l *RTPListener) LocalAddr() *net.UDPAddr {
	if a, ok := l.conn.LocalAddr().(*net.UDPAddr); ok {
		return a
	}
	return nil
}

// Serve runs the UDP read loop + the per-session watchdog until ctx is
// cancelled or Stop is called. Returns nil on graceful shutdown; the
// underlying conn close races with the read loop so a "use of closed
// network connection" error from ReadFromUDP is treated as a graceful exit.
func (l *RTPListener) Serve(ctx context.Context) error {
	if !l.serveEntered.CompareAndSwap(false, true) {
		return fmt.Errorf("rtp listener: Serve already called")
	}
	defer close(l.doneCh)

	// Watchdog: tick every second & drop sessions older than SessionTimeout.
	wdCtx, wdCancel := context.WithCancel(ctx)
	defer wdCancel()
	go l.watchdog(wdCtx)

	// Cancel-context plumbing: when ctx fires, close the conn so the read
	// loop unblocks. Stop() does the same; idempotent via stopOnce.
	go func() {
		select {
		case <-ctx.Done():
			_ = l.shutdownConn()
		case <-l.stopCh:
		}
	}()

	buf := make([]byte, rtpReadBufferSize)
	for {
		n, remote, err := l.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-l.stopCh:
				return nil
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("rtp read: %w", err)
			}
		}
		l.handlePacket(buf[:n], remote)
	}
}

// Stop unblocks Serve. Idempotent. Safe to call from any goroutine.
func (l *RTPListener) Stop() {
	l.stopOnce.Do(func() {
		close(l.stopCh)
		_ = l.shutdownConn()
	})
	// Wait for Serve to actually return so the caller knows the read loop
	// has stopped before sessions get drained. If Serve was never called we
	// have nothing to wait for; jump straight to draining.
	if l.serveEntered.Load() {
		<-l.doneCh
	}
	l.closeAllSessions()
}

func (l *RTPListener) shutdownConn() error {
	if l.conn == nil {
		return nil
	}
	return l.conn.Close()
}

// handlePacket parses one UDP datagram as an RTP packet, looks up or
// creates the per-SSRC session, and forwards the raw packet bytes to its
// pipeline. Malformed packets are silently dropped (logging is the
// caller's job; the listener returns no error so the loop keeps reading).
func (l *RTPListener) handlePacket(data []byte, remote *net.UDPAddr) {
	pkt := &rtp.Packet{}
	if err := pkt.Unmarshal(data); err != nil {
		return
	}
	sess := l.getOrCreate(pkt.SSRC, pkt.PayloadType, remote)
	if sess == nil {
		return
	}
	sess.mu.Lock()
	sess.lastPacket = time.Now()
	sess.bytesIn += uint64(len(data))
	push := sess.push
	sess.mu.Unlock()
	if push != nil {
		// Forward the full RTP packet (header included); the per-session
		// pipeline's rtpL16depay strips the header & emits PCM samples.
		_ = push(append([]byte(nil), data...))
	}
}

// getOrCreate returns the existing session for ssrc or, if none exists,
// asks the builder to make one. Returns nil if the builder errors (the
// listener drops the packet in that case).
func (l *RTPListener) getOrCreate(ssrc uint32, pt uint8, remote *net.UDPAddr) *rtpSession {
	l.mu.Lock()
	if s, ok := l.sessions[ssrc]; ok {
		l.mu.Unlock()
		return s
	}
	l.mu.Unlock()

	s, err := l.builder(ssrc, pt, remote)
	if err != nil || s == nil {
		return nil
	}
	s.createdAt = time.Now()
	s.lastPacket = s.createdAt

	l.mu.Lock()
	// Re-check under the lock; a concurrent packet from the same SSRC
	// could have lost the race & built a second session. The first one
	// wins; the loser gets closed.
	if existing, ok := l.sessions[ssrc]; ok {
		l.mu.Unlock()
		_ = s.close()
		return existing
	}
	l.sessions[ssrc] = s
	l.mu.Unlock()
	return s
}

// watchdog tears down sessions whose last packet is older than
// l.sessionTimeout. Runs until ctx is cancelled.
func (l *RTPListener) watchdog(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			l.reapStale(now)
		}
	}
}

// reapStale removes & closes every session whose lastPacket is older than
// l.sessionTimeout relative to now. Exposed for testing the timeout
// behaviour deterministically.
func (l *RTPListener) reapStale(now time.Time) {
	l.mu.Lock()
	var stale []*rtpSession
	for ssrc, s := range l.sessions {
		s.mu.Lock()
		age := now.Sub(s.lastPacket)
		s.mu.Unlock()
		if age > l.sessionTimeout {
			stale = append(stale, s)
			delete(l.sessions, ssrc)
		}
	}
	l.mu.Unlock()
	for _, s := range stale {
		_ = s.close()
	}
}

// SessionCount returns the number of live RTP sessions. Used in tests &
// for metrics scraping.
func (l *RTPListener) SessionCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.sessions)
}

// closeAllSessions tears down every live session. Called on Stop.
func (l *RTPListener) closeAllSessions() {
	l.mu.Lock()
	all := make([]*rtpSession, 0, len(l.sessions))
	for _, s := range l.sessions {
		all = append(all, s)
	}
	l.sessions = make(map[uint32]*rtpSession)
	l.mu.Unlock()
	for _, s := range all {
		_ = s.close()
	}
}

// NewRTPSessionBuilder wires a production builder that uses SessionMgr to
// allocate IDs + register sessions and constructs a per-session Pipeline
// configured to depay incoming RTP/L16 into PCM, then fan it out to the
// configured media engines.
//
// engines is the list of media-engine PCM-RTP destinations (host:port);
// every per-session pipeline gets the same fan-out target list. Returns
// the builder closure ready to hand to NewRTPListener.
func NewRTPSessionBuilder(mgr *SessionMgr, engines []string) rtpSessionBuilder {
	return func(ssrc uint32, pt uint8, _ *net.UDPAddr) (*rtpSession, error) {
		// Phase 1: payload type 10 = L16/44100/stereo (RFC 3551 static).
		// Other PTs are accepted but the depay element below is the
		// constraint — a non-L16 stream will produce a GStreamer ERROR
		// and the pipeline's Done() will close, taking the session with
		// it. That failure mode is acceptable for phase 1; the plan
		// calls for Opus/AAC support in a follow-up.
		_ = pt

		sess := mgr.Create(ProtocolRTP)
		launch := "appsrc name=audio_in is-live=true format=time " +
			"caps=application/x-rtp,media=audio,clock-rate=44100,encoding-name=L16,channels=2,payload=10 " +
			"! rtpL16depay"
		pipe, err := NewPipeline(PipelineConfig{
			Engines:      engines,
			SourceLaunch: launch,
		})
		if err != nil {
			mgr.Remove(sess.ID)
			return nil, fmt.Errorf("build pipeline for ssrc %d: %w", ssrc, err)
		}
		if err := pipe.Start(); err != nil {
			mgr.Remove(sess.ID)
			return nil, fmt.Errorf("start pipeline for ssrc %d: %w", ssrc, err)
		}
		sess.AttachPipeline(pipe)
		_ = sess.transitionTo(SessionAuthenticating)
		_ = sess.transitionTo(SessionActive)

		var closeOnce sync.Once
		return &rtpSession{
			id:   sess.ID,
			ssrc: ssrc,
			push: func(rtpBytes []byte) error {
				sess.recordBytesIn(uint64(len(rtpBytes)))
				sess.markPacket(time.Now())
				return pipe.PushPCM(rtpBytes)
			},
			close: func() error {
				var err error
				closeOnce.Do(func() {
					err = pipe.Stop()
					_ = sess.transitionTo(SessionEnded)
					mgr.Remove(sess.ID)
				})
				return err
			},
		}, nil
	}
}
