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
	"os/exec"
	"strings"

	"github.com/rs/zerolog"
)

// decoderProc wraps a GStreamer subprocess that decodes compressed audio
// (MP3, Ogg, AAC, etc.) from stdin into raw S16LE PCM on stdout.
type decoderProc struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	cancel    context.CancelFunc
	stderrBuf *bytes.Buffer
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
	shellCmd := fmt.Sprintf("%s -e %s", gstreamerBin, pipeline)
	cmd := exec.CommandContext(cmdCtx, "sh", "-c", shellCmd)

	// Capture stderr for diagnostic output from GStreamer.
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("decoder stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("decoder stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start decoder: %w", err)
	}

	logger.Debug().
		Int("pid", cmd.Process.Pid).
		Str("content_type", contentType).
		Int("sample_rate", sampleRate).
		Int("channels", channels).
		Msg("harbor decoder started")

	return &decoderProc{
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		cancel:    cancel,
		stderrBuf: &stderrBuf,
	}, nil
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
	if d.stdin != nil {
		_ = d.stdin.Close()
	}
	if d.cancel != nil {
		d.cancel()
	}
	if d.cmd != nil && d.cmd.Process != nil {
		_ = d.cmd.Process.Kill()
	}
	return nil
}
