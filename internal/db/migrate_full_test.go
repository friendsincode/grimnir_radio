/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package db

import (
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
)

// TestMigrate_FullSchema runs the production Migrate against a fresh Postgres
// database. This covers Migrate plus every custom step it calls — the
// Postgres-specific ones (exclusion-constraint overlap guard, content-hash
// unique index, legacy-role normalization) could not run on sqlite at all.
func TestMigrate_FullSchema(t *testing.T) {
	database := dbtest.Open(t) // empty DB; Migrate creates the whole schema

	if err := Migrate(database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// A sampling of tables the migration must have created.
	for _, table := range []string{"stations", "mounts", "media_items", "schedule_entries", "users", "recordings"} {
		if !database.Migrator().HasTable(table) {
			t.Errorf("expected table %q after Migrate", table)
		}
	}

	// Migrate must be idempotent — a second run over the same schema is a no-op.
	if err := Migrate(database); err != nil {
		t.Fatalf("Migrate (rerun) should be idempotent: %v", err)
	}
}

func TestRegisterCallbacksAndMetrics(t *testing.T) {
	database := dbtest.Open(t)
	if err := RegisterCallbacks(database); err != nil {
		t.Fatalf("RegisterCallbacks: %v", err)
	}
	// Reads pool stats and updates gauges; must not panic on a live pool.
	UpdateConnectionMetrics(database)
}

func TestClose(t *testing.T) {
	database := dbtest.Open(t)
	if err := Close(database); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
