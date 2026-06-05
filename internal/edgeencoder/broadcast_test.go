/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"io"
	"testing"
	"time"

	"github.com/go-gst/go-gst/gst"
	"github.com/go-gst/go-gst/gst/app"
)

func TestAppsinkReader_ReadsBytes(t *testing.T) {
	Init()
	// Build a small in-process pipeline: audiotestsrc num-buffers=10 -> lamemp3enc -> appsink
	pipeline, err := gst.NewPipelineFromString("audiotestsrc num-buffers=10 ! audioconvert ! lamemp3enc ! appsink name=sink")
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.SetState(gst.StateNull)

	sinkElt, err := pipeline.GetElementByName("sink")
	if err != nil || sinkElt == nil {
		t.Fatalf("GetElementByName(sink): %v", err)
	}
	sink := app.SinkFromElement(sinkElt)
	if sink == nil {
		t.Fatal("SinkFromElement returned nil")
	}

	if err := pipeline.SetState(gst.StatePlaying); err != nil {
		t.Fatal(err)
	}

	r := NewAppsinkReader(sink)
	buf := make([]byte, 4096)
	deadline := time.Now().Add(3 * time.Second)
	total := 0
	for total < 100 && time.Now().Before(deadline) {
		n, err := r.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read after %d bytes: %v", total, err)
		}
		total += n
	}
	if total == 0 {
		t.Error("Read produced 0 bytes; want some encoded MP3 output")
	} else {
		t.Logf("Read %d bytes total", total)
	}
}

func TestAppsinkReader_CloseReturnsEOF(t *testing.T) {
	Init()
	pipeline, err := gst.NewPipelineFromString("audiotestsrc num-buffers=2 ! audioconvert ! lamemp3enc ! appsink name=sink")
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.SetState(gst.StateNull)
	sinkElt, _ := pipeline.GetElementByName("sink")
	sink := app.SinkFromElement(sinkElt)
	pipeline.SetState(gst.StatePlaying)

	r := NewAppsinkReader(sink)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	_, err = r.Read(buf)
	if err != io.EOF {
		t.Errorf("Read after Close: err = %v, want io.EOF", err)
	}
}
