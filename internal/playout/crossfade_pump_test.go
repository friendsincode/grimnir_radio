/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// errWriteCloser returns errWrite on every Write call.
var errWrite = errors.New("test write error")

type errWriteCloser struct{}

func (errWriteCloser) Write([]byte) (int, error) { return 0, errWrite }
func (errWriteCloser) Close() error              { return nil }

// nopWriteCloser wraps a writer with a no-op Close so it satisfies io.WriteCloser.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// frameSize returns the PCM frame size in bytes for a given rate/channels (20 ms frame).
func frameSize(rate, ch int) int {
	if rate <= 0 {
		rate = 44100
	}
	if ch <= 0 {
		ch = 2
	}
	return (rate / 50) * ch * 2
}

// TestCrossfadeSession_Pump_WritesFramesToEncoder covers the normal (no crossfade) write path.
func TestCrossfadeSession_Pump_WritesFramesToEncoder(t *testing.T) {
	var buf bytes.Buffer
	enc := nopWriteCloser{&buf}

	sess := newPCMCrossfadeSession(
		sessionConfig{SampleRate: 44100, Channels: 2},
		enc,
		zerolog.Nop(),
		nil,
	)

	fs := frameSize(44100, 2)
	curPr, curPw := io.Pipe()

	sess.mu.Lock()
	sess.cur = &decoderProc{stdout: curPr, cancel: func() {}}
	sess.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())

	pumpDone := make(chan error, 1)
	go func() { pumpDone <- sess.Pump(ctx) }()

	// Write one frame of silence.
	frame := make([]byte, fs)
	if _, err := curPw.Write(frame); err != nil {
		t.Fatalf("write to cur pipe: %v", err)
	}

	// Give pump time to consume the frame.
	time.Sleep(60 * time.Millisecond)
	cancel()
	curPw.Close()

	select {
	case <-pumpDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Pump did not exit after context cancel")
	}

	if buf.Len() < fs {
		t.Errorf("encoder received %d bytes, want at least %d", buf.Len(), fs)
	}
}

// TestCrossfadeSession_Pump_CrossfadeMixPath covers the crossfade mixing code path.
func TestCrossfadeSession_Pump_CrossfadeMixPath(t *testing.T) {
	var buf bytes.Buffer
	enc := nopWriteCloser{&buf}

	sess := newPCMCrossfadeSession(
		sessionConfig{SampleRate: 44100, Channels: 2},
		enc,
		zerolog.Nop(),
		nil,
	)

	fs := frameSize(44100, 2)
	curPr, curPw := io.Pipe()
	nextPr, nextPw := io.Pipe()

	// xfade duration in the past → p = 1.0 on the first mix → instant complete.
	sess.mu.Lock()
	sess.cur = &decoderProc{stdout: curPr, cancel: func() {}}
	sess.next = &decoderProc{stdout: nextPr, cancel: func() {}}
	sess.xfade = &xfadeState{
		start:    time.Now().Add(-2 * time.Second),
		duration: 500 * time.Millisecond,
	}
	sess.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())

	pumpDone := make(chan error, 1)
	go func() { pumpDone <- sess.Pump(ctx) }()

	// Write one frame to each pipe so the mix can proceed.
	frame := make([]byte, fs)
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		_, _ = curPw.Write(frame)
		_, _ = nextPw.Write(frame)
	}()
	select {
	case <-writeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout writing frames to decoder pipes")
	}

	// Allow pump to process.
	time.Sleep(60 * time.Millisecond)
	cancel()
	curPw.Close()
	nextPw.Close()

	select {
	case <-pumpDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Pump did not exit")
	}

	// We should have received at least one mixed frame.
	if buf.Len() < fs {
		t.Errorf("encoder received %d bytes after crossfade mix, want at least %d", buf.Len(), fs)
	}
}

