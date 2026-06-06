/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&Entry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestWriteStartThenComplete(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	id, err := s.WriteStart(ctx, "alice", "10.0.0.1", "deploy", map[string]any{"tag": "v1.2.3"})
	if err != nil {
		t.Fatalf("WriteStart: %v", err)
	}
	if id.String() == "" {
		t.Fatal("WriteStart returned empty id")
	}

	if err := s.WriteComplete(ctx, id, "success", 1234*time.Millisecond, "ok"); err != nil {
		t.Fatalf("WriteComplete: %v", err)
	}

	var got Entry
	if err := db.First(&got, "id = ?", id).Error; err != nil {
		t.Fatalf("query back: %v", err)
	}
	if got.Phase != PhaseCompleted {
		t.Errorf("Phase = %q, want %q", got.Phase, PhaseCompleted)
	}
	if got.Outcome != "success" {
		t.Errorf("Outcome = %q, want success", got.Outcome)
	}
	if got.DurationMS != 1234 {
		t.Errorf("DurationMS = %d, want 1234", got.DurationMS)
	}
	if got.Notes != "ok" {
		t.Errorf("Notes = %q, want ok", got.Notes)
	}
}

func TestSecretRedaction(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	args := map[string]any{
		"tag":               "v1.2.3",
		"ntfy-token":        "tk_secret",
		"postgres-password": "hunter2",
		"ssh-key":           "PRIVATEKEY",
		"signing-secret":    "sssh",
		"reason":            "fixing #999",
	}
	id, err := s.WriteStart(ctx, "alice", "10.0.0.1", "deploy", args)
	if err != nil {
		t.Fatalf("WriteStart: %v", err)
	}
	var got Entry
	if err := db.First(&got, "id = ?", id).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if strings.Contains(got.ArgsJSON, "tk_secret") {
		t.Errorf("ntfy-token was not redacted: %s", got.ArgsJSON)
	}
	if strings.Contains(got.ArgsJSON, "hunter2") {
		t.Errorf("postgres-password was not redacted: %s", got.ArgsJSON)
	}
	if strings.Contains(got.ArgsJSON, "PRIVATEKEY") {
		t.Errorf("ssh-key was not redacted: %s", got.ArgsJSON)
	}
	if strings.Contains(got.ArgsJSON, "sssh") {
		t.Errorf("signing-secret was not redacted: %s", got.ArgsJSON)
	}
	if !strings.Contains(got.ArgsJSON, "fixing #999") {
		t.Errorf("reason should NOT be redacted: %s", got.ArgsJSON)
	}
	if !strings.Contains(got.ArgsJSON, "REDACTED") {
		t.Errorf("redactor should leave REDACTED markers: %s", got.ArgsJSON)
	}
}

func TestWriteCompleteFailedPhase(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	id, _ := s.WriteStart(ctx, "alice", "10.0.0.1", "deploy", nil)
	if err := s.WriteFailed(ctx, id, "image not found in registry", 500*time.Millisecond); err != nil {
		t.Fatalf("WriteFailed: %v", err)
	}
	var got Entry
	_ = db.First(&got, "id = ?", id).Error
	if got.Phase != PhaseFailed {
		t.Errorf("Phase = %q, want %q", got.Phase, PhaseFailed)
	}
	if got.Outcome != "image not found in registry" {
		t.Errorf("Outcome = %q", got.Outcome)
	}
	if got.DurationMS != 500 {
		t.Errorf("DurationMS = %d, want 500", got.DurationMS)
	}
}

func TestWriteStartNilArgs(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	id, err := s.WriteStart(context.Background(), "alice", "10.0.0.1", "verify", nil)
	if err != nil {
		t.Fatalf("WriteStart with nil args: %v", err)
	}
	var got Entry
	_ = db.First(&got, "id = ?", id).Error
	if got.ArgsJSON != "{}" {
		t.Errorf("ArgsJSON for nil args = %q, want {}", got.ArgsJSON)
	}
	if got.Phase != PhaseStarted {
		t.Errorf("Phase = %q, want %q", got.Phase, PhaseStarted)
	}
}
