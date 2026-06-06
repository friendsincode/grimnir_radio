/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// fakeRecorder is a thread-safe in-memory Recorder for wrapper tests.
type fakeRecorder struct {
	mu            sync.Mutex
	startCalls    int
	completeCalls int
	failedCalls   int
	ntfyCalls     int
	lastOutcome   string
	lastTitle     string
	lastPriority  Priority
	lastNotes     string
	lastArgsSeen  map[string]any
	startErr      error
	ntfyErr       error
}

func (f *fakeRecorder) WriteStart(_ context.Context, _, _, _ string, args map[string]any) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls++
	f.lastArgsSeen = args
	if f.startErr != nil {
		return uuid.Nil, f.startErr
	}
	return uuid.New(), nil
}

func (f *fakeRecorder) WriteComplete(_ context.Context, _ uuid.UUID, outcome string, _ time.Duration, notes string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completeCalls++
	f.lastOutcome = outcome
	f.lastNotes = notes
	return nil
}

func (f *fakeRecorder) WriteFailed(_ context.Context, _ uuid.UUID, outcome string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failedCalls++
	f.lastOutcome = outcome
	return nil
}

func (f *fakeRecorder) PostNtfy(_ context.Context, title, _ string, priority Priority) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ntfyCalls++
	f.lastTitle = title
	f.lastPriority = priority
	return f.ntfyErr
}

func TestWrapHappyPath(t *testing.T) {
	r := &fakeRecorder{}
	w := NewWrapper(r, "alice", "10.0.0.1")
	called := false
	inner := func(_ context.Context) error { called = true; return nil }
	if err := w.Wrap(context.Background(), "deploy", map[string]any{"tag": "v1"}, inner); err != nil {
		t.Fatalf("Wrap returned err: %v", err)
	}
	if !called {
		t.Error("inner was not called")
	}
	if r.startCalls != 1 {
		t.Errorf("startCalls = %d, want 1", r.startCalls)
	}
	if r.completeCalls != 1 {
		t.Errorf("completeCalls = %d, want 1", r.completeCalls)
	}
	if r.failedCalls != 0 {
		t.Errorf("failedCalls = %d, want 0", r.failedCalls)
	}
	if r.ntfyCalls != 2 {
		t.Errorf("ntfyCalls = %d, want 2 (start + complete)", r.ntfyCalls)
	}
	if r.lastOutcome != "success" {
		t.Errorf("lastOutcome = %q, want success", r.lastOutcome)
	}
}

func TestWrapErrorPath(t *testing.T) {
	r := &fakeRecorder{}
	w := NewWrapper(r, "alice", "10.0.0.1")
	want := errors.New("boom")
	err := w.Wrap(context.Background(), "deploy", nil, func(_ context.Context) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("Wrap should propagate inner error; got %v", err)
	}
	if r.failedCalls != 1 {
		t.Errorf("failedCalls = %d, want 1", r.failedCalls)
	}
	if r.completeCalls != 0 {
		t.Errorf("completeCalls = %d, want 0", r.completeCalls)
	}
	if r.lastOutcome != "boom" {
		t.Errorf("lastOutcome = %q", r.lastOutcome)
	}
	if r.lastPriority != PriorityHigh {
		t.Errorf("failure ntfy priority = %d, want %d", r.lastPriority, PriorityHigh)
	}
}

func TestWrapPanicPath(t *testing.T) {
	r := &fakeRecorder{}
	w := NewWrapper(r, "alice", "10.0.0.1")
	defer func() {
		if rec := recover(); rec == nil {
			t.Fatal("Wrap should re-panic")
		}
		if r.failedCalls != 1 {
			t.Errorf("failedCalls = %d, want 1", r.failedCalls)
		}
		if r.lastPriority != PriorityUrgent {
			t.Errorf("panic ntfy priority = %d, want %d", r.lastPriority, PriorityUrgent)
		}
	}()
	_ = w.Wrap(context.Background(), "deploy", nil, func(_ context.Context) error {
		panic("kaboom")
	})
}

func TestWrapStartFailureSkipsInner(t *testing.T) {
	r := &fakeRecorder{startErr: errors.New("db down")}
	w := NewWrapper(r, "alice", "10.0.0.1")
	called := false
	err := w.Wrap(context.Background(), "deploy", nil, func(_ context.Context) error { called = true; return nil })
	if err == nil {
		t.Fatal("Wrap should fail when audit start fails")
	}
	if called {
		t.Error("inner should not run if audit start failed")
	}
}

// TestWrapNotifierFailureDoesNotBlockAuditWrite confirms a flaky ntfy does
// NOT prevent the DB rows from being written. The audit row is the
// system-of-record; ntfy is best-effort glance-able notification.
func TestWrapNotifierFailureDoesNotBlockAuditWrite(t *testing.T) {
	r := &fakeRecorder{ntfyErr: errors.New("ntfy 502 bad gateway")}
	w := NewWrapper(r, "alice", "10.0.0.1")
	if err := w.Wrap(context.Background(), "deploy", nil, func(_ context.Context) error { return nil }); err != nil {
		t.Fatalf("ntfy failure should not surface as Wrap error: %v", err)
	}
	if r.startCalls != 1 {
		t.Errorf("startCalls = %d, want 1", r.startCalls)
	}
	if r.completeCalls != 1 {
		t.Errorf("completeCalls = %d, want 1 (ntfy failure must not block DB completion)", r.completeCalls)
	}
}

