/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package vrrphealth

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redis/go-redis/v9"

	"github.com/friendsincode/grimnir_radio/internal/metrics"
)

func newRedis(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, func() {
		_ = rdb.Close()
		mr.Close()
	}
}

func TestVrrpHolder_OneMaster(t *testing.T) {
	ctx := context.Background()
	rdb, cleanup := newRedis(t)
	defer cleanup()

	rdb.HSet(ctx, "grimnir:vrrp:listener-onemaster", "node-1", "master")
	rdb.HSet(ctx, "grimnir:vrrp:listener-onemaster", "node-2", "backup")

	p := NewVrrpPoller(rdb, []string{"listener-onemaster"})
	p.Poll(ctx)
	if got := testutil.ToFloat64(metrics.VrrpHolderCount.WithLabelValues("listener-onemaster")); got != 1 {
		t.Errorf("holder count = %v, want 1", got)
	}
}

func TestVrrpHolder_SplitBrain(t *testing.T) {
	ctx := context.Background()
	rdb, cleanup := newRedis(t)
	defer cleanup()

	rdb.HSet(ctx, "grimnir:vrrp:listener-split", "node-1", "master")
	rdb.HSet(ctx, "grimnir:vrrp:listener-split", "node-2", "master")

	p := NewVrrpPoller(rdb, []string{"listener-split"})
	p.Poll(ctx)
	if got := testutil.ToFloat64(metrics.VrrpHolderCount.WithLabelValues("listener-split")); got != 2 {
		t.Errorf("holder count = %v, want 2 (split brain)", got)
	}
}

func TestVrrpHolder_NoHolder(t *testing.T) {
	ctx := context.Background()
	rdb, cleanup := newRedis(t)
	defer cleanup()

	rdb.HSet(ctx, "grimnir:vrrp:listener-noholder", "node-1", "fault")
	rdb.HSet(ctx, "grimnir:vrrp:listener-noholder", "node-2", "backup")

	p := NewVrrpPoller(rdb, []string{"listener-noholder"})
	p.Poll(ctx)
	if got := testutil.ToFloat64(metrics.VrrpHolderCount.WithLabelValues("listener-noholder")); got != 0 {
		t.Errorf("holder count = %v, want 0", got)
	}
}
