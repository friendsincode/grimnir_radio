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
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/probe"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

// restoreFakeProbeOK returns healthy results for every host so the post-restore
// verify step passes by default.
func restoreFakeProbeOK() *fakeAllProber {
	return &fakeAllProber{result: probe.Result{
		ControlPlaneOK: true, MediaEngineOK: true, EdgeEncoderOK: true, FanOutOK: true,
	}}
}

// primeRestoreRunner registers canned responses for every runner command the
// happy-path restore flow issues: pgbackrest info (with a known backup id and
// WAL window), compose stop/up, pgbackrest restore, systemctl, pg_isready.
func primeRestoreRunner(f *runner.Fake) {
	f.SetResponsePrefix("cd /srv/docker", "", "", 0, nil)
	f.SetResponsePrefix("pgbackrest info",
		`stanza: grimnir
    status: ok
        full backup: 20260601-120000F
            timestamp start/stop: 2026-06-01 12:00:00 / 2026-06-01 12:05:00
        archive min/max: 000000010000000000000001 / 0000000100000000000000FF
        oldest wal timestamp: 2026-06-01 12:00:00+00
`, "", 0, nil)
	f.SetResponsePrefix("pgbackrest restore", "restored\n", "", 0, nil)
	f.SetResponsePrefix("systemctl", "", "", 0, nil)
	f.SetResponsePrefix("pg_isready", "ok\n", "", 0, nil)
}

func TestRestoreLatestBuildsExpectedCommand(t *testing.T) {
	f := runner.NewFake()
	primeRestoreRunner(f)

	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runRestore(context.Background(), RestoreOpts{
		From: "latest", Hosts: []string{"local", "node-2"},
		Runner:  f,
		Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		Prober:  restoreFakeProbeOK(),
		Wrapper: w, Out: &out,
		PostgresIsRunning: func(_ context.Context, _ string) bool { return false },
		Sleep:             func(_ interface{}) {},
	})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	found := false
	for _, c := range f.Calls {
		if strings.Contains(c.Cmd, "pgbackrest restore --stanza=grimnir") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected pgbackrest restore call; got: %+v", f.Calls)
	}
}

func TestRestoreWithTargetTimeAddsPITRFlags(t *testing.T) {
	f := runner.NewFake()
	primeRestoreRunner(f)
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	err := runRestore(context.Background(), RestoreOpts{
		From: "latest", TargetTime: "2026-06-01T12:30:00Z",
		Hosts: []string{"local"}, Runner: f,
		Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		Prober:  restoreFakeProbeOK(),
		Wrapper: w, Out: &bytes.Buffer{},
		PostgresIsRunning: func(_ context.Context, _ string) bool { return false },
		Sleep:             func(_ interface{}) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range f.Calls {
		if strings.Contains(c.Cmd, "--type=time") && strings.Contains(c.Cmd, "2026-06-01T12:30:00Z") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected PITR flags in pgbackrest call; got: %+v", f.Calls)
	}
}

// TestRestoreRefusesWhenBackupIDMissing: if --from=<id> isn't in `pgbackrest
// info` output, refuse before stopping any services.
func TestRestoreRefusesWhenBackupIDMissing(t *testing.T) {
	f := runner.NewFake()
	primeRestoreRunner(f)
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	err := runRestore(context.Background(), RestoreOpts{
		From: "99999999-doesnotexist", Hosts: []string{"local"},
		Runner:  f,
		Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		Prober:  restoreFakeProbeOK(),
		Wrapper: w, Out: &bytes.Buffer{},
		PostgresIsRunning: func(_ context.Context, _ string) bool { return false },
		Sleep:             func(_ interface{}) {},
	})
	if err == nil {
		t.Fatal("restore should refuse when backup id is unknown")
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "unknown") {
		t.Errorf("error should mention missing backup id: %v", err)
	}
	for _, c := range f.Calls {
		if strings.Contains(c.Cmd, "docker compose stop") {
			t.Errorf("pre-flight refusal must not stop services; saw: %s", c.Cmd)
		}
		if strings.Contains(c.Cmd, "pgbackrest restore") {
			t.Errorf("pre-flight refusal must not run pgbackrest restore; saw: %s", c.Cmd)
		}
	}
}

// TestRestoreRefusesTargetTimeOlderThanOldestWAL: --target-time before the
// archive window has no WAL to replay and would silently restore to an
// arbitrary point. Refuse instead.
func TestRestoreRefusesTargetTimeOlderThanOldestWAL(t *testing.T) {
	f := runner.NewFake()
	primeRestoreRunner(f)
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	err := runRestore(context.Background(), RestoreOpts{
		From: "latest", TargetTime: "2020-01-01T00:00:00Z",
		Hosts: []string{"local"}, Runner: f,
		Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		Prober:  restoreFakeProbeOK(),
		Wrapper: w, Out: &bytes.Buffer{},
		PostgresIsRunning: func(_ context.Context, _ string) bool { return false },
		Sleep:             func(_ interface{}) {},
	})
	if err == nil {
		t.Fatal("restore should refuse when --target-time predates oldest WAL")
	}
	if !strings.Contains(err.Error(), "target-time") && !strings.Contains(err.Error(), "WAL") {
		t.Errorf("error should mention target-time/WAL window: %v", err)
	}
}

// TestRestoreRefusesWhenPostgresStillRunning: pg_isready returning 0 before
// the stop sequence signals Postgres is still up. Restoring on top of a live
// cluster corrupts the data directory.
func TestRestoreRefusesWhenPostgresStillRunning(t *testing.T) {
	f := runner.NewFake()
	primeRestoreRunner(f)
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")

	err := runRestore(context.Background(), RestoreOpts{
		From: "latest", Hosts: []string{"local"},
		Runner:  f,
		Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		Prober:  restoreFakeProbeOK(),
		Wrapper: w, Out: &bytes.Buffer{},
		PostgresIsRunning: func(_ context.Context, _ string) bool { return true },
		Sleep:             func(_ interface{}) {},
	})
	if err == nil {
		t.Fatal("restore should refuse when Postgres is still running on target")
	}
	if !strings.Contains(err.Error(), "Postgres") && !strings.Contains(err.Error(), "still running") {
		t.Errorf("error should mention Postgres still running: %v", err)
	}
}

func TestRestoreDryRunMakesNoMutatingCalls(t *testing.T) {
	f := runner.NewFake()
	primeRestoreRunner(f)
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runRestore(context.Background(), RestoreOpts{
		From: "latest", Hosts: []string{"local", "node-2"},
		Runner:  f,
		Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		Prober:  restoreFakeProbeOK(),
		Wrapper: w, Out: &out, DryRun: true,
		Sleep: func(_ interface{}) {},
	})
	if err != nil {
		t.Fatalf("dry-run restore: %v", err)
	}
	for _, c := range f.Calls {
		if strings.Contains(c.Cmd, "docker compose stop") ||
			strings.Contains(c.Cmd, "pgbackrest restore") ||
			strings.Contains(c.Cmd, "systemctl start") ||
			strings.Contains(c.Cmd, "up -d") {
			t.Errorf("dry-run made mutating call: %s", c.Cmd)
		}
	}
	if !strings.Contains(out.String(), "[dry-run]") {
		t.Errorf("dry-run output missing marker: %q", out.String())
	}
}
