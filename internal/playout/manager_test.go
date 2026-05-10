/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"strings"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/rs/zerolog"
)

// ── Manager (concrete) ────────────────────────────────────────────────────

func TestNewManager_Constructs(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "gst-launch-1.0"}
	m := NewManager(cfg, zerolog.Nop())
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
}

func TestManager_GetPipeline_NilWhenMissing(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "gst-launch-1.0"}
	m := NewManager(cfg, zerolog.Nop())

	p := m.GetPipeline("nonexistent-mount")
	if p != nil {
		t.Error("GetPipeline should return nil for unknown mount")
	}
}

func TestManager_StopPipeline_NoOpWhenMissing(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "gst-launch-1.0"}
	m := NewManager(cfg, zerolog.Nop())

	if err := m.StopPipeline("nonexistent-mount"); err != nil {
		t.Errorf("StopPipeline returned error for unknown mount: %v", err)
	}
}

func TestManager_Shutdown_EmptyMap_NoOp(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "gst-launch-1.0"}
	m := NewManager(cfg, zerolog.Nop())

	if err := m.Shutdown(); err != nil {
		t.Errorf("Shutdown on empty manager returned error: %v", err)
	}
}

// ── NewPipeline ───────────────────────────────────────────────────────────

func TestNewPipeline_Constructs(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "gst-launch-1.0"}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())
	if p == nil {
		t.Fatal("NewPipeline returned nil")
	}
	// Done() returns nil when no process started.
	if ch := p.Done(); ch != nil {
		t.Error("expected nil Done() channel before any Start")
	}
}

func TestPipeline_Stop_NoProcessNoError(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "gst-launch-1.0"}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())
	// Stop before any process is started should be a no-op.
	if err := p.Stop(); err != nil {
		t.Errorf("Stop on unstarted pipeline returned error: %v", err)
	}
}

// ── limitedBuffer ─────────────────────────────────────────────────────────

func TestLimitedBuffer_WritesAndReads(t *testing.T) {
	var b limitedBuffer
	data := []byte("hello world")
	n, err := b.Write(data)
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len(data) {
		t.Errorf("Write returned %d, want %d", n, len(data))
	}
	got := b.String()
	if got != "hello world" {
		t.Errorf("String() = %q, want %q", got, "hello world")
	}
}

func TestLimitedBuffer_Overflow_DropsExcess(t *testing.T) {
	var b limitedBuffer
	// Write more than 4096 bytes — excess should be dropped silently.
	large := make([]byte, 5000)
	for i := range large {
		large[i] = 'A'
	}
	n, err := b.Write(large)
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len(large) {
		t.Errorf("Write returned %d, want %d (should report all written even if truncated)", n, len(large))
	}
	got := b.String()
	if len(got) > 4096 {
		t.Errorf("buffer should not exceed 4096 bytes, got %d", len(got))
	}
}

func TestLimitedBuffer_TrimSpace(t *testing.T) {
	var b limitedBuffer
	b.Write([]byte("  test  "))
	got := b.String()
	if got != "test" {
		t.Errorf("String() = %q, want %q (should trim whitespace)", got, "test")
	}
}

func TestLimitedBuffer_MultipleWrites(t *testing.T) {
	var b limitedBuffer
	b.Write([]byte("foo"))
	b.Write([]byte("bar"))
	got := b.String()
	if !strings.Contains(got, "foobar") {
		t.Errorf("String() = %q, want to contain \"foobar\"", got)
	}
}

// TestPipeline_StartUsesProcessGroup locks in the v1.40.1 fix:
// gst-launch must be launched with Setpgid so we can later kill the WHOLE
// process group (sh wrapper + gst-launch grandchild) via a single signal.
// Without Setpgid, killing cmd.Process only reaps the shell; gst-launch
// orphans to PID 1 and keeps writing to its broadcast pipe → audible echo.
func TestPipeline_StartUsesProcessGroup(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"} // /bin/true exits immediately, suitable as a no-op
	p := NewPipeline(cfg, "mount-pg-test", zerolog.Nop())

	if err := p.Start(testCtx(), "fakesrc num-buffers=0 ! fakesink"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Stop()

	p.mu.Lock()
	cmd := p.cmd
	p.mu.Unlock()
	if cmd == nil || cmd.SysProcAttr == nil {
		t.Fatal("expected SysProcAttr to be set so the process becomes its own group leader")
	}
	if !cmd.SysProcAttr.Setpgid {
		t.Errorf("expected Setpgid=true so killing the group also reaps gst-launch grandchild; got Setpgid=%v", cmd.SysProcAttr.Setpgid)
	}
}

// TestManager_EnsurePipeline_StopsPreviousFirst locks in the v1.40.1 fix:
// EnsurePipelineWithDualOutput must terminate the previous pipeline before
// starting a new one for the same mount. Without this, the previous and new
// pipelines both feed the same broadcast mount → listeners hear both tracks
// overlapping (the audible "echo" reported on RLMradio.xyz - M).
//
// Test verifies behavioral contract: after a second EnsurePipeline call on
// the same mount, the FIRST pipeline's Done() channel is closed (i.e. it
// was actually stopped, not left running).
func TestManager_EnsurePipeline_StopsPreviousFirst(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	mgr := NewManager(cfg, zerolog.Nop())

	if err := mgr.EnsurePipeline(testCtx(), "m1", "fakesrc ! fakesink"); err != nil {
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

	if err := mgr.EnsurePipeline(testCtx(), "m1", "fakesrc ! fakesink"); err != nil {
		t.Fatalf("second EnsurePipeline: %v", err)
	}

	select {
	case <-firstDone:
		// good — previous pipeline was stopped before the second Start()
	default:
		t.Fatal("previous pipeline still running after second EnsurePipeline; auto-stop did not happen → echo regression")
	}
}

func testCtx() context.Context { return context.Background() }
