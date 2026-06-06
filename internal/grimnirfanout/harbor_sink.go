/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// PipelineHarborSink is the production HarborSessionSink. For each Begin it
//  1. constructs a *Pipeline targeting the configured engine list,
//  2. spawns a gst-launch-1.0 subprocess that decodes whatever encoded
//     audio (MP3/AAC/Ogg/Opus/FLAC) the DJ pushes into raw PCM,
//  3. feeds Bytes(...) into the subprocess's stdin, and
//  4. pumps the subprocess's stdout into Pipeline.PushPCM.
//
// Why subprocess: decodebin + audioconvert via CGo would force us to maintain
// per-format plugin glue. The mediaengine already uses a gst-launch
// subprocess; matching that pattern keeps the binary's CGo surface to
// "single per-session output pipeline" (cmd/edge-encoder already shows
// programmatic go-gst is fine for the output side).
type PipelineHarborSink struct {
	Engines    []string
	DecoderCmd []string // overridden in tests; default is gst-launch chain

	mu      sync.Mutex
	streams map[string]*harborStream
}

type harborStream struct {
	pipe    *Pipeline
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	pumpErr chan error
}

// NewPipelineHarborSink returns a sink wired against the given engine list.
// engines is the comma-separated multiudpsink target set ([host:port,...]).
func NewPipelineHarborSink(engines []string) *PipelineHarborSink {
	return &PipelineHarborSink{
		Engines: engines,
		DecoderCmd: []string{
			"gst-launch-1.0", "-q",
			"fdsrc", "fd=0", "!",
			"decodebin", "!",
			"audioconvert", "!",
			"audio/x-raw,format=S16LE,rate=44100,channels=2", "!",
			"fdsink", "fd=1",
		},
		streams: make(map[string]*harborStream),
	}
}

// Begin constructs the per-session pipeline & decoder subprocess. Returns
// an error if either fails; the listener responds 500 + closes the conn.
func (p *PipelineHarborSink) Begin(sess *Session, mount string) error {
	pipe, err := NewPipeline(PipelineConfig{
		Engines: p.Engines,
		SourceLaunch: "appsrc name=audio_in format=time is-live=true block=true " +
			"caps=audio/x-raw,format=S16LE,rate=44100,channels=2",
	})
	if err != nil {
		return fmt.Errorf("harbor sink: new pipeline: %w", err)
	}
	if err := pipe.Start(); err != nil {
		return fmt.Errorf("harbor sink: pipeline start: %w", err)
	}
	sess.AttachPipeline(pipe)

	cmd := exec.Command(p.DecoderCmd[0], p.DecoderCmd[1:]...) //nolint:gosec // operator-supplied launch line
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = pipe.Stop()
		return fmt.Errorf("harbor sink: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		_ = pipe.Stop()
		return fmt.Errorf("harbor sink: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = pipe.Stop()
		return fmt.Errorf("harbor sink: decoder spawn: %w", err)
	}

	stream := &harborStream{
		pipe:    pipe,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		pumpErr: make(chan error, 1),
	}

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				if perr := pipe.PushPCM(buf[:n]); perr != nil {
					stream.pumpErr <- perr
					return
				}
			}
			if err != nil {
				stream.pumpErr <- err
				return
			}
		}
	}()

	p.mu.Lock()
	p.streams[sess.ID] = stream
	p.mu.Unlock()
	return nil
}

// Bytes forwards a chunk read off the DJ's TCP conn into the decoder
// subprocess's stdin. The decoder's stdout is being pumped into the
// pipeline's appsrc by the goroutine started in Begin.
func (p *PipelineHarborSink) Bytes(sess *Session, b []byte) error {
	p.mu.Lock()
	stream, ok := p.streams[sess.ID]
	p.mu.Unlock()
	if !ok {
		return fmt.Errorf("harbor sink: session %s not begun", sess.ID)
	}
	_, err := stream.stdin.Write(b)
	return err
}

// End tears down the decoder subprocess + pipeline. Called once per session
// regardless of how the conn terminated.
func (p *PipelineHarborSink) End(sess *Session) {
	p.mu.Lock()
	stream, ok := p.streams[sess.ID]
	if ok {
		delete(p.streams, sess.ID)
	}
	p.mu.Unlock()
	if !ok {
		return
	}
	_ = stream.stdin.Close()
	_ = stream.cmd.Wait()
	_ = stream.stdout.Close()
	if stream.pipe != nil {
		_ = stream.pipe.Stop()
	}
}
