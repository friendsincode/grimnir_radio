/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package harbor

import (
	"bytes"
	"io"
	"testing"

	"github.com/rs/zerolog"
)

func TestDecoderProc_Close_Nil(t *testing.T) {
	// Close on nil should not panic.
	var d *decoderProc
	if err := d.Close(); err != nil {
		t.Errorf("Close() on nil decoder returned error: %v", err)
	}
}

func TestDecoderProc_Stderr_Nil(t *testing.T) {
	var d *decoderProc
	if got := d.Stderr(); got != "" {
		t.Errorf("Stderr() on nil decoder = %q, want empty", got)
	}
}

func TestDecoderProc_Stderr_Empty(t *testing.T) {
	d := &decoderProc{}
	if got := d.Stderr(); got != "" {
		t.Errorf("Stderr() on empty decoder = %q, want empty", got)
	}
}

func TestDecoderProc_Stderr_Content(t *testing.T) {
	buf := bytes.NewBufferString("  GStreamer warning: something  \n")
	d := &decoderProc{stderrBuf: buf}
	got := d.Stderr()
	if got != "GStreamer warning: something" {
		t.Errorf("Stderr() = %q, want %q", got, "GStreamer warning: something")
	}
}

func TestStartDecoder_InvalidBinary(t *testing.T) {
	// sh -c wrapping means Start succeeds even for nonexistent binaries —
	// the shell starts fine, then the inner command fails. We verify the
	// decoder can be created and closed without panic.
	ctx := t.Context()
	dec, err := startDecoder(ctx, "/nonexistent/gst-launch-1.0", "audio/mpeg", 44100, 2, zerolog.Nop())
	if err != nil {
		// If it fails at Start, that's also acceptable.
		return
	}
	// Close should not panic.
	dec.Close()
}

func TestStartDecoder_DefaultSampleRate(t *testing.T) {
	// With sampleRate=0 and channels=0, should use defaults (44100, 2)
	// and not panic.
	ctx := t.Context()
	dec, err := startDecoder(ctx, "/nonexistent/gst-launch-1.0", "audio/mpeg", 0, 0, zerolog.Nop())
	if err != nil {
		return
	}
	dec.Close()
}

func TestDecoderProc_Close_WithStdin(t *testing.T) {
	pr, pw := io.Pipe()
	d := &decoderProc{
		stdin: pw,
	}
	if err := d.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
	// Verify stdin was closed — reading should return error.
	buf := make([]byte, 1)
	_, err := pr.Read(buf)
	if err == nil {
		t.Error("expected read from closed pipe to fail")
	}
}
