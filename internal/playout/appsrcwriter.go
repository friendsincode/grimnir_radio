/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"sync"

	"github.com/go-gst/go-gst/gst"
	"github.com/go-gst/go-gst/gst/app"
)

// appsrcWriter adapts a GStreamer appsrc to io.WriteCloser. Each Write copies
// the slice into a fresh GstBuffer and pushes it; PushBuffer blocks naturally
// when the appsrc's internal queue is full, which mirrors the back-pressure
// the previous os.Pipe-based stdin path gave us.
//
// Close signals EOS to the upstream pipeline by calling EndStream; the
// pipeline then propagates EOS downstream and the bus consumer wakes.
//
// Safe for concurrent Close + Write in the sense that Close is idempotent &
// thread-safe; a subsequent Write returns the same EOS error.
type appsrcWriter struct {
	src *app.Source

	mu     sync.Mutex
	closed bool
}

func newAppsrcWriter(src *app.Source) *appsrcWriter {
	return &appsrcWriter{src: src}
}

// Write implements io.Writer. Returns the byte count on success or 0 + an
// error matching the GStreamer FlowReturn when the pipeline rejects the push.
func (w *appsrcWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return 0, errAppsrcClosed
	}
	w.mu.Unlock()

	buf := gst.NewBufferFromBytes(p)
	if buf == nil {
		return 0, errBufferAlloc
	}
	ret := w.src.PushBuffer(buf)
	switch ret {
	case gst.FlowOK, gst.FlowFlushing, gst.FlowEOS:
		return len(p), nil
	default:
		return 0, flowError(ret)
	}
}

// Close signals end-of-stream to the appsrc. Idempotent; subsequent calls and
// subsequent Writes both return without further effect.
func (w *appsrcWriter) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	w.mu.Unlock()
	w.src.EndStream()
	return nil
}

// Sentinel errors so callers can match without depending on go-gst constants.
var (
	errAppsrcClosed = appsrcError("appsrc writer closed")
	errBufferAlloc  = appsrcError("gst.NewBufferFromBytes returned nil")
)

type appsrcError string

func (e appsrcError) Error() string { return string(e) }

func flowError(ret gst.FlowReturn) error {
	return appsrcError("appsrc push: " + ret.String())
}
