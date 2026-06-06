/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package history reads and writes the deploy_history table. Used by
// grimnir-deploy deploy (writes), grimnir-deploy deploy --rollback (reads
// previous successful tag + checks contract-migration crossings), and
// dashboards (reads outcome counts).
package history

import (
	"time"

	"github.com/google/uuid"
)

// Outcome values stored in the outcome column.
const (
	OutcomeSuccess           = "success"
	OutcomeRolledBackMidRoll = "rolled_back_mid_roll"
	OutcomeRollback          = "rollback"
	OutcomeSoakFailed        = "soak_failed"
	OutcomeFailed            = "failed"
)

// Soak outcome values stored in the soak_outcome column.
const (
	SoakPassed  = "passed"
	SoakFailed  = "failed"
	SoakSkipped = "skipped"
)

// Entry is one row in deploy_history.
type Entry struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey"`
	// Region is the deploy target region (e.g. "us-east"). Together with Tag
	// this typically identifies a deploy event, though the same tag may
	// legitimately be deployed twice (e.g. retry after failure), so this is
	// not a unique key.
	Region      string `gorm:"column:region;not null"`
	Tag         string `gorm:"column:tag;not null"`
	PreviousTag string `gorm:"column:previous_tag"`
	// StartedAt has no GORM default: tag because dialect-specific defaults
	// (Postgres now() vs SQLite CURRENT_TIMESTAMP) trip AutoMigrate on SQLite
	// test DBs. The DEFAULT now() in migrations/006_deploy_history.sql applies
	// to Postgres; every Go-side writer (Start) sets StartedAt explicitly.
	StartedAt   time.Time  `gorm:"column:started_at;not null"`
	CompletedAt *time.Time `gorm:"column:completed_at"`
	Operator    string     `gorm:"column:operator;not null"`
	Outcome     string     `gorm:"column:outcome"`
	Reason      string     `gorm:"column:reason"`
	SoakOutcome string     `gorm:"column:soak_outcome"`
	FailureLog  string     `gorm:"column:failure_log"`
}

// TableName tells GORM the underlying table name.
func (Entry) TableName() string { return "deploy_history" }
