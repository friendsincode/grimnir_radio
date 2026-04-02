/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/rs/zerolog"
)

// fakeLongBinary creates a temp shell script that sleeps for 60 seconds.
// Used to simulate a long-running GStreamer process in tests.
func fakeLongBinary(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("", "grimnir-test-gst-*.sh")
	if err != nil {
		t.Fatalf("create temp script: %v", err)
	}
	if _, err := f.WriteString("#!/bin/sh\nexec sleep 60\n"); err != nil {
		t.Fatalf("write temp script: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp script: %v", err)
	}
	if err := os.Chmod(f.Name(), 0o755); err != nil {
		t.Fatalf("chmod temp script: %v", err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

// ── Pipeline.Start ─────────────────────────────────────────────────────────

func TestPipeline_Start_ProcessLifecycle(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())

	if err := p.Start(context.Background(), "fake-pipeline"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	done := p.Done()
	if done == nil {
		t.Fatal("Done() returned nil after Start")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pipeline process did not exit within 3s")
	}
}

func TestPipeline_Start_AlreadyRunning(t *testing.T) {
	scriptPath := fakeLongBinary(t)
	cfg := &config.Config{GStreamerBin: scriptPath}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := p.Start(ctx, "fake-pipeline"); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	// Second start while running should return an error.
	if err := p.Start(ctx, "fake-pipeline"); err == nil {
		t.Error("expected error when starting already-running pipeline")
	}
	cancel()
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Error("pipeline did not exit after context cancel")
	}
}

func TestPipeline_Start_RestartAfterExit(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())
	ctx := context.Background()

	if err := p.Start(ctx, "fake-pipeline"); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("first run did not exit")
	}
	// After exit, starting again should succeed.
	if err := p.Start(ctx, "fake-pipeline"); err != nil {
		t.Errorf("restart after exit failed: %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("second run did not exit")
	}
}

// ── Pipeline.StartWithOutput ───────────────────────────────────────────────

func TestPipeline_StartWithOutput_NilHandler(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())

	if err := p.StartWithOutput(context.Background(), "fake-pipeline", nil); err != nil {
		t.Fatalf("StartWithOutput(nil handler) failed: %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("pipeline did not exit")
	}
}

func TestPipeline_StartWithOutput_HandlerReceivesEOF(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())

	called := make(chan struct{})
	handler := func(r io.Reader) {
		_, _ = io.Copy(io.Discard, r)
		close(called)
	}
	if err := p.StartWithOutput(context.Background(), "fake-pipeline", handler); err != nil {
		t.Fatalf("StartWithOutput failed: %v", err)
	}
	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("output handler was not invoked")
	}
}

// ── Pipeline.StartWithDualOutput ──────────────────────────────────────────

func TestPipeline_StartWithDualOutput_NilHandlers(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())

	if err := p.StartWithDualOutput(context.Background(), "fake-pipeline", nil, nil, nil); err != nil {
		t.Fatalf("StartWithDualOutput(nil handlers) failed: %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("pipeline did not exit")
	}
}

func TestPipeline_StartWithDualOutput_HandlersInvoked(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())

	var wg sync.WaitGroup
	wg.Add(2)
	hq := func(r io.Reader) { _, _ = io.Copy(io.Discard, r); wg.Done() }
	lq := func(r io.Reader) { _, _ = io.Copy(io.Discard, r); wg.Done() }

	if err := p.StartWithDualOutput(context.Background(), "fake-pipeline", nil, hq, lq); err != nil {
		t.Fatalf("StartWithDualOutput failed: %v", err)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("HQ/LQ handlers not called within 3s")
	}
}

