//go:build linux

package playout

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// These fixtures exec their worker, just as the decoder shell must: no
// background descendants survive a failed test or a cancelled decoder.
func decoderFixture(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "decoder")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return p
}

func decoderReaped(t *testing.T, d *decoderProc) {
	t.Helper()
	select {
	case <-d.done:
	case <-time.After(3 * time.Second):
		t.Fatal("decoder was not waited on")
	}
	// Wait4 must report no child, not return this PID (an unreaped zombie).
	var status syscall.WaitStatus
	pid, err := syscall.Wait4(d.cmd.Process.Pid, &status, syscall.WNOHANG, nil)
	if pid != -1 || !errors.Is(err, syscall.ECHILD) {
		t.Fatalf("decoder not reaped: wait4 pid=%d err=%v", pid, err)
	}
	if d.cmd.ProcessState == nil {
		t.Fatal("Cmd.Wait did not record process state")
	}
}

func TestDecoderLifecycle_NaturalExitPreservesPCM(t *testing.T) {
	binary := decoderFixture(t, "printf 'final PCM frame'")
	sess := newPCMCrossfadeSession(sessionConfig{GStreamerBin: binary}, nopWriteCloser{io.Discard}, zerolog.Nop(), nil)
	ctx := t.Context()
	offset := time.Duration(0)
	d, err := sess.startDecoder(ctx, "unused media.mp3", offset)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.stop() })
	decoderReaped(t, d) // deliberately wait BEFORE reading final buffered audio
	pcm, err := io.ReadAll(d.stdout)
	if err != nil || string(pcm) != "final PCM frame" {
		t.Fatalf("lost buffered PCM: %q, %v", pcm, err)
	}
}

func TestDecoderLifecycle_RepeatedStopAndCancel(t *testing.T) {
	binary := decoderFixture(t, "printf x; exec /bin/sleep 30")
	sess := newPCMCrossfadeSession(sessionConfig{GStreamerBin: binary}, nopWriteCloser{io.Discard}, zerolog.Nop(), nil)
	offset := time.Duration(0)
	for i := 0; i < 40; i++ {
		ctx, cancel := context.WithCancel(t.Context())
		d, err := sess.startDecoder(ctx, "unused media.mp3", offset)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		t.Cleanup(func() { d.stop(); cancel() })
		if _, err := io.ReadFull(d.stdout, make([]byte, 1)); err != nil {
			t.Fatal(err)
		}
		if i%2 == 0 {
			cancel()
			decoderReaped(t, d) // cancellation alone must reap, without cleanup
		}
		var wg sync.WaitGroup
		for j := 0; j < 4; j++ {
			wg.Add(1)
			go func() { defer wg.Done(); d.stop() }()
		}
		wg.Wait()
		cancel()
		decoderReaped(t, d)
	}
}

func TestDecoderLifecycle_StartupFailureClosesPipes(t *testing.T) {
	// Removing sh forces Cmd.Start itself to fail after pipes are allocated.
	t.Setenv("PATH", t.TempDir())
	binary := "/nonexistent/decoder"
	sess := newPCMCrossfadeSession(sessionConfig{GStreamerBin: binary}, nopWriteCloser{io.Discard}, zerolog.Nop(), nil)
	ctx := t.Context()
	offset := time.Duration(0)
	before, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		d, err := sess.startDecoder(ctx, "unused media.mp3", offset)
		if err == nil {
			d.stop()
			t.Fatal("expected startup failure")
		}
		if d != nil {
			t.Fatal("failed startup returned decoder")
		}
	}
	after, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) > len(before) {
		t.Fatalf("startup leaked descriptors: before=%d after=%d", len(before), len(after))
	}
}

// Exercise the direct FFmpeg path as well as the GStreamer exec-shell path.
func TestDecoderLifecycle_FFmpegSeek(t *testing.T) {
	binary := decoderFixture(t, "printf x; exec /bin/sleep 30")
	dir := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(dir, "ffmpeg")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	sess := newPCMCrossfadeSession(sessionConfig{}, nopWriteCloser{io.Discard}, zerolog.Nop(), nil)
	for i := 0; i < 40; i++ {
		ctx, cancel := context.WithCancel(t.Context())
		d, err := sess.startDecoder(ctx, "unused.mp3", time.Second)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		t.Cleanup(func() { d.stop(); cancel() })
		if _, err := io.ReadFull(d.stdout, make([]byte, 1)); err != nil {
			t.Fatal(err)
		}
		cancel()
		decoderReaped(t, d)
		_ = d.stop()
	}
}

