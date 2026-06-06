/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSRT_BuildSourceLaunchDefault(t *testing.T) {
	// Default mode is listener; URI should embed the bind addr + port and
	// terminate with the decode/convert/resample chain so the pipeline yields
	// the S16LE/48kHz/stereo caps the upstream fan-out expects.
	got := buildSRTSourceLaunch(SRTListenerConfig{
		BindAddr: "0.0.0.0",
		Port:     1935,
	})
	for _, want := range []string{
		"srtsrc",
		"srt://0.0.0.0:1935",
		"mode=listener",
		"decodebin",
		"audioconvert",
		"audioresample",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("source launch missing %q; got %q", want, got)
		}
	}
}

func TestSRT_BuildSourceLaunchOverride(t *testing.T) {
	// SourceLaunchOverride is the test seam: when non-empty it replaces the
	// real srtsrc fragment so unit tests can drive the listener with
	// audiotestsrc and never bind to the network.
	override := "audiotestsrc num-buffers=10"
	got := buildSRTSourceLaunch(SRTListenerConfig{
		BindAddr:             "0.0.0.0",
		Port:                 1935,
		SourceLaunchOverride: override,
	})
	if got != override {
		t.Errorf("override should be returned verbatim; got %q want %q", got, override)
	}
}

func TestSRT_BuildSourceLaunchCallerMode(t *testing.T) {
	// Mode is configurable; the default is listener but operators can flip
	// to caller mode when the upstream broadcaster is the SRT listener.
	got := buildSRTSourceLaunch(SRTListenerConfig{
		BindAddr: "studio.example.com",
		Port:     9000,
		Mode:     "caller",
	})
	if !strings.Contains(got, "mode=caller") {
		t.Errorf("caller mode missing: %q", got)
	}
	if !strings.Contains(got, "srt://studio.example.com:9000") {
		t.Errorf("caller URI missing: %q", got)
	}
}

func TestSRT_NewListenerValidatesConfig(t *testing.T) {
	// Engines list is mandatory; Sessions manager is mandatory.
	if _, err := NewSRTListener(SRTListenerConfig{Port: 1935}); err == nil {
		t.Error("NewSRTListener without engines: want error, got nil")
	}
	if _, err := NewSRTListener(SRTListenerConfig{
		Port:    1935,
		Engines: []string{"127.0.0.1:65000"},
	}); err == nil {
		t.Error("NewSRTListener without SessionMgr: want error, got nil")
	}
}

func TestSRT_ServeCreatesSessionAndCleansUp(t *testing.T) {
	// Drives the listener with a long-running audiotestsrc-backed source so
	// we never actually open an SRT socket. The listener should create the
	// SRT session, keep it registered while the pipeline runs, and remove
	// it after the context is cancelled.
	gstInit()
	mgr := NewSessionMgr()
	lis, err := NewSRTListener(SRTListenerConfig{
		BindAddr:             "127.0.0.1",
		Port:                 0,
		Engines:              []string{"127.0.0.1:65000"},
		Sessions:             mgr,
		SourceLaunchOverride: "audiotestsrc is-live=true samplesperbuffer=480",
	})
	if err != nil {
		t.Fatalf("NewSRTListener: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- lis.Serve(ctx) }()

	// Wait for the session to register with the manager.
	deadline := time.Now().Add(3 * time.Second)
	sawSession := false
	for time.Now().Before(deadline) {
		if mgr.CountByProtocol(ProtocolSRT) >= 1 {
			sawSession = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sawSession {
		cancel()
		<-done
		t.Fatal("SRT session never registered with the manager")
	}

	// Lifetime counter records the session even after it ends.
	if got := mgr.TotalSessionsServed(); got < 1 {
		t.Errorf("TotalSessionsServed = %d, want >= 1", got)
	}

	// Cancel the context; Serve must return and remove the session.
	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Errorf("Serve returned %v, want nil/Canceled", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("Serve did not return within 4s of cancel")
	}

	if got := mgr.CountByProtocol(ProtocolSRT); got != 0 {
		t.Errorf("after cancel, SRT session count = %d, want 0", got)
	}
}

func TestSRT_ServeReturnsOnContextCancel(t *testing.T) {
	// Cancelling the context should unblock Serve without an error.
	gstInit()
	mgr := NewSessionMgr()
	lis, err := NewSRTListener(SRTListenerConfig{
		BindAddr: "127.0.0.1",
		Port:     0,
		Engines:  []string{"127.0.0.1:65000"},
		Sessions: mgr,
		// Long-running source so the pipeline doesn't EOS before cancel.
		SourceLaunchOverride: "audiotestsrc num-buffers=10000 samplesperbuffer=480",
	})
	if err != nil {
		t.Fatalf("NewSRTListener: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- lis.Serve(ctx) }()

	// Give the goroutine a moment to start the pipeline, then cancel.
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Errorf("Serve = %v, want nil or Canceled", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("Serve did not return within 4s of cancel")
	}
	if got := mgr.CountByProtocol(ProtocolSRT); got != 0 {
		t.Errorf("after cancel, SRT session count = %d, want 0", got)
	}
}

func TestSRT_ListenerErrorOnPipelineBuild(t *testing.T) {
	// A malformed override should surface as an error from Serve so the
	// caller can log + retry without leaking sessions.
	gstInit()
	mgr := NewSessionMgr()
	lis, err := NewSRTListener(SRTListenerConfig{
		BindAddr:             "127.0.0.1",
		Port:                 0,
		Engines:              []string{"127.0.0.1:65000"},
		Sessions:             mgr,
		SourceLaunchOverride: "no-such-element-xyzzy",
	})
	if err != nil {
		t.Fatalf("NewSRTListener: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gotErr := lis.Serve(ctx)
	if gotErr == nil {
		t.Error("Serve with broken pipeline: want error, got nil")
	}
	// And no SRT session should have been left behind.
	if n := mgr.CountByProtocol(ProtocolSRT); n != 0 {
		t.Errorf("dangling SRT sessions after error: %d", n)
	}
}
