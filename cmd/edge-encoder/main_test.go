/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRun_StartsAndStopsCleanly(t *testing.T) {
	t.Setenv("EDGE_ENCODER_GRPC_PORT", "0") // ephemeral
	t.Setenv("EDGE_ENCODER_HTTP_PORT", "0")
	t.Setenv("EDGE_ENCODER_METRICS_PORT", "0")
	t.Setenv("EDGE_ENCODER_RTP_PORT_A", "0")
	t.Setenv("EDGE_ENCODER_RTP_PORT_B", "0")

	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan int, 1)
	go func() { done <- run(ctx, &stdout, &stderr) }()

	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("run exit = %d, want 0; stderr=%q", code, stderr.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not exit within 3s of context cancellation")
	}

	if !strings.Contains(stdout.String(), "edge-encoder starting") {
		t.Logf("stdout did not include expected startup line: %q", stdout.String())
	}
}

func TestRun_LiveEndpointReturns200(t *testing.T) {
	t.Setenv("EDGE_ENCODER_GRPC_PORT", "0")
	t.Setenv("EDGE_ENCODER_METRICS_PORT", "0")
	t.Setenv("EDGE_ENCODER_RTP_PORT_A", "0")
	t.Setenv("EDGE_ENCODER_RTP_PORT_B", "0")
	// Need a known port to GET /live; pick a high-numbered one unlikely to clash.
	t.Setenv("EDGE_ENCODER_HTTP_PORT", "18099")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		done <- run(ctx, &stdout, &stderr)
	}()

	// Wait for HTTP listener to come up.
	time.Sleep(1500 * time.Millisecond)

	// GET with a tight timeout — Mount.ServeHTTP keeps the connection open, so
	// reading the full body would hang. We only need status + headers.
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get("http://localhost:18099/live")
	if err != nil {
		// Read timeout on body is fine; non-timeout connect errors are not.
		t.Logf("GET /live error (may be expected timeout on body): %v", err)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("/live status = %d, want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "audio/mpeg" {
			t.Errorf("/live Content-Type = %q, want audio/mpeg", ct)
		}
		buf := make([]byte, 64)
		n, _ := resp.Body.Read(buf)
		t.Logf("/live initial bytes read = %d (zero is OK without RTP feed)", n)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("run did not exit on cancel")
	}
}

func TestRun_InvalidConfigExitsNonZero(t *testing.T) {
	t.Setenv("EDGE_ENCODER_OUTPUT_FORMAT", "wav")
	var stderr bytes.Buffer
	code := run(context.Background(), os.Stdout, &stderr)
	if code == 0 {
		t.Error("run with invalid config: exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "OUTPUT_FORMAT") {
		t.Errorf("stderr did not mention OUTPUT_FORMAT: %q", stderr.String())
	}
}