// TestCrossfadeSession_Pump_CrossfadeNextEOF covers the fallback when next frame is unavailable.
func TestCrossfadeSession_Pump_CrossfadeNextEOF(t *testing.T) {
	var buf bytes.Buffer
	enc := nopWriteCloser{&buf}

	sess := newPCMCrossfadeSession(
		sessionConfig{SampleRate: 44100, Channels: 2},
		enc,
		zerolog.Nop(),
		nil,
	)

	fs := frameSize(44100, 2)
	curPr, curPw := io.Pipe()
	// Close next immediately → EOF on first read.
	nextPr, nextPw := io.Pipe()
	nextPw.Close()

	sess.mu.Lock()
	sess.cur = &decoderProc{stdout: curPr, cancel: func() {}}
	sess.next = &decoderProc{stdout: nextPr, cancel: func() {}}
	sess.xfade = &xfadeState{
		start:    time.Now(),
		duration: 5 * time.Second, // long duration so p < 1
	}
	sess.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())

	pumpDone := make(chan error, 1)
	go func() { pumpDone <- sess.Pump(ctx) }()

	// Write one frame to cur so pump can proceed.
	frame := make([]byte, fs)
	go func() { _, _ = curPw.Write(frame) }()

	time.Sleep(60 * time.Millisecond)
	cancel()
	curPw.Close()

	select {
	case <-pumpDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Pump did not exit")
	}
}

// TestCrossfadeSession_Pump_NilCurSleeps covers the path where cur is nil but enc is set.
func TestCrossfadeSession_Pump_NilCurSleeps(t *testing.T) {
	var buf bytes.Buffer
	enc := nopWriteCloser{&buf}

	sess := newPCMCrossfadeSession(
		sessionConfig{SampleRate: 44100, Channels: 2},
		enc,
		zerolog.Nop(),
		nil,
	)
	// enc is set but cur is nil → pump sleeps.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := sess.Pump(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("Pump returned %v, want context.DeadlineExceeded", err)
	}
}

// TestCrossfadeSession_startDecoder_FFmpegBranch covers the startOffset>0 code path.
// If ffmpeg is not installed the Start call fails, exercising the error return.
// If ffmpeg is installed, the decoder process is started and immediately cancelled.
func TestCrossfadeSession_startDecoder_FFmpegBranch(t *testing.T) {
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
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())

	ctx := context.Background()
	dec, err := sess.startDecoder(ctx, f.Name(), 500*time.Millisecond)
	if err != nil {
		// Expected if ffmpeg is not installed — the error path is still covered.
		t.Logf("startDecoder (ffmpeg path) returned error (ok if ffmpeg absent): %v", err)
		return
	}
	// If ffmpeg started, clean up.
	if dec != nil {
		_ = dec.stop()
	}
}

