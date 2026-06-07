/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package vrrphealth

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/metrics"
)

func TestStart_NoVIPs_IsNoOp(t *testing.T) {
	rdb, p, err := Start(context.Background(), Config{}, zerolog.New(io.Discard))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if rdb != nil || p != nil {
		t.Fatalf("expected nil returns for empty VIPs; got rdb=%v p=%v", rdb, p)
	}
}

func TestStart_BadRedis_ReturnsError(t *testing.T) {
	_, _, err := Start(
		context.Background(),
		Config{RedisAddr: "127.0.0.1:1", VIPs: []string{"listener"}},
		zerolog.New(io.Discard),
	)
	if err == nil {
		t.Fatal("expected error when Redis is unreachable")
	}
}

func TestStart_PollerUpdatesGauge(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	// Seed VRRP hash for a known VIP before starting; the first Poll tick
	// must reflect this state in the gauge.
	mr.HSet("grimnir:vrrp:listener-wired", "node-a", "master")
	mr.HSet("grimnir:vrrp:listener-wired", "node-b", "backup")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rdb, p, err := Start(
		ctx,
		Config{RedisAddr: mr.Addr(), VIPs: []string{"listener-wired"}},
		zerolog.New(io.Discard),
	)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if rdb == nil || p == nil {
		t.Fatal("expected non-nil rdb & poller")
	}
	defer rdb.Close()

	// Run() ticks every 5s; trigger an immediate Poll so the test isn't slow.
	p.Poll(ctx)

	got := testutil.ToFloat64(metrics.VrrpHolderCount.WithLabelValues("listener-wired"))
	if got != 1 {
		t.Errorf("holder count = %v, want 1", got)
	}

	// Mutate Redis to simulate failover & verify subsequent Poll reflects it.
	mr.HSet("grimnir:vrrp:listener-wired", "node-a", "backup")
	mr.HSet("grimnir:vrrp:listener-wired", "node-b", "master")
	p.Poll(ctx)
	got = testutil.ToFloat64(metrics.VrrpHolderCount.WithLabelValues("listener-wired"))
	if got != 1 {
		t.Errorf("holder count after failover = %v, want 1", got)
	}

	// Cancel & give the goroutine a beat to exit cleanly.
	cancel()
	time.Sleep(20 * time.Millisecond)
}
