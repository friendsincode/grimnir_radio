/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"
)

// makeFakeBuilder returns a builder that records every SSRC it sees & the
// payload bytes pushed into each per-SSRC session. Used to drive the
// listener without spinning a real GStreamer pipeline.
func makeFakeBuilder() (rtpSessionBuilder, func() map[uint32]*fakeSessionRecord) {
	mu := sync.Mutex{}
	out := map[uint32]*fakeSessionRecord{}

	builder := func(ssrc uint32, pt uint8, remote *net.UDPAddr) (*rtpSession, error) {
		mu.Lock()
		rec := &fakeSessionRecord{ssrc: ssrc, pt: pt, remote: remote}
		out[ssrc] = rec
		mu.Unlock()
		return &rtpSession{
			ssrc: ssrc,
			push: func(b []byte) error {
				rec.mu.Lock()
				rec.pushed = append(rec.pushed, append([]byte(nil), b...))
				rec.mu.Unlock()
				return nil
			},
			close: func() error {
				rec.closed.Store(true)
				return nil
			},
		}, nil
	}
	snapshot := func() map[uint32]*fakeSessionRecord {
		mu.Lock()
		defer mu.Unlock()
		cp := make(map[uint32]*fakeSessionRecord, len(out))
		for k, v := range out {
			cp[k] = v
		}
		return cp
	}
	return builder, snapshot
}

type fakeSessionRecord struct {
	ssrc   uint32
	pt     uint8
	remote *net.UDPAddr

	mu     sync.Mutex
	pushed [][]byte
	closed atomic.Bool
}

// sendRTP writes a single RTP packet with the given SSRC + sequence to addr.
func sendRTP(t *testing.T, addr *net.UDPAddr, ssrc uint32, seq uint16) {
	t.Helper()
	pkt := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    10, // L16/44100/stereo (RFC 3551 static PT 10)
			SequenceNumber: seq,
			Timestamp:      uint32(seq) * 480,
			SSRC:           ssrc,
		},
		Payload: []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
	}
	raw, err := pkt.Marshal()
	if err != nil {
		t.Fatalf("rtp marshal: %v", err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatalf("dial udp: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write(raw); err != nil {
		t.Fatalf("udp write: %v", err)
	}
}

func newTestListener(t *testing.T) (*RTPListener, func() map[uint32]*fakeSessionRecord) {
	t.Helper()
	builder, snap := makeFakeBuilder()
	l, err := NewRTPListener("127.0.0.1:0", builder)
	if err != nil {
		t.Fatalf("NewRTPListener: %v", err)
	}
	return l, snap
}

func TestRTPListener_RequiresBuilder(t *testing.T) {
	_, err := NewRTPListener("127.0.0.1:0", nil)
	if err == nil {
		t.Fatal("nil builder: want error, got nil")
	}
}

func TestRTPListener_BindError(t *testing.T) {
	builder, _ := makeFakeBuilder()
	_, err := NewRTPListener("256.256.256.256:5006", builder)
	if err == nil {
		t.Fatal("invalid bind: want error, got nil")
	}
}

func TestRTPListener_LocalAddrReportsBoundPort(t *testing.T) {
	l, _ := newTestListener(t)
	defer l.Stop()
	addr := l.LocalAddr()
	if addr == nil {
		t.Fatal("LocalAddr returned nil")
	}
	if addr.Port == 0 {
		t.Errorf("bound port should be non-zero, got %d", addr.Port)
	}
}

func TestRTPListener_CreatesSessionOnFirstPacket(t *testing.T) {
	l, snap := newTestListener(t)
	defer l.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = l.Serve(ctx) }()

	sendRTP(t, l.LocalAddr(), 0xdeadbeef, 1)

	if !waitForCount(t, func() int { return len(snap()) }, 1, time.Second) {
		t.Fatal("session for SSRC was not created within 1s")
	}
	rec := snap()[0xdeadbeef]
	if rec == nil {
		t.Fatal("expected record for SSRC 0xdeadbeef")
	}
	if rec.pt != 10 {
		t.Errorf("payload type = %d, want 10", rec.pt)
	}
	rec.mu.Lock()
	pushedCount := len(rec.pushed)
	rec.mu.Unlock()
	if pushedCount == 0 {
		t.Error("expected at least one pushed packet, got 0")
	}
}

