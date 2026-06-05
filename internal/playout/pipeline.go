/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/go-gst/go-gst/gst"
	"github.com/go-gst/go-gst/gst/app"
	"github.com/rs/zerolog"
)

// PipelineInterface abstracts a GStreamer pipeline for testing.
type PipelineInterface interface {
	Done() <-chan struct{}
	Stop() error
}

// Pipeline owns one programmatic GStreamer pipeline for a broadcast mount.
//
// As of NetClock Chunk 1 (Task 1.2) this no longer spawns gst-launch-1.0 as a
// subprocess; it constructs the pipeline in-process via go-gst's parse_launch
// wrapper (gst.NewPipelineFromString). The pipeline STRINGS in director.go are
// unchanged — they're translated at the boundary here:
//
//   - "fdsink fd=3" → "appsink name=hq emit-signals=false sync=false ..."
//   - "fdsink fd=4" → "appsink name=lq ..."
//   - "fdsrc fd=0"  → "appsrc name=stdin format=time is-live=true"
//   - "fdsrc fd=5"  → "fdsrc fd=<seekFile.Fd()>" (the file descriptor lives in
//     our process now, so we substitute the actual fd number)
//
// Encoded bytes that previously flowed out of fdsink fd=3/4 into os.Pipe read
// ends now flow out of the appsinks via appsinkReader (PullSample). Stdin bytes
// that previously went into fdsrc fd=0 via cmd.StdinPipe() now go into the
// appsrc via appsrcWriter (PushBuffer). External byte streams are preserved.
//
// Necessary prerequisite for NetClock binding (Chunk 3): pipeline.UseClock()
// requires programmatic pipeline ownership, which gst-launch cannot provide.
type Pipeline struct {
	cfg    *config.Config
	logger zerolog.Logger

	mu       sync.Mutex
	gst      *gst.Pipeline
	bus      *gst.Bus
	mountID  string
	done     chan struct{} // closed when bus consumer exits (ERROR/EOS/Stop)
	doneOnce sync.Once

	stopRequested bool

	// stdinWriter, if non-nil, wraps the pipeline's "stdin" appsrc and is
	// returned from StartWithDualOutputAndInput so the director can push PCM
	// into the encoder. Closed on Stop to ensure the upstream EOS path runs.
	stdinWriter *appsrcWriter

	// seekFile, if non-nil, is the *os.File whose Fd() was substituted into a
	// "fdsrc fd=5" launch fragment. Kept here so the GC doesn't close it while
	// GStreamer is reading; closed by Stop().
	seekFile *os.File
}

// NewPipeline constructs a pipeline for a mount.
func NewPipeline(cfg *config.Config, mountID string, logger zerolog.Logger) *Pipeline {
	return &Pipeline{cfg: cfg, mountID: mountID, logger: logger}
}

// translateLaunch rewrites fdsink/fdsrc markers in a director-built launch
// string into the appsink/appsrc/fd-substitution form the in-process go-gst
// pipeline understands. Stable element NAMES (hq/lq/stdin) let the Start path
// look up the elements afterwards via GetElementByName.
//
// Pure function so unit tests can exercise the rewrite without spinning up
// GStreamer.
func translateLaunch(launch string, seekFile *os.File) string {
	out := launch
	out = strings.ReplaceAll(out, "fdsink fd=3",
		"appsink name=hq emit-signals=false sync=false max-buffers=10 drop=false")
	out = strings.ReplaceAll(out, "fdsink fd=4",
		"appsink name=lq emit-signals=false sync=false max-buffers=10 drop=false")
	out = strings.ReplaceAll(out, "fdsrc fd=0",
		"appsrc name=stdin format=time is-live=true block=true")
	if seekFile != nil {
		out = strings.ReplaceAll(out, "fdsrc fd=5",
			fmt.Sprintf("fdsrc fd=%d", seekFile.Fd()))
	}
	return out
}

