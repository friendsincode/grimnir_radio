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

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

func TestBackupDrillReportsTimings(t *testing.T) {
	f := runner.NewFake()
	f.SetResponsePrefix("docker run", "started\n", "", 0, nil)
	f.SetResponsePrefix("pgbackrest restore", "restored\n", "", 0, nil)
	f.SetResponsePrefix("docker rm -f", "", "", 0, nil)
	_, store, ntfy := setupTestEnv(t)
	rec := audit.NewRecorder(store, nullPoster{}, "grimnir-audit-test")
	_ = ntfy
	w := audit.NewWrapper(rec, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runBackupDrill(context.Background(), BackupDrillOpts{
		Region: "us-east", DrillHost: "drill-host",
		Runner: f, Wrapper: w, Out: &out,
	})
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	if !strings.Contains(out.String(), "RTO") {
		t.Errorf("output missing RTO timing: %s", out.String())
	}
}

func TestBackupDrillDryRunSkipsMutations(t *testing.T) {
	f := runner.NewFake()
	_, store, _ := setupTestEnv(t)
	rec := audit.NewRecorder(store, nullPoster{}, "grimnir-audit-test")
	w := audit.NewWrapper(rec, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runBackupDrill(context.Background(), BackupDrillOpts{
		Region: "us-east", DrillHost: "drill-host",
		Runner: f, Wrapper: w, Out: &out, DryRun: true,
	})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(out.String(), "[dry-run]") {
		t.Errorf("dry-run output missing marker: %s", out.String())
	}
	if len(f.Calls) != 0 {
		t.Errorf("dry-run should not invoke runner; got %d calls", len(f.Calls))
	}
}

func TestBackupDrillReportsRestoreFailure(t *testing.T) {
	f := runner.NewFake()
	f.SetResponsePrefix("docker run", "started\n", "", 0, nil)
	f.SetResponsePrefix("pgbackrest restore", "", "no backup found", 1, nil)
	f.SetResponsePrefix("docker rm -f", "", "", 0, nil)
	_, store, _ := setupTestEnv(t)
	rec := audit.NewRecorder(store, nullPoster{}, "grimnir-audit-test")
	w := audit.NewWrapper(rec, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runBackupDrill(context.Background(), BackupDrillOpts{
		Region: "us-east", DrillHost: "drill-host",
		Runner: f, Wrapper: w, Out: &out,
	})
	if err == nil {
		t.Fatalf("expected pgbackrest restore failure, got nil")
	}
	if !strings.Contains(err.Error(), "pgbackrest restore") {
		t.Errorf("error should mention pgbackrest restore; got: %v", err)
	}
}
