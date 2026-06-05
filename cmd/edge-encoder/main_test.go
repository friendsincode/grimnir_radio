/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRun_StartsAndStopsCleanly(t *testing.T) {
	t.Setenv("EDGE_ENCODER_GRPC_PORT", "0")    // ephemeral
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
