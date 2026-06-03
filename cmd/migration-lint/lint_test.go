/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"testing"
)

func TestDetectDestructive_TableOfPatterns(t *testing.T) {
	tests := []struct {
		name   string
		sql    string
		wantOp string // empty if no destructive op should be found
	}{
		{"drop column", "ALTER TABLE foo DROP COLUMN bar;", "DROP COLUMN"},
		{"drop column lowercase", "alter table foo drop column bar;", "DROP COLUMN"},
		{"drop table", "DROP TABLE foo;", "DROP TABLE"},
		{"drop index", "DROP INDEX idx_foo;", "DROP INDEX"},
		{"rename column", "ALTER TABLE foo RENAME COLUMN a TO b;", "RENAME COLUMN"},
		{"rename table", "ALTER TABLE foo RENAME TO bar;", "RENAME TABLE"},
		{"alter type", "ALTER TABLE foo ALTER COLUMN bar TYPE integer;", "ALTER COLUMN TYPE"},
		{"set not null", "ALTER TABLE foo ALTER COLUMN bar SET NOT NULL;", "ALTER COLUMN SET NOT NULL"},
		{"truncate", "TRUNCATE foo;", "TRUNCATE"},
		{"truncate table", "TRUNCATE TABLE foo;", "TRUNCATE"},
		{"in comment, ignored", "-- DROP COLUMN bar -- this is a comment", ""},
		{"add column, safe", "ALTER TABLE foo ADD COLUMN bar text;", ""},
		{"create table, safe", "CREATE TABLE foo (id uuid PRIMARY KEY);", ""},
		{"create index, safe", "CREATE INDEX idx_foo ON foo(bar);", ""},
		{"column named drop_count, safe", "ALTER TABLE foo ADD COLUMN drop_count int;", ""},
		{"drop not null is relaxing, safe", "ALTER TABLE foo ALTER COLUMN bar DROP NOT NULL;", ""},
		{"set data type long form", "ALTER TABLE foo ALTER COLUMN bar SET DATA TYPE integer;", "ALTER COLUMN TYPE"},
		{"rename table quoted identifier", "ALTER TABLE \"foo\" RENAME TO \"bar\";", "RENAME TABLE"},
		{"rename table schema-qualified", "ALTER TABLE public.foo RENAME TO bar;", "RENAME TABLE"},
		{"alter column type quoted identifier", "ALTER TABLE \"foo\" ALTER COLUMN \"bar\" TYPE integer;", "ALTER COLUMN TYPE"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ops := detectDestructive(tc.sql)
			if tc.wantOp == "" {
				if len(ops) != 0 {
					t.Errorf("detectDestructive(%q) = %v, want no findings", tc.sql, ops)
				}
				return
			}
			if len(ops) != 1 {
				t.Fatalf("detectDestructive(%q) = %v, want 1 finding (%q)", tc.sql, ops, tc.wantOp)
			}
			if ops[0].Op != tc.wantOp {
				t.Errorf("detectDestructive(%q) op = %q, want %q", tc.sql, ops[0].Op, tc.wantOp)
			}
		})
	}
}

func TestHasContractAnnotation(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{"explicit annotation", "-- migration-contract: dropping foo, replaced by bar in v1.40\nDROP COLUMN foo;", true},
		{"annotation with surrounding whitespace", "  --   migration-contract:   reason here\n", true},
		{"missing annotation", "DROP COLUMN foo;", false},
		{"empty annotation", "-- migration-contract:\nDROP COLUMN foo;", false},
		{"annotation with only whitespace reason", "-- migration-contract:    \nDROP COLUMN foo;", false},
		{"annotation case-insensitive marker", "-- MIGRATION-CONTRACT: reason\nDROP COLUMN foo;", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := hasContractAnnotation(tc.sql)
			if got != tc.want {
				t.Errorf("hasContractAnnotation(%q) = %v, want %v", tc.sql, got, tc.want)
			}
		})
	}
}

func TestLintFile(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		sql          string
		wantFindings int
	}{
		{
			name:         "clean migration",
			path:         "migrations/100_add_column.sql",
			sql:          "ALTER TABLE foo ADD COLUMN bar text;",
			wantFindings: 0,
		},
		{
			name:         "destructive without annotation",
			path:         "migrations/101_drop_column.sql",
			sql:          "ALTER TABLE foo DROP COLUMN bar;",
			wantFindings: 1,
		},
		{
			name:         "destructive with annotation",
			path:         "migrations/102_drop_column_annotated.sql",
			sql:          "-- migration-contract: foo deprecated in v1.40, contracted in v1.43\nALTER TABLE foo DROP COLUMN bar;",
			wantFindings: 0,
		},
		{
			name:         "two destructive ops without annotation",
			path:         "migrations/103_two_drops.sql",
			sql:          "DROP TABLE foo;\nDROP TABLE bar;",
			wantFindings: 2,
		},
		{
			name:         "two destructive ops with annotation",
			path:         "migrations/104_two_drops_annotated.sql",
			sql:          "-- migration-contract: deprecated tables\nDROP TABLE foo;\nDROP TABLE bar;",
			wantFindings: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := LintFile(tc.path, tc.sql)
			if len(got) != tc.wantFindings {
				t.Errorf("LintFile findings = %d, want %d (%+v)", len(got), tc.wantFindings, got)
			}
			for _, f := range got {
				if f.Path != tc.path {
					t.Errorf("finding path = %q, want %q", f.Path, tc.path)
				}
			}
		})
	}
}
