/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-gst/go-glib/glib"
	"github.com/go-gst/go-gst/gst"
	"github.com/go-gst/go-gst/gst/app"
)

// setEnumProperty sets an enum-typed GObject property to the given int value.
// glib's Object.SetProperty rejects a plain int for enum properties ("invalid
// type gint for property X"), so we have to allocate a GValue of the property's
// actual enum Type and set the enum int directly.
func setEnumProperty(elt *gst.Element, name string, val int) error {
	t, err := elt.GetPropertyType(name)
	if err != nil {
		return err
	}
	gv, err := glib.ValueInit(t)
	if err != nil {
		return err
	}
	gv.SetEnum(val)
	return elt.SetPropertyValue(name, gv)
}

// Pipeline owns the GStreamer pipeline that ingests two RTP inputs, switches
// between them via input-selector, and encodes the active stream to MP3 via
// an appsink that the broadcast adapter reads from.
//
// Pipeline graph (Chunk 4; HLS branch added in Chunk 7):
//
//	udpsrc:A -> rtpjitterbuffer -> rtpL16depay -> audioconvert -\
//	                                                              input-selector -> audioconvert -> tee -> queue -> lamemp3enc -> appsink
//	udpsrc:B -> rtpjitterbuffer -> rtpL16depay -> audioconvert -/
type Pipeline struct {
	cfg *Config

	gst           *gst.Pipeline
	inputSelector *gst.Element
	mp3Appsink    *app.Sink

	// Track which input is active. Mirror of the input-selector's state so
	// callers don't pay the cost of a GStreamer property query per call.
	mu          sync.Mutex
	activeInput string

	// Map from logical input name ("A"/"B") to the request sink pad on
	// input-selector that represents that input.
	inputPads map[string]*gst.Pad

	// bus is the GStreamer bus for the pipeline. Cached here so we don't call
	// GetPipelineBus() from multiple goroutines (go-gst v1.4.0 lazy-initializes
	// the wrapper, which is racy under -race).
	bus *gst.Bus

	// done receives a signal (nil or error) when the bus consumer goroutine
	// exits, either due to a bus ERROR, an EOS, or Close() flushing the bus.
	// Buffered with capacity 1; sends are non-blocking.
	done chan error
}

