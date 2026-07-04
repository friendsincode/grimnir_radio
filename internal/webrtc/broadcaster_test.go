/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webrtc

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/rs/zerolog"
)

// The broadcaster rewrites RTP so listeners hear one continuous stream across
// pipeline restarts, and its counts feed the stats surface (issue #250).
// Writing these tests confirmed two latent bugs now fixed: bytesReceived was
// incremented outside the lock Stats() reads under, and a peer's done channel
// could be closed twice (Failed→Closed state transition, or state callback
// racing Stop).

// freeUDPPort grabs an ephemeral UDP port. NewBroadcaster rewrites port 0 to
// 5004, so tests must pass a concrete free port instead.
func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		t.Fatalf("probe udp port: %v", err)
	}
	port := c.LocalAddr().(*net.UDPAddr).Port
	_ = c.Close()
	return port
}

func startTestBroadcaster(t *testing.T) *Broadcaster {
	t.Helper()
	b, err := NewBroadcaster(Config{RTPPort: freeUDPPort(t)}, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewBroadcaster: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = b.Stop()
	})
	return b
}

func rtpSender(t *testing.T, port int) *net.UDPConn {
	t.Helper()
	conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatalf("dial udp: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func sendRTP(t *testing.T, conn *net.UDPConn, seq uint16, ts uint32) {
	t.Helper()
	pkt := rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    111,
			SequenceNumber: seq,
			Timestamp:      ts,
			SSRC:           0xAABBCCDD,
		},
		Payload: []byte{0x01, 0x02, 0x03, 0x04},
	}
	data, err := pkt.Marshal()
	if err != nil {
		t.Fatalf("marshal rtp: %v", err)
	}
	if _, err := conn.Write(data); err != nil {
		t.Fatalf("send rtp: %v", err)
	}
}

func waitState(t *testing.T, b *Broadcaster, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		b.mu.RLock()
		ok := cond()
		b.mu.RUnlock()
		if ok {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestReadRTP_RewriteContinuityAcrossDiscontinuity(t *testing.T) {
	b := startTestBroadcaster(t)
	conn := rtpSender(t, b.rtpPort)

	// Two in-order packets: rewrite counts up, no offset introduced.
	sendRTP(t, conn, 100, 1000)
	if !waitState(t, b, 2*time.Second, func() bool { return b.seqNum == 1 }) {
		t.Fatal("first packet never processed")
	}
	sendRTP(t, conn, 101, 1960)
	if !waitState(t, b, 2*time.Second, func() bool { return b.seqNum == 2 }) {
		t.Fatal("second packet never processed")
	}
	b.mu.RLock()
	if b.tsOffset != 0 || b.lastOutTS != 1960 {
		t.Errorf("in-order stream: tsOffset=%d lastOutTS=%d, want 0/1960", b.tsOffset, b.lastOutTS)
	}
	b.mu.RUnlock()

	// New pipeline: sequence jumps by >30000. Outgoing timestamps must
	// continue from the last output plus one 20ms opus frame (960 @48kHz),
	// not follow the new pipeline's unrelated clock.
	sendRTP(t, conn, 40000, 555)
	if !waitState(t, b, 2*time.Second, func() bool { return b.seqNum == 3 }) {
		t.Fatal("post-discontinuity packet never processed")
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.lastOutTS != 1960+960 {
		t.Errorf("discontinuity: lastOutTS=%d, want %d (previous out + 960)", b.lastOutTS, 1960+960)
	}
	if b.tsOffset != 1960+960-555 {
		t.Errorf("discontinuity: tsOffset=%d, want %d", b.tsOffset, 1960+960-555)
	}
}

func TestReadRTP_ActiveSourceLock(t *testing.T) {
	b := startTestBroadcaster(t)
	srcA := rtpSender(t, b.rtpPort)
	srcB := rtpSender(t, b.rtpPort)

	// A locks the source.
	sendRTP(t, srcA, 10, 100)
	if !waitState(t, b, 2*time.Second, func() bool { return b.activeSource != "" }) {
		t.Fatal("source never locked")
	}
	b.mu.RLock()
	lockedSource := b.activeSource
	b.mu.RUnlock()

	// B interleaves while A is fresh: dropped, rewrite state untouched by B's
	// sequence numbers.
	sendRTP(t, srcB, 9999, 100)
	time.Sleep(150 * time.Millisecond)
	b.mu.RLock()
	if b.activeSource != lockedSource {
		t.Errorf("source switched to interloper while fresh: %s", b.activeSource)
	}
	if b.lastInSeq == 9999 {
		t.Error("interloper packet was processed while the source was fresh")
	}
	b.mu.RUnlock()

	// After the lock goes stale (300ms without A sending), B may take over.
	time.Sleep(350 * time.Millisecond)
	sendRTP(t, srcB, 10000, 200)
	if !waitState(t, b, 2*time.Second, func() bool { return b.lastInSeq == 10000 }) {
		t.Fatal("stale source was never replaced by the new sender")
	}
}

func TestStats_ConcurrentWithTraffic(t *testing.T) {
	b := startTestBroadcaster(t)
	conn := rtpSender(t, b.rtpPort)

	// Hammer the stats surface while packets flow; -race fails this test if
	// bytesReceived is written outside the lock (the pre-fix behavior).
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = b.Stats()
					_ = b.PeerCount()
					_, _ = b.MarshalJSON()
				}
			}
		}()
	}

	for seq := uint16(1); seq <= 50; seq++ {
		sendRTP(t, conn, seq, uint32(seq)*960)
		time.Sleep(time.Millisecond)
	}
	close(stop)
	wg.Wait()

	if !waitState(t, b, 2*time.Second, func() bool { return b.bytesReceived > 0 }) {
		t.Fatal("no bytes counted")
	}
	stats := b.Stats()
	if stats["rtp_port"] != b.rtpPort {
		t.Errorf("stats rtp_port = %v", stats["rtp_port"])
	}
}

func TestPeerLifecycle_StopAndRepeatedMarkDone(t *testing.T) {
	b := startTestBroadcaster(t)

	// Offline peer connections are enough to exercise registration & teardown.
	pc, err := b.createPeerConnection("peer-1")
	if err != nil {
		t.Fatalf("createPeerConnection: %v", err)
	}
	peer := &peerConnection{id: "peer-1", pc: pc, done: make(chan struct{})}
	b.mu.Lock()
	b.peers[peer.id] = peer
	b.totalPeers++
	b.mu.Unlock()

	if got := b.PeerCount(); got != 1 {
		t.Fatalf("PeerCount = %d, want 1", got)
	}
	if got := b.Stats()["total_peers"]; got != int64(1) {
		t.Errorf("total_peers = %v, want 1", got)
	}

	// The state callback can fire Failed then Closed — two markDone calls —
	// and Stop closes every peer's done as well. All of it must be safe.
	peer.markDone()
	peer.markDone()
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := b.PeerCount(); got != 0 {
		t.Errorf("PeerCount after Stop = %d, want 0", got)
	}
	// totalPeers is cumulative, not current.
	if got := b.Stats()["total_peers"]; got != int64(1) {
		t.Errorf("total_peers after Stop = %v, want 1 (cumulative)", got)
	}
}