// startLocked constructs the pipeline from `launch`, sets up bus consumer and
// (when present) the hq/lq/stdin element wrappers, and transitions to PLAYING.
// Must be called with p.mu held. The hqHandler/lqHandler callbacks, if not nil,
// receive io.Readers wired to the appsinks and run in their own goroutines —
// matching the previous semantics exactly.
func (p *Pipeline) startLocked(launch string, seekFile *os.File, hqHandler, lqHandler, outputHandler func(io.Reader)) error {
	if p.gst != nil && p.done != nil {
		select {
		case <-p.done:
			// Previous pipeline finished; ok to start a new one. Drop the old.
			p.resetLocked()
		default:
			return fmt.Errorf("pipeline already running")
		}
	}

	translated := translateLaunch(launch, seekFile)
	pipeline, err := gst.NewPipelineFromString(translated)
	if err != nil {
		if seekFile != nil {
			_ = seekFile.Close()
		}
		return fmt.Errorf("parse pipeline: %w", err)
	}

	// Wire up appsinks (HQ/LQ broadcast outputs) if the launch produced them.
	var hqReader, lqReader io.Reader
	if hqElt, _ := pipeline.GetElementByName("hq"); hqElt != nil {
		if sink := app.SinkFromElement(hqElt); sink != nil {
			hqReader = newAppsinkReader(sink)
		}
	}
	if lqElt, _ := pipeline.GetElementByName("lq"); lqElt != nil {
		if sink := app.SinkFromElement(lqElt); sink != nil {
			lqReader = newAppsinkReader(sink)
		}
	}

	// Wire up appsrc (stdin) if the launch produced one.
	var stdinW *appsrcWriter
	if stdinElt, _ := pipeline.GetElementByName("stdin"); stdinElt != nil {
		if src := app.SrcFromElement(stdinElt); src != nil {
			stdinW = newAppsrcWriter(src)
		}
	}

	p.gst = pipeline
	p.bus = pipeline.GetPipelineBus()
	p.done = make(chan struct{})
	p.doneOnce = sync.Once{}
	p.stopRequested = false
	p.seekFile = seekFile
	p.stdinWriter = stdinW

	// Bus consumer drains messages until ERROR/EOS, then closes done. Mirrors
	// the edge encoder's consumeBus pattern (internal/edgeencoder/pipeline.go).
	doneCh := p.done
	go p.consumeBus(doneCh)

	if err := pipeline.SetState(gst.StatePlaying); err != nil {
		// Best-effort teardown; doneCh will close via the bus consumer once
		// SetState(Null) propagates.
		_ = pipeline.SetState(gst.StateNull)
		return fmt.Errorf("set state PLAYING: %w", err)
	}

	// Spawn handler goroutines AFTER state change so the appsink starts to
	// receive samples. If a handler is nil but the appsink exists we still
	// drain it in a background goroutine — otherwise the appsink backpressures
	// (max-buffers=10) and the whole pipeline stalls before reaching EOS.
	// Production callers always pass real handlers; this guard protects tests
	// and any future "no-output" mode.
	if hqReader != nil {
		go runHandler(hqHandler, hqReader)
	}
	if lqReader != nil {
		go runHandler(lqHandler, lqReader)
	}
	if outputHandler != nil {
		// Single-output mode: feed the handler a reader that EOFs when the
		// pipeline finishes. Production callers (api.go EnsurePipeline) pass
		// nil here; this branch exists for the test that wants to observe
		// the handler being invoked.
		go func(done <-chan struct{}) {
			pr, pw := io.Pipe()
			go func() {
				<-done
				_ = pw.Close()
			}()
			outputHandler(pr)
			_ = pr.Close()
		}(p.done)
	}

	return nil
}

// runHandler invokes the caller's handler with the reader (or drains the
// reader into io.Discard when handler is nil). Closes the reader on return.
// Pulled out so nil-handler and real-handler paths share teardown.
func runHandler(handler func(io.Reader), r io.Reader) {
	if handler != nil {
		handler(r)
	} else {
		_, _ = io.Copy(io.Discard, r)
	}
	if c, ok := r.(io.Closer); ok {
		_ = c.Close()
	}
}

// resetLocked clears the per-run state so a previously-finished pipeline can
// be restarted on the same Pipeline instance. Caller must hold p.mu.
func (p *Pipeline) resetLocked() {
	p.gst = nil
	p.bus = nil
	p.done = nil
	p.stdinWriter = nil
	if p.seekFile != nil {
		_ = p.seekFile.Close()
		p.seekFile = nil
	}
}

// consumeBus drains pipeline bus messages until ERROR/EOS/teardown, then
// transitions the pipeline to NULL and closes the per-run done channel.
// Mirrors the edge encoder's pattern; the EOS-wake on Stop() is required so
// this goroutine exits even when there's no upstream EOS in flight.
//
// The SetState(NULL) on exit is important: without it, GStreamer logs
// CRITICAL warnings about elements being disposed while still in PLAYING
// when the *gst.Pipeline is later GC'd. Stop() also calls SetState(NULL)
// but only via Stop's own path; tests (and any caller that just waits on
// Done) need this fallback.
func (p *Pipeline) consumeBus(done chan struct{}) {
	defer func() {
		p.mu.Lock()
		pipeline := p.gst
		p.mu.Unlock()
		if pipeline != nil {
			_ = pipeline.SetState(gst.StateNull)
		}
		p.doneOnce.Do(func() { close(done) })
		p.logExit()
	}()
	for {
		msg := p.bus.TimedPop(gst.ClockTimeNone)
		if msg == nil {
			return
		}
		switch msg.Type() {
		case gst.MessageError:
			gerr := msg.ParseError()
			p.mu.Lock()
			stopReq := p.stopRequested
			p.mu.Unlock()
			if !stopReq {
				p.logger.Warn().
					Err(gerr).
					Str("mount", p.mountID).
					Msg("gstreamer pipeline error")
			}
			return
		case gst.MessageEOS:
			return
		}
	}
}

