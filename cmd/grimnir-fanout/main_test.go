/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRun_StartsAndStopsCleanly(t *testing.T) {
	t.Setenv("FANOUT_ENGINE_A_RTP", "127.0.0.1:5004")
	t.Setenv("FANOUT_GRPC_PORT", "0") // ephemeral
	t.Setenv("FANOUT_HTTP_PORT", "0")
	t.Setenv("FANOUT_METRICS_PORT", "0")

	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan int, 1)
	go func() { done <- run(ctx, &stdout, &stderr) }()

	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("run exit = %d, want 0; stderr=%q", code, stderr.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not exit within 3s of context cancellation")
	}

	if !strings.Contains(stdout.String(), "grimnir-fanout starting") {
		t.Errorf("stdout missing startup line: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "grimnir-fanout shutting down") {
		t.Errorf("stdout missing shutdown line: %q", stdout.String())
	}
}

func TestRun_HealthzEndpoint(t *testing.T) {
	port := pickPort(t)
	t.Setenv("FANOUT_ENGINE_A_RTP", "127.0.0.1:5004")
	t.Setenv("FANOUT_GRPC_PORT", "0")
	t.Setenv("FANOUT_METRICS_PORT", "0")
	t.Setenv("FANOUT_HTTP_PORT", fmt.Sprintf("%d", port))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		done <- run(ctx, &stdout, &stderr)
	}()

	// Poll until /healthz responds or we give up. ~1s budget.
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	client := &http.Client{Timeout: 200 * time.Millisecond}
	var resp *http.Response
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = client.Get(url)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GET /healthz never succeeded: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("run did not exit on cancel")
	}
}

func TestRun_MetricsEndpoint(t *testing.T) {
	port := pickPort(t)
	t.Setenv("FANOUT_ENGINE_A_RTP", "127.0.0.1:5004")
	t.Setenv("FANOUT_GRPC_PORT", "0")
	t.Setenv("FANOUT_HTTP_PORT", "0")
	t.Setenv("FANOUT_METRICS_PORT", fmt.Sprintf("%d", port))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		done <- run(ctx, &stdout, &stderr)
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/metrics", port)
	client := &http.Client{Timeout: 200 * time.Millisecond}
	var resp *http.Response
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = client.Get(url)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GET /metrics never succeeded: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/metrics status = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("run did not exit on cancel")
	}
}

func TestRun_MissingRequiredConfigExitsNonZero(t *testing.T) {
	t.Setenv("FANOUT_ENGINE_A_RTP", "")
	t.Setenv("RLM_FANOUT_ENGINE_A_RTP", "")
	var stderr bytes.Buffer
	code := run(context.Background(), os.Stdout, &stderr)
	if code == 0 {
		t.Error("run with missing FANOUT_ENGINE_A_RTP: exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "ENGINE_A_RTP") {
		t.Errorf("stderr did not mention ENGINE_A_RTP: %q", stderr.String())
	}
}

// pickPort grabs an unused TCP port by listening on :0 then closing.
func pickPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
