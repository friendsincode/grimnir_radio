/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/rs/zerolog"
)

// ── Manager (concrete) ────────────────────────────────────────────────────

func TestNewManager_Constructs(t *testing.T) {
	cfg := &config.Config{}
	m := NewManager(cfg, zerolog.Nop())
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
}

func TestManager_GetPipeline_NilWhenMissing(t *testing.T) {
	cfg := &config.Config{}
	m := NewManager(cfg, zerolog.Nop())

	p := m.GetPipeline("nonexistent-mount")
	if p != nil {
		t.Error("GetPipeline should return nil for unknown mount")
	}
}

func TestManager_StopPipeline_NoOpWhenMissing(t *testing.T) {
	cfg := &config.Config{}
	m := NewManager(cfg, zerolog.Nop())

	if err := m.StopPipeline("nonexistent-mount"); err != nil {
		t.Errorf("StopPipeline returned error for unknown mount: %v", err)
	}
}

func TestManager_Shutdown_EmptyMap_NoOp(t *testing.T) {
	cfg := &config.Config{}
	m := NewManager(cfg, zerolog.Nop())

	if err := m.Shutdown(); err != nil {
		t.Errorf("Shutdown on empty manager returned error: %v", err)
	}
}

// ── NewPipeline ───────────────────────────────────────────────────────────

func TestNewPipeline_Constructs(t *testing.T) {
	cfg := &config.Config{}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())
	if p == nil {
		t.Fatal("NewPipeline returned nil")
	}
	// Done() returns nil when no pipeline started.
	if ch := p.Done(); ch != nil {
		t.Error("expected nil Done() channel before any Start")
	}
}

func TestPipeline_Stop_NoProcessNoError(t *testing.T) {
	cfg := &config.Config{}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())
	// Stop before any pipeline is started should be a no-op.
	if err := p.Stop(); err != nil {
		t.Errorf("Stop on unstarted pipeline returned error: %v", err)
	}
}

// TestManager_EnsurePipeline_StopsPreviousFirst locks in the v1.40.1 contract:
// EnsurePipelineWithDualOutput must terminate the previous pipeline before
// starting a new one for the same mount. Pre-migration: two gst-launch
// subprocesses would both feed the same broadcast mount and listeners heard
// overlapping tracks. Post-migration: same risk applies to programmatic
// pipelines, so the contract stays.
//
// After a second EnsurePipeline call on the same mount, the FIRST pipeline's
// Done() channel must be closed (i.e. it was stopped, not left running).
func TestManager_EnsurePipeline_StopsPreviousFirst(t *testing.T) {
	cfg := &config.Config{}
	mgr := NewManager(cfg, zerolog.Nop())

	if err := mgr.EnsurePipeline(testCtx(), "m1", longPipeline); err != nil {
		t.Fatalf("first EnsurePipeline: %v", err)
	}
	first := mgr.GetPipeline("m1")
	if first == nil {
		t.Fatal("expected pipeline registered under m1")
	}
	// Capture the FIRST pipeline's Done channel before the second Ensure
	// reassigns p.done — the contract is that Stop closes the *current*
	// done channel, which is what we want to observe here.
	firstDone := first.Done()

	if err := mgr.EnsurePipeline(testCtx(), "m1", longPipeline); err != nil {
		t.Fatalf("second EnsurePipeline: %v", err)
	}

	select {
	case <-firstDone:
		// good — previous pipeline was stopped before the second Start()
	default:
		t.Fatal("previous pipeline still running after second EnsurePipeline; auto-stop did not happen → echo regression")
	}
	_ = mgr.StopPipeline("m1")
}

func testCtx() context.Context { return context.Background() }
