//go:build integration

/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	pb "github.com/friendsincode/grimnir_radio/proto/edgeencoder/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// End-to-end tests for the edge encoder. Each test:
//   1. spawns two real gst-launch processes producing PCM-over-RTP at different
//      frequencies (so the audio is distinguishable but we only assert byte flow)
//   2. starts the edge encoder in-process via run() with EDGE_ENCODER_RTP_PORT_A/B
//      pointed at the two engines
//   3. connects an HTTP client to /live and reads encoded MP3 bytes
//
// Issue #236 (divergence detection) is now implemented in phase 1
// (RTP-timestamp comparison only); see TestEdgeEncoder_DivergenceDetection.

const (
	engineFreqA = 440
	engineFreqB = 880
)

func skipIfNoGstreamer(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		t.Skip("gst-launch-1.0 not available; skipping E2E test")
	}
}

// freePort returns an OS-chosen free TCP port. Race-prone (another process
// could grab it before the test binds) but fine for short-lived tests.
func freePort(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer lis.Close()
	return lis.Addr().(*net.TCPAddr).Port
}

// spawnTestEngine launches a gst-launch process emitting a sine wave over
// RTP-L16 (PT=10, 44.1kHz stereo S16BE) to the given UDP port. Returns the
// command so the caller can SIGKILL it mid-test for failover simulation.
//
// We use Setpgid so killing -PID reaps the whole process group, not just the
// shell. gst-launch occasionally spawns helper threads / child processes.
func spawnTestEngine(t *testing.T, freq, udpPort int) *exec.Cmd {
	return spawnTestEngineWithOffsets(t, freq, udpPort, 0, 0)
}

// spawnTestEngineWithOffsets is the variant used by the divergence test:
// fixed seqnum-offset and timestamp-offset on rtpL16pay let us produce two
// engines whose matched sequence numbers carry deterministically-different
// timestamps. With NetClock-synced engines in production the timestamps
// would agree; this fakes a divergence shape the detector should flag.
func spawnTestEngineWithOffsets(t *testing.T, freq, udpPort int, seqOffset, tsOffset int) *exec.Cmd {
	t.Helper()
	args := []string{
		"-q",
		"audiotestsrc", "is-live=true", "wave=sine", fmt.Sprintf("freq=%d", freq),
		"!", "audioconvert",
		"!", "audio/x-raw,rate=44100,channels=2,format=S16BE",
		"!", "rtpL16pay", "pt=10", "mtu=1400",
		fmt.Sprintf("seqnum-offset=%d", seqOffset),
		fmt.Sprintf("timestamp-offset=%d", tsOffset),
		"!", "udpsink", "host=127.0.0.1", fmt.Sprintf("port=%d", udpPort), "sync=true",
	}
	cmd := exec.Command("gst-launch-1.0", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Capture stderr in case we need to debug a flaky run.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gst-launch freq=%d port=%d: %v", freq, udpPort, err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = cmd.Wait()
		}
		if t.Failed() && stderr.Len() > 0 {
			t.Logf("gst-launch freq=%d stderr:\n%s", freq, stderr.String())
		}
	})
	return cmd
}

// runEdgeEncoderInProcess starts run() on a goroutine with the supplied env
// vars set via t.Setenv. Returns a cancel func; t.Cleanup waits for exit.
func runEdgeEncoderInProcess(t *testing.T, rtpA, rtpB, httpPort, grpcPort int) context.CancelFunc {
	t.Helper()
	t.Setenv("EDGE_ENCODER_RTP_PORT_A", strconv.Itoa(rtpA))
	t.Setenv("EDGE_ENCODER_RTP_PORT_B", strconv.Itoa(rtpB))
	t.Setenv("EDGE_ENCODER_HTTP_PORT", strconv.Itoa(httpPort))
	t.Setenv("EDGE_ENCODER_GRPC_PORT", strconv.Itoa(grpcPort))
	t.Setenv("EDGE_ENCODER_METRICS_PORT", "0")
	t.Setenv("EDGE_ENCODER_BIND_ADDR", "127.0.0.1")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var stdout, stderr bytes.Buffer
	var mu sync.Mutex
	go func() {
		code := run(ctx, &lockedWriter{w: &stdout, mu: &mu}, &lockedWriter{w: &stderr, mu: &mu})
		done <- code
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("edge encoder run() did not exit within 5s of cancel")
		}
		if t.Failed() {
			mu.Lock()
			defer mu.Unlock()
			t.Logf("edge-encoder stdout:\n%s", stdout.String())
			t.Logf("edge-encoder stderr:\n%s", stderr.String())
		}
	})
	return cancel
}

