/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package harbor

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// fakeInjector stands in for *playout.Director. It hands streamAudio a pipe as
// the "encoder stdin" so the test can read whatever PCM the decoder emits, and
// records whether release() ran.
type fakeInjector struct {
	w           io.WriteCloser
	err         error
	releaseCall chan struct{}
}

func (f *fakeInjector) InjectLiveSource(ctx context.Context, stationID, mountID string) (io.WriteCloser, func(), error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.w, func() {
		select {
		case <-f.releaseCall:
		default:
			close(f.releaseCall)
		}
	}, nil
}

// decoderStub makes startDecoder a stdin->stdout passthrough: "cat #" runs cat
// and comments out the gst pipeline args, so no GStreamer is needed.
const decoderStub = "cat #"

// gatedReader yields its payload once, then blocks on Read until stop is closed
// (returning EOF). Keeping the source open lets the test read the decoded output
// deterministically before ending the stream, sidestepping streamAudio's
// "source EOF kills the decoder" truncation race.
type gatedReader struct {
	data []byte
	sent bool
	stop chan struct{}
}

func (r *gatedReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, r.data), nil
	}
	<-r.stop
	return 0, io.EOF
}

func TestStreamAudio_PipesSourceToEncoder(t *testing.T) {
	pr, pw := io.Pipe()
	inj := &fakeInjector{w: pw, releaseCall: make(chan struct{})}
	s := &Server{
		director: inj,
		cfg:      Config{GStreamerBin: decoderStub},
		logger:   zerolog.Nop(),
	}

	want := []byte("compressed-audio-bytes-\x00\x01\x02")
	src := &gatedReader{data: want, stop: make(chan struct{})}
	t.Cleanup(func() { close(src.stop) })

	ctx, cancel := context.WithCancel(context.Background())
	conn := &SourceConnection{SessionID: "s1", StationID: "st1", MountID: "m1"}
	mount := models.Mount{ID: "m1", SampleRate: 44100, Channels: 2}

	done := make(chan struct{})
	go func() {
		s.streamAudio(ctx, conn, mount, "audio/mpeg", src)
		close(done)
	}()

	// Read exactly the bytes the decoder passthrough should emit, then compare.
	got := make([]byte, len(want))
	readErr := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(pr, got)
		readErr <- err
	}()
	select {
	case err := <-readErr:
		if err != nil {
			t.Fatalf("read encoder output: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("never read the decoded bytes on the encoder side")
	}
	if string(got) != string(want) {
		t.Fatalf("encoder received %q, want %q", got, want)
	}

	// End the stream and confirm streamAudio unwinds and releases the encoder.
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("streamAudio did not return after cancel")
	}
	select {
	case <-inj.releaseCall:
	case <-time.After(time.Second):
		t.Fatal("release() was not called")
	}
}

func TestStreamAudio_InjectError_ReturnsEarly(t *testing.T) {
	inj := &fakeInjector{err: errors.New("no encoder"), releaseCall: make(chan struct{})}
	s := &Server{
		director: inj,
		cfg:      Config{GStreamerBin: decoderStub},
		logger:   zerolog.Nop(),
	}

	conn := &SourceConnection{SessionID: "s1", StationID: "st1", MountID: "m1"}
	mount := models.Mount{ID: "m1"}

	done := make(chan struct{})
	go func() {
		// Inject fails before any decoder starts; must return without blocking.
		s.streamAudio(context.Background(), conn, mount, "audio/mpeg", &gatedReader{stop: make(chan struct{})})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("streamAudio did not return on inject error")
	}
	select {
	case <-inj.releaseCall:
		t.Fatal("release() should not run when inject failed")
	default:
	}
}

func TestStreamAudio_ZeroByteSource_WarnsAndReturns(t *testing.T) {
	pr, pw := io.Pipe()
	go func() { _, _ = io.Copy(io.Discard, pr) }()
	inj := &fakeInjector{w: pw, releaseCall: make(chan struct{})}
	s := &Server{
		director: inj,
		cfg:      Config{GStreamerBin: decoderStub},
		logger:   zerolog.Nop(),
	}

	conn := &SourceConnection{SessionID: "s1", StationID: "st1", MountID: "m1"}
	mount := models.Mount{ID: "m1", SampleRate: 44100, Channels: 2}

	done := make(chan struct{})
	go func() {
		// Empty source: the copy completes with zero bytes, exercising the
		// zero-byte-warning + decoder-stderr branch, and streamAudio returns.
		s.streamAudio(context.Background(), conn, mount, "audio/mpeg", strings.NewReader(""))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("streamAudio did not return on empty source")
	}
	select {
	case <-inj.releaseCall:
	case <-time.After(time.Second):
		t.Fatal("release() was not called")
	}
}

func TestLockedBuffer_ConcurrentWriteAndRead(t *testing.T) {
	b := &lockedBuffer{}
	if _, err := b.Write([]byte("hello ")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := b.Write([]byte("world")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := b.String(); got != "hello world" {
		t.Fatalf("String() = %q, want %q", got, "hello world")
	}
}

func TestStreamAudio_ContextCancel_Returns(t *testing.T) {
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pr.Close() })
	inj := &fakeInjector{w: pw, releaseCall: make(chan struct{})}
	s := &Server{
		director: inj,
		cfg:      Config{GStreamerBin: decoderStub},
		logger:   zerolog.Nop(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	conn := &SourceConnection{SessionID: "s1", StationID: "st1", MountID: "m1"}
	mount := models.Mount{ID: "m1", SampleRate: 44100, Channels: 2}

	// A source that stays open, so only ctx cancellation can end the stream.
	src := &gatedReader{data: []byte("x"), stop: make(chan struct{})}
	t.Cleanup(func() { close(src.stop) })

	done := make(chan struct{})
	go func() {
		s.streamAudio(ctx, conn, mount, "audio/mpeg", src)
		close(done)
	}()

	// Drain the encoder side so the pipe copy never blocks streamAudio.
	go func() { _, _ = io.Copy(io.Discard, pr) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("streamAudio did not return after context cancel")
	}
}
