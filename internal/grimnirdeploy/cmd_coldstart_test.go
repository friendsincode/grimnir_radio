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

func TestColdStartOrdersDependencies(t *testing.T) {
	f := runner.NewFake()
	// All operations succeed.
	for _, p := range []string{"iptables", "wg", "pg_isready", "redis-cli", "curl", "./grimnir"} {
		f.SetResponsePrefix(p, "ok\n", "", 0, nil)
	}
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && ./grimnir", "ok\n", "", 0, nil)
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runColdStartRegion(context.Background(), ColdStartOpts{
		Region: "us-east", Hosts: []string{"local", "node-2"},
		Runner: f, Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		Wrapper: w, Out: &out,
	})
	if err != nil {
		t.Fatalf("coldstart: %v", err)
	}
	// Verify dependency order via output milestones.
	s := out.String()
	idxFW := strings.Index(s, "firewall")
	idxWG := strings.Index(s, "wireguard")
	idxPG := strings.Index(s, "postgres")
	idxRD := strings.Index(s, "redis")
	idxApp := strings.Index(s, "grimnir-radio")
	if !(idxFW < idxWG && idxWG < idxPG && idxPG < idxRD && idxRD < idxApp) {
		t.Errorf("dependency order wrong; got:\n%s", s)
	}
}

func TestColdStartDryRunSkipsMutation(t *testing.T) {
	f := runner.NewFake()
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runColdStartRegion(context.Background(), ColdStartOpts{
		Region: "us-east", Hosts: []string{"local", "node-2"},
		Runner: f, Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		Wrapper: w, Out: &out, DryRun: true,
	})
	if err != nil {
		t.Fatalf("coldstart dry-run: %v", err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("dry-run made %d runner calls, want 0", len(f.Calls))
	}
	if !strings.Contains(out.String(), "[dry-run]") {
		t.Errorf("dry-run output missing marker: %q", out.String())
	}
}
