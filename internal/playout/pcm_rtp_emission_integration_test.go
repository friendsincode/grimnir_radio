//go:build integration

/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestEngineEmitsRTPInHAMode runs the real GStreamer pipeline string produced
// by buildDualBroadcastPipeline with HA mode enabled, against a 10-second
// silence MP3 fixture, and asserts that well-formed RTP-L16 packets actually
// arrive on the multiudpsink target. This catches regressions where the
// pipeline string parses & compiles cleanly but no packets ever leave the box
// (e.g. caps mismatch on the tee branch, missing plugin, wrong PT, etc.).
//
// Shortcut: the fdsink fd=3 / fd=4 outputs in the real pipeline expect the Go
// parent to have opened those file descriptors before fork. Rather than
// reproduce the full StartWithDualOutput plumbing, we substitute fdsink with
// fakesink for this test. The PCM-RTP branch under test is unmodified.
func TestEngineEmitsRTPInHAMode(t *testing.T) {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		t.Skip("gst-launch-1.0 not available; skipping integration test")
	}

	fixture, err := filepath.Abs("testdata/silence-10s.mp3")
	if err != nil {
		t.Fatalf("resolve fixture path: %v", err)
	}

	// Bind a UDP listener on an OS-chosen free port so parallel test runs
	// don't collide.
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve udp addr: %v", err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	target := conn.LocalAddr().String()
	t.Logf("RTP listener bound to %s", target)

	// Build the real pipeline string via the production code path with HA
	// mode flipped on and our test listener as the only target.
	d, _ := newMockDirector(t)
	d.cfg.HAPCMRTPEnabled = true
	d.cfg.HAPCMRTPTargets = []string{target}

	mount := models.Mount{
		Name:       "integration-test",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	seekFile, pipelineStr, err := d.buildDualBroadcastPipeline(fixture, mount, 128, 64, 0, 0, 0)
	if err != nil {
		t.Fatalf("buildDualBroadcastPipeline: %v", err)
	}
	if seekFile != nil {
		seekFile.Close()
	}

	// Sanity check that the HA branch is in fact present.
	if !strings.Contains(pipelineStr, "rtpL16pay") {
		t.Fatalf("pipeline missing rtpL16pay branch:\n%s", pipelineStr)
	}
	if !strings.Contains(pipelineStr, "multiudpsink") {
		t.Fatalf("pipeline missing multiudpsink:\n%s", pipelineStr)
	}

	// Swap the two fdsinks (which require fd 3/4 from the parent) for
	// fakesinks so gst-launch can run standalone.
	launch := strings.ReplaceAll(pipelineStr, "fdsink fd=3", "fakesink sync=true")
	launch = strings.ReplaceAll(launch, "fdsink fd=4", "fakesink sync=true")

	t.Logf("launching pipeline:\n%s", launch)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	t.Cleanup(cancel)

	// gst-launch-1.0 accepts the pipeline as positional argv; splitting on
	// whitespace is fine here because nothing in the produced string
	// contains spaces inside a single element token.
	args := strings.Fields(launch)
	cmd := startGstLaunch(ctx, args)
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	})
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gst-launch-1.0: %v", err)
	}

	// Collect packets for up to 3 seconds; expect at least 10 RTP packets.
	const wantPackets = 10
	got := readAndValidateRTP(t, conn, wantPackets, 3*time.Second)
	if got < wantPackets {
		t.Fatalf("only %d RTP packets received in 3s, want >= %d", got, wantPackets)
	}
	t.Logf("received %d well-formed RTP packets", got)
}

// startGstLaunch constructs the subprocess. Split out so the test body stays
// linear; flags are kept minimal so failures in the pipeline don't get
// swallowed.
func startGstLaunch(ctx context.Context, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "gst-launch-1.0", args...)
	return cmd
}

// readAndValidateRTP pulls UDP datagrams off the socket and verifies each is a
// well-formed RTP packet with version=2 and payload type=10 (L16 stereo
// 44.1kHz per RFC 3551). Returns the count of valid packets seen.
func readAndValidateRTP(t *testing.T, conn *net.UDPConn, want int, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	if err := conn.SetReadDeadline(deadline); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	buf := make([]byte, 2048)
	count := 0
	for count < want && time.Now().Before(deadline) {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				break
			}
			t.Fatalf("read udp: %v", err)
		}
		if err := validateRTPPacket(buf[:n]); err != nil {
			t.Fatalf("packet %d invalid: %v (len=%d)", count+1, err, n)
		}
		count++
	}
	return count
}

// validateRTPPacket checks the fixed RTP header per RFC 3550 plus the L16
// payload type we expect. We don't decode the payload; presence of well-formed
// headers from the rtpL16pay element is enough to prove the branch is wired.
func validateRTPPacket(p []byte) error {
	if len(p) < 12 {
		return fmt.Errorf("packet too short for RTP header: %d bytes", len(p))
	}
	version := (p[0] >> 6) & 0x03
	if version != 2 {
		return fmt.Errorf("RTP version=%d, want 2", version)
	}
	pt := p[1] & 0x7F
	if pt != 10 {
		return fmt.Errorf("RTP payload type=%d, want 10 (L16 stereo)", pt)
	}
	return nil
}
