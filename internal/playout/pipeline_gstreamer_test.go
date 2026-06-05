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
	"github.com/go-gst/go-gst/gst"
	"github.com/rs/zerolog"
)

// Pipeline tests after the NetClock Chunk 1 Task 1.2 migration:
// gst-launch-1.0 subprocess → programmatic gst.NewPipelineFromString.
//
// The launch string is now real GStreamer text that go-gst parses. Tests use
// fast-finishing audiotestsrc graphs (num-buffers=N) instead of the previous
// `GStreamerBin="true"` subprocess trick. The contract surface (Start, Done,
// Stop, restart-after-exit, handler invocation) is identical to the pre-migration
// surface; only the underlying spawning layer changed.

func init() {
	// gst.Init is idempotent; calling once per test process is enough.
	gst.Init(nil)
}

// shortPipeline is a launch string that produces 20 buffers (~250 ms of audio)
// and naturally EOSes. Used wherever the prior tests wanted "process exits
// quickly on its own".
const shortPipeline = "audiotestsrc num-buffers=20 ! audioconvert ! audioresample ! audio/x-raw,rate=44100,channels=2 ! tee name=t " +
	"t. ! queue ! lamemp3enc target=1 bitrate=128 cbr=true ! fdsink fd=3 " +
	"t. ! queue ! lamemp3enc target=1 bitrate=64 cbr=true  ! fdsink fd=4"

// longPipeline runs indefinitely (no num-buffers); used wherever the prior
// tests wanted a long-running process to exercise Stop / "already running".
const longPipeline = "audiotestsrc is-live=true ! audioconvert ! audio/x-raw,rate=44100,channels=2 ! tee name=t " +
	"t. ! queue ! lamemp3enc target=1 bitrate=128 cbr=true ! fdsink fd=3 " +
	"t. ! queue ! lamemp3enc target=1 bitrate=64 cbr=true  ! fdsink fd=4"

// stdinPipeline reads raw PCM via "fdsrc fd=0" (translated to appsrc name=stdin)
// and encodes to two MP3 streams. Mirrors the shape director.go builds for
// PCM-crossfade encoders.
const stdinPipeline = "fdsrc fd=0 ! queue ! audio/x-raw,format=S16LE,rate=44100,channels=2,layout=interleaved ! " +
	"audioconvert ! audio/x-raw,format=S16LE,rate=44100,channels=2,layout=interleaved ! tee name=t " +
	"t. ! queue ! lamemp3enc target=1 bitrate=128 cbr=true ! fdsink fd=3 " +
	"t. ! queue ! lamemp3enc target=1 bitrate=64 cbr=true  ! fdsink fd=4"

func newTestPipeline(t *testing.T) *Pipeline {
	t.Helper()
	return NewPipeline(&config.Config{}, "test-mount", zerolog.Nop())
}

// ── translateLaunch (pure rewrite) ─────────────────────────────────────────

func TestTranslateLaunch_RewritesFdsinkFd3AndFd4(t *testing.T) {
	in := "filesrc location=x ! decodebin ! tee name=t " +
		"t. ! queue ! lamemp3enc ! fdsink fd=3 " +
		"t. ! queue ! lamemp3enc ! fdsink fd=4"
	got := translateLaunch(in, nil)
	if !contains(got, "appsink name=hq") || !contains(got, "appsink name=lq") {
		t.Fatalf("translation missing appsinks: %q", got)
	}
	if contains(got, "fdsink fd=3") || contains(got, "fdsink fd=4") {
		t.Fatalf("translation left fdsink markers in place: %q", got)
	}
}

func TestTranslateLaunch_RewritesFdsrcFd0ToAppsrc(t *testing.T) {
	in := "fdsrc fd=0 ! audioconvert ! fakesink"
	got := translateLaunch(in, nil)
	if !contains(got, "appsrc name=stdin") {
		t.Fatalf("translation missing appsrc: %q", got)
	}
	if contains(got, "fdsrc fd=0") {
		t.Fatalf("translation left fdsrc fd=0 in place: %q", got)
	}
}

