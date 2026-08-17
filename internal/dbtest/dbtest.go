/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package dbtest opens gorm databases for tests — real Postgres, period.
// sqlite is banned from the test suite: its permissiveness shipped the same
// defect class three times (it accepts "" in uuid columns where Postgres
// raises SQLSTATE 22P02 — the #223/#228/#242 delete failures, re-shipped on
// 1.x as GitLab #44 — & it leaves foreign keys unenforced by default, the
// media_tag_links failure). Tests run on the engine production runs.
//
// Every call returns a UNIQUE database on the server named by TEST_DB_DSN
// (CI provisions postgres:16 & exports it), dropped on test cleanup. Without
// TEST_DB_DSN the local dev default below is tried; if that's unreachable
// the test fails with instructions.
package dbtest

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// UUID maps a mnemonic seed ("st-1", "user-a") to a deterministic RFC 4122
// UUID (v5, SHA-1 over NameSpaceOID). Postgres uuid columns reject short test
// ids with SQLSTATE 22P02; this keeps seeds readable & stable across runs
// while satisfying the column type. Equal seeds always yield equal UUIDs.
func UUID(seed string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(seed)).String()
}

// localDefaultDSN matches `make testdb` (a throwaway postgres:16 container).
const localDefaultDSN = "host=localhost port=15432 user=postgres password=postgres dbname=postgres sslmode=disable"

var dbCounter atomic.Int64

// Open returns a fresh, isolated Postgres database, migrated for the given
// models & dropped on cleanup.
func Open(t *testing.T, migrate ...any) *gorm.DB {
	t.Helper()

	adminDSN := os.Getenv("TEST_DB_DSN")
	if adminDSN == "" {
		adminDSN = localDefaultDSN
	}

	dbName := fmt.Sprintf("grimnir_test_%d_%d", os.Getpid(), dbCounter.Add(1))

	adminDB, err := gorm.Open(postgres.Open(adminDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("dbtest: no Postgres at %q. Tests require Postgres (sqlite is banned; see package doc). Start one with:\n  docker run -d --rm --name pgtest -e POSTGRES_PASSWORD=postgres -e POSTGRES_USER=postgres -e POSTGRES_DB=postgres -p 15432:5432 postgres:16\nor set TEST_DB_DSN. Error: %v", adminDSN, err)
	}
	if err := adminDB.Exec("CREATE DATABASE " + dbName).Error; err != nil {
		t.Fatalf("dbtest: create %s: %v", dbName, err)
	}

	testDSN := strings.ReplaceAll(adminDSN, "dbname=postgres", "dbname="+dbName)
	if testDSN == adminDSN {
		testDSN = adminDSN + " dbname=" + dbName
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("dbtest: open %s: %v", dbName, err)
	}

	t.Cleanup(func() {
		if sqlDB, _ := db.DB(); sqlDB != nil {
			_ = sqlDB.Close()
		}
		if err := adminDB.Exec("DROP DATABASE IF EXISTS " + dbName).Error; err != nil {
			t.Logf("dbtest: drop %s: %v", dbName, err)
		}
		if adminSQL, _ := adminDB.DB(); adminSQL != nil {
			_ = adminSQL.Close()
		}
	})

	if len(migrate) > 0 {
		if err := db.AutoMigrate(migrate...); err != nil {
			t.Fatalf("dbtest: automigrate: %v", err)
		}
	}
	return db
}
