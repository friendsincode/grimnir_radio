/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-gst/go-gst/gst"
	"github.com/go-gst/go-gst/gst/app"
)

// PipelineConfig is the input to NewPipeline. Engines is the slice of media
// engine PCM-RTP destinations (host:port); the pipeline targets all of them
// via multiudpsink, so a single PushBuffer into the appsrc reaches every
// engine simultaneously.
//
// Clock is the optional NetClock-bound *gst.Clock; pass the value returned by
// gstnet.NewNetClientClock when FANOUT_NETCLOCK_ENABLED is true. Nil means
// the pipeline uses the default GStreamer system clock.
//
// SourceLaunch is the GStreamer launch fragment that produces decoded PCM
// matching audio/x-raw,format=S16LE,rate=48000,channels=2 on its src pad.
// In Chunks 3-6 the protocol terminators substitute this with
// "appsrc name=audio_in ... ! audioconvert ! audioresample" (and the bytes
// come in via PushBuffer); tests pass "audiotestsrc num-buffers=10" so the
// pipeline produces sample data without a network terminator.
type PipelineConfig struct {
	Engines      []string
	Clock        *gst.Clock
	SourceLaunch string
}

// Pipeline is the per-session GStreamer pipeline. It owns the appsrc that the
// protocol terminator pushes PCM into, the rtpL16pay element that packetizes
// to PCM-over-RTP, and the multiudpsink that fans out to every engine.
//
// The pipeline shape, when SourceLaunch is the default appsrc fragment:
//
//	appsrc name=audio_in
//	  ! audioconvert
//	  ! audioresample
//	  ! audio/x-raw,format=S16BE,rate=44100,channels=2
//	  ! rtpL16pay pt=10 mtu=1400
//	  ! multiudpsink clients=engineA:port,engineB:port
//
// S16BE + 44100 + channels=2 matches the udpsrc caps the edge encoder &
// media engines already speak (internal/edgeencoder/pipeline.go:346).
type Pipeline struct {
	cfg PipelineConfig

	gst    *gst.Pipeline
	appsrc *app.Source
	bus    *gst.Bus

	mu        sync.Mutex
	started   bool
	busActive bool // true once consumeBus goroutine is running
	stopped   bool
	doneCh    chan struct{}
	doneOnce  sync.Once
}

// NewPipeline builds the per-session pipeline from cfg. Does NOT call
// SetState(PLAYING); call Start() after construction. Returns an error if the
// launch string fails to parse or required elements are missing.
func NewPipeline(cfg PipelineConfig) (*Pipeline, error) {
	if len(cfg.Engines) == 0 {
		return nil, fmt.Errorf("pipeline: at least one engine target required")
	}
	if cfg.SourceLaunch == "" {
		cfg.SourceLaunch = "appsrc name=audio_in format=time is-live=true block=true " +
			"caps=audio/x-raw,format=S16LE,rate=48000,channels=2"
	}

	clients := strings.Join(cfg.Engines, ",")
	launch := fmt.Sprintf(
		"%s ! audioconvert ! audioresample "+
			"! audio/x-raw,format=S16BE,rate=44100,channels=2 "+
			"! rtpL16pay pt=10 mtu=1400 "+
			"! multiudpsink name=fanout_sink clients=%s sync=false async=false",
		cfg.SourceLaunch, clients,
	)

	pipeline, err := gst.NewPipelineFromString(launch)
	if err != nil {
		return nil, fmt.Errorf("parse pipeline: %w", err)
	}

	var src *app.Source
	if elt, _ := pipeline.GetElementByName("audio_in"); elt != nil {
		src = app.SrcFromElement(elt)
	}

	p := &Pipeline{
		cfg:    cfg,
		gst:    pipeline,
		appsrc: src,
		bus:    pipeline.GetPipelineBus(),
		doneCh: make(chan struct{}),
	}
	return p, nil
}

