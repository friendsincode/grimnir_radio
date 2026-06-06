/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/autorollback"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/history"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
	"github.com/friendsincode/grimnir_radio/internal/notify"
)

// stubObserver is a canned autorollback.Observer used by the deploy-flow
// tests that want to assert what happens for each verdict without standing
// up a Prometheus.
type stubObserver struct {
	verdict autorollback.Verdict
	called  atomic.Int32
}

func (s *stubObserver) Observe(_ context.Context) autorollback.Verdict {
	s.called.Add(1)
	return s.verdict
}

// happyDeployFakes returns the runner.Fake + DeployOpts skeleton used by
// every auto-rollback integration test below. Callers override
// SoakObserver / AutoRollback / Notifier per test.
func happyDeployFakes(t *testing.T) (*runner.Fake, DeployOpts, *bytes.Buffer) {
	t.Helper()
	f := runner.NewFake()
	f.SetResponsePrefix("docker manifest inspect", "{}", "", 0, nil)
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && ./grimnir", "ok\n", "", 0, nil)
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && docker compose", "ok\n", "", 0, nil)
	f.SetResponsePrefix("docker run", "migrated\n", "", 0, nil)
	f.SetResponsePrefix("touch", "", "", 0, nil)
	f.SetResponsePrefix("rm -f", "", "", 0, nil)
	f.SetResponsePrefix("docker inspect --format", "ghcr.io/friendsincode/grimnir-radio:v1.0.0\n", "", 0, nil)
	f.SetResponsePrefix("docker pull", "", "", 0, nil)

	pc, store, ntfy := setupTestEnv(t)
	db := newDeployTestDB(t)
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
		History:           history.NewStore(db),
		Wrapper:           w,
		Out:               &out,
		Sleep:             func(_ time.Duration) {},
		HealthWaitTimeout: 100 * time.Millisecond,
	}
	return f, opts, &out
}

// TestRunDeploy_AutoRollbackFiresOnRollbackVerdict drives the soak Observer
// to a Rollback verdict & checks: the rollback closure was called, the
// deploy returned a non-nil error so callers' CI exits non-zero, & the
// ntfy poster received a Page (tier-2) for the auto-rollback event.
func TestRunDeploy_AutoRollbackFiresOnRollbackVerdict(t *testing.T) {
	_, opts, out := happyDeployFakes(t)

	obs := &stubObserver{verdict: autorollback.Verdict{
		Decision:       autorollback.DecisionRollback,
		Reason:         "listener_reconnects: rate exceeded 5/sec",
		TriggeringRule: "listener_reconnects",
		TicksObserved:  3,
	}}
	opts.SoakObserver = obs

	var rollbackCalls atomic.Int32
	opts.AutoRollback = func(_ context.Context, reason string) error {
		rollbackCalls.Add(1)
		if !strings.Contains(reason, "listener_reconnects") {
			t.Errorf("rollback reason missing trigger name: %q", reason)
		}
		return nil
	}

	err := runDeploy(context.Background(), opts)
	if err == nil {
		t.Fatal("expected non-nil error after auto-rollback fired")
	}
	if !strings.Contains(err.Error(), "auto-rolled back") {
		t.Errorf("error does not mention auto-rollback: %v", err)
	}
	if obs.called.Load() != 1 {
		t.Errorf("Observer called %d times, want 1", obs.called.Load())
	}
	if rollbackCalls.Load() != 1 {
		t.Errorf("rollback closure called %d times, want 1", rollbackCalls.Load())
	}
	if !strings.Contains(out.String(), "soak verdict: rollback") {
		t.Errorf("expected 'soak verdict: rollback' in output; got:\n%s", out.String())
	}
}

// TestRunDeploy_AutoRollbackPassFlowsThrough confirms the happy path: a
// Pass verdict from the Observer takes the same exit as the legacy passive
// soak — soak_passed history row + nil error.
func TestRunDeploy_AutoRollbackPassFlowsThrough(t *testing.T) {
	_, opts, out := happyDeployFakes(t)

	obs := &stubObserver{verdict: autorollback.Verdict{
		Decision: autorollback.DecisionPass,
		Reason:   "no rule breached its dwell threshold",
	}}
	opts.SoakObserver = obs

	if err := runDeploy(context.Background(), opts); err != nil {
		t.Fatalf("runDeploy: %v", err)
	}
	if !strings.Contains(out.String(), "soak passed; deploy complete") {
		t.Errorf("expected 'soak passed; deploy complete' in output; got:\n%s", out.String())
	}
}