func TestPipeline_StartWithDualOutput_AlreadyRunning(t *testing.T) {
	scriptPath := fakeLongBinary(t)
	cfg := &config.Config{GStreamerBin: scriptPath}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := p.StartWithDualOutput(ctx, "fake-pipeline", nil, nil, nil); err != nil {
		t.Fatalf("first StartWithDualOutput failed: %v", err)
	}
	if err := p.StartWithDualOutput(ctx, "fake-pipeline", nil, nil, nil); err == nil {
		t.Error("expected error for already-running pipeline")
	}
	cancel()
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Error("pipeline did not exit after context cancel")
	}
}

// ── Pipeline.StartWithDualOutputAndInput ──────────────────────────────────

func TestPipeline_StartWithDualOutputAndInput_ReturnsWriter(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())

	stdin, err := p.StartWithDualOutputAndInput(context.Background(), "fake-pipeline", nil, nil)
	if err != nil {
		t.Fatalf("StartWithDualOutputAndInput failed: %v", err)
	}
	if stdin == nil {
		t.Fatal("expected non-nil stdin writer")
	}
	_ = stdin.Close()
}

func TestPipeline_StartWithDualOutputAndInput_ReturnsExistingStdin(t *testing.T) {
	scriptPath := fakeLongBinary(t)
	cfg := &config.Config{GStreamerBin: scriptPath}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdin1, err := p.StartWithDualOutputAndInput(ctx, "fake-pipeline", nil, nil)
	if err != nil {
		t.Fatalf("first StartWithDualOutputAndInput failed: %v", err)
	}
	// Second call while running: should return same stdin (not error).
	stdin2, err := p.StartWithDualOutputAndInput(ctx, "fake-pipeline", nil, nil)
	if err != nil {
		t.Errorf("second StartWithDualOutputAndInput: unexpected error: %v", err)
	}
	if stdin2 != stdin1 {
		t.Error("expected second call to return same stdin writer as first")
	}
}

func TestPipeline_StartWithDualOutputAndInput_HandlersInvoked(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())

	var wg sync.WaitGroup
	wg.Add(2)
	hq := func(r io.Reader) { _, _ = io.Copy(io.Discard, r); wg.Done() }
	lq := func(r io.Reader) { _, _ = io.Copy(io.Discard, r); wg.Done() }

	_, err := p.StartWithDualOutputAndInput(context.Background(), "fake-pipeline", hq, lq)
	if err != nil {
		t.Fatalf("StartWithDualOutputAndInput failed: %v", err)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("HQ/LQ handlers not called within 3s")
	}
}

// ── Pipeline.Stop ─────────────────────────────────────────────────────────

func TestPipeline_Stop_RunningProcess(t *testing.T) {
	scriptPath := fakeLongBinary(t)
	cfg := &config.Config{GStreamerBin: scriptPath}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())

	if err := p.Start(context.Background(), "fake-pipeline"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := p.Stop(); err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("pipeline did not stop within 10s")
	}
}

func TestPipeline_Stop_AlreadyExited(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())
	_ = p.Start(context.Background(), "fake-pipeline")
	select {
	case <-p.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("process did not exit")
	}
	if err := p.Stop(); err != nil {
		t.Errorf("Stop on exited process returned error: %v", err)
	}
}

// ── Pipeline.StartWithDualOutput seek file ─────────────────────────────────

func TestPipeline_StartWithDualOutput_WithSeekFile(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())

	// Create a real temp file to serve as the seek file (fd=5).
	sf, err := os.CreateTemp("", "grimnir-seek-*.bin")
	if err != nil {
		t.Fatalf("create seek file: %v", err)
	}
	sf.Close()
	defer os.Remove(sf.Name())

	// Re-open for passing to StartWithDualOutput.
	seekFile, err := os.Open(sf.Name())
	if err != nil {
		t.Fatalf("open seek file: %v", err)
	}
	// seekFile is closed by StartWithDualOutput after passing to child.

	var wg sync.WaitGroup
	wg.Add(2)
	hq := func(r io.Reader) { _, _ = io.Copy(io.Discard, r); wg.Done() }
	lq := func(r io.Reader) { _, _ = io.Copy(io.Discard, r); wg.Done() }

	if err := p.StartWithDualOutput(context.Background(), "fake-pipeline", seekFile, hq, lq); err != nil {
		t.Fatalf("StartWithDualOutput with seekFile failed: %v", err)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handlers not called within 3s")
	}
}