func TestTranslateLaunch_SubstitutesSeekFileFd(t *testing.T) {
	f, err := os.CreateTemp("", "grimnir-seek-fd-*.bin")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	in := "fdsrc fd=5 ! decodebin ! fakesink"
	got := translateLaunch(in, f)
	if contains(got, "fdsrc fd=5") {
		t.Fatalf("seek fd substitution left fd=5 in place: %q", got)
	}
	// Should contain the actual fd number from the file.
	want := "fdsrc fd="
	if !contains(got, want) {
		t.Fatalf("expected %q in %q", want, got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ── Pipeline.Start ─────────────────────────────────────────────────────────

func TestPipeline_Start_PipelineRunsToEOS(t *testing.T) {
	p := newTestPipeline(t)
	if err := p.Start(context.Background(), shortPipeline); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	done := p.Done()
	if done == nil {
		t.Fatal("Done() returned nil after Start")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline did not reach EOS within 5s (num-buffers=20 should finish in <1s)")
	}
}

func TestPipeline_Start_AlreadyRunning(t *testing.T) {
	p := newTestPipeline(t)
	if err := p.Start(context.Background(), longPipeline); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	defer p.Stop()

	if err := p.Start(context.Background(), longPipeline); err == nil {
		t.Error("expected error when starting already-running pipeline")
	}
}

func TestPipeline_Start_RestartAfterExit(t *testing.T) {
	p := newTestPipeline(t)
	ctx := context.Background()

	if err := p.Start(ctx, shortPipeline); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("first run did not exit")
	}
	if err := p.Start(ctx, shortPipeline); err != nil {
		t.Errorf("restart after exit failed: %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("second run did not exit")
	}
}

// ── Pipeline.StartWithOutput ───────────────────────────────────────────────

func TestPipeline_StartWithOutput_NilHandler(t *testing.T) {
	p := newTestPipeline(t)
	if err := p.StartWithOutput(context.Background(), shortPipeline, nil); err != nil {
		t.Fatalf("StartWithOutput(nil handler) failed: %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline did not exit")
	}
}

func TestPipeline_StartWithOutput_HandlerReceivesEOF(t *testing.T) {
	p := newTestPipeline(t)
	called := make(chan struct{})
	handler := func(r io.Reader) {
		_, _ = io.Copy(io.Discard, r)
		close(called)
	}
	if err := p.StartWithOutput(context.Background(), shortPipeline, handler); err != nil {
		t.Fatalf("StartWithOutput failed: %v", err)
	}
	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("output handler was not invoked / did not see EOF")
	}
}

// ── Pipeline.StartWithDualOutput ──────────────────────────────────────────

func TestPipeline_StartWithDualOutput_NilHandlers(t *testing.T) {
	p := newTestPipeline(t)
	if err := p.StartWithDualOutput(context.Background(), shortPipeline, nil, nil, nil); err != nil {
		t.Fatalf("StartWithDualOutput(nil handlers) failed: %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline did not exit")
	}
}

func TestPipeline_StartWithDualOutput_HandlersReceiveBytes(t *testing.T) {
	p := newTestPipeline(t)
	var wg sync.WaitGroup
	wg.Add(2)
	var hqBytes, lqBytes int64
	hq := func(r io.Reader) {
		n, _ := io.Copy(io.Discard, r)
		hqBytes = n
		wg.Done()
	}
	lq := func(r io.Reader) {
		n, _ := io.Copy(io.Discard, r)
		lqBytes = n
		wg.Done()
	}

	if err := p.StartWithDualOutput(context.Background(), shortPipeline, nil, hq, lq); err != nil {
		t.Fatalf("StartWithDualOutput failed: %v", err)
	}
	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("HQ/LQ handlers did not return within 5s")
	}
	if hqBytes == 0 || lqBytes == 0 {
		t.Errorf("expected non-zero bytes on both streams; got hq=%d lq=%d", hqBytes, lqBytes)
	}
}

func TestPipeline_StartWithDualOutput_AlreadyRunning(t *testing.T) {
	p := newTestPipeline(t)
	if err := p.StartWithDualOutput(context.Background(), longPipeline, nil, nil, nil); err != nil {
		t.Fatalf("first StartWithDualOutput failed: %v", err)
	}
	defer p.Stop()
	if err := p.StartWithDualOutput(context.Background(), longPipeline, nil, nil, nil); err == nil {
		t.Error("expected error for already-running pipeline")
	}
}

// ── Pipeline.StartWithDualOutputAndInput ──────────────────────────────────

func TestPipeline_StartWithDualOutputAndInput_ReturnsWriter(t *testing.T) {
	p := newTestPipeline(t)
	stdin, err := p.StartWithDualOutputAndInput(context.Background(), stdinPipeline, nil, nil)
	if err != nil {
		t.Fatalf("StartWithDualOutputAndInput failed: %v", err)
	}
	if stdin == nil {
		t.Fatal("expected non-nil stdin writer")
	}
	_ = stdin.Close()
	_ = p.Stop()
}

func TestPipeline_StartWithDualOutputAndInput_ReturnsExistingStdin(t *testing.T) {
	p := newTestPipeline(t)
	stdin1, err := p.StartWithDualOutputAndInput(context.Background(), stdinPipeline, nil, nil)
	if err != nil {
		t.Fatalf("first StartWithDualOutputAndInput failed: %v", err)
	}
	defer p.Stop()
	stdin2, err := p.StartWithDualOutputAndInput(context.Background(), stdinPipeline, nil, nil)
	if err != nil {
		t.Errorf("second StartWithDualOutputAndInput: unexpected error: %v", err)
	}
	if stdin2 != stdin1 {
		t.Error("expected second call to return same stdin writer as first")
	}
}

// ── Pipeline.Stop ─────────────────────────────────────────────────────────

func TestPipeline_Stop_RunningPipeline(t *testing.T) {
	p := newTestPipeline(t)
	if err := p.Start(context.Background(), longPipeline); err != nil {
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
	p := newTestPipeline(t)
	if err := p.Start(context.Background(), shortPipeline); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline did not exit")
	}
	if err := p.Stop(); err != nil {
		t.Errorf("Stop on exited pipeline returned error: %v", err)
	}
}

// ── Pipeline.StartWithDualOutput seek file ─────────────────────────────────

func TestPipeline_StartWithDualOutput_WithSeekFile_TranslatesFd(t *testing.T) {
	// Seek file: open a tiny binary blob. The pipeline below doesn't actually
	// read it (we use audiotestsrc as the real source) — this test only
	// verifies that providing a seekFile to StartWithDualOutput doesn't break
	// anything and that the file fd gets retained for the pipeline's lifetime.
	sf, err := os.CreateTemp("", "grimnir-seek-*.bin")
	if err != nil {
		t.Fatalf("create seek file: %v", err)
	}
	if _, err := sf.Write([]byte("dummy")); err != nil {
		t.Fatalf("write seek file: %v", err)
	}
	sf.Close()
	defer os.Remove(sf.Name())
	seekFile, err := os.Open(sf.Name())
	if err != nil {
		t.Fatalf("open seek file: %v", err)
	}

	p := newTestPipeline(t)
	var wg sync.WaitGroup
	wg.Add(2)
	hq := func(r io.Reader) { _, _ = io.Copy(io.Discard, r); wg.Done() }
	lq := func(r io.Reader) { _, _ = io.Copy(io.Discard, r); wg.Done() }
	if err := p.StartWithDualOutput(context.Background(), shortPipeline, seekFile, hq, lq); err != nil {
		t.Fatalf("StartWithDualOutput with seekFile failed: %v", err)
	}
	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("handlers not called within 5s")
	}
}

