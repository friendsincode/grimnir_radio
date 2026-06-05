/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"testing"
	"time"

	"github.com/go-gst/go-gst/gst"
)

// TestGoGstCanParseDualBroadcastPipeline confirms gst.NewPipelineFromString
// can parse the kind of pipeline string director.go produces. Prerequisite
// for Chunk 1 Task 1.2 (migrating Pipeline.Start to programmatic go-gst).
// Pipeline strings stay unchanged in director.go; only the spawning layer
// flips from exec.Command(gst-launch-1.0) to go-gst.
func TestGoGstCanParseDualBroadcastPipeline(t *testing.T) {
	gst.Init(nil)
	// Trimmed version of buildDualBroadcastPipeline's shape with fakesinks
	// (real version uses fdsink fd=3/4; Task 1.2 will translate those to
	// appsinks at the construction layer).
	pipelineStr := "audiotestsrc num-buffers=20 ! audioconvert ! audioresample ! " +
		"audio/x-raw,rate=44100,channels=2 ! tee name=t " +
		"t. ! queue ! lamemp3enc target=1 bitrate=128 cbr=true ! fakesink sync=true " +
		"t. ! queue ! lamemp3enc target=1 bitrate=64 cbr=true  ! fakesink sync=true"

	pipeline, err := gst.NewPipelineFromString(pipelineStr)
	if err != nil {
		t.Fatalf("NewPipelineFromString: %v", err)
	}
	defer pipeline.SetState(gst.StateNull)

	if err := pipeline.SetState(gst.StatePlaying); err != nil {
		t.Fatalf("SetState(Playing): %v", err)
	}

	bus := pipeline.GetPipelineBus()
	deadline := time.Now().Add(3 * time.Second)
	gotEOS := false
	for time.Now().Before(deadline) && !gotEOS {
		msg := bus.TimedPop(gst.ClockTime(50 * time.Millisecond.Nanoseconds()))
		if msg == nil {
			continue
		}
		switch msg.Type() {
		case gst.MessageError:
			t.Fatalf("pipeline error: %v", msg.ParseError())
		case gst.MessageEOS:
			gotEOS = true
		}
	}
	if !gotEOS {
		t.Errorf("pipeline did not reach EOS within 3s (num-buffers=20 should finish in <1s)")
	}
}

// TestGoGstCanParseWebstreamShape sanity-checks a webstream-shaped pipeline
// string (souphttpsrc → ... → tee → MP3+RTP) parses cleanly. Doesn't
// actually run it (souphttpsrc needs a real URL); just verifies parse.
func TestGoGstCanParseWebstreamShape(t *testing.T) {
	gst.Init(nil)
	pipelineStr := "souphttpsrc location=https://example.org/stream ! " +
		"watchdog timeout=15000 ! decodebin ! audioconvert ! audioresample ! " +
		"audio/x-raw,rate=48000,channels=2 ! tee name=t " +
		"t. ! queue ! lamemp3enc bitrate=128 ! fakesink sync=true " +
		"t. ! queue ! lamemp3enc bitrate=64 ! fakesink sync=true"

	pipeline, err := gst.NewPipelineFromString(pipelineStr)
	if err != nil {
		t.Fatalf("NewPipelineFromString: %v", err)
	}
	defer pipeline.SetState(gst.StateNull)

	// Don't try to play it; souphttpsrc would block on a real network call.
	// Just confirm the graph constructed.
	if pipeline == nil {
		t.Fatal("pipeline nil after successful parse")
	}
}
