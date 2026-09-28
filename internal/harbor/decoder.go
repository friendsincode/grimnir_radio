/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package harbor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

// lockedBuffer is a bytes.Buffer safe for the concurrent access decoderProc
// needs: os/exec's stderr-copy goroutine writes it while streamAudio reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// decoderProc wraps a GStreamer subprocess that decodes compressed audio
// (MP3, Ogg, AAC, etc.) from stdin into raw S16LE PCM on stdout.
type decoderProc struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	cancel    context.CancelFunc
	stderrBuf *lockedBuffer
	closeOnce sync.Once
	done      chan struct{} // closed by the sole cmd.Wait owner
}

// startDecoder launches a GStreamer pipeline that reads compressed audio from stdin
// and outputs raw S16LE PCM (44100 Hz, stereo) on stdout.
//
// The caller writes compressed audio to stdin and reads decoded PCM from stdout
// (or pipes stdout directly into an encoder stdin).
func startDecoder(ctx context.Context, gstreamerBin string, contentType string, sampleRate, channels int, logger zerolog.Logger) (*decoderProc, error) {
	if sampleRate <= 0 {
		sampleRate = 44100
	}
	if channels <= 0 {
		channels = 2
	}

	// Use decodebin for automatic format detection — handles MP3, Ogg, AAC, Opus, FLAC, etc.
	pipeline := fmt.Sprintf(
		`fdsrc fd=0 ! decodebin ! audioconvert ! audioresample ! audio/x-raw,format=S16LE,rate=%d,channels=%d ! fdsink fd=1`,
		sampleRate, channels,
	)

	cmdCtx, cancel := context.WithCancel(ctx)
	shellCmd := fmt.Sprintf("exec %s -q -e %s", gstreamerBin, pipeline)
	cmd := exec.CommandContext(cmdCtx, "sh", "-c", shellCmd)

	// Capture stderr for diagnostic output from GStreamer.
	stderrBuf := &lockedBuffer{}
	cmd.Stderr = stderrBuf

	input, stdin, err := os.Pipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("decoder stdin pipe: %w", err)
	}

	// Keep buffered PCM readable after Wait; StdoutPipe would be closed by it.
	stdout, output, err := os.Pipe()
	if err != nil {
		cancel()
		_ = input.Close()
		_ = stdin.Close()
		return nil, fmt.Errorf("decoder stdout pipe: %w", err)
	}

	cmd.Stdin = input
	cmd.Stdout = output
	if err := cmd.Start(); err != nil {
		_ = input.Close()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = output.Close()
		cancel()
		return nil, fmt.Errorf("start decoder: %w", err)
	}

	_ = input.Close()  // only the child keeps the read end
	_ = output.Close() // only the child keeps the write end
	d := &decoderProc{
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		cancel:    cancel,
		stderrBuf: stderrBuf,
		done:      make(chan struct{}),
	}
	go func() {
		_ = cmd.Wait() // reaps natural exits even before Close is called
		_ = stdin.Close()
		cancel()
		close(d.done)
	}()
	logger.Debug().
		Int("pid", cmd.Process.Pid).
		Str("content_type", contentType).
		Int("sample_rate", sampleRate).
		Int("channels", channels).
		Msg("harbor decoder started")
	return d, nil
}

// Stderr returns any accumulated stderr output from the decoder process.
func (d *decoderProc) Stderr() string {
	if d == nil || d.stderrBuf == nil {
		return ""
	}
	return strings.TrimSpace(d.stderrBuf.String())
}

// Close terminates the decoder process.
func (d *decoderProc) Close() error {
	if d == nil {
		return nil
	}
	d.closeOnce.Do(func() {
		if d.stdin != nil {
			_ = d.stdin.Close()
		}
		if d.cancel != nil {
			d.cancel()
		}
		if d.stdout != nil {
			_ = d.stdout.Close()
		}
		if d.done != nil {
			<-d.done
		}
	})
	return nil
}