// lockedWriter serializes writes for safe interleaving when run() spawns
// goroutines that all write to stdout/stderr.
type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// waitForHealthz blocks until GET http://127.0.0.1:port/healthz returns 200,
// or times out.
func waitForHealthz(t *testing.T, port int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	for time.Now().Before(deadline) {
		client := &http.Client{Timeout: 250 * time.Millisecond}
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("edge encoder /healthz did not become ready within %v", timeout)
}

// readBytesFor reads from r for the given duration and returns the total byte
// count. Stops early on EOF or any error.
func readBytesFor(r io.Reader, dur time.Duration) int {
	buf := make([]byte, 4096)
	deadline := time.Now().Add(dur)
	total := 0
	for time.Now().Before(deadline) {
		n, err := r.Read(buf)
		total += n
		if err != nil {
			return total
		}
	}
	return total
}

// statusString fetches the gRPC GetStatus snapshot for diagnostics.
func statusString(t *testing.T, grpcPort int) string {
	t.Helper()
	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", grpcPort), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Sprintf("grpc dial: %v", err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	resp, err := pb.NewEdgeEncoderClient(conn).GetStatus(ctx, &pb.StatusRequest{})
	if err != nil {
		return fmt.Sprintf("grpc GetStatus: %v", err)
	}
	return fmt.Sprintf("active=%s a_healthy=%v b_healthy=%v switches=%d listeners=%d",
		resp.ActiveInput, resp.InputAHealthy, resp.InputBHealthy, resp.SwitchCount, resp.ListenerCount)
}

// TestEdgeEncoder_EndToEnd_ServesEncodedAudio verifies the full pipeline:
// two RTP feeders -> udpsrc x2 -> input-selector -> mp3 encode -> appsink ->
// broadcast.Mount -> HTTP /live. Asserts the HTTP listener receives a
// non-trivial number of encoded bytes within a short window.
//
// This is the primary E2E acceptance test for Chunks 1-8: if it passes, the
// full transport path from RTP ingest to encoded-byte HTTP egress works.
func TestEdgeEncoder_EndToEnd_ServesEncodedAudio(t *testing.T) {
	skipIfNoGstreamer(t)

	rtpA, rtpB := 15004, 15005
	httpPort := freePort(t)
	grpcPort := freePort(t)

	spawnTestEngine(t, engineFreqA, rtpA)
	spawnTestEngine(t, engineFreqB, rtpB)
	runEdgeEncoderInProcess(t, rtpA, rtpB, httpPort, grpcPort)

	waitForHealthz(t, httpPort, 5*time.Second)
	// Give the pipeline a moment to actually start carrying audio through the
	// encoder before opening /live. The encoder needs ~1 frame of PCM before
	// lamemp3enc produces its first output buffer.
	time.Sleep(1500 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://127.0.0.1:%d/live", httpPort), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /live: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("/live status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "audio/") {
		t.Errorf("/live Content-Type = %q, want audio/*", ct)
	}

	total := readBytesFor(resp.Body, 2*time.Second)
	// 128kbps MP3 = 16 KB/sec, so 2s should yield ~32 KB. 1024 is a deliberately
	// generous floor: if we're getting under 1KB in 2 seconds, the pipeline is
	// broken, not just slow.
	if total < 1024 {
		t.Errorf("/live read %d bytes in 2s; want >= 1024 (status: %s)",
			total, statusString(t, grpcPort))
	}
	t.Logf("/live received %d bytes in 2s (status: %s)", total, statusString(t, grpcPort))
}

// TestEdgeEncoder_EndToEnd_FailoverWhenEngineDies verifies that killing the
// active RTP source mid-stream lets the switcher flip to engine B & keep
// audio flowing.
//
// History: this used to be skipped because input-selector inactive-pad
// backpressure stalled the inactive branch, so the InputHealth pad probe
// stopped firing & the switcher saw both inputs as unhealthy. Fixed by
// inserting a per-branch leaky queue (max-size-buffers=8 leaky=downstream)
// between audioconvert & input-selector, so the inactive branch keeps
// draining buffers & the pad probe keeps firing.
func TestEdgeEncoder_EndToEnd_FailoverWhenEngineDies(t *testing.T) {
	skipIfNoGstreamer(t)

	rtpA, rtpB := 15006, 15007
	httpPort := freePort(t)
	grpcPort := freePort(t)

	engineA := spawnTestEngine(t, engineFreqA, rtpA)
	spawnTestEngine(t, engineFreqB, rtpB)
	runEdgeEncoderInProcess(t, rtpA, rtpB, httpPort, grpcPort)

	waitForHealthz(t, httpPort, 5*time.Second)
	time.Sleep(1500 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://127.0.0.1:%d/live", httpPort), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /live: %v", err)
	}
	defer resp.Body.Close()

	beforeKill := readBytesFor(resp.Body, 1500*time.Millisecond)
	if beforeKill < 1024 {
		t.Fatalf("pre-kill bytes = %d, want >= 1024", beforeKill)
	}
	t.Logf("status before kill: %s", statusString(t, grpcPort))

	if engineA.Process != nil {
		if err := syscall.Kill(-engineA.Process.Pid, syscall.SIGKILL); err != nil {
			t.Fatalf("kill engine A: %v", err)
		}
	}

	// Bytes should keep flowing through the ~200ms switcher confirmation
	// window & beyond. The threshold (32 KB) reflects ~2 seconds of 128kbps
	// MP3, so an encoder-buffer-drain (~10 KB) won't accidentally satisfy it.
	afterKill := readBytesFor(resp.Body, 3*time.Second)
	if afterKill < 32*1024 {
		t.Errorf("post-kill bytes = %d, want >= %d (failover did not keep audio flowing; status: %s)",
			afterKill, 32*1024, statusString(t, grpcPort))
	}
	t.Logf("pre-kill=%d bytes post-kill=%d bytes (status: %s)",
		beforeKill, afterKill, statusString(t, grpcPort))
}

// TestEdgeEncoder_DivergenceDetection_ReportsWhenStreamsDiffer drives two
// engines whose rtpL16pay payloaders share an initial sequence-number offset
// (so the detector sees matched seqs) but differ wildly on timestamp-offset
// (so |tsA - tsB| greatly exceeds the 4410-tick threshold). The detector
// should flip DivergenceDetected within a couple of seconds.
//
// In production, NetClock-synced engines would converge on identical
// timestamps for matched seqs; this test fakes the divergence shape so we
// can exercise the detector wiring end-to-end.
func TestEdgeEncoder_DivergenceDetection_ReportsWhenStreamsDiffer(t *testing.T) {
	skipIfNoGstreamer(t)

	rtpA, rtpB := 15008, 15009
	httpPort := freePort(t)
	grpcPort := freePort(t)

	// Both engines start their seq counter at 0 so the detector observes
	// matched seqs. Engine B's timestamp-offset is shifted by 100000 ticks
	// (~2.27 seconds at 44.1 kHz) — far past the 4410-tick (=100 ms)
	// divergence threshold. Engine A uses tsOffset=0; engine B uses 100000.
	spawnTestEngineWithOffsets(t, engineFreqA, rtpA, 0, 0)
	spawnTestEngineWithOffsets(t, engineFreqB, rtpB, 0, 100000)
	runEdgeEncoderInProcess(t, rtpA, rtpB, httpPort, grpcPort)

	waitForHealthz(t, httpPort, 5*time.Second)

	// The detector ticks once per second. Give it up to ~4 seconds to
	// (a) accumulate ring-buffer samples (1 every 10 packets = ~5 / s) and
	// (b) flip DivergenceDetected on a Check() call.
	deadline := time.Now().Add(4 * time.Second)
	var detected bool
	var count int64
	var lastSecAgo int64
	for time.Now().Before(deadline) {
		detected, count, lastSecAgo = divergenceStatus(t, grpcPort)
		if detected {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !detected {
		t.Errorf("DivergenceDetected = false after 4s; want true (count=%d last_secs_ago=%d status=%s)",
			count, lastSecAgo, statusString(t, grpcPort))
	}
	if count <= 0 {
		t.Errorf("DivergenceCount = %d after detection; want > 0", count)
	}
	t.Logf("divergence detected=%v count=%d last_secs_ago=%d (status=%s)",
		detected, count, lastSecAgo, statusString(t, grpcPort))
}

// divergenceStatus fetches divergence fields from GetStatus.
func divergenceStatus(t *testing.T, grpcPort int) (bool, int64, int64) {
	t.Helper()
	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", grpcPort), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return false, 0, 0
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	resp, err := pb.NewEdgeEncoderClient(conn).GetStatus(ctx, &pb.StatusRequest{})
	if err != nil {
		return false, 0, 0
	}
	return resp.DivergenceDetected, resp.DivergenceCount, resp.LastDivergenceSecondsAgo
}
