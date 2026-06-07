//go:build integration

/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// End-to-end integration test for the fan-out (Chunk 10 of the live-input
// plan). Drives the Harbor ingress (simplest protocol to script from pure Go
// — no SRT lib, no WebRTC handshake) against:
//
//   - Two fake "media engine" UDP receivers that record bytes-in counts
//   - A real *Pipeline whose multiudpsink targets both receivers
//   - A miniredis-backed *SessionReplicator wired into SessionMgr, so we can
//     verify the session metadata round-trips into Redis
//
// What this validates:
//
//  1. Harbor SOURCE auth path accepts the DJ & emits "HTTP/1.0 200 OK"
//  2. A Session lands in SessionMgr & gets replicated to Redis as
//     dj:session:<id> with the expected hash fields
//  3. multiudpsink fans the PCM-RTP stream out to BOTH engine sinks — both
//     receivers see >= 1 KB of UDP payload within a few seconds
//  4. When the DJ disconnects, the session is removed from SessionMgr & the
//     Redis hash is deleted
//
// Why a custom HarborSessionSink: the production PipelineHarborSink spawns a
// gst-launch decodebin subprocess that expects encoded MP3/Ogg/AAC bytes on
// stdin. Generating that in pure Go is fiddly; instead the test sink builds
// a Pipeline that uses audiotestsrc as its SourceLaunch, so the pipeline
// produces PCM on its own & the harbor body bytes are discarded. This
// exercises every layer EXCEPT the per-format decoder — that's covered by
// the harbor_sink unit tests.
//
// Run with:
//
//	go test -tags=integration -v -run TestFanout_E2E ./internal/grimnirfanout/...
//
// Build tag keeps it out of `make ci` / `make test` so the default flow
// stays fast; it needs GStreamer + plugins-good (rtpL16pay, multiudpsink,
// audiotestsrc) which not every dev rig has.

package grimnirfanout

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// skipIfNoGstreamer bails out early when gst-launch-1.0 is unavailable on
// the dev rig. Matches the edge-encoder E2E test pattern.
func skipIfNoGstreamer(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		t.Skip("gst-launch-1.0 not available; skipping fan-out E2E test")
	}
}

// engineSink is a fake media engine: binds a UDP socket, counts inbound
// bytes & packets, exposes the bound port so the Pipeline can target it.
type engineSink struct {
	name    string
	conn    *net.UDPConn
	bytes   atomic.Uint64
	packets atomic.Uint64
	done    chan struct{}
}

func newEngineSink(t *testing.T, name string) *engineSink {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("engine %s listen: %v", name, err)
	}
	e := &engineSink{
		name: name,
		conn: conn,
		done: make(chan struct{}),
	}
	go e.serve()
	return e
}

func (e *engineSink) serve() {
	defer close(e.done)
	buf := make([]byte, 2048)
	for {
		n, _, err := e.conn.ReadFromUDP(buf)
		if n > 0 {
			e.bytes.Add(uint64(n))
			e.packets.Add(1)
		}
		if err != nil {
			return
		}
	}
}

func (e *engineSink) addr() string {
	a := e.conn.LocalAddr().(*net.UDPAddr)
	return fmt.Sprintf("127.0.0.1:%d", a.Port)
}

func (e *engineSink) close() {
	_ = e.conn.Close()
	<-e.done
}

// audiotestsrcSink is the test HarborSessionSink: on Begin it constructs a
// real *Pipeline using audiotestsrc as the source (no appsrc, no decoder
// subprocess) so the multiudpsink fans real PCM-RTP packets to the engines
// without us having to feed encoded bytes from the test. Bytes is a no-op;
// End tears the pipeline down.
type audiotestsrcSink struct {
	engines []string

	mu    sync.Mutex
	pipes map[string]*Pipeline
}

func newAudiotestsrcSink(engines []string) *audiotestsrcSink {
	return &audiotestsrcSink{
		engines: engines,
		pipes:   make(map[string]*Pipeline),
	}
}

func (s *audiotestsrcSink) Begin(sess *Session, mount string) error {
	pipe, err := NewPipeline(PipelineConfig{
		Engines: s.engines,
		// is-live=true so the pipeline runs in real time (~PCM rate) instead
		// of bursting; samplesperbuffer=480 = 10ms at 48kHz, similar to what
		// the real decoder produces.
		SourceLaunch: "audiotestsrc is-live=true samplesperbuffer=480",
	})
	if err != nil {
		return fmt.Errorf("e2e sink: build pipeline: %w", err)
	}
	if err := pipe.Start(); err != nil {
		return fmt.Errorf("e2e sink: start pipeline: %w", err)
	}
	sess.AttachPipeline(pipe)
	s.mu.Lock()
	s.pipes[sess.ID] = pipe
	s.mu.Unlock()
	return nil
}

func (s *audiotestsrcSink) Bytes(sess *Session, p []byte) error {
	// audiotestsrc generates its own samples; ignore the DJ's body bytes.
	return nil
}

