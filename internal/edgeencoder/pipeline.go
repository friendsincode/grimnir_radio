/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"fmt"
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

	return &Pipeline{
		cfg:           cfg,
		gst:           pipeline,
		inputSelector: selector,
		mp3Appsink:    appsink,
		activeInput:   "A",
		inputPads: map[string]*gst.Pad{
			"A": padA,
			"B": padB,
		},
	}, nil
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

// Close stops the pipeline and releases all resources.
func (p *Pipeline) Close() error {
	return p.gst.SetState(gst.StateNull)
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
