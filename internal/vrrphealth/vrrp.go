/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package vrrphealth polls a Redis-backed VRRP state table and feeds the
// grimnir_vrrp_holder_count gauge. keepalived's notify_master / notify_backup
// / notify_fault scripts write per-node state into a Redis hash keyed by VIP:
//
//	grimnir:vrrp:<vip> -> { node-1: master, node-2: backup }
//
// The gauge value per VIP is the count of nodes reporting "master". In
// steady state with N HA nodes the value should equal 1; 0 means no holder
// (page) and >=2 means split-brain (page).
package vrrphealth

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/friendsincode/grimnir_radio/internal/metrics"
)

// Poller queries Redis on a fixed interval and updates the gauge per VIP.
type Poller struct {
	rdb  *redis.Client
	vips []string
}

// NewVrrpPoller builds a poller watching the given VIP names.
// VIP names are application-level identifiers (e.g., "listener", "api"),
// NOT IP addresses, and form the suffix of the Redis hash key.
func NewVrrpPoller(rdb *redis.Client, vips []string) *Poller {
	return &Poller{rdb: rdb, vips: vips}
}

// Poll runs one observation pass across every configured VIP.
// Errors from Redis cause that VIP's gauge to be left at its previous value
// rather than zeroed — a brief Redis blip shouldn't trigger split-brain
// false positives.
func (p *Poller) Poll(ctx context.Context) {
	for _, vip := range p.vips {
		states, err := p.rdb.HGetAll(ctx, "grimnir:vrrp:"+vip).Result()
		if err != nil {
			continue
		}
		count := 0
		for _, s := range states {
			if s == "master" {
				count++
			}
		}
		metrics.VrrpHolderCount.WithLabelValues(vip).Set(float64(count))
	}
}

// Run polls every 5 seconds until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.Poll(ctx)
		}
	}
}