// logExit emits a single info line when the pipeline finishes. Downgraded
// to INFO when Stop() was the cause (matches the prior subprocess behavior
// of treating signal-induced exits as expected).
func (p *Pipeline) logExit() {
	p.mu.Lock()
	stopReq := p.stopRequested
	mount := p.mountID
	p.mu.Unlock()
	if stopReq {
		p.logger.Info().Str("mount", mount).Msg("gstreamer pipeline stopped (requested)")
	} else {
		p.logger.Info().Str("mount", mount).Msg("gstreamer pipeline stopped")
	}
}

// Start launches the pipeline with the provided launch string.
func (p *Pipeline) Start(ctx context.Context, launch string) error {
	return p.StartWithOutput(ctx, launch, nil)
}

// StartWithOutput launches the pipeline. If outputHandler is non-nil it is
// invoked with an io.Reader that EOFs when the pipeline finishes.
//
// ctx is retained for API compatibility; programmatic go-gst doesn't tie its
// lifecycle to a process exit code, so the ctx parameter is unused inside the
// pipeline itself. Callers that want cancellation should call Stop().
func (p *Pipeline) StartWithOutput(ctx context.Context, launch string, outputHandler func(io.Reader)) error {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.startLocked(launch, nil, nil, nil, outputHandler)
}

// StartWithDualOutput launches a pipeline that produces HQ + LQ encoded
// streams. The handlers receive io.Readers wired to "appsink name=hq" /
// "appsink name=lq" (translated from the historical fdsink fd=3 / fd=4).
// If seekFile is non-nil, its Fd() is substituted into any "fdsrc fd=5"
// fragment in the launch string and the file stays open for the pipeline's
// lifetime.
func (p *Pipeline) StartWithDualOutput(ctx context.Context, launch string, seekFile *os.File, hqHandler, lqHandler func(io.Reader)) error {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.startLocked(launch, seekFile, hqHandler, lqHandler, nil)
}

// StartWithDualOutputAndInput launches a pipeline that accepts PCM input via
// an "appsrc name=stdin" element (translated from fdsrc fd=0) and emits HQ +
// LQ outputs. Returns an io.WriteCloser that pushes bytes as GstBuffers; Close
// signals EOS.
func (p *Pipeline) StartWithDualOutputAndInput(ctx context.Context, launch string, hqHandler, lqHandler func(io.Reader)) (io.WriteCloser, error) {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()

	// Match the prior behavior where a second call while running with a known
	// stdin returns that same stdin instead of erroring.
	if p.gst != nil && p.done != nil {
		select {
		case <-p.done:
			// Previous run already finished; fall through to start a fresh one.
		default:
			if p.stdinWriter != nil {
				return p.stdinWriter, nil
			}
			return nil, fmt.Errorf("pipeline already running")
		}
	}

	if err := p.startLocked(launch, nil, hqHandler, lqHandler, nil); err != nil {
		return nil, err
	}
	if p.stdinWriter == nil {
		return nil, fmt.Errorf("pipeline launch did not include an appsrc named 'stdin' (was 'fdsrc fd=0' present?)")
	}
	return p.stdinWriter, nil
}

// Done returns a channel that is closed when the pipeline exits.
// Returns nil if no pipeline has been started yet.
func (p *Pipeline) Done() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.done
}

// CurrentPID always returns 0 now that pipelines run in-process. The orphan
// reaper (reaper.go) uses this to distinguish tracked broadcast pipelines from
// leaked ones; in-process pipelines have no separate pid to track, so they
// contribute nothing to the OwnedPIDs set. Genuine subprocess orphans (e.g.,
// from crossfade.go's decoder spawning) are still detected and reaped via the
// /proc scan as before.
func (p *Pipeline) CurrentPID() int { return 0 }

// Stop transitions the pipeline to NULL, wakes the bus consumer with a
// synthetic EOS (per the edge encoder Chunk 4 Task 4.2 lesson), and waits for
// the per-run done channel to close.
func (p *Pipeline) Stop() error {
	p.mu.Lock()
	pipeline := p.gst
	bus := p.bus
	done := p.done
	stdinW := p.stdinWriter
	if pipeline != nil && done != nil {
		select {
		case <-done:
			// already exited
		default:
			p.stopRequested = true
		}
	}
	p.mu.Unlock()

	if pipeline == nil || done == nil {
		return nil
	}

	// Already exited.
	select {
	case <-done:
		return nil
	default:
	}

	// Close stdin first so any encoder waiting on more PCM unblocks and the
	// EOS we post below isn't racing the push path.
	if stdinW != nil {
		_ = stdinW.Close()
	}

	// Wake the bus consumer. Without this it would park inside TimedPop until
	// the upstream pipeline itself produced an EOS — which Stop callers can't
	// rely on for arbitrary pipeline shapes.
	if bus != nil {
		bus.Post(gst.NewEOSMessage(pipeline))
	}

	if err := pipeline.SetState(gst.StateNull); err != nil {
		p.logger.Warn().Err(err).Str("mount", p.mountID).Msg("SetState(NULL) returned error")
	}

	<-done
	return nil
}
