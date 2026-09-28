//go:build linux

package harbor

import (
	"context"
	"encoding/binary"
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

	ctx := t.Context()

	d, err := startDecoder(ctx, binary, "audio/mpeg", 44100, 2, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	decoderReaped(t, d) // deliberately wait BEFORE reading final buffered audio
	pcm, err := io.ReadAll(d.stdout)
	if err != nil || string(pcm) != "final PCM frame" {
		t.Fatalf("lost buffered PCM: %q, %v", pcm, err)
	}
}

func TestDecoderLifecycle_RepeatedStopAndCancel(t *testing.T) {
	binary := decoderFixture(t, "printf x; exec /bin/sleep 30")

	for i := 0; i < 40; i++ {
		ctx, cancel := context.WithCancel(t.Context())
		d, err := startDecoder(ctx, binary, "audio/mpeg", 44100, 2, zerolog.Nop())
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close(); cancel() })
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
			go func() { defer wg.Done(); d.Close() }()
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

	ctx := t.Context()

	before, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		d, err := startDecoder(ctx, binary, "audio/mpeg", 44100, 2, zerolog.Nop())
		if err == nil {
			d.Close()
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
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		d, err := startDecoder(ctx, gst, "audio/wav", 44100, 2, zerolog.Nop())
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close(); cancel() })
		written := make(chan error, 1)
		go func() {
			_, err := d.stdin.Write(decoderWAV())
			_ = d.stdin.Close()
			written <- err
		}()
		pcm, err := io.ReadAll(d.stdout)
		writeErr := <-written
		if err != nil || writeErr != nil || len(pcm) != 4410*4 {
			t.Fatalf("PCM bytes=%d read=%v write=%v stderr=%s", len(pcm), err, writeErr, d.Stderr())
		}
		decoderReaped(t, d)
		if !d.cmd.ProcessState.Success() {
			t.Fatalf("decoder failed: %v %s", d.cmd.ProcessState, d.Stderr())
		}
		_ = d.Close()
		cancel()
	}
}

func TestDecoderLifecycle_NonzeroExit(t *testing.T) {
	binary := decoderFixture(t, "exit 7")

	ctx := t.Context()
	d, err := startDecoder(ctx, binary, "audio/mpeg", 44100, 2, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	decoderReaped(t, d)
	if d.cmd.ProcessState.ExitCode() != 7 {
		t.Fatalf("exit status: %v", d.cmd.ProcessState)
	}
	d.Close()
}

func TestDecoderLifecycle_AlreadyCancelled(t *testing.T) {
	binary := decoderFixture(t, "exec /bin/sleep 30")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	d, err := startDecoder(ctx, binary, "audio/mpeg", 44100, 2, zerolog.Nop())
	if err == nil {
		d.Close()
		t.Fatal("expected cancelled startup to fail")
	}
	if d != nil {
		t.Fatal("cancelled startup returned a decoder")
	}
}