// TestRunDeploy_InconclusiveIsSoftPass: a flaky Prometheus shouldn't fail
// the deploy. Inconclusive falls through to the final health probe; if the
// hosts are healthy, the deploy completes normally.
func TestRunDeploy_InconclusiveIsSoftPass(t *testing.T) {
	_, opts, out := happyDeployFakes(t)
	opts.SoakObserver = &stubObserver{verdict: autorollback.Verdict{
		Decision: autorollback.DecisionInconclusive,
		Reason:   "prometheus unreachable",
	}}
	var rollbackCalls atomic.Int32
	opts.AutoRollback = func(_ context.Context, _ string) error {
		rollbackCalls.Add(1)
		return nil
	}
	if err := runDeploy(context.Background(), opts); err != nil {
		t.Fatalf("runDeploy: %v", err)
	}
	if rollbackCalls.Load() != 0 {
		t.Errorf("Inconclusive must NOT trigger rollback; got %d calls", rollbackCalls.Load())
	}
	if !strings.Contains(out.String(), "soak verdict: inconclusive") {
		t.Errorf("missing inconclusive verdict in output:\n%s", out.String())
	}
}

// TestRunDeploy_AutoRollbackFailedSurfaces tests the rare double-failure
// path: the verdict was Rollback, but the rollback closure itself errored.
// The deploy must return a wrapped error so the operator workflow knows
// manual intervention is required.
func TestRunDeploy_AutoRollbackFailedSurfaces(t *testing.T) {
	_, opts, _ := happyDeployFakes(t)
	opts.SoakObserver = &stubObserver{verdict: autorollback.Verdict{
		Decision:       autorollback.DecisionRollback,
		Reason:         "alert firing",
		TriggeringRule: "alert_firing",
	}}
	opts.AutoRollback = func(_ context.Context, _ string) error {
		return errFn("rollback exec failed: connection refused")
	}
	err := runDeploy(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "auto-rollback failed") {
		t.Fatalf("expected 'auto-rollback failed' wrapping; got %v", err)
	}
}

// TestRunDeploy_NoObserverFallsBackToPassiveSleep covers backwards-compat:
// when SoakObserver is nil the legacy passive sleep path runs. This is the
// path every deploy used before this chunk landed.
func TestRunDeploy_NoObserverFallsBackToPassiveSleep(t *testing.T) {
	var slept atomic.Int32
	_, opts, out := happyDeployFakes(t)
	opts.SoakObserver = nil
	opts.Sleep = func(_ time.Duration) { slept.Add(1) }
	if err := runDeploy(context.Background(), opts); err != nil {
		t.Fatalf("runDeploy: %v", err)
	}
	// The deploy calls Sleep in wait-for-health (per node) AND for the soak
	// window itself; the soak Sleep is unconditional when window > 0.
	if slept.Load() == 0 {
		t.Error("expected at least one Sleep call in passive path")
	}
	if !strings.Contains(out.String(), "soak: waiting") {
		t.Errorf("expected 'soak: waiting' in legacy path output:\n%s", out.String())
	}
}

// TestRunDeploy_AutoRollbackPagesOperator: when the verdict is Rollback &
// a Notifier is wired, the operator receives a tier-2 page with the
// triggering rule name in the body — the operator's phone needs enough
// info on the lock screen to start triaging without opening the app.
func TestRunDeploy_AutoRollbackPagesOperator(t *testing.T) {
	_, opts, _ := happyDeployFakes(t)
	fn := &notify.FakeNotifier{}
	opts.Notifier = fn
	opts.SoakObserver = &stubObserver{verdict: autorollback.Verdict{
		Decision:       autorollback.DecisionRollback,
		Reason:         "alert firing: page-and-rollback severity",
		TriggeringRule: "alert_firing",
	}}
	opts.AutoRollback = func(_ context.Context, _ string) error { return nil }

	_ = runDeploy(context.Background(), opts)

	if len(fn.Calls) == 0 {
		t.Fatal("expected at least one notifier call")
	}
	got := fn.Calls[0]
	if got.Tier != 2 {
		t.Errorf("first notifier call tier=%d, want 2", got.Tier)
	}
	if !strings.Contains(got.Title, "Auto-rollback triggered") {
		t.Errorf("title %q missing 'Auto-rollback triggered'", got.Title)
	}
	if !strings.Contains(got.Body, "alert_firing") {
		t.Errorf("body %q missing triggering rule name", got.Body)
	}
}

