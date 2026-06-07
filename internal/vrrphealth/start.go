/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package vrrphealth

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// Config is the minimum surface needed to start the VRRP poller alongside
// the control plane. The poller is a no-op when VIPs is empty.
type Config struct {
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	VIPs          []string
}

// Start constructs a Redis client, builds the poller, & runs it in a goroutine.
// It returns the redis client (so the caller can Close it on shutdown) plus
// the poller. When cfg.VIPs is empty both returns are nil & no goroutine
// runs; callers should treat that as "feature disabled".
func Start(ctx context.Context, cfg Config, logger zerolog.Logger) (*redis.Client, *Poller, error) {
	if len(cfg.VIPs) == 0 {
		return nil, nil, nil
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	// One round-trip to verify Redis is reachable before claiming success.
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, nil, fmt.Errorf("vrrphealth: ping redis %s: %w", cfg.RedisAddr, err)
	}
	p := NewVrrpPoller(rdb, cfg.VIPs)
	go p.Run(ctx)
	logger.Info().
		Strs("vips", cfg.VIPs).
		Str("redis_addr", cfg.RedisAddr).
		Msg("VRRP holder poller started")
	return rdb, p, nil
}