// ── Pipeline.StartWithDualOutputAndInput already-running nil stdin ─────────

func TestPipeline_StartWithDualOutputAndInput_AlreadyRunningNilStdin(t *testing.T) {
	// Start a pipeline first with StartWithDualOutput (which sets stdin = nil),
	// then call StartWithDualOutputAndInput while it's still running.
	// This covers the "already running, stdin == nil → error" branch.
	scriptPath := fakeLongBinary(t)
	cfg := &config.Config{GStreamerBin: scriptPath}
	p := NewPipeline(cfg, "test-mount", zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start with plain Start (sets p.stdin = nil).
	if err := p.Start(ctx, "fake-pipeline"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Now call StartWithDualOutputAndInput while running (stdin is nil) → error.
	_, err := p.StartWithDualOutputAndInput(ctx, "fake-pipeline", nil, nil)
	if err == nil {
		t.Error("expected error when StartWithDualOutputAndInput called while running with nil stdin")
	}
}

// ── Manager.Ensure* ────────────────────────────────────────────────────────

func TestManager_EnsurePipeline_StartsProcess(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	m := NewManager(cfg, zerolog.Nop())

	if err := m.EnsurePipeline(context.Background(), "mount-1", "fake-pipeline"); err != nil {
		t.Fatalf("EnsurePipeline failed: %v", err)
	}
	p := m.GetPipeline("mount-1")
	if p == nil {
		t.Fatal("GetPipeline returned nil after EnsurePipeline")
	}
	select {
	case <-p.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("pipeline did not exit")
	}
}

func TestManager_EnsurePipeline_ReuseAfterExit(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	m := NewManager(cfg, zerolog.Nop())
	ctx := context.Background()

	if err := m.EnsurePipeline(ctx, "mount-1", "fake-pipeline"); err != nil {
		t.Fatalf("first EnsurePipeline failed: %v", err)
	}
	p := m.GetPipeline("mount-1")
	select {
	case <-p.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("pipeline did not exit")
	}
	// After exit, EnsurePipeline should restart it.
	if err := m.EnsurePipeline(ctx, "mount-1", "fake-pipeline"); err != nil {
		t.Errorf("second EnsurePipeline failed: %v", err)
	}
}

func TestManager_EnsurePipelineWithOutput_HandlerCalled(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	m := NewManager(cfg, zerolog.Nop())

	called := make(chan struct{})
	handler := func(r io.Reader) {
		_, _ = io.Copy(io.Discard, r)
		close(called)
	}
	if err := m.EnsurePipelineWithOutput(context.Background(), "mount-1", "fake-pipeline", handler); err != nil {
		t.Fatalf("EnsurePipelineWithOutput failed: %v", err)
	}
	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("output handler was not called")
	}
}

func TestManager_EnsurePipelineWithDualOutput_HandlersInvoked(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	m := NewManager(cfg, zerolog.Nop())

	var wg sync.WaitGroup
	wg.Add(2)
	hq := func(r io.Reader) { _, _ = io.Copy(io.Discard, r); wg.Done() }
	lq := func(r io.Reader) { _, _ = io.Copy(io.Discard, r); wg.Done() }

	if err := m.EnsurePipelineWithDualOutput(context.Background(), "mount-1", "fake-pipeline", nil, hq, lq); err != nil {
		t.Fatalf("EnsurePipelineWithDualOutput failed: %v", err)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("HQ/LQ handlers not invoked within 3s")
	}
}

func TestManager_EnsurePipelineWithDualOutputAndInput_ReturnsWriter(t *testing.T) {
	cfg := &config.Config{GStreamerBin: "true"}
	m := NewManager(cfg, zerolog.Nop())

	w, err := m.EnsurePipelineWithDualOutputAndInput(context.Background(), "mount-1", "fake-pipeline", nil, nil)
	if err != nil {
		t.Fatalf("EnsurePipelineWithDualOutputAndInput failed: %v", err)
	}
	if w == nil {
		t.Fatal("expected non-nil stdin writer")
	}
	_ = w.Close()
}

func TestManager_StopPipeline_RunningProcess(t *testing.T) {
	scriptPath := fakeLongBinary(t)
	cfg := &config.Config{GStreamerBin: scriptPath}
	m := NewManager(cfg, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.EnsurePipeline(ctx, "mount-1", "fake-pipeline"); err != nil {
		t.Fatalf("EnsurePipeline failed: %v", err)
	}
	if err := m.StopPipeline("mount-1"); err != nil {
		t.Errorf("StopPipeline returned error: %v", err)
	}
}

func TestManager_Shutdown_WithRunningPipeline(t *testing.T) {
	scriptPath := fakeLongBinary(t)
	cfg := &config.Config{GStreamerBin: scriptPath}
	m := NewManager(cfg, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.EnsurePipeline(ctx, "mount-1", "fake-pipeline"); err != nil {
		t.Fatalf("EnsurePipeline failed: %v", err)
	}
	if err := m.Shutdown(); err != nil {
		t.Errorf("Shutdown returned error: %v", err)
	}
}

// ── Crossfade: decoderProc.stop ────────────────────────────────────────────

func TestDecoderProc_stop_NilReceiver(t *testing.T) {
	var d *decoderProc
	if err := d.stop(); err != nil {
		t.Errorf("nil stop returned error: %v", err)
	}
}

func TestDecoderProc_stop_EmptyProc(t *testing.T) {
	d := &decoderProc{}
	if err := d.stop(); err != nil {
		t.Errorf("empty decoderProc stop returned error: %v", err)
	}
}

// ── Crossfade: pcmCrossfadeSession.Play ───────────────────────────────────

func TestCrossfadeSession_Play_StartsFirstDecoder(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()

	sess := newPCMCrossfadeSession(
		sessionConfig{GStreamerBin: "true", SampleRate: 44100, Channels: 2},
		pw,
		zerolog.Nop(),
		nil,
	)
	f, err := os.CreateTemp("", "grimnir-test-media-*.mp3")
	if err != nil {
		t.Fatalf("create temp media: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())

	if err := sess.Play(context.Background(), f.Name(), 0, 0); err != nil {
		t.Fatalf("Play failed: %v", err)
	}
	sess.mu.Lock()
	hasCur := sess.cur != nil
	sess.mu.Unlock()
	if !hasCur {
		t.Error("expected sess.cur to be set after Play")
	}
	// Cleanup
	if err := sess.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestCrossfadeSession_Play_SecondCallCrossfades(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()

	sess := newPCMCrossfadeSession(
		sessionConfig{GStreamerBin: "true", SampleRate: 44100, Channels: 2},
		pw,
		zerolog.Nop(),
		nil,
	)
	f, err := os.CreateTemp("", "grimnir-test-media-*.mp3")
	if err != nil {
		t.Fatalf("create temp media: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())
	ctx := context.Background()

	if err := sess.Play(ctx, f.Name(), 0, 0); err != nil {
		t.Fatalf("first Play failed: %v", err)
	}
	// Second call should trigger xfade logic.
	if err := sess.Play(ctx, f.Name(), 500*time.Millisecond, 0); err != nil {
		t.Fatalf("second Play (crossfade) failed: %v", err)
	}
	sess.mu.Lock()
	hasXfade := sess.xfade != nil
	sess.mu.Unlock()
	if !hasXfade {
		t.Error("expected xfade state after second Play with fade duration")
	}
	if err := sess.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestCrossfadeSession_Play_SecondCallNoFade(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()

	sess := newPCMCrossfadeSession(
		sessionConfig{GStreamerBin: "true", SampleRate: 44100, Channels: 2},
		pw,
		zerolog.Nop(),
		nil,
	)
	f, err := os.CreateTemp("", "grimnir-test-media-*.mp3")
	if err != nil {
		t.Fatalf("create temp media: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())
	ctx := context.Background()

	if err := sess.Play(ctx, f.Name(), 0, 0); err != nil {
		t.Fatalf("first Play failed: %v", err)
	}
	// Second call with zero fade duration.
	if err := sess.Play(ctx, f.Name(), 0, 0); err != nil {
		t.Fatalf("second Play (no fade) failed: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestCrossfadeSession_Play_ErrorWhenClosing(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()

	sess := newPCMCrossfadeSession(
		sessionConfig{GStreamerBin: "true"},
		pw,
		zerolog.Nop(),
		nil,
	)
	sess.mu.Lock()
	sess.closing = true
	sess.mu.Unlock()

	err := sess.Play(context.Background(), "/some/file.mp3", 0, 0)
	if err == nil {
		t.Error("expected error when session is closing")
	}
}

// ── Crossfade: pcmCrossfadeSession.Pump ───────────────────────────────────

func TestCrossfadeSession_Pump_ContextCancel(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()

	sess := newPCMCrossfadeSession(
		sessionConfig{GStreamerBin: "true", SampleRate: 44100, Channels: 2},
		pw,
		zerolog.Nop(),
		nil,
	)
	ctx, cancel := context.WithCancel(context.Background())

	pumpDone := make(chan error, 1)
	go func() { pumpDone <- sess.Pump(ctx) }()

	cancel()
	select {
	case err := <-pumpDone:
		if err != context.Canceled {
			t.Errorf("Pump returned %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Pump did not exit after context cancel")
	}
}

func TestCrossfadeSession_Pump_ClosingExitsCleanly(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()

	sess := newPCMCrossfadeSession(
		sessionConfig{GStreamerBin: "true", SampleRate: 44100, Channels: 2},
		pw,
		zerolog.Nop(),
		nil,
	)
	sess.mu.Lock()
	sess.closing = true
	sess.mu.Unlock()

	err := sess.Pump(context.Background())
	if err != nil {
		t.Errorf("Pump with closing=true returned error: %v", err)
	}
}

func TestCrossfadeSession_Pump_EOFCallsOnTrackEnd(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()

	trackEnded := make(chan struct{}, 1)
	sess := newPCMCrossfadeSession(
		sessionConfig{GStreamerBin: "true", SampleRate: 44100, Channels: 2},
		pw,
		zerolog.Nop(),
		func() {
			select {
			case trackEnded <- struct{}{}:
			default:
			}
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = sess.Pump(ctx) }()

	f, err := os.CreateTemp("", "grimnir-test-media-*.mp3")
	if err != nil {
		t.Fatalf("create temp media: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())

	if err := sess.Play(ctx, f.Name(), 0, 0); err != nil {
		t.Fatalf("Play failed: %v", err)
	}

	select {
	case <-trackEnded:
		// onTrackEnd was called after the decoder emitted EOF
	case <-time.After(5 * time.Second):
		t.Fatal("onTrackEnd not called after decoder EOF")
	}
	pw.Close()
}

func TestCrossfadeSession_Pump_NilEncoderSleeps(t *testing.T) {
	// Pump should not crash when encoderIn is nil; it sleeps and polls.
	sess := newPCMCrossfadeSession(
		sessionConfig{GStreamerBin: "true", SampleRate: 44100, Channels: 2},
		nil, // nil encoderIn
		zerolog.Nop(),
		nil,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Pump will sleep repeatedly until ctx expires.
	err := sess.Pump(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("Pump returned %v, want context.DeadlineExceeded", err)
	}
}