func TestRTPListener_DemuxesBySSRC(t *testing.T) {
	l, snap := newTestListener(t)
	defer l.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = l.Serve(ctx) }()

	sendRTP(t, l.LocalAddr(), 0x11111111, 1)
	sendRTP(t, l.LocalAddr(), 0x22222222, 1)
	sendRTP(t, l.LocalAddr(), 0x11111111, 2)
	sendRTP(t, l.LocalAddr(), 0x22222222, 2)

	if !waitForCount(t, func() int { return len(snap()) }, 2, time.Second) {
		t.Fatalf("expected 2 sessions, got %d", len(snap()))
	}
	if l.SessionCount() != 2 {
		t.Errorf("SessionCount = %d, want 2", l.SessionCount())
	}
}

func TestRTPListener_DropsMalformedPackets(t *testing.T) {
	l, snap := newTestListener(t)
	defer l.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = l.Serve(ctx) }()

	// Garbage; can't unmarshal as an RTP packet.
	conn, err := net.DialUDP("udp", nil, l.LocalAddr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_, _ = conn.Write([]byte{0xff})
	_ = conn.Close()

	// Give the listener a moment to read & drop.
	time.Sleep(100 * time.Millisecond)
	if got := len(snap()); got != 0 {
		t.Errorf("malformed packet created %d session(s); want 0", got)
	}
}

func TestRTPListener_ReapStaleClosesTimedOutSessions(t *testing.T) {
	l, snap := newTestListener(t)
	defer l.Stop()
	l.sessionTimeout = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = l.Serve(ctx) }()

	sendRTP(t, l.LocalAddr(), 0xaabbccdd, 1)
	if !waitForCount(t, func() int { return l.SessionCount() }, 1, time.Second) {
		t.Fatal("session was not created")
	}
	// Force reaper to run with a "future" now-tick beyond the timeout.
	l.reapStale(time.Now().Add(time.Second))

	if l.SessionCount() != 0 {
		t.Errorf("after reap: SessionCount = %d, want 0", l.SessionCount())
	}
	rec := snap()[0xaabbccdd]
	if rec == nil || !rec.closed.Load() {
		t.Error("expected reaped session to have been closed")
	}
}

func TestRTPListener_StopClosesEverySession(t *testing.T) {
	l, snap := newTestListener(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = l.Serve(ctx) }()

	sendRTP(t, l.LocalAddr(), 0x11111111, 1)
	sendRTP(t, l.LocalAddr(), 0x22222222, 1)
	if !waitForCount(t, func() int { return l.SessionCount() }, 2, time.Second) {
		t.Fatal("sessions were not created")
	}

	l.Stop()

	if l.SessionCount() != 0 {
		t.Errorf("after Stop: SessionCount = %d, want 0", l.SessionCount())
	}
	for ssrc, rec := range snap() {
		if !rec.closed.Load() {
			t.Errorf("session %x not closed after Stop", ssrc)
		}
	}
}

func TestRTPListener_StopIsIdempotent(t *testing.T) {
	l, _ := newTestListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = l.Serve(ctx) }()
	// First Stop closes; second is a no-op.
	l.Stop()
	l.Stop()
}

func TestRTPListener_ContextCancelExitsServe(t *testing.T) {
	l, _ := newTestListener(t)
	defer l.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- l.Serve(ctx) }()

	// Confirm Serve is running by sending a packet, then cancel.
	sendRTP(t, l.LocalAddr(), 0x33333333, 1)
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Serve returned %v on ctx cancel; want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not exit within 2s of ctx cancel")
	}
}

func TestNewRTPSessionBuilder_ConstructsPipeline(t *testing.T) {
	gstInit()
	mgr := NewSessionMgr()
	builder := NewRTPSessionBuilder(mgr, []string{"127.0.0.1:65000"})

	remote := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}
	sess, err := builder(0x12345678, 10, remote)
	if err != nil {
		t.Fatalf("builder: %v", err)
	}
	if sess == nil {
		t.Fatal("builder returned nil session")
	}
	defer sess.close()

	if mgr.CountByProtocol(ProtocolRTP) != 1 {
		t.Errorf("CountByProtocol(RTP) = %d, want 1", mgr.CountByProtocol(ProtocolRTP))
	}
	// Pushing an RTP packet should not error (multiudpsink target is a
	// black-hole port; the pipeline drops silently).
	pkt := &rtp.Packet{
		Header:  rtp.Header{Version: 2, PayloadType: 10, SequenceNumber: 1, Timestamp: 480, SSRC: 0x12345678},
		Payload: []byte{0x00, 0x01, 0x02, 0x03},
	}
	raw, err := pkt.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := sess.push(raw); err != nil {
		t.Errorf("push: %v", err)
	}
}

// waitForCount polls f() every 10ms until it equals want or timeout elapses.
func waitForCount(t *testing.T, f func() int, want int, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f() == want {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return f() == want
}
