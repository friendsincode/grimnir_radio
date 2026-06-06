/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-gst/go-gst/gst"
)

var initOnce sync.Once

func gstInit() {
	initOnce.Do(func() { gst.Init(nil) })
}

func TestNewPipeline_RequiresEngines(t *testing.T) {
	gstInit()
	_, err := NewPipeline(PipelineConfig{Engines: nil})
	if err == nil {
		t.Fatal("NewPipeline with no engines: want error, got nil")
	}
	if !strings.Contains(err.Error(), "engine") {
		t.Errorf("error %q should mention engine", err.Error())
	}
}

func TestNewPipeline_DefaultLaunchIncludesFanout(t *testing.T) {
	gstInit()
	// Use a black-hole port so multiudpsink discards silently.
	p, err := NewPipeline(PipelineConfig{
		Engines: []string{"127.0.0.1:65000", "127.0.0.1:65001"},
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	defer p.Stop()

	// Pipeline should have parsed; appsrc must be present in the default
	// launch so PushPCM works.
	if p.appsrc == nil {
		t.Fatal("default pipeline has no audio_in appsrc")
	}
	// And the multiudpsink (named in the launch) should exist.
	if sink, _ := p.gst.GetElementByName("fanout_sink"); sink == nil {
		t.Error("multiudpsink fanout_sink missing from pipeline")
	}
}

func TestPipeline_StartReachesPlayingWithAudiotestsrc(t *testing.T) {
	gstInit()
	// Override the source with audiotestsrc so we don't need to push PCM
	// from the test — the source generates samples on its own. num-buffers
	// keeps the pipeline finite so the test can wait for EOS.
	p, err := NewPipeline(PipelineConfig{
		Engines:      []string{"127.0.0.1:65000"},
		SourceLaunch: "audiotestsrc num-buffers=5 samplesperbuffer=480",
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	defer p.Stop()

	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// audiotestsrc with num-buffers=5 will EOS quickly; wait up to 5s.
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline did not reach Done within 5s")
	}
}

func TestPipeline_StartTwiceErrors(t *testing.T) {
	gstInit()
	p, err := NewPipeline(PipelineConfig{
		Engines:      []string{"127.0.0.1:65000"},
		SourceLaunch: "audiotestsrc num-buffers=100",
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	defer p.Stop()

	if err := p.Start(); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := p.Start(); err == nil {
		t.Error("second Start: want error, got nil")
	}
}

func TestPipeline_PushPCMRequiresAppsrc(t *testing.T) {
	gstInit()
	// audiotestsrc-only pipeline has no appsrc, so PushPCM must fail.
	p, err := NewPipeline(PipelineConfig{
		Engines:      []string{"127.0.0.1:65000"},
		SourceLaunch: "audiotestsrc num-buffers=1",
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	defer p.Stop()

	err = p.PushPCM([]byte{0x00, 0x00, 0x00, 0x00})
	if err == nil {
		t.Error("PushPCM with no appsrc: want error, got nil")
	}
}

func TestPipeline_PushPCMAcceptsBytes(t *testing.T) {
	gstInit()
	// Default SourceLaunch includes appsrc; push a small PCM chunk and
	// confirm no error. The sink target is a black-hole port; multiudpsink
	// drops silently.
	p, err := NewPipeline(PipelineConfig{
		Engines: []string{"127.0.0.1:65000"},
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	defer p.Stop()
	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// 480 samples * 2 channels * 2 bytes/sample = 1920 bytes ~= 10ms at 48kHz.
	chunk := make([]byte, 1920)
	if err := p.PushPCM(chunk); err != nil {
		t.Errorf("PushPCM: %v", err)
	}
	// Push zero bytes is a no-op.
	if err := p.PushPCM(nil); err != nil {
		t.Errorf("PushPCM(nil): %v", err)
	}
}

func TestPipeline_StopIsIdempotent(t *testing.T) {
	gstInit()
	p, err := NewPipeline(PipelineConfig{
		Engines:      []string{"127.0.0.1:65000"},
		SourceLaunch: "audiotestsrc num-buffers=100",
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := p.Stop(); err != nil {
		t.Errorf("first Stop: %v", err)
	}
	if err := p.Stop(); err != nil {
		t.Errorf("second Stop: %v", err)
	}
}
