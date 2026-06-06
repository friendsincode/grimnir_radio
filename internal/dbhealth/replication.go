/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package dbhealth polls Postgres for replication state and feeds the
// grimnir_postgres_replication_lag_seconds gauge. The poller runs on the
// control plane (which connects to the primary via pgbouncer) so the
// primary-side view of pg_stat_replication is what's queried.
package dbhealth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/metrics"
)

// ReplicationLagPoller polls Postgres for the current replay lag of the
// primary's connected replicas and updates metrics.PostgresReplicationLagSeconds.
//
// The query intentionally reports only the first row from pg_stat_replication;
// in this deployment the topology is one primary + one replica, so there is
// at most one row. When no replica is connected, pg_stat_replication is empty
// and the gauge is set to 0 (treated as "in sync" — nothing to lag behind).
type ReplicationLagPoller struct {
	db *gorm.DB
}

// NewReplicationLagPoller builds a poller bound to the given DB.
func NewReplicationLagPoller(db *gorm.DB) *ReplicationLagPoller {
	return &ReplicationLagPoller{db: db}
}

// Poll runs a single replication-lag query and updates the gauge.
// Callers should invoke this on a ticker (see Run).
func (p *ReplicationLagPoller) Poll(ctx context.Context) error {
	var lag float64
	// replay_lag is an interval (server-side time between commit on primary
	// and apply on replica). EXTRACT(EPOCH FROM ...) returns seconds as
	// float8. COALESCE handles the case where replay_lag is NULL (e.g.,
	// brand-new replica with no traffic yet).
	row := p.db.WithContext(ctx).Raw(`
		SELECT COALESCE(EXTRACT(EPOCH FROM replay_lag), 0)
		FROM pg_stat_replication
		LIMIT 1
	`).Row()
	if err := row.Scan(&lag); err != nil {
		// No replica connected -> no rows; report 0 lag and don't error.
		if errors.Is(err, sql.ErrNoRows) {
			metrics.PostgresReplicationLagSeconds.Set(0)
			return nil
		}
		return err
	}
	metrics.PostgresReplicationLagSeconds.Set(lag)
	return nil
}

// Run polls every 10 seconds until ctx is cancelled. Errors are swallowed
// (the gauge simply isn't updated for that tick) because a brief query
// failure shouldn't crash the control plane.
func (p *ReplicationLagPoller) Run(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = p.Poll(ctx)
		}
	}
}