// NewPipeline constructs and links the GStreamer pipeline. The pipeline is
// NOT started; call Start() after wiring pad probes (Chunk 5).
func NewPipeline(cfg *Config) (*Pipeline, error) {
	pipeline, err := gst.NewPipeline("edge-encoder")
	if err != nil {
		return nil, fmt.Errorf("gst.NewPipeline: %w", err)
	}

	branchA, err := buildInputBranch(pipeline, "a", cfg.RTPPortA)
	if err != nil {
		return nil, fmt.Errorf("input branch A: %w", err)
	}

	branchB, err := buildInputBranch(pipeline, "b", cfg.RTPPortB)
	if err != nil {
		return nil, fmt.Errorf("input branch B: %w", err)
	}

	selector, err := gst.NewElementWithName("input-selector", "input-selector")
	if err != nil {
		return nil, fmt.Errorf("input-selector: %w", err)
	}
	if err := selector.SetProperty("cache-buffers", true); err != nil {
		return nil, fmt.Errorf("set cache-buffers: %w", err)
	}
	if err := selector.SetProperty("sync-streams", true); err != nil {
		return nil, fmt.Errorf("set sync-streams: %w", err)
	}
	if err := pipeline.Add(selector); err != nil {
		return nil, fmt.Errorf("add selector: %w", err)
	}

	padA := selector.GetRequestPad("sink_%u")
	if padA == nil {
		return nil, fmt.Errorf("input-selector: GetRequestPad(sink_%%u) returned nil for A")
	}
	srcA := branchA.GetStaticPad("src")
	if srcA == nil {
		return nil, fmt.Errorf("branch A: GetStaticPad(src) returned nil")
	}
	if ret := srcA.Link(padA); ret != gst.PadLinkOK {
		return nil, fmt.Errorf("link branch A -> input-selector: %s", ret)
	}

	padB := selector.GetRequestPad("sink_%u")
	if padB == nil {
		return nil, fmt.Errorf("input-selector: GetRequestPad(sink_%%u) returned nil for B")
	}
	srcB := branchB.GetStaticPad("src")
	if srcB == nil {
		return nil, fmt.Errorf("branch B: GetStaticPad(src) returned nil")
	}
	if ret := srcB.Link(padB); ret != gst.PadLinkOK {
		return nil, fmt.Errorf("link branch B -> input-selector: %s", ret)
	}

	if err := selector.SetProperty("active-pad", padA); err != nil {
		return nil, fmt.Errorf("set initial active-pad: %w", err)
	}

	// Output side: audioconvert -> tee -> queue -> encoder -> appsink. The
	// tee exists now so Chunk 7 can attach the HLS branch without rebuilding.
	convOut, err := gst.NewElementWithName("audioconvert", "audioconvert_out")
	if err != nil {
		return nil, fmt.Errorf("audioconvert_out: %w", err)
	}
	tee, err := gst.NewElementWithName("tee", "output_tee")
	if err != nil {
		return nil, fmt.Errorf("tee: %w", err)
	}
	mp3Queue, err := gst.NewElementWithName("queue", "mp3_queue")
	if err != nil {
		return nil, fmt.Errorf("mp3_queue: %w", err)
	}
	encoder, err := buildEncoder(cfg)
	if err != nil {
		return nil, fmt.Errorf("encoder: %w", err)
	}
	appsinkElt, err := gst.NewElementWithName("appsink", "mp3sink")
	if err != nil {
		return nil, fmt.Errorf("appsink: %w", err)
	}

	outChain := append([]*gst.Element{convOut, tee, mp3Queue}, encoder...)
	outChain = append(outChain, appsinkElt)
	for _, e := range outChain {
		if err := pipeline.Add(e); err != nil {
			return nil, fmt.Errorf("add %s: %w", e.GetName(), err)
		}
	}

	if err := selector.Link(convOut); err != nil {
		return nil, fmt.Errorf("link selector -> audioconvert_out: %w", err)
	}
	if err := convOut.Link(tee); err != nil {
		return nil, fmt.Errorf("link audioconvert_out -> tee: %w", err)
	}
	if err := tee.Link(mp3Queue); err != nil {
		return nil, fmt.Errorf("link tee -> mp3_queue: %w", err)
	}
	prev := mp3Queue
	for _, e := range append(encoder, appsinkElt) {
		if err := prev.Link(e); err != nil {
			return nil, fmt.Errorf("link %s -> %s: %w", prev.GetName(), e.GetName(), err)
		}
		prev = e
	}

	appsink := app.SinkFromElement(appsinkElt)
	if appsink == nil {
		return nil, fmt.Errorf("appsink element is not an appsink")
	}
	if err := appsinkElt.SetProperty("emit-signals", false); err != nil {
		return nil, fmt.Errorf("appsink emit-signals: %w", err)
	}
	if err := appsinkElt.SetProperty("sync", false); err != nil {
		return nil, fmt.Errorf("appsink sync: %w", err)
	}
	appsink.SetMaxBuffers(10)
	appsink.SetDrop(false)

	if cfg.HLSEnabled {
		// Ensure the segment dir exists; hlssink2 won't create parents itself
		// and will go ERROR at state change if the path is missing.
		if err := os.MkdirAll(cfg.HLSSegmentDir, 0o755); err != nil {
			return nil, fmt.Errorf("hls segment dir: %w", err)
		}

		hlsQueue, err := gst.NewElementWithName("queue", "hls_queue")
		if err != nil {
			return nil, fmt.Errorf("hls queue: %w", err)
		}
		if err := pipeline.Add(hlsQueue); err != nil {
			return nil, fmt.Errorf("add hls queue: %w", err)
		}

		// hlssink2 muxes to MPEG-TS internally; it requires encoded audio on
		// its sink pad. Encode AAC for HLS (industry standard for the format)
		// independently of the MP3 branch's encoder.
		hlsEnc, err := gst.NewElementWithName("avenc_aac", "hls_aac_encoder")
		if err != nil {
			return nil, fmt.Errorf("hls avenc_aac: %w", err)
		}
		if err := hlsEnc.SetProperty("bitrate", cfg.OutputBitrateKbps*1000); err != nil {
			return nil, fmt.Errorf("hls aac bitrate: %w", err)
		}
		if err := pipeline.Add(hlsEnc); err != nil {
			return nil, fmt.Errorf("add hls aac encoder: %w", err)
		}
		hlsParse, err := gst.NewElementWithName("aacparse", "hls_aac_parser")
		if err != nil {
			return nil, fmt.Errorf("hls aacparse: %w", err)
		}
		if err := pipeline.Add(hlsParse); err != nil {
			return nil, fmt.Errorf("add hls aac parser: %w", err)
		}

		hlssink, err := gst.NewElementWithName("hlssink2", "hlssink")
		if err != nil {
			return nil, fmt.Errorf("hlssink2: %w", err)
		}
		if err := hlssink.SetProperty("location", filepath.Join(cfg.HLSSegmentDir, "segment%05d.ts")); err != nil {
			return nil, fmt.Errorf("hlssink2 location: %w", err)
		}
		if err := hlssink.SetProperty("playlist-location", filepath.Join(cfg.HLSSegmentDir, "playlist.m3u8")); err != nil {
			return nil, fmt.Errorf("hlssink2 playlist-location: %w", err)
		}
		if err := hlssink.SetProperty("target-duration", uint(4)); err != nil {
			return nil, fmt.Errorf("hlssink2 target-duration: %w", err)
		}
		if err := hlssink.SetProperty("max-files", uint(10)); err != nil {
			return nil, fmt.Errorf("hlssink2 max-files: %w", err)
		}
		if err := pipeline.Add(hlssink); err != nil {
			return nil, fmt.Errorf("add hlssink: %w", err)
		}

		// Request a second src pad on the output tee and link it through
		// hls_queue to hlssink's "audio" request pad. hlssink2 muxes audio
		// (and optional video) into MPEG-TS segments internally.
		teeSrcHLS := tee.GetRequestPad("src_%u")
		if teeSrcHLS == nil {
			return nil, fmt.Errorf("request src_%%u from output_tee for HLS failed")
		}
		if lr := teeSrcHLS.Link(hlsQueue.GetStaticPad("sink")); lr != gst.PadLinkOK {
			return nil, fmt.Errorf("link tee -> hls_queue failed: %v", lr)
		}
		if err := gst.ElementLinkMany(hlsQueue, hlsEnc, hlsParse); err != nil {
			return nil, fmt.Errorf("link hls_queue -> aac encoder chain: %w", err)
		}
		hlsAudioSink := hlssink.GetRequestPad("audio")
		if hlsAudioSink == nil {
			return nil, fmt.Errorf("request audio pad from hlssink2 failed")
		}
		if lr := hlsParse.GetStaticPad("src").Link(hlsAudioSink); lr != gst.PadLinkOK {
			return nil, fmt.Errorf("link aacparse -> hlssink failed: %v", lr)
		}
	}

	p := &Pipeline{
		cfg:           cfg,
		gst:           pipeline,
		inputSelector: selector,
		mp3Appsink:    appsink,
		activeInput:   "A",
		inputPads: map[string]*gst.Pad{
			"A": padA,
			"B": padB,
		},
		bus:  pipeline.GetPipelineBus(),
		done: make(chan error, 1),
	}
	go p.consumeBus()
	return p, nil
}