func (s *audiotestsrcSink) End(sess *Session) {
	s.mu.Lock()
	pipe, ok := s.pipes[sess.ID]
	delete(s.pipes, sess.ID)
	s.mu.Unlock()
	if ok && pipe != nil {
		_ = pipe.Stop()
	}
}

// startE2EHarbor wires a HarborListener with the given sink + replicating
// SessionMgr on an ephemeral port. Returns the port + a teardown.
func startE2EHarbor(t *testing.T, sink HarborSessionSink, mgr *SessionMgr) (int, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("harbor listen: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	hl := NewHarborListener(HarborListenerConfig{
		Listener:    lis,
		Auth:        AcceptAllAuthenticator{},
		Sink:        sink,
		Sessions:    mgr,
		ReadTimeout: 1 * time.Second,
		// Bigger than ingress_harbor_test.go because the E2E test holds the
		// conn open while audiotestsrc produces packets (~3s of streaming).
		IdleTimeout:   5 * time.Second,
		MaxHeaderSize: 4096,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = hl.Serve(ctx)
		close(done)
	}()
	return port, func() {
		cancel()
		_ = lis.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("harbor Serve did not exit within 3s of cancel")
		}
	}
}

// connectHarborDJ opens a TCP conn, sends a Harbor SOURCE request with
// Basic auth, and reads the response status line. Returns the live conn so
// the caller can keep it open (driving audiotestsrc output) & close it on
// teardown.
func connectHarborDJ(t *testing.T, port int) (net.Conn, string) {
	t.Helper()
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial harbor: %v", err)
	}
	// Basic auth: "djuser:pass" -> "ZGp1c2VyOnBhc3M="
	req := "SOURCE /live HTTP/1.0\r\n" +
		"Authorization: Basic ZGp1c2VyOnBhc3M=\r\n" +
		"Content-Type: audio/mpeg\r\n" +
		"User-Agent: e2e-dj/1.0\r\n" +
		"\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		_ = conn.Close()
		t.Fatalf("write SOURCE: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		_ = conn.Close()
		t.Fatalf("set read deadline: %v", err)
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		_ = conn.Close()
		t.Fatalf("read status: %v", err)
	}
	// Clear deadline so the conn stays open while audiotestsrc streams.
	_ = conn.SetReadDeadline(time.Time{})
	return conn, strings.TrimRight(string(buf[:n]), "\r\n")
}

// newE2ERedis returns a miniredis-backed redis client + cleanup.
func newE2ERedis(t *testing.T) (*redis.Client, *miniredis.Miniredis, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, mr, func() {
		_ = rdb.Close()
		mr.Close()
	}
}