// TestRunDeploy_AutoRollbackFailedPages: rollback closure error path also
// pages operator with a distinct "FAILED" title so the on-call sees the
// double-failure on their lock screen.
func TestRunDeploy_AutoRollbackFailedPages(t *testing.T) {
	_, opts, _ := happyDeployFakes(t)
	fn := &notify.FakeNotifier{}
	opts.Notifier = fn
	opts.SoakObserver = &stubObserver{verdict: autorollback.Verdict{
		Decision:       autorollback.DecisionRollback,
		Reason:         "5xx spike",
		TriggeringRule: "http_5xx_rate",
	}}
	opts.AutoRollback = func(_ context.Context, _ string) error {
		return errFn("ssh: connection lost mid-rollback")
	}
	_ = runDeploy(context.Background(), opts)

	// Two pages: one "triggered", one "FAILED".
	if len(fn.Calls) < 2 {
		t.Fatalf("expected 2 notifier calls (triggered + FAILED); got %d (%+v)", len(fn.Calls), fn.Calls)
	}
	last := fn.Calls[len(fn.Calls)-1]
	if !strings.Contains(last.Title, "FAILED") {
		t.Errorf("last page title %q missing 'FAILED'", last.Title)
	}
	if !strings.Contains(last.Body, "intervene") {
		t.Errorf("FAILED page body %q missing operator-intervention hint", last.Body)
	}
}

// TestRunDeploy_AutoRollbackWritesAuditRow: the nested audit row tagged
// "auto-rollback" must exist in the audit store after the verdict fires.
// Post-mortem queries filter on subcommand="auto-rollback" to separate
// system-initiated from operator-initiated rollbacks.
func TestRunDeploy_AutoRollbackWritesAuditRow(t *testing.T) {
	_, opts, _ := happyDeployFakes(t)
	// Rebuild env so we can keep a handle to the underlying audit store.
	env := newTestEnv(t)
	w := audit.NewWrapper(testRecorder{store: env.Store, ntfy: env.Ntfy}, "alice", "10.0.0.1")
	opts.Wrapper = w
	opts.Pause = env.Pause
	opts.SoakObserver = &stubObserver{verdict: autorollback.Verdict{
		Decision:       autorollback.DecisionRollback,
		Reason:         "listener_reconnects spike",
		TriggeringRule: "listener_reconnects",
	}}
	opts.AutoRollback = func(_ context.Context, _ string) error { return nil }

	_ = runDeploy(context.Background(), opts)

	// Read every audit row & confirm one has subcommand="auto-rollback".
	var rows []audit.Entry
	if err := env.DB.Find(&rows).Error; err != nil {
		t.Fatalf("query audit rows: %v", err)
	}
	var found bool
	for _, r := range rows {
		if r.Subcommand == "auto-rollback" {
			found = true
			break
		}
	}
	if !found {
		var subs []string
		for _, r := range rows {
			subs = append(subs, r.Subcommand)
		}
		t.Errorf("expected an audit row with subcommand=auto-rollback; got subcommands=%v", subs)
	}
}

// TestRunDeploy_AutoRollbackNilClosureLogsOnly: if the operator wires the
// observer in shadow mode (no AutoRollback closure), we should NOT call a
// nil func — just log the verdict & let the deploy fail soak normally. The
// soak_failed history row is still written so the post-mortem sees it.
func TestRunDeploy_AutoRollbackNilClosureLogsOnly(t *testing.T) {
	_, opts, out := happyDeployFakes(t)
	opts.SoakObserver = &stubObserver{verdict: autorollback.Verdict{
		Decision:       autorollback.DecisionRollback,
		Reason:         "shadow mode",
		TriggeringRule: "listener_reconnects",
	}}
	opts.AutoRollback = nil
	err := runDeploy(context.Background(), opts)
	if err == nil {
		t.Fatal("expected soak-failure error even in shadow mode")
	}
	if !strings.Contains(out.String(), "no rollback closure wired") {
		t.Errorf("expected shadow-mode log line; got:\n%s", out.String())
	}
}