// Hold startup just after cmd.Start, before Play can publish the decoder.
type decoderStartGate struct {
	started chan int
	release chan struct{}
}

func (g *decoderStartGate) Write(p []byte) (int, error) {
	var event struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(p, &event); err != nil {
		return 0, err
	}
	g.started <- event.PID
	<-g.release
	return len(p), nil
}

func TestDecoderLifecycle_PlayCloseRace(t *testing.T) {
	binary := decoderFixture(t, "exec /bin/sleep 30")
	gate := &decoderStartGate{started: make(chan int, 1), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var release sync.Once
	defer release.Do(func() { close(gate.release) })
	sess := newPCMCrossfadeSession(sessionConfig{GStreamerBin: binary}, nopWriteCloser{io.Discard}, zerolog.New(gate), nil)
	done := make(chan error, 1)
	go func() { done <- sess.Play(ctx, "unused.mp3", 0, 0) }()
	var pid int
	select {
	case pid = <-gate.started:
	case <-time.After(3 * time.Second):
		t.Fatal("decoder did not start")
	}
	_ = sess.Close()
	release.Do(func() { close(gate.release) })
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Play accepted a decoder after Close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Play/Close deadlocked")
	}
	var status syscall.WaitStatus
	if got, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil); got != -1 || !errors.Is(err, syscall.ECHILD) {
		t.Fatalf("unpublished decoder was not reaped: pid=%d err=%v", got, err)
	}
	if sess.cur != nil || sess.next != nil {
		t.Fatal("closed session retained a decoder")
	}
}

// A short real WAV keeps this integration check bounded and independent of
// repository media files. Real GStreamer must parse the production pipeline.
func decoderWAV() []byte {
	const pcmBytes = 4410 * 4
	b := make([]byte, 44+pcmBytes)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 2)
	binary.LittleEndian.PutUint32(b[24:], 44100)
	binary.LittleEndian.PutUint32(b[28:], 44100*4)
	binary.LittleEndian.PutUint16(b[32:], 4)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], pcmBytes)
	return b
}

func TestDecoderLifecycle_RealMedia(t *testing.T) {
	gst, err := exec.LookPath("gst-launch-1.0")
	if err != nil {
		t.Skip("GStreamer not installed")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg not installed")
	}
	path := filepath.Join(t.TempDir(), "audio with spaces.wav")
	if err := os.WriteFile(path, decoderWAV(), 0600); err != nil {
		t.Fatal(err)
	}
	sess := newPCMCrossfadeSession(sessionConfig{GStreamerBin: gst}, nopWriteCloser{io.Discard}, zerolog.Nop(), nil)
	for _, offset := range []time.Duration{0, 10 * time.Millisecond} {
		for i := 0; i < 3; i++ {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			d, err := sess.startDecoder(ctx, path, offset)
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			t.Cleanup(func() { d.stop(); cancel() })
			pcm, err := io.ReadAll(d.stdout)
			if err != nil || len(pcm) == 0 {
				t.Fatalf("offset=%v PCM bytes=%d err=%v", offset, len(pcm), err)
			}
			decoderReaped(t, d)
			if !d.cmd.ProcessState.Success() {
				t.Fatalf("decoder failed: %v", d.cmd.ProcessState)
			}
			if offset == 0 && len(pcm) != 4410*4 {
				t.Fatalf("truncated PCM: %d bytes", len(pcm))
			}
			_ = d.stop()
			cancel()
		}
	}
}

func TestDecoderLifecycle_NonzeroExit(t *testing.T) {
	binary := decoderFixture(t, "exit 7")
	sess := newPCMCrossfadeSession(sessionConfig{GStreamerBin: binary}, nopWriteCloser{io.Discard}, zerolog.Nop(), nil)
	ctx := t.Context()
	d, err := sess.startDecoder(ctx, "unused.mp3", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.stop() })
	decoderReaped(t, d)
	if d.cmd.ProcessState.ExitCode() != 7 {
		t.Fatalf("exit status: %v", d.cmd.ProcessState)
	}
	d.stop()
}

func TestDecoderLifecycle_AlreadyCancelled(t *testing.T) {
	binary := decoderFixture(t, "exec /bin/sleep 30")
	sess := newPCMCrossfadeSession(sessionConfig{GStreamerBin: binary}, nopWriteCloser{io.Discard}, zerolog.Nop(), nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	d, err := sess.startDecoder(ctx, "unused.mp3", 0)
	if err == nil {
		d.stop()
		t.Fatal("expected cancelled startup to fail")
	}
	if d != nil {
		t.Fatal("cancelled startup returned a decoder")
	}
}
