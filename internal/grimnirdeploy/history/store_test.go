/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package history

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Entry{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestStartCompleteAndQuery(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	id1, err := s.Start(ctx, "us-east", "v1.0.0", "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(ctx, id1, OutcomeSuccess, SoakPassed); err != nil {
		t.Fatal(err)
	}

	// Tiny sleep ensures started_at ordering is deterministic on fast
	// machines where two inserts can land in the same microsecond.
	time.Sleep(2 * time.Millisecond)

	id2, err := s.Start(ctx, "us-east", "v1.1.0", "v1.0.0", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(ctx, id2, OutcomeSuccess, SoakPassed); err != nil {
		t.Fatal(err)
	}

	last, err := s.LastSuccessful(ctx, "us-east")
	if err != nil {
		t.Fatal(err)
	}
	if last == nil {
		t.Fatal("LastSuccessful returned nil")
	}
	if last.Tag != "v1.1.0" {
		t.Errorf("LastSuccessful.Tag = %q, want v1.1.0", last.Tag)
	}
	if last.PreviousTag != "v1.0.0" {
		t.Errorf("PreviousTag = %q, want v1.0.0", last.PreviousTag)
	}
	if last.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}
	if last.SoakOutcome != SoakPassed {
		t.Errorf("SoakOutcome = %q, want %q", last.SoakOutcome, SoakPassed)
	}
}

func TestLastSuccessfulSkipsRolledBack(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	id1, _ := s.Start(ctx, "us-east", "v1.0.0", "", "alice")
	_ = s.Complete(ctx, id1, OutcomeSuccess, SoakPassed)

	time.Sleep(2 * time.Millisecond)

	id2, _ := s.Start(ctx, "us-east", "v1.1.0", "v1.0.0", "alice")
	_ = s.Complete(ctx, id2, OutcomeRolledBackMidRoll, SoakSkipped)

	last, _ := s.LastSuccessful(ctx, "us-east")
	if last == nil || last.Tag != "v1.0.0" {
		t.Errorf("LastSuccessful = %+v, want tag v1.0.0 (v1.1.0 was rolled back)", last)
	}
}

func TestLastSuccessfulNoneReturnsNil(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	last, err := s.LastSuccessful(context.Background(), "no-such-region")
	if err != nil {
		t.Fatalf("LastSuccessful err = %v", err)
	}
	if last != nil {
		t.Errorf("LastSuccessful = %+v, want nil", last)
	}
}

func TestLastSuccessfulScopedByRegion(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	id1, _ := s.Start(ctx, "us-east", "v1.0.0", "", "alice")
	_ = s.Complete(ctx, id1, OutcomeSuccess, SoakPassed)

	id2, _ := s.Start(ctx, "us-west", "v2.0.0", "", "bob")
	_ = s.Complete(ctx, id2, OutcomeSuccess, SoakPassed)

	east, _ := s.LastSuccessful(ctx, "us-east")
	if east == nil || east.Tag != "v1.0.0" {
		t.Errorf("us-east LastSuccessful = %+v, want v1.0.0", east)
	}
	west, _ := s.LastSuccessful(ctx, "us-west")
	if west == nil || west.Tag != "v2.0.0" {
		t.Errorf("us-west LastSuccessful = %+v, want v2.0.0", west)
	}
}

func TestFailStampsFailureLog(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	id, _ := s.Start(ctx, "us-east", "v1.0.0", "", "alice")
	if err := s.Fail(ctx, id, OutcomeSoakFailed, "soak check tripped at t+45s: 503 from /health"); err != nil {
		t.Fatal(err)
	}

	var e Entry
	if err := db.First(&e, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if e.Outcome != OutcomeSoakFailed {
		t.Errorf("Outcome = %q, want %q", e.Outcome, OutcomeSoakFailed)
	}
	if e.FailureLog == "" {
		t.Error("FailureLog should be populated")
	}
	if e.CompletedAt == nil {
		t.Error("CompletedAt should be set on Fail")
	}
}

func TestWithinEligibility(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	id1, _ := s.Start(ctx, "us-east", "v1.0.0", "", "alice")
	_ = s.Complete(ctx, id1, OutcomeSuccess, SoakPassed)

	ok, err := s.WithinEligibility(ctx, "us-east", 4*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("recent success should be within 4h eligibility")
	}

	// Backdate to 5h ago.
	if err := db.Model(&Entry{}).Where("id = ?", id1).Update("completed_at", time.Now().UTC().Add(-5*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	ok, _ = s.WithinEligibility(ctx, "us-east", 4*time.Hour)
	if ok {
		t.Error("5h-old success should NOT be within 4h eligibility")
	}
}

func TestWithinEligibilityNoHistory(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ok, err := s.WithinEligibility(context.Background(), "fresh-region", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("no history should report ineligible")
	}
}

func TestContractCrossings(t *testing.T) {
	dir := t.TempDir()
	// One migration with the contract annotation, one without, plus a template
	// that must be skipped.
	if err := os.WriteFile(filepath.Join(dir, "010_safe.sql"), []byte("-- Phase: expand\nCREATE TABLE x();\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "011_contract.sql"), []byte("-- migration-contract: drop old column\nALTER TABLE x DROP COLUMN y;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "TEMPLATE.sql"), []byte("-- migration-contract: should be skipped because TEMPLATE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not sql"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := newTestDB(t)
	s := NewStore(db).WithMigrationsDir(dir)

	got, err := s.ContractCrossings(context.Background(), "us-east", "v1.0.0", "v1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "011_contract.sql" {
		t.Errorf("ContractCrossings = %v, want [011_contract.sql]", got)
	}
}

func TestContractCrossingsMissingDir(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db).WithMigrationsDir(filepath.Join(t.TempDir(), "does-not-exist"))
	_, err := s.ContractCrossings(context.Background(), "us-east", "v1.0.0", "v1.1.0")
	if err == nil {
		t.Error("expected error for missing migrations dir")
	}
}