// buildInputBranch creates udpsrc -> rtpjitterbuffer -> rtpL16depay ->
// audioconvert and returns the last element (whose static src pad links into
// the input-selector).
func buildInputBranch(pipe *gst.Pipeline, suffix string, port int) (*gst.Element, error) {
	udpsrc, err := gst.NewElementWithName("udpsrc", "udpsrc_"+suffix)
	if err != nil {
		return nil, fmt.Errorf("udpsrc: %w", err)
	}
	if err := udpsrc.SetProperty("port", port); err != nil {
		return nil, fmt.Errorf("udpsrc port: %w", err)
	}
	caps := gst.NewCapsFromString(
		"application/x-rtp,media=audio,clock-rate=44100,encoding-name=L16,payload=10,channels=2")
	if err := udpsrc.SetProperty("caps", caps); err != nil {
		return nil, fmt.Errorf("udpsrc caps: %w", err)
	}

	jb, err := gst.NewElementWithName("rtpjitterbuffer", "jitter_"+suffix)
	if err != nil {
		return nil, fmt.Errorf("rtpjitterbuffer: %w", err)
	}
	if err := jb.SetProperty("latency", uint(80)); err != nil {
		return nil, fmt.Errorf("rtpjitterbuffer latency: %w", err)
	}

	depay, err := gst.NewElementWithName("rtpL16depay", "depay_"+suffix)
	if err != nil {
		return nil, fmt.Errorf("rtpL16depay: %w", err)
	}

	conv, err := gst.NewElementWithName("audioconvert", "audioconvert_"+suffix)
	if err != nil {
		return nil, fmt.Errorf("audioconvert: %w", err)
	}

	for _, e := range []*gst.Element{udpsrc, jb, depay, conv} {
		if err := pipe.Add(e); err != nil {
			return nil, fmt.Errorf("add %s: %w", e.GetName(), err)
		}
	}
	if err := gst.ElementLinkMany(udpsrc, jb, depay, conv); err != nil {
		return nil, fmt.Errorf("link input branch %s: %w", suffix, err)
	}
	return conv, nil
}

