/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/history"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

// newAuditTestDB builds a sqlite DB with both deploy_history + audit_log
// migrated. The rollback tests use it for the history store; the audit store
// rides on a separate sqlite from setupTestEnv. Two DBs is fine because the
// rollback path Start/Complete writes deploy_history rows but the Wrapper
// writes audit_log rows; nothing joins across them.
func newAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	if err := db.AutoMigrate(&history.Entry{}, &audit.Entry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestRollbackRefusesWithoutReason(t *testing.T) {
	f := runner.NewFake()
	pc, store, ntfy := setupTestEnv(t)
	histStore := history.NewStore(newAuditTestDB(t))
	id, err := histStore.Start(context.Background(), "us-east", "v1.0.0", "", "alice")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := histStore.Complete(context.Background(), id, history.OutcomeSuccess, history.SoakPassed); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	w := audit.NewWrapper(testRecorder{store: store, ntfy: ntfy}, "alice", "10.0.0.1")
	err = runRollback(context.Background(), RollbackOpts{
		Cfg:         &Config{Region: "us-east", RollbackWindow: 4 * time.Hour},
		Reason:      "",
		Pause:       pc,
		History:     histStore,
		Wrapper:     w,
		Out:         &bytes.Buffer{},
		Runner:      f,
		Compose:     runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		HealthProbe: alwaysHealthy{},
		Hosts:       []string{"local", "node-2"},
		FirstHost:   "node-2",
		SecondHost:  "local",
		Sleep:       func(time.Duration) {},
	})
	if err == nil || !strings.Contains(err.Error(), "reason") {
		t.Errorf("expected reason-required error; got %v", err)
	}
}

func TestRollbackRefusesAgedRollback(t *testing.T) {
	f := runner.NewFake()
	pc, store, ntfy := setupTestEnv(t)
	histStore := history.NewStore(newAuditTestDB(t))
	id, err := histStore.Start(context.Background(), "us-east", "v1.0.0", "", "alice")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := histStore.Complete(context.Background(), id, history.OutcomeSuccess, history.SoakPassed); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// Set the docker inspect response so CurrentTag returns something other
	// than the last-successful tag; otherwise the "nothing to roll back" check
	// short-circuits before we hit the eligibility refusal.
	f.SetResponsePrefix("docker inspect --format", "ghcr.io/friendsincode/grimnir-radio:v1.1.0\n", "", 0, nil)

	w := audit.NewWrapper(testRecorder{store: store, ntfy: ntfy}, "alice", "10.0.0.1")
	err = runRollback(context.Background(), RollbackOpts{
		Cfg:         &Config{Region: "us-east", RollbackWindow: 0},
		Reason:      "incident",
		Pause:       pc,
		History:     histStore,
		Wrapper:     w,
		Out:         &bytes.Buffer{},
		Runner:      f,
		Compose:     runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		HealthProbe: alwaysHealthy{},
		Hosts:       []string{"local", "node-2"},
		FirstHost:   "node-2",
		SecondHost:  "local",
		ForceAged:   false,
		Sleep:       func(time.Duration) {},
	})
	if err == nil || !strings.Contains(err.Error(), "eligibility") {
		t.Errorf("expected eligibility-window refusal; got %v", err)
	}
}

func TestRollbackRefusesContractCrossing(t *testing.T) {
	f := runner.NewFake()
	pc, store, ntfy := setupTestEnv(t)

	db := newAuditTestDB(t)
	histStore := history.NewStore(db)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "100_contract.sql"),
		[]byte("-- migration-contract: drop col\nALTER TABLE x DROP COLUMN y;\n"), 0o644); err != nil {
		t.Fatalf("write migration: %v", err)
	}
	histStore = histStore.WithMigrationsDir(dir)

	id, err := histStore.Start(context.Background(), "us-east", "v1.0.0", "", "alice")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := histStore.Complete(context.Background(), id, history.OutcomeSuccess, history.SoakPassed); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	w := audit.NewWrapper(testRecorder{store: store, ntfy: ntfy}, "alice", "10.0.0.1")
	f.SetResponsePrefix("docker inspect --format", "ghcr.io/friendsincode/grimnir-radio:v1.1.0\n", "", 0, nil)
	err = runRollback(context.Background(), RollbackOpts{
		Cfg:           &Config{Region: "us-east", RollbackWindow: 4 * time.Hour},
		Reason:        "incident",
		Pause:         pc,
		History:       histStore,
		Wrapper:       w,
		Out:           &bytes.Buffer{},
		Runner:        f,
		Compose:       runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		HealthProbe:   alwaysHealthy{},
		Hosts:         []string{"local", "node-2"},
		FirstHost:     "node-2",
		SecondHost:    "local",
		ForceContract: false,
		Sleep:         func(time.Duration) {},
	})
	if err == nil || !strings.Contains(err.Error(), "contract") {
		t.Errorf("expected contract-crossing refusal; got %v", err)
	}
}

func TestRollbackRefusesNoPreviousDeploy(t *testing.T) {
	f := runner.NewFake()
	pc, store, ntfy := setupTestEnv(t)
	histStore := history.NewStore(newAuditTestDB(t))

	w := audit.NewWrapper(testRecorder{store: store, ntfy: ntfy}, "alice", "10.0.0.1")
	err := runRollback(context.Background(), RollbackOpts{
		Cfg:         &Config{Region: "us-east", RollbackWindow: 4 * time.Hour},
		Reason:      "incident",
		Pause:       pc,
		History:     histStore,
		Wrapper:     w,
		Out:         &bytes.Buffer{},
		Runner:      f,
		Compose:     runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		HealthProbe: alwaysHealthy{},
		Hosts:       []string{"local", "node-2"},
		FirstHost:   "node-2",
		SecondHost:  "local",
		Sleep:       func(time.Duration) {},
	})
	if err == nil || !strings.Contains(err.Error(), "no previous successful") {
		t.Errorf("expected no-previous-deploy error; got %v", err)
	}
}
