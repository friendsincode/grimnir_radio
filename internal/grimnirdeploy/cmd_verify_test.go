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

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/probe"
)

// fakeAllProber returns a fixed Result for every host. ProbeAll stamps the
// requested Host onto the returned Result so the formatter renders one row
// per host even though the same canned response is reused.
type fakeAllProber struct {
	result probe.Result
	calls  int32
}

func (f *fakeAllProber) ProbeAll(_ context.Context, host string) probe.Result {
	atomic.AddInt32(&f.calls, 1)
	r := f.result
	r.Host = host
	return r
}

func TestVerifyHappyPath(t *testing.T) {
	env := newTestEnv(t)
	w := audit.NewWrapper(testRecorder{store: env.Store, ntfy: env.Ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer

	good := probe.Result{
		ControlPlaneOK: true,
		MediaEngineOK:  true,
		EdgeEncoderOK:  true,
		FanOutOK:       true,
	}
	prober := &fakeAllProber{result: good}
	err := runVerify(context.Background(), VerifyOpts{
		Hosts:   []string{"local", "node-2"},
		Prober:  prober,
		Wrapper: w,
		Out:     &out,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	s := out.String()
	for _, want := range []string{"local", "node-2", "control plane", "media engine", "edge encoder", "fan-out"} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%s", want, s)
		}
	}
	if got := atomic.LoadInt32(&prober.calls); got != 2 {
		t.Errorf("ProbeAll calls = %d, want 2 (one per host)", got)
	}
	// Audit row should land as a completed (verify is read-only).
	var rows []audit.Entry
	if err := env.DB.Find(&rows).Error; err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("audit row count = %d, want 1", len(rows))
	}
	if rows[0].Subcommand != "verify" {
		t.Errorf("Subcommand = %q, want verify", rows[0].Subcommand)
	}
	if rows[0].Phase != audit.PhaseCompleted {
		t.Errorf("Phase = %q, want %q", rows[0].Phase, audit.PhaseCompleted)
	}
}

func TestVerifyReportsFailure(t *testing.T) {
	env := newTestEnv(t)
	w := audit.NewWrapper(testRecorder{store: env.Store, ntfy: env.Ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer

	bad := probe.Result{
		ControlPlaneOK:  false,
		ControlPlaneErr: "status 500",
		MediaEngineOK:   true,
		EdgeEncoderOK:   true,
		FanOutOK:        true,
	}
	err := runVerify(context.Background(), VerifyOpts{
		Hosts:   []string{"local"},
		Prober:  &fakeAllProber{result: bad},
		Wrapper: w,
		Out:     &out,
	})
	if err == nil {
		t.Fatal("verify should error when something is down")
	}
	s := out.String()
	if !strings.Contains(s, "FAIL: status 500") {
		t.Errorf("report missing failure detail:\n%s", s)
	}
	// Audit row should be a FAILED row.
	var rows []audit.Entry
	if err := env.DB.Find(&rows).Error; err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if len(rows) != 1 || rows[0].Phase != audit.PhaseFailed {
		t.Fatalf("expected 1 FAILED row; got %+v", rows)
	}
}

// TestVerifyMixedHosts confirms the report renders one row per host and
// surfaces a failure on any host as the overall verdict.
func TestVerifyMixedHosts(t *testing.T) {
	env := newTestEnv(t)
	w := audit.NewWrapper(testRecorder{store: env.Store, ntfy: env.Ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer

	mixed := probe.Result{
		ControlPlaneOK: true, MediaEngineErr: "grpc: connection refused",
		EdgeEncoderOK: true, FanOutOK: true,
	}
	err := runVerify(context.Background(), VerifyOpts{
		Hosts:   []string{"local", "node-2"},
		Prober:  &fakeAllProber{result: mixed},
		Wrapper: w,
		Out:     &out,
	})
	if err == nil {
		t.Fatal("verify should error when any component is down")
	}
	s := out.String()
	if !strings.Contains(s, "local") || !strings.Contains(s, "node-2") {
		t.Errorf("expected both hosts in report:\n%s", s)
	}
	if !strings.Contains(s, "FAIL: grpc: connection refused") {
		t.Errorf("expected the gRPC failure detail:\n%s", s)
	}
}
