/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package dbhealth

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/metrics"
)

func TestReplicationLagPoller_PrimaryWithReplica(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"lag_seconds"}).AddRow(2.5)
	mock.ExpectQuery("EXTRACT.*FROM pg_stat_replication").WillReturnRows(rows)

	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	p := NewReplicationLagPoller(gdb)
	if err := p.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := testutil.ToFloat64(metrics.PostgresReplicationLagSeconds); got != 2.5 {
		t.Errorf("lag = %v, want 2.5", got)
	}
}

func TestReplicationLagPoller_NoReplicaReportsZero(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	// Set lag to a sentinel first so we can assert Poll resets it.
	metrics.PostgresReplicationLagSeconds.Set(99)
	mock.ExpectQuery("pg_stat_replication").WillReturnRows(sqlmock.NewRows([]string{"lag_seconds"}))

	gdb, _ := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	p := NewReplicationLagPoller(gdb)
	if err := p.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Zero rows -> 0 lag (no replica connected) is the correct value.
	if got := testutil.ToFloat64(metrics.PostgresReplicationLagSeconds); got != 0 {
		t.Errorf("lag = %v, want 0 when no replica", got)
	}
}
