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

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

func TestRunDrainStopsServicesInOrder(t *testing.T) {
	f := runner.NewFake()
	f.SetResponsePrefix("touch", "", "", 0, nil)
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && docker compose stop", "", "", 0, nil)
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runDrain(context.Background(), DrainOpts{
		Node:    "node-2",
		Runner:  f,
		Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		Wrapper: w, Out: &out,
		Sleep: func(d time.Duration) {},
	})
	if err != nil {
		t.Fatalf("runDrain: %v", err)
	}
	// Find the order of docker compose stop calls.
	var order []string
	for _, c := range f.Calls {
		if strings.Contains(c.Cmd, "docker compose stop ") {
			parts := strings.Split(c.Cmd, "docker compose stop ")
			order = append(order, parts[1])
		}
	}
	want := []string{"grimnir-radio", "edge-encoder", "grimnir-fanout", "grimnir-mediaengine"}
	if len(order) != len(want) {
		t.Fatalf("stop order len %d, want %d (%v)", len(order), len(want), order)
	}
	for i, svc := range want {
		if order[i] != svc {
			t.Errorf("order[%d] = %s, want %s", i, order[i], svc)
		}
	}
}

func TestRunDrainDryRunSkipsMutation(t *testing.T) {
	f := runner.NewFake()
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runDrain(context.Background(), DrainOpts{
		Node: "node-2", Runner: f,
		Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		Wrapper: w, Out: &out, DryRun: true,
		Sleep: func(d time.Duration) {},
	})
	if err != nil {
		t.Fatalf("runDrain dry-run: %v", err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("dry-run made %d runner calls, want 0", len(f.Calls))
	}
	if !strings.Contains(out.String(), "[dry-run]") {
		t.Errorf("dry-run output missing marker: %q", out.String())
	}
}