// Start transitions the pipeline to PLAYING. Binds the NetClock first when
// cfg.Clock is non-nil — ordering matters; ForceClock must precede the state
// change so the clock is in use the moment the first sample flows.
//
// Asynchronous: returning does not guarantee PLAYING has been reached. Use
// Done() to wait for an EOS/error.
func (p *Pipeline) Start() error {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return fmt.Errorf("pipeline already started")
	}
	p.started = true
	doneCh := p.doneCh
	p.mu.Unlock()

	if p.cfg.Clock != nil {
		p.gst.ForceClock(p.cfg.Clock)
	}
	if err := p.gst.SetState(gst.StatePlaying); err != nil {
		_ = p.gst.SetState(gst.StateNull)
		return fmt.Errorf("set state PLAYING: %w", err)
	}
	p.mu.Lock()
	p.busActive = true
	p.mu.Unlock()
	go p.consumeBus(doneCh)
	return nil
}

// PushPCM pushes a chunk of PCM (matching the source caps: S16LE/48kHz/stereo
// by default) into the appsrc. Returns an error when the appsrc is absent
// (e.g. the SourceLaunch didn't include appsrc name=audio_in) or when the
// PushBuffer flow return is non-OK.
//
// Used by the protocol terminators in Chunks 3-6 once they've decoded a frame.
func (p *Pipeline) PushPCM(pcm []byte) error {
	if p.appsrc == nil {
		return fmt.Errorf("pipeline has no appsrc named audio_in")
	}
	if len(pcm) == 0 {
		return nil
	}
	buf := gst.NewBufferFromBytes(pcm)
	if buf == nil {
		return fmt.Errorf("gst.NewBufferFromBytes returned nil")
	}
	ret := p.appsrc.PushBuffer(buf)
	switch ret {
	case gst.FlowOK, gst.FlowFlushing, gst.FlowEOS:
		return nil
	default:
		return fmt.Errorf("appsrc push: %s", ret.String())
	}
}

// Stop transitions the pipeline to NULL and unblocks any consumer waiting on
// Done(). Idempotent & safe to call concurrently with the bus-consumer
// goroutine: we drive the bus to EOS, wait for consumeBus to exit, & only
// then call SetState(NULL). Tearing down the pipeline while consumeBus is
// blocked inside gst_bus_timed_pop has caused intermittent CGo SIGSEGVs in
// gst_element_set_state under -race.
func (p *Pipeline) Stop() error {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return nil
	}
	p.stopped = true
	doneCh := p.doneCh
	busActive := p.busActive
	p.mu.Unlock()

	if p.appsrc != nil {
		p.appsrc.EndStream()
	}
	if p.bus != nil {
		// Posting EOS wakes consumeBus's TimedPop so it exits & closes
		// doneCh. The defer in consumeBus runs doneOnce.Do(close(doneCh)).
		p.bus.Post(gst.NewEOSMessage(p.gst))
	}

	// Only wait for the bus goroutine if it was actually launched (Start
	// completed successfully). Belt-&-braces: 2s timeout so a misbehaving
	// bus can't deadlock Stop.
	if busActive {
		select {
		case <-doneCh:
		case <-time.After(2 * time.Second):
		}
	} else {
		// consumeBus was never launched; nobody will close doneCh. Close
		// it here so callers waiting on Done() unblock.
		p.doneOnce.Do(func() { close(doneCh) })
	}

	return p.gst.SetState(gst.StateNull)
}

// Done returns a channel that closes when the pipeline exits (EOS, error, or
// explicit Stop). Receive-only; never gets a value, only closes.
func (p *Pipeline) Done() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.doneCh
}

// consumeBus drains GStreamer bus messages until EOS or ERROR, then closes
// doneCh. Mirrors the edge-encoder consumeBus contract (one-shot close on
// pipeline termination).
func (p *Pipeline) consumeBus(doneCh chan struct{}) {
	defer p.doneOnce.Do(func() { close(doneCh) })
	for {
		msg := p.bus.TimedPop(gst.ClockTimeNone)
		if msg == nil {
			return
		}
		switch msg.Type() {
		case gst.MessageError, gst.MessageEOS:
			return
		}
	}
}