// ── Pipeline.StartWithDualOutputAndInput already-running nil stdin ─────────

func TestPipeline_StartWithDualOutputAndInput_AlreadyRunningNilStdin(t *testing.T) {
	// Start a long pipeline that has NO appsrc (Start uses shortPipeline-style
	// graph without fdsrc fd=0). Calling StartWithDualOutputAndInput while it's
	// running should error since p.stdinWriter is nil.
	p := newTestPipeline(t)
	if err := p.Start(context.Background(), longPipeline); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer p.Stop()
	if _, err := p.StartWithDualOutputAndInput(context.Background(), stdinPipeline, nil, nil); err == nil {
		t.Error("expected error when StartWithDualOutputAndInput called while running with nil stdin")
	}
}

// ── Pipeline.CurrentPID ────────────────────────────────────────────────────

func TestPipeline_CurrentPID_AlwaysZeroPostMigration(t *testing.T) {
	// After Chunk 1 Task 1.2, pipelines are in-process so there's no separate
	// pid. CurrentPID returns 0 unconditionally; the orphan reaper relies on
	// this to mean "no subprocess to track".
	p := newTestPipeline(t)
	if got := p.CurrentPID(); got != 0 {
		t.Errorf("CurrentPID before Start: got %d, want 0", got)
	}
	if err := p.Start(context.Background(), shortPipeline); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := p.CurrentPID(); got != 0 {
		t.Errorf("CurrentPID after Start: got %d, want 0", got)
	}
	<-p.Done()
}