// buildEncoder returns the chain of encoder elements (excluding the final
// appsink). The caller adds them to the pipeline and links them.
func buildEncoder(cfg *Config) ([]*gst.Element, error) {
	switch cfg.OutputFormat {
	case "mp3":
		enc, err := gst.NewElementWithName("lamemp3enc", "mp3_encoder")
		if err != nil {
			return nil, fmt.Errorf("lamemp3enc: %w", err)
		}
		if err := setEnumProperty(enc, "target", 1); err != nil { // 1 = bitrate mode
			return nil, fmt.Errorf("lamemp3enc target: %w", err)
		}
		if err := enc.SetProperty("bitrate", cfg.OutputBitrateKbps); err != nil {
			return nil, fmt.Errorf("lamemp3enc bitrate: %w", err)
		}
		if err := enc.SetProperty("cbr", true); err != nil {
			return nil, fmt.Errorf("lamemp3enc cbr: %w", err)
		}
		return []*gst.Element{enc}, nil
	case "aac":
		enc, err := gst.NewElementWithName("avenc_aac", "aac_encoder")
		if err != nil {
			return nil, fmt.Errorf("avenc_aac: %w", err)
		}
		if err := enc.SetProperty("bitrate", cfg.OutputBitrateKbps*1000); err != nil {
			return nil, fmt.Errorf("avenc_aac bitrate: %w", err)
		}
		return []*gst.Element{enc}, nil
	default:
		return nil, fmt.Errorf("unsupported output format %q", cfg.OutputFormat)
	}
}

// Start transitions the pipeline to PLAYING. The state change is asynchronous;
// returning here does not guarantee the pipeline has reached PLAYING.
func (p *Pipeline) Start() error {
	if err := p.gst.SetState(gst.StatePlaying); err != nil {
		return fmt.Errorf("set state to PLAYING: %w", err)
	}
	return nil
}

// Close stops the pipeline and releases all resources. Before transitioning
// to NULL, a synthetic EOS message is posted to wake the blocked consumeBus
// goroutine so it exits cleanly (no goroutine leak).
func (p *Pipeline) Close() error {
	if p.bus != nil {
		p.bus.Post(gst.NewEOSMessage(p.gst))
	}
	return p.gst.SetState(gst.StateNull)
}

// consumeBus pumps GStreamer bus messages on a dedicated goroutine.
// On ERROR or EOS, the message is surfaced via the done channel and the
// goroutine exits. The done channel is buffered, so the send is non-blocking;
// late listeners won't miss the first signal.
func (p *Pipeline) consumeBus() {
	for {
		msg := p.bus.TimedPop(gst.ClockTimeNone)
		if msg == nil {
			// Bus was flushed (e.g., by Close) or otherwise returned no
			// message. Treat as clean shutdown.
			select {
			case p.done <- nil:
			default:
			}
			return
		}
		switch msg.Type() {
		case gst.MessageError:
			gerr := msg.ParseError()
			var err error
			if gerr != nil {
				err = gerr
			}
			select {
			case p.done <- err:
			default:
			}
			return
		case gst.MessageEOS:
			select {
			case p.done <- nil:
			default:
			}
			return
		}
	}
}

// Done returns a channel that receives a value (or nil) when the pipeline
// stops, either due to a bus ERROR, an EOS, or Close(). Receivers should
// treat any value (nil or error) as "pipeline stopped, do not use further".
func (p *Pipeline) Done() <-chan error {
	return p.done
}

// ActiveInput returns the logical name ("A" or "B") of the currently selected
// input.
func (p *Pipeline) ActiveInput() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.activeInput
}

// SetActiveInput switches the input-selector's active pad. input-selector
// performs the switch at a sample boundary (sync-streams=true), so no audible
// click. Safe to call from any goroutine.
func (p *Pipeline) SetActiveInput(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	pad, ok := p.inputPads[name]
	if !ok {
		return fmt.Errorf("unknown input %q (want A or B)", name)
	}
	if err := p.inputSelector.SetProperty("active-pad", pad); err != nil {
		return fmt.Errorf("set active-pad: %w", err)
	}
	p.activeInput = name
	return nil
}

// MP3Appsink returns the appsink that broadcast.go (Chunk 6) reads encoded
// MP3 bytes from.
func (p *Pipeline) MP3Appsink() *app.Sink {
	return p.mp3Appsink
}

// InputPad returns the input-selector's sink pad for the given logical input
// name ("A" or "B"), or nil for an unknown name. Used by health.go (Chunk 5)
// to attach pad probes for packet-arrival monitoring.
func (p *Pipeline) InputPad(name string) *gst.Pad {
	return p.inputPads[name]
}

// AttachHealthProbes installs pad probes on both input branches; the probes
// call RecordPacket() on the corresponding InputHealth. Should be called
// before Start() so probes are in place by the time data flows.
func (p *Pipeline) AttachHealthProbes(a, b *InputHealth) {
	a.AttachPadProbe(p.inputPads["A"])
	b.AttachPadProbe(p.inputPads["B"])
}
