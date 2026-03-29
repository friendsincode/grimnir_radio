/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
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
