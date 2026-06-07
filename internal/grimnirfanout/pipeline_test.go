/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"
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

// TestPipeline_ConcurrentStopRace exercises the shutdown path under -race.
// Several goroutines call Stop() & Done() concurrently while the consumeBus
// goroutine is still draining bus messages. Before the Stop-waits-for-bus
// fix this triggered a CGo SIGSEGV in gst_element_set_state ~once per few
// dozen iterations.
func TestPipeline_ConcurrentStopRace(t *testing.T) {
	gstInit()
	const iterations = 50
	for i := 0; i < iterations; i++ {
		p, err := NewPipeline(PipelineConfig{
			Engines:      []string{"127.0.0.1:65000"},
			SourceLaunch: "audiotestsrc is-live=true samplesperbuffer=480",
		})
		if err != nil {
			t.Fatalf("iter %d NewPipeline: %v", i, err)
		}
		if err := p.Start(); err != nil {
			t.Fatalf("iter %d Start: %v", i, err)
		}

		// Fire several concurrent Stop()s plus a Done() reader to mimic the
		// real shutdown shape: SRTListener.Serve has one Stop in the
		// select-case, but the bus goroutine, ctx cancel, and a test defer
		// can all race to call Stop or read Done.
		var wg sync.WaitGroup
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = p.Stop()
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-p.Done():
			case <-time.After(2 * time.Second):
			}
		}()
		wg.Wait()
	}
}

// TestSRT_ServeLifecycleStress runs the full SRTListener.Serve lifecycle
// (Create session, build + start pipeline, cancel, Stop, Remove session)
// many times in a row to flush out CGo SIGSEGVs in the shutdown path.
// Pre-fix this could SEGV intermittently inside gst_element_set_state.
func TestSRT_ServeLifecycleStress(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test; use -short to skip")
	}
	gstInit()
	const iterations = 25
	for i := 0; i < iterations; i++ {
		mgr := NewSessionMgr()
		lis, err := NewSRTListener(SRTListenerConfig{
			BindAddr:             "127.0.0.1",
			Port:                 0,
			Engines:              []string{"127.0.0.1:65000"},
			Sessions:             mgr,
			SourceLaunchOverride: "audiotestsrc is-live=true samplesperbuffer=480",
		})
		if err != nil {
			t.Fatalf("iter %d NewSRTListener: %v", i, err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- lis.Serve(ctx) }()

		// Let the pipeline actually reach PLAYING before yanking it.
		time.Sleep(40 * time.Millisecond)
		cancel()

		select {
		case err := <-done:
			if err != nil && err != context.Canceled {
				t.Fatalf("iter %d Serve: %v", i, err)
			}
		case <-time.After(4 * time.Second):
			t.Fatalf("iter %d Serve did not return", i)
		}
		if n := mgr.CountByProtocol(ProtocolSRT); n != 0 {
			t.Fatalf("iter %d dangling sessions: %d", i, n)
		}
	}
}