// TestFanout_E2E_HarborToTwoEnginesAndRedis is the headline E2E acceptance
// test. It wires every layer (Harbor ingress -> Session lifecycle -> Redis
// replication -> Pipeline -> multiudpsink) & asserts:
//
//   - HTTP/1.0 200 OK comes back on the Harbor conn
//   - SessionMgr ends up holding exactly one harbor session
//   - dj:session:<id> shows up in Redis with the right protocol + state
//   - Both fake engine sinks receive a non-trivial volume of UDP packets
//     within ~3 seconds (proves multiudpsink fan-out works)
//   - Closing the conn flushes the session out of SessionMgr & deletes the
//     Redis key
func TestFanout_E2E_HarborToTwoEnginesAndRedis(t *testing.T) {
	skipIfNoGstreamer(t)
	gstInit()

	engineA := newEngineSink(t, "A")
	defer engineA.close()
	engineB := newEngineSink(t, "B")
	defer engineB.close()

	rdb, mr, redisCleanup := newE2ERedis(t)
	defer redisCleanup()

	mgr := NewSessionMgr()
	mgr.SetReplicator(NewSessionReplicator(rdb))

	sink := newAudiotestsrcSink([]string{engineA.addr(), engineB.addr()})

	port, stop := startE2EHarbor(t, sink, mgr)
	defer stop()

	conn, status := connectHarborDJ(t, port)
	defer conn.Close()
	if !strings.Contains(status, "200") {
		t.Fatalf("harbor status = %q, want HTTP/1.0 200 OK", status)
	}

	// Wait for the session to register + replicate. Both happen synchronously
	// inside SessionMgr.Create so a short poll suffices.
	var sessID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.CountByProtocol(ProtocolHarbor) >= 1 {
			for _, s := range mgr.List() {
				if s.Protocol == ProtocolHarbor {
					sessID = s.ID
					break
				}
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if sessID == "" {
		t.Fatal("Harbor session never registered with SessionMgr")
	}

	// Redis side: the key must exist & carry the canonical fields.
	key := "dj:session:" + sessID
	if !mr.Exists(key) {
		t.Fatalf("expected Redis key %s after Create; not present", key)
	}
	got, err := rdb.HGetAll(context.Background(), key).Result()
	if err != nil {
		t.Fatalf("HGetAll %s: %v", key, err)
	}
	if got["id"] != sessID {
		t.Errorf("redis hash[id] = %q, want %q", got["id"], sessID)
	}
	if got["protocol"] != "harbor" {
		t.Errorf("redis hash[protocol] = %q, want harbor", got["protocol"])
	}
	// State on first replicate is "idle" (Create fires Replicate before the
	// listener's transitionTo(Authenticating)); that's a known-correct race
	// per the SessionMgr design — the heartbeat catches up within 5s. We
	// only assert the field is one of the known states.
	switch got["state"] {
	case "idle", "authenticating", "active":
		// OK
	default:
		t.Errorf("redis hash[state] = %q, want idle|authenticating|active", got["state"])
	}

	// Fan-out side: both engines must see PCM-RTP packets. audiotestsrc with
	// is-live=true produces ~100 packets/sec (10ms buffers). 3 seconds = 300
	// packets * ~1KB MTU = ~300 KB. Floor at 1 KB to keep the test robust
	// against slow CI hardware.
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if engineA.bytes.Load() >= 1024 && engineB.bytes.Load() >= 1024 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	aBytes := engineA.bytes.Load()
	bBytes := engineB.bytes.Load()
	aPkts := engineA.packets.Load()
	bPkts := engineB.packets.Load()
	if aBytes < 1024 {
		t.Errorf("engine A bytes = %d (packets=%d), want >= 1024 (fan-out broken on side A)", aBytes, aPkts)
	}
	if bBytes < 1024 {
		t.Errorf("engine B bytes = %d (packets=%d), want >= 1024 (fan-out broken on side B)", bBytes, bPkts)
	}
	t.Logf("fan-out OK: engineA=%d bytes / %d pkts; engineB=%d bytes / %d pkts; session=%s",
		aBytes, aPkts, bBytes, bPkts, sessID)

	// Disconnect: closing the DJ conn must drain the session out of mgr &
	// drop the Redis key.
	_ = conn.Close()
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.CountByProtocol(ProtocolHarbor) == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := mgr.CountByProtocol(ProtocolHarbor); got != 0 {
		t.Errorf("after disconnect, harbor session count = %d, want 0", got)
	}
	// The Remove path calls SessionReplicator.Delete which removes the hash.
	if mr.Exists(key) {
		t.Errorf("Redis key %s still exists after disconnect; want deleted", key)
	}
}

// TestFanout_E2E_RedisHeartbeatRefreshesTTL exercises the second half of the
// replication contract: while a session is live, RunReplicationHeartbeat
// must refresh the 60s TTL on the Redis hash so a 60s+ broadcast doesn't
// see the key expire & fail a peer-takeover hydrate.
//
// Drives a short audiotestsrc pipeline (so we don't hold the network for
// minutes), then fast-forwards miniredis past the TTL & confirms the key
// is still present because the heartbeat tick refreshed it.
func TestFanout_E2E_RedisHeartbeatRefreshesTTL(t *testing.T) {
	skipIfNoGstreamer(t)
	gstInit()

	engineA := newEngineSink(t, "A")
	defer engineA.close()
	engineB := newEngineSink(t, "B")
	defer engineB.close()

	rdb, mr, redisCleanup := newE2ERedis(t)
	defer redisCleanup()

	mgr := NewSessionMgr()
	mgr.SetReplicator(NewSessionReplicator(rdb))

	// Heartbeat every 50ms — well under miniredis's FastForward window.
	hbCtx, hbCancel := context.WithCancel(context.Background())
	hbDone := make(chan struct{})
	go func() {
		mgr.RunReplicationHeartbeat(hbCtx, 50*time.Millisecond)
		close(hbDone)
	}()
	defer func() {
		hbCancel()
		<-hbDone
	}()

	sink := newAudiotestsrcSink([]string{engineA.addr(), engineB.addr()})
	port, stop := startE2EHarbor(t, sink, mgr)
	defer stop()

	conn, status := connectHarborDJ(t, port)
	defer conn.Close()
	if !strings.Contains(status, "200") {
		t.Fatalf("harbor status = %q, want 200", status)
	}

	// Wait for the session to land in Redis.
	var key string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.CountByProtocol(ProtocolHarbor) >= 1 {
			for _, s := range mgr.List() {
				key = "dj:session:" + s.ID
				break
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if key == "" || !mr.Exists(key) {
		t.Fatalf("session never replicated to Redis (key=%q)", key)
	}

	// Wait two heartbeat ticks then fast-forward past the 60s TTL. Without
	// the heartbeat the key would have expired; with it the TTL is freshly
	// reset on every tick & the key survives.
	time.Sleep(150 * time.Millisecond)
	mr.FastForward(45 * time.Second)
	time.Sleep(150 * time.Millisecond)
	mr.FastForward(45 * time.Second)
	time.Sleep(150 * time.Millisecond)

	if !mr.Exists(key) {
		t.Errorf("Redis key %s expired despite heartbeat (heartbeat path broken)", key)
	}
}
