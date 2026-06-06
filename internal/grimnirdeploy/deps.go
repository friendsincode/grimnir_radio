/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/pause"
	"github.com/friendsincode/grimnir_radio/internal/notify"
)

// Deps is the bag of cluster connections a subcommand needs. Later chunks
// extend it (SSH runner, ntfy poster, deploy_history store).
type Deps struct {
	Cfg     *Config
	DB      *gorm.DB
	Redis   *redis.Client
	Pause   *pause.Client
	Store   *audit.Store
	Wrapper *audit.Wrapper
	// NotifyClient is the tier-aware ntfy client used by auto-rollback for
	// tier-2 page notifications. The audit Wrapper's ntfy path is tier-1
	// only; auto-rollback needs its own client so paging-priority alerts
	// route to the on-call topic. Nil disables auto-rollback ntfy.
	NotifyClient notify.Notifier
}

// Close releases the Redis connection and the underlying SQL connection.
func (d *Deps) Close() {
	if d == nil {
		return
	}
	if d.Redis != nil {
		_ = d.Redis.Close()
	}
	if d.DB != nil {
		if sqlDB, err := d.DB.DB(); err == nil && sqlDB != nil {
			_ = sqlDB.Close()
		}
	}
}

// wireDeps opens Redis (required) and Postgres (optional for early chunks)
// and assembles the audit Wrapper. If the Postgres DSN is empty the
// returned Wrapper has a nil recorder; Wrap then runs the body without
// writing audit rows. Emergency-pause Redis failure is fatal: pre-flight
// gates depend on this key being readable.
func wireDeps(ctx context.Context, cfg *Config) (*Deps, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("redis ping %s: %w", cfg.RedisAddr, err)
	}

	deps := &Deps{
		Cfg:          cfg,
		Redis:        rdb,
		Pause:        pause.NewClient(rdb),
		NotifyClient: notify.FromEnv(),
	}

	// Postgres + audit store are optional in early chunks; only wire them if
	// a DSN is set. Once Chunk 1 is committed and the audit_log table exists
	// in every cluster, this should become required.
	if cfg.DBDSN != "" {
		db, err := gorm.Open(postgres.Open(cfg.DBDSN), &gorm.Config{})
		if err != nil {
			_ = rdb.Close()
			return nil, fmt.Errorf("postgres open: %w", err)
		}
		deps.DB = db
		deps.Store = audit.NewStore(db)
		// Wire the tier-aware notify client into the audit Wrapper. When
		// GRIMNIR_NTFY_URL is unset, FromEnv returns a NopNotifier so dev
		// boxes don't need a real ntfy endpoint; the adapter is still
		// constructed so deploy / failure events traverse the same code path
		// in dev as in prod.
		poster := audit.NewNotifierPoster(notify.FromEnv())
		rec := audit.NewRecorder(deps.Store, poster, "")
		deps.Wrapper = audit.NewWrapper(rec, cfg.Operator, localSourceIP())
	} else {
		// Wrapper with nil recorder; Wrap still runs fn but skips DB writes.
		deps.Wrapper = audit.NewWrapper(nil, cfg.Operator, localSourceIP())
	}

	return deps, nil
}

// localSourceIP returns the IP of the SSH connection the operator is on
// ($SSH_CLIENT first field), or "local" for direct console invocation.
func localSourceIP() string {
	v := os.Getenv("SSH_CLIENT")
	if v == "" {
		return "local"
	}
	if i := strings.IndexByte(v, ' '); i > 0 {
		return v[:i]
	}
	return v
}
