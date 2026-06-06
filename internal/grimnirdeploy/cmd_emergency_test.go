/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/pause"
)

func newTestDeps(t *testing.T) (*pause.Client, *audit.Wrapper, *gorm.DB, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	pc := pause.NewClient(rdb)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&audit.Entry{}); err != nil {
		t.Fatal(err)
	}
	store := audit.NewStore(db)
	rec := audit.NewRecorder(store, nil, "")
	w := audit.NewWrapper(rec, "alice", "10.0.0.1")
	return pc, w, db, mr
}

func TestRunEmergencyPause_SetsKey(t *testing.T) {
	pc, w, _, _ := newTestDeps(t)
	var out bytes.Buffer
	err := runEmergencyPause(context.Background(), runEmergencyOpts{
		Region: "default", Reason: "fixing #999", DryRun: false,
		Pause: pc, Wrapper: w, Out: &out,
	})
	if err != nil {
		t.Fatalf("runEmergencyPause: %v", err)
	}
	st, _ := pc.Read(context.Background(), "default")
	if st == nil {
		t.Fatal("pause not set")
	}
	if st.Reason != "fixing #999" {
		t.Errorf("Reason = %q, want fixing #999", st.Reason)
	}
	if st.Operator != "alice" {
		t.Errorf("Operator = %q, want alice", st.Operator)
	}
	if st.Region != "default" {
		t.Errorf("Region = %q, want default", st.Region)
	}
	if !strings.Contains(out.String(), "SET") {
		t.Errorf("expected SET message; got %q", out.String())
	}
}

func TestRunEmergencyPause_DryRunDoesNotSet(t *testing.T) {
	pc, w, _, _ := newTestDeps(t)
	var out bytes.Buffer
	err := runEmergencyPause(context.Background(), runEmergencyOpts{
		Region: "default", Reason: "drill", DryRun: true,
		Pause: pc, Wrapper: w, Out: &out,
	})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	st, _ := pc.Read(context.Background(), "default")
	if st != nil {
		t.Errorf("dry-run set the key: %+v", st)
	}
	if !strings.Contains(out.String(), "dry-run") {
		t.Errorf("expected dry-run message; got %q", out.String())
	}
}

func TestRunEmergencyPause_RegionScoped(t *testing.T) {
	pc, w, _, _ := newTestDeps(t)
	var out bytes.Buffer
	err := runEmergencyPause(context.Background(), runEmergencyOpts{
		Region: "us-east", Reason: "issue", Pause: pc, Wrapper: w, Out: &out,
	})
	if err != nil {
		t.Fatalf("us-east pause: %v", err)
	}
	if st, _ := pc.Read(context.Background(), "eu-west"); st != nil {
		t.Errorf("eu-west should not be paused; got %+v", st)
	}
	if st, _ := pc.Read(context.Background(), "us-east"); st == nil {
		t.Error("us-east should be paused")
	}
}

func TestRunEmergencyPause_RedisDownAuditsFailed(t *testing.T) {
	pc, w, db, mr := newTestDeps(t)
	mr.Close() // kill Redis after building deps

	var out bytes.Buffer
	// Mirror the wrap that realEmergencyPauseRunE does. Asserts that when
	// Redis is dead, the audit row lands as phase=failed so operators can
	// see the failed attempt in audit_log even though Redis itself never
	// recorded the pause.
	err := w.Wrap(context.Background(), "emergency-pause", map[string]any{"reason": "x"}, func(ctx context.Context) error {
		return runEmergencyPause(ctx, runEmergencyOpts{
			Region: "default", Reason: "x", Pause: pc, Wrapper: w, Out: &out,
		})
	})
	if err == nil {
		t.Fatal("expected error with Redis down")
	}

	// Verify the audit row was written with phase=failed.
	var rows []audit.Entry
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("audit row count = %d, want 1", len(rows))
	}
	if rows[0].Phase != audit.PhaseFailed {
		t.Errorf("Phase = %q, want %q", rows[0].Phase, audit.PhaseFailed)
	}
	if rows[0].Subcommand != "emergency-pause" {
		t.Errorf("Subcommand = %q, want emergency-pause", rows[0].Subcommand)
	}
}

func TestRunEmergencyResume_ClearsKey(t *testing.T) {
	pc, w, _, _ := newTestDeps(t)
	_ = pc.Set(context.Background(), "default", "prior", "bob", 0)

	var out bytes.Buffer
	err := runEmergencyResume(context.Background(), runEmergencyOpts{
		Region: "default", Reason: "incident resolved",
		Pause: pc, Wrapper: w, Out: &out,
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if st, _ := pc.Read(context.Background(), "default"); st != nil {
		t.Errorf("resume did not clear: %+v", st)
	}
	if !strings.Contains(out.String(), "CLEARED") {
		t.Errorf("expected CLEARED message; got %q", out.String())
	}
}

func TestRunEmergencyResume_NoPauseSet(t *testing.T) {
	pc, w, _, _ := newTestDeps(t)
	var out bytes.Buffer
	err := runEmergencyResume(context.Background(), runEmergencyOpts{
		Region: "default", Reason: "x", Pause: pc, Wrapper: w, Out: &out,
	})
	if err != nil {
		t.Fatalf("resume on empty: %v", err)
	}
	if !strings.Contains(out.String(), "nothing to clear") {
		t.Errorf("expected 'nothing to clear'; got %q", out.String())
	}
}

func TestRunEmergencyResume_DryRunDoesNotClear(t *testing.T) {
	pc, w, _, _ := newTestDeps(t)
	_ = pc.Set(context.Background(), "default", "prior", "bob", 0)

	var out bytes.Buffer
	err := runEmergencyResume(context.Background(), runEmergencyOpts{
		Region: "default", Reason: "drill", DryRun: true,
		Pause: pc, Wrapper: w, Out: &out,
	})
	if err != nil {
		t.Fatalf("dry-run resume: %v", err)
	}
	if st, _ := pc.Read(context.Background(), "default"); st == nil {
		t.Error("dry-run cleared the key")
	}
}
