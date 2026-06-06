/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package audit is the grimnir-deploy audit log: per-subcommand rows in the
// audit_log table plus paired ntfy notifications.
//
// This is separate from internal/models.AuditLog (table audit_logs, plural)
// which handles app-level audit events (priority changes, DJ connect /
// disconnect, etc). The two stores happen to live in the same Postgres for
// backup convenience; they do not share code.
package audit

import (
	"time"

	"github.com/google/uuid"
)

// Phase constants for the audit_log.phase column.
const (
	PhaseStarted   = "started"
	PhaseCompleted = "completed"
	PhaseFailed    = "failed"
)

// Entry is one row in the audit_log table.
type Entry struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey"`
	// TS has no GORM default: tag because dialect-specific defaults (Postgres
	// now() vs SQLite CURRENT_TIMESTAMP) trip AutoMigrate on SQLite test DBs.
	// The DEFAULT now() in migrations/005_audit_log.sql applies to Postgres;
	// every Go-side writer (WriteStart) sets TS explicitly.
	TS         time.Time `gorm:"column:ts;not null"`
	Operator   string    `gorm:"column:operator;not null"`
	SourceIP   string    `gorm:"column:source_ip;not null"`
	Subcommand string    `gorm:"column:subcommand;not null"`
	ArgsJSON   string    `gorm:"column:args_json;type:jsonb;not null"`
	Phase      string    `gorm:"column:phase;not null"` // started | completed | failed
	Outcome    string    `gorm:"column:outcome"`
	DurationMS int64     `gorm:"column:duration_ms"`
	Notes      string    `gorm:"column:notes"`
}

// TableName tells GORM the underlying table name.
func (Entry) TableName() string { return "audit_log" }
