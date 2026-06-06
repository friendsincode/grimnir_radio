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
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/gates"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/history"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

// alwaysHealthy is a HealthProbe that always returns nil. Used by tests that
// want to exercise the orchestration path past the health-gate.
type alwaysHealthy struct{}

func (alwaysHealthy) Probe(_ context.Context, _ string) error { return nil }

// alwaysSick is a HealthProbe that always returns a fixed error. Drives the
// wait-for-health timeout + revert path.
type alwaysSick struct{ msg string }

func (s alwaysSick) Probe(_ context.Context, _ string) error { return errFn(s.msg) }

type errFn string

func (e errFn) Error() string { return string(e) }

// newDeployTestDB builds a sqlite DB with both deploy_history + audit_log
// migrated so the orchestration test can read back the row Start/Complete
// wrote.
func newDeployTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&history.Entry{}, &audit.Entry{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRunDeployHappyPath(t *testing.T) {
	f := runner.NewFake()
	// docker manifest inspect succeeds on both hosts.
	f.SetResponsePrefix("docker manifest inspect", "{}", "", 0, nil)
	// ./grimnir compose wrapper accepts every operation.
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && ./grimnir", "ok\n", "", 0, nil)
	// docker compose stop <svc>
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && docker compose", "ok\n", "", 0, nil)
	// migration step
	f.SetResponsePrefix("docker run", "migrated\n", "", 0, nil)
	// VRRP toggle file ops.
	f.SetResponsePrefix("touch", "", "", 0, nil)
	f.SetResponsePrefix("rm -f", "", "", 0, nil)
	// Current tag query: container reports its image tag.
	f.SetResponsePrefix("docker inspect --format", "ghcr.io/friendsincode/grimnir-radio:v1.0.0\n", "", 0, nil)
	// revert path (not hit on happy path; safe pre-stage)
	f.SetResponsePrefix("docker pull", "", "", 0, nil)

	pc, store, ntfy := setupTestEnv(t)
	db := newDeployTestDB(t)
	histStore := history.NewStore(db)
	w := audit.NewWrapper(testRecorder{store: store, ntfy: ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer

	opts := DeployOpts{
		Tag:               "v1.1.0",
		Cfg:               &Config{Region: "us-east", SoakWindow: 10 * time.Millisecond, DeployPolicy: "auto"},
		Hosts:             []string{"local", "node-2"},
		FirstHost:         "node-2",
		SecondHost:        "local",
		Runner:            f,
		Compose:           runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		HealthProbe:       alwaysHealthy{},
		Pause:             pc,
		History:           histStore,
		Wrapper:           w,
		Out:               &out,
		Sleep:             func(_ time.Duration) {},
		HealthWaitTimeout: 100 * time.Millisecond,
	}

	if err := runDeploy(context.Background(), opts); err != nil {
		t.Fatalf("runDeploy: %v", err)
	}
	if !strings.Contains(out.String(), "soak passed") {
		t.Errorf("expected soak passed in output; got:\n%s", out.String())
	}

	last, err := histStore.LastSuccessful(context.Background(), "us-east")
	if err != nil {
		t.Fatalf("LastSuccessful: %v", err)
	}
	if last == nil {
		t.Fatal("expected deploy_history success row")
	}
	if last.Tag != "v1.1.0" {
		t.Errorf("history row tag = %q, want v1.1.0", last.Tag)
	}
	if last.PreviousTag != "v1.0.0" {
		t.Errorf("history row previous_tag = %q, want v1.0.0", last.PreviousTag)
	}
	if last.SoakOutcome != history.SoakPassed {
		t.Errorf("soak_outcome = %q, want %q", last.SoakOutcome, history.SoakPassed)
	}
}

func TestRunDeployAbortsOnPause(t *testing.T) {
	f := runner.NewFake()
	pc, store, ntfy := setupTestEnv(t)
	_ = pc.Set(context.Background(), "us-east", "incident #999", "bob", 0)
	w := audit.NewWrapper(testRecorder{store: store, ntfy: ntfy}, "alice", "10.0.0.1")
	opts := DeployOpts{
		Tag:               "v1.1.0",
		Cfg:               &Config{Region: "us-east", SoakWindow: 0, DeployPolicy: "auto"},
		Hosts:             []string{"local", "node-2"},
		FirstHost:         "node-2",
		SecondHost:        "local",
		Runner:            f,
		Compose:           runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		HealthProbe:       alwaysHealthy{},
		Pause:             pc,
		History:           history.NewStore(newDeployTestDB(t)),
		Wrapper:           w,
		Out:               &bytes.Buffer{},
		Sleep:             func(_ time.Duration) {},
		HealthWaitTimeout: 100 * time.Millisecond,
	}
	err := runDeploy(context.Background(), opts)
	if !gates.IsAborted(err) {
		t.Errorf("expected Aborted; got %v", err)
	}
}

func TestRunDeployRevertsOnFirstNodeUnhealthy(t *testing.T) {
	f := runner.NewFake()
	f.SetResponsePrefix("docker manifest inspect", "{}", "", 0, nil)
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && ./grimnir", "ok\n", "", 0, nil)
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && docker compose", "ok\n", "", 0, nil)
	f.SetResponsePrefix("docker run", "migrated\n", "", 0, nil)
	f.SetResponsePrefix("touch", "", "", 0, nil)
	f.SetResponsePrefix("rm -f", "", "", 0, nil)
	f.SetResponsePrefix("docker inspect --format", "ghcr.io/friendsincode/grimnir-radio:v1.0.0\n", "", 0, nil)
	f.SetResponsePrefix("docker pull", "", "", 0, nil)

	// The pause gate + image gate run before wait-for-health; both need to
	// pass for revert to be reached. Health probe stays sick across the
	// orchestration: gate evaluation uses it once (both hosts), then the
	// wait-for-health on the first node keeps failing -> revert.
	pc, store, ntfy := setupTestEnv(t)
	histDB := newDeployTestDB(t)
	histStore := history.NewStore(histDB)
	w := audit.NewWrapper(testRecorder{store: store, ntfy: ntfy}, "alice", "10.0.0.1")

	// We need both-nodes-healthy to pass, then per-node health to fail. Two
	// probes in one test: use a counter-based probe.
	hp := &countingProbe{healthyUntilCall: 2, msg: "503"}
	opts := DeployOpts{
		Tag:               "v1.1.0",
		Cfg:               &Config{Region: "us-east", SoakWindow: 0, DeployPolicy: "auto"},
		Hosts:             []string{"local", "node-2"},
		FirstHost:         "node-2",
		SecondHost:        "local",
		Runner:            f,
		Compose:           runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		HealthProbe:       hp,
		Pause:             pc,
		History:           histStore,
		Wrapper:           w,
		Out:               &bytes.Buffer{},
		Sleep:             func(_ time.Duration) {},
		HealthWaitTimeout: 5 * time.Millisecond,
	}
	err := runDeploy(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error on unhealthy first node")
	}
	// No successful history row should exist.
	last, _ := histStore.LastSuccessful(context.Background(), "us-east")
	if last != nil {
		t.Errorf("no successful row expected; got %+v", last)
	}
	// One row should exist with outcome=rolled_back_mid_roll.
	var rows []history.Entry
	if err := histDB.Find(&rows).Error; err != nil {
		t.Fatalf("query history rows: %v", err)
	}
	if len(rows) != 1 || rows[0].Outcome != history.OutcomeRolledBackMidRoll {
		t.Errorf("expected one rolled_back_mid_roll row; got %+v", rows)
	}
}

// countingProbe returns nil for the first healthyUntilCall calls, then errors.
// Drives the wait-for-health revert path: the pre-flight HealthGate sees 2
// healthy probes (one per host), the per-node wait-for-health loop sees only
// errors.
type countingProbe struct {
	healthyUntilCall int
	msg              string
	calls            int
}

func (c *countingProbe) Probe(_ context.Context, _ string) error {
	c.calls++
	if c.calls <= c.healthyUntilCall {
		return nil
	}
	return errFn(c.msg)
}
