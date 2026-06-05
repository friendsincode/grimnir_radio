/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"io"
	"sync"

	"github.com/go-gst/go-gst/gst"
	"github.com/go-gst/go-gst/gst/app"
)

// appsinkReader adapts a GStreamer appsink to io.Reader. Read blocks until
// a sample is available, EOS, or Close. Bytes from successive samples are
// concatenated; partial samples are buffered for the next Read.
//
// Mirrors internal/edgeencoder/broadcast.go::AppsinkReader. Duplicated here
// (rather than extracted to a shared package) because the surface is small,
// stable, and a shared package would be a one-line wrapper around go-gst that
// adds an import dependency without a clearer abstraction.
//
// Close is idempotent & thread-safe; a subsequent Read returns io.EOF. Close
// does not unblock a Read that is already parked inside PullSample; the
// upstream pipeline must reach EOS or be torn down (SetState(StateNull)) for
// that to return.
type appsinkReader struct {
	sink     *app.Sink
	mu       sync.Mutex
	leftover []byte
	closed   bool
}

// newAppsinkReader wraps a non-nil *app.Sink as an io.ReadCloser.
func newAppsinkReader(sink *app.Sink) *appsinkReader {
	return &appsinkReader{sink: sink}
}

// Read implements io.Reader. Returns as soon as any bytes are available,
// blocking on PullSample only if no leftover from a prior sample is queued.
func (r *appsinkReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return 0, io.EOF
	}
	if len(r.leftover) > 0 {
		n := copy(p, r.leftover)
		r.leftover = r.leftover[n:]
		r.mu.Unlock()
		return n, nil
	}
	r.mu.Unlock()

	// Block (outside the lock) on PullSample so Close can flip the flag if
	// the caller races. PullSample returns nil on EOS or on flush/teardown.
	sample := r.sink.PullSample()
	if sample == nil {
		if r.sink.IsEOS() {
			return 0, io.EOF
		}
		r.mu.Lock()
		closed := r.closed
		r.mu.Unlock()
		if closed {
			return 0, io.EOF
		}
		return 0, io.ErrUnexpectedEOF
	}

	buf := sample.GetBuffer()
	if buf == nil {
		return 0, nil
	}

	mapInfo := buf.Map(gst.MapRead)
	if mapInfo == nil {
		return 0, nil
	}
	data := mapInfo.Bytes()
	buf.Unmap()

	n := copy(p, data)
	if n < len(data) {
		r.mu.Lock()
		r.leftover = append(r.leftover[:0], data[n:]...)
		r.mu.Unlock()
	}
	return n, nil
}

// Close marks the reader as closed; subsequent Reads return io.EOF. It does
// not interrupt a Read already parked inside PullSample (see type doc).
func (r *appsinkReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}