func TestWrapNilWrapperStillRunsInner(t *testing.T) {
	var w *Wrapper
	called := false
	err := w.Wrap(context.Background(), "x", nil, func(_ context.Context) error { called = true; return nil })
	if err != nil || !called {
		t.Errorf("nil wrapper: err=%v called=%v", err, called)
	}
}

func TestWrapAccessors(t *testing.T) {
	w := NewWrapper(&fakeRecorder{}, "alice", "10.0.0.1")
	if w.Operator() != "alice" {
		t.Errorf("Operator = %q, want alice", w.Operator())
	}
	if w.SourceIP() != "10.0.0.1" {
		t.Errorf("SourceIP = %q, want 10.0.0.1", w.SourceIP())
	}
}

// TestResolveOperatorAndSourceIP covers env-based identity resolution.
// USER -> operator (fallback "unknown"); SSH_CLIENT first field -> source IP
// (fallback "local").
func TestResolveOperatorAndSourceIP(t *testing.T) {
	cases := []struct {
		name      string
		user      string
		sshClient string
		wantOp    string
		wantIP    string
	}{
		{name: "both set", user: "alice", sshClient: "10.0.0.5 54321 22", wantOp: "alice", wantIP: "10.0.0.5"},
		{name: "no SSH", user: "bob", sshClient: "", wantOp: "bob", wantIP: "local"},
		{name: "no USER", user: "", sshClient: "192.168.1.2 1 22", wantOp: "unknown", wantIP: "192.168.1.2"},
		{name: "neither", user: "", sshClient: "", wantOp: "unknown", wantIP: "local"},
		{name: "malformed SSH_CLIENT empty", user: "carol", sshClient: "   ", wantOp: "carol", wantIP: "local"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("USER", tc.user)
			t.Setenv("SSH_CLIENT", tc.sshClient)
			op := ResolveOperator()
			ip := ResolveSourceIP()
			if op != tc.wantOp {
				t.Errorf("operator = %q, want %q", op, tc.wantOp)
			}
			if ip != tc.wantIP {
				t.Errorf("sourceIP = %q, want %q", ip, tc.wantIP)
			}
		})
	}
}

// TestWrapCobraCapturesFlagsAndPositional confirms the cobra middleware
// installs a RunE that pushes the operator's flag choices through to the
// audit row.
func TestWrapCobraCapturesFlagsAndPositional(t *testing.T) {
	r := &fakeRecorder{}
	w := NewWrapper(r, "alice", "10.0.0.1")

	cmd := &cobra.Command{
		Use:  "deploy",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return nil },
	}
	cmd.Flags().String("reason", "", "")
	cmd.Flags().Bool("dry-run", false, "")
	WrapCobra(w, cmd)

	cmd.SetArgs([]string{"--reason=fixing #999", "--dry-run", "v1.2.3"})
	cmd.SetOut(&strings.Builder{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if r.startCalls != 1 || r.completeCalls != 1 {
		t.Fatalf("WrapCobra did not invoke recorder: start=%d complete=%d", r.startCalls, r.completeCalls)
	}
	if r.lastArgsSeen["reason"] != "fixing #999" {
		t.Errorf("reason flag missing from audit args: %v", r.lastArgsSeen)
	}
	if r.lastArgsSeen["dry-run"] != "true" {
		t.Errorf("dry-run flag missing: %v", r.lastArgsSeen)
	}
	pos, _ := r.lastArgsSeen["_positional"].([]string)
	if len(pos) != 1 || pos[0] != "v1.2.3" {
		t.Errorf("positional = %v, want [v1.2.3]", pos)
	}
}

// TestWrapCobraTransparentForStubError confirms the wrapper correctly captures
// a stub RunE's errNotImplemented and writes a FAILED row with that outcome.
// This is the user's stated acceptance criterion for Chunk 1.
func TestWrapCobraCapturesStubErrorAsFailedRow(t *testing.T) {
	r := &fakeRecorder{}
	w := NewWrapper(r, "alice", "local")

	stubErr := errors.New("not yet implemented")
	cmd := &cobra.Command{
		Use:  "verify",
		RunE: func(_ *cobra.Command, _ []string) error { return stubErr },
	}
	WrapCobra(w, cmd)

	cmd.SetArgs(nil)
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	if !errors.Is(err, stubErr) {
		t.Fatalf("Execute should propagate stub err; got %v", err)
	}
	if r.failedCalls != 1 {
		t.Errorf("expected 1 failed row; got %d", r.failedCalls)
	}
	if r.lastOutcome != "not yet implemented" {
		t.Errorf("FAILED outcome = %q, want 'not yet implemented'", r.lastOutcome)
	}
}

func TestWrapCobraNilWrapperPassthrough(t *testing.T) {
	called := false
	cmd := &cobra.Command{
		Use:  "x",
		RunE: func(_ *cobra.Command, _ []string) error { called = true; return nil },
	}
	WrapCobra(nil, cmd)
	cmd.SetArgs(nil)
	cmd.SetOut(&strings.Builder{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !called {
		t.Error("inner not called with nil wrapper")
	}
}