// ── Manager.Ensure* ────────────────────────────────────────────────────────

func TestManager_EnsurePipeline_StartsAndRunsToEOS(t *testing.T) {
	m := NewManager(&config.Config{}, zerolog.Nop())
	if err := m.EnsurePipeline(context.Background(), "mount-1", shortPipeline); err != nil {
		t.Fatalf("EnsurePipeline failed: %v", err)
	}
	p := m.GetPipeline("mount-1")
	if p == nil {
		t.Fatal("GetPipeline returned nil after EnsurePipeline")
	}
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline did not exit")
	}
}

func TestManager_EnsurePipeline_ReuseAfterExit(t *testing.T) {
	m := NewManager(&config.Config{}, zerolog.Nop())
	ctx := context.Background()
	if err := m.EnsurePipeline(ctx, "mount-1", shortPipeline); err != nil {
		t.Fatalf("first EnsurePipeline failed: %v", err)
	}
	p := m.GetPipeline("mount-1")
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline did not exit")
	}
	if err := m.EnsurePipeline(ctx, "mount-1", shortPipeline); err != nil {
		t.Errorf("second EnsurePipeline failed: %v", err)
	}
}

func TestManager_EnsurePipelineWithOutput_HandlerCalled(t *testing.T) {
	m := NewManager(&config.Config{}, zerolog.Nop())
	called := make(chan struct{})
	handler := func(r io.Reader) {
		_, _ = io.Copy(io.Discard, r)
		close(called)
	}
	if err := m.EnsurePipelineWithOutput(context.Background(), "mount-1", shortPipeline, handler); err != nil {
		t.Fatalf("EnsurePipelineWithOutput failed: %v", err)
	}
	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("output handler was not called")
	}
}

func TestManager_EnsurePipelineWithDualOutput_HandlersInvoked(t *testing.T) {
	m := NewManager(&config.Config{}, zerolog.Nop())
	var wg sync.WaitGroup
	wg.Add(2)
	hq := func(r io.Reader) { _, _ = io.Copy(io.Discard, r); wg.Done() }
	lq := func(r io.Reader) { _, _ = io.Copy(io.Discard, r); wg.Done() }
	if err := m.EnsurePipelineWithDualOutput(context.Background(), "mount-1", shortPipeline, nil, hq, lq); err != nil {
		t.Fatalf("EnsurePipelineWithDualOutput failed: %v", err)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("HQ/LQ handlers not invoked within 5s")
	}
}

func TestManager_EnsurePipelineWithDualOutputAndInput_ReturnsWriter(t *testing.T) {
	m := NewManager(&config.Config{}, zerolog.Nop())
	w, err := m.EnsurePipelineWithDualOutputAndInput(context.Background(), "mount-1", stdinPipeline, nil, nil)
	if err != nil {
		t.Fatalf("EnsurePipelineWithDualOutputAndInput failed: %v", err)
	}
	if w == nil {
		t.Fatal("expected non-nil stdin writer")
	}
	_ = w.Close()
	_ = m.StopPipeline("mount-1")
}

func TestManager_StopPipeline_RunningPipeline(t *testing.T) {
	m := NewManager(&config.Config{}, zerolog.Nop())
	if err := m.EnsurePipeline(context.Background(), "mount-1", longPipeline); err != nil {
		t.Fatalf("EnsurePipeline failed: %v", err)
	}
	if err := m.StopPipeline("mount-1"); err != nil {
		t.Errorf("StopPipeline returned error: %v", err)
	}
}

func TestManager_Shutdown_WithRunningPipeline(t *testing.T) {
	m := NewManager(&config.Config{}, zerolog.Nop())
	if err := m.EnsurePipeline(context.Background(), "mount-1", longPipeline); err != nil {
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
