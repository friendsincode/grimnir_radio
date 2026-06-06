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

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

// hostRunner is a per-host VIP-response shim used by the recover-partition
// tests. The shared runner.Fake matches by command, not by host, so it can't
// directly model "local says held, node-2 says not-held" for the same VIP
// check. This shim does the per-host dispatch and falls through to the Fake
// for everything else (e.g. WAL LSN reads).
type hostRunner struct {
	base    *runner.Fake
	perHost map[string]string
}

func (h *hostRunner) Run(ctx context.Context, host, cmd string) (string, string, int, error) {
	if strings.Contains(cmd, "ip addr show") {
		if v, ok := h.perHost[host]; ok {
			return v, "", 0, nil
		}
		return "not-held\n", "", 0, nil
	}
	return h.base.Run(ctx, host, cmd)
}

// TestRecoverPartitionReportsConflict simulates split-brain: both nodes claim
// the VIP. runRecoverPartition must report the conflict in the output and
// return a non-nil error so the operator decides which side wins.
func TestRecoverPartitionReportsConflict(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()
	if err := rdb.Set(context.Background(), "grimnir:leader", "node-1", 0).Err(); err != nil {
		t.Fatalf("seed leader: %v", err)
	}

	base := runner.NewFake()
	base.SetResponsePrefix("psql ", "0/A0000000\n", "", 0, nil)
	// Both hosts return "held": split-brain.
	r := &hostRunner{base: base, perHost: map[string]string{"local": "held\n", "node-2": "held\n"}}

	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err = runRecoverPartition(context.Background(), RecoverOpts{
		Hosts:   []string{"local", "node-2"},
		VIPs:    []string{"<edge-vps>00"},
		Runner:  r,
		Redis:   rdb,
		Wrapper: w,
		Out:     &out,
	})
	if err == nil {
		t.Fatal("split-brain VIP should produce an error")
	}
	if !strings.Contains(out.String(), "VIP") {
		t.Errorf("output missing VIP report: %s", out.String())
	}
	if !strings.Contains(out.String(), "CONFLICTS") {
		t.Errorf("output missing CONFLICTS section: %s", out.String())
	}
	if !strings.Contains(out.String(), "2 holders") {
		t.Errorf("output should call out 2 holders for the split-brain VIP: %s", out.String())
	}
}

// TestRecoverPartitionHealthy exercises the happy path: exactly one VIP
// holder, a leader lease present, no conflicts -> no error.
func TestRecoverPartitionHealthy(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()
	if err := rdb.Set(context.Background(), "grimnir:leader", "node-1", 0).Err(); err != nil {
		t.Fatalf("seed leader: %v", err)
	}

	base := runner.NewFake()
	base.SetResponsePrefix("psql ", "0/A0000000\n", "", 0, nil)
	// Only local holds the VIP; node-2 falls through to the default "not-held".
	r := &hostRunner{base: base, perHost: map[string]string{"local": "held\n"}}

	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err = runRecoverPartition(context.Background(), RecoverOpts{
		Hosts:   []string{"local", "node-2"},
		VIPs:    []string{"<edge-vps>00"},
		Runner:  r,
		Redis:   rdb,
		Wrapper: w,
		Out:     &out,
	})
	if err != nil {
		t.Fatalf("runRecoverPartition (healthy): %v\noutput:\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "no conflicts detected") {
		t.Errorf("healthy output missing success line: %s", out.String())
	}
}

// TestRecoverPartitionDryRun: this command is read-only by design, but must
// still honour --dry-run by short-circuiting the SSH + Redis reads and
// printing the marker.
func TestRecoverPartitionDryRun(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	f := runner.NewFake()
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err = runRecoverPartition(context.Background(), RecoverOpts{
		Hosts:   []string{"local", "node-2"},
		VIPs:    []string{"<edge-vps>00"},
		Runner:  f,
		Redis:   rdb,
		Wrapper: w,
		Out:     &out,
		DryRun:  true,
	})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("dry-run made %d runner calls, want 0", len(f.Calls))
	}
	if !strings.Contains(out.String(), "[dry-run]") {
		t.Errorf("dry-run output missing marker: %q", out.String())
	}
}