// TestCrossfadeSession_Pump_EncWriteError_NoCrossfade covers the enc.Write error path
// when no crossfade is active (line 275-277).
func TestCrossfadeSession_Pump_EncWriteError_NoCrossfade(t *testing.T) {
	sess := newPCMCrossfadeSession(
		sessionConfig{SampleRate: 44100, Channels: 2},
		errWriteCloser{},
		zerolog.Nop(),
		nil,
	)

	fs := frameSize(44100, 2)
	curPr, curPw := io.Pipe()

	sess.mu.Lock()
	sess.cur = &decoderProc{stdout: curPr, cancel: func() {}}
	sess.mu.Unlock()

	ctx := context.Background()
	pumpDone := make(chan error, 1)
	go func() { pumpDone <- sess.Pump(ctx) }()

	frame := make([]byte, fs)
	go func() { _, _ = curPw.Write(frame) }()

	select {
	case err := <-pumpDone:
		if err == nil {
			t.Error("expected error from Pump when enc.Write fails")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Pump did not exit after enc.Write error")
	}
	curPw.Close()
}

// TestCrossfadeSession_Pump_EncWriteError_FallbackAfterNextEOF covers the enc.Write
// error in the fallback path after next-decoder EOF (line 284-286).
func TestCrossfadeSession_Pump_EncWriteError_FallbackAfterNextEOF(t *testing.T) {
	sess := newPCMCrossfadeSession(
		sessionConfig{SampleRate: 44100, Channels: 2},
		errWriteCloser{},
		zerolog.Nop(),
		nil,
	)

	fs := frameSize(44100, 2)
	curPr, curPw := io.Pipe()
	// Close next immediately so readFrame(next) → EOF → falls back to writing curBuf.
	nextPr, nextPw := io.Pipe()
	nextPw.Close()

	sess.mu.Lock()
	sess.cur = &decoderProc{stdout: curPr, cancel: func() {}}
	sess.next = &decoderProc{stdout: nextPr, cancel: func() {}}
	sess.xfade = &xfadeState{start: time.Now(), duration: 5 * time.Second}
	sess.mu.Unlock()

	ctx := context.Background()
	pumpDone := make(chan error, 1)
	go func() { pumpDone <- sess.Pump(ctx) }()

	frame := make([]byte, fs)
	go func() { _, _ = curPw.Write(frame) }()

	select {
	case err := <-pumpDone:
		if err == nil {
			t.Error("expected error from Pump when fallback enc.Write fails")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Pump did not exit after enc.Write error in fallback")
	}
	curPw.Close()
}

// TestCrossfadeSession_Pump_EncWriteError_MixBuf covers the enc.Write error when
// writing the mixed crossfade buffer (line 308-310).
func TestCrossfadeSession_Pump_EncWriteError_MixBuf(t *testing.T) {
	sess := newPCMCrossfadeSession(
		sessionConfig{SampleRate: 44100, Channels: 2},
		errWriteCloser{},
		zerolog.Nop(),
		nil,
	)

	fs := frameSize(44100, 2)
	curPr, curPw := io.Pipe()
	nextPr, nextPw := io.Pipe()

	// Both cur and next with readable frames, xfade active with duration in past → p >= 1.
	sess.mu.Lock()
	sess.cur = &decoderProc{stdout: curPr, cancel: func() {}}
	sess.next = &decoderProc{stdout: nextPr, cancel: func() {}}
	sess.xfade = &xfadeState{
		start:    time.Now().Add(-2 * time.Second),
		duration: 500 * time.Millisecond,
	}
	sess.mu.Unlock()

	ctx := context.Background()
	pumpDone := make(chan error, 1)
	go func() { pumpDone <- sess.Pump(ctx) }()

	frame := make([]byte, fs)
	go func() {
		_, _ = curPw.Write(frame)
		_, _ = nextPw.Write(frame)
	}()

	select {
	case err := <-pumpDone:
		if err == nil {
			t.Error("expected error from Pump when mix enc.Write fails")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Pump did not exit after enc.Write error for mix buffer")
	}
	curPw.Close()
	nextPw.Close()
}

// TestCrossfadeSession_Pump_XfadeDurationZero covers the dur<=0 branch (line 293-295).
func TestCrossfadeSession_Pump_XfadeDurationZero(t *testing.T) {
	var buf bytes.Buffer
	enc := nopWriteCloser{&buf}

	sess := newPCMCrossfadeSession(
		sessionConfig{SampleRate: 44100, Channels: 2},
		enc,
		zerolog.Nop(),
		nil,
	)

	fs := frameSize(44100, 2)
	curPr, curPw := io.Pipe()
	nextPr, nextPw := io.Pipe()

	// xfade.duration = 0 → p = 1.0 immediately (dur <= 0 branch).
	sess.mu.Lock()
	sess.cur = &decoderProc{stdout: curPr, cancel: func() {}}
	sess.next = &decoderProc{stdout: nextPr, cancel: func() {}}
	sess.xfade = &xfadeState{start: time.Now(), duration: 0}
	sess.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	pumpDone := make(chan error, 1)
	go func() { pumpDone <- sess.Pump(ctx) }()

	frame := make([]byte, fs)
	go func() {
		_, _ = curPw.Write(frame)
		_, _ = nextPw.Write(frame)
	}()

	time.Sleep(80 * time.Millisecond)
	cancel()
	curPw.Close()
	nextPw.Close()

	select {
	case <-pumpDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Pump did not exit")
	}
}

// TestCrossfadeSession_Close_IdempotentAndNilSafe exercises Close and double-close.
func TestCrossfadeSession_Close_IdempotentAndNilSafe(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()

	sess := newPCMCrossfadeSession(
		sessionConfig{GStreamerBin: "true", SampleRate: 44100, Channels: 2},
		pw,
		zerolog.Nop(),
		nil,
	)

	// Close with no decoders running.
	if err := sess.Close(); err != nil {
		t.Errorf("first Close returned error: %v", err)
	}
	// Second close is a no-op.
	if err := sess.Close(); err != nil {
		t.Errorf("second Close returned error: %v", err)
	}
}
