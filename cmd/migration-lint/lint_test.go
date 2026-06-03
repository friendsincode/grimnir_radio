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
