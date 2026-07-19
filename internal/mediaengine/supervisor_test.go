/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func newSupervisor() *Supervisor {
	// A nil pipeline manager is safe here: the tests never reach checkPipeline,
	// which is the only method that dereferences it.
	return NewSupervisor(&Config{}, zerolog.Nop(), nil)
}

func TestSupervisor_MonitorUnmonitor(t *testing.T) {
	s := newSupervisor()
	s.MonitorPipeline("st1")
	if _, ok := s.GetHealth("st1"); !ok {
		t.Fatal("expected st1 to be monitored")
	}
	// Monitoring the same station twice is idempotent.
	s.MonitorPipeline("st1")
	if len(s.GetAllHealth()) != 1 {
		t.Errorf("GetAllHealth size = %d, want 1", len(s.GetAllHealth()))
	}

	s.UnmonitorPipeline("st1")
	if _, ok := s.GetHealth("st1"); ok {
		t.Error("expected st1 removed after unmonitor")
	}
}

func TestSupervisor_UpdateHeartbeatResetsFails(t *testing.T) {
	s := newSupervisor()
	s.MonitorPipeline("st1")
	s.monitoredPipelines["st1"].ConsecutiveFails = 5

	s.UpdateHeartbeat("st1")
	if got := s.monitoredPipelines["st1"].ConsecutiveFails; got != 0 {
		t.Errorf("ConsecutiveFails = %d, want 0 after heartbeat", got)
	}

	// Heartbeat for an unmonitored station is a no-op, not a panic.
	s.UpdateHeartbeat("ghost")
}

func TestSupervisor_GetHealthReturnsCopy(t *testing.T) {
	s := newSupervisor()
	if _, ok := s.GetHealth("missing"); ok {
		t.Error("GetHealth should report not-found for unknown station")
	}

	s.MonitorPipeline("st1")
	h, ok := s.GetHealth("st1")
	if !ok {
		t.Fatal("expected health for st1")
	}
	// Mutating the returned copy must not affect the internal record.
	h.RestartCount = 99
	if s.monitoredPipelines["st1"].RestartCount == 99 {
		t.Error("GetHealth returned a live reference, expected a copy")
	}
}

func TestSupervisor_StartStop(t *testing.T) {
	s := newSupervisor()
	s.Start()
	// Stop cancels the context immediately, so the health loop exits via
	// ctx.Done before the 5s ticker ever fires checkPipeline.
	s.Stop()
}

// The next tests use a real (empty) PipelineManager. CreatePipeline builds a
// struct without spawning GStreamer, and Stop/Destroy on a never-started
// pipeline is a no-op, so the check/restart paths run without any process.

func TestSupervisor_CheckPipeline_UnmonitorsWhenMissing(t *testing.T) {
	pm := NewPipelineManager(&Config{}, zerolog.Nop())
	s := NewSupervisor(&Config{}, zerolog.Nop(), pm)
	s.MonitorPipeline("st1") // monitored, but pm has no such pipeline

	s.checkPipeline("st1")

	if _, ok := s.GetHealth("st1"); ok {
		t.Error("checkPipeline should unmonitor a station with no pipeline in the manager")
	}
}

func TestSupervisor_CheckPipeline_HealthyKeepsMonitored(t *testing.T) {
	pm := NewPipelineManager(&Config{}, zerolog.Nop())
	if _, err := pm.CreatePipeline(context.Background(), "st1", "mt1", nil, nil); err != nil {
		t.Fatalf("CreatePipeline() error: %v", err)
	}
	s := NewSupervisor(&Config{}, zerolog.Nop(), pm)
	s.MonitorPipeline("st1") // MonitorPipeline sets a fresh heartbeat

	s.checkPipeline("st1")

	h, ok := s.GetHealth("st1")
	if !ok {
		t.Fatal("healthy pipeline should stay monitored")
	}
	if h.ConsecutiveFails != 0 {
		t.Errorf("ConsecutiveFails = %d, want 0 for a fresh heartbeat", h.ConsecutiveFails)
	}
}

func TestSupervisor_CheckPipeline_RestartsOnHeartbeatTimeout(t *testing.T) {
	pm := NewPipelineManager(&Config{}, zerolog.Nop())
	if _, err := pm.CreatePipeline(context.Background(), "st1", "mt1", nil, nil); err != nil {
		t.Fatalf("CreatePipeline() error: %v", err)
	}
	s := NewSupervisor(&Config{}, zerolog.Nop(), pm)
	s.MonitorPipeline("st1")

	// Force a stale heartbeat and prime the fail counter so this check crosses
	// the restart threshold.
	h := s.monitoredPipelines["st1"]
	h.LastHeartbeat = time.Now().Add(-time.Hour)
	h.ConsecutiveFails = maxConsecutiveFails - 1

	s.checkPipeline("st1")

	// restartPipeline destroys then recreates the pipeline from its saved graph.
	if _, err := pm.GetPipeline("st1"); err != nil {
		t.Errorf("pipeline should be recreated after restart: %v", err)
	}
	if got := s.monitoredPipelines["st1"].RestartCount; got != 1 {
		t.Errorf("RestartCount = %d, want 1 after a restart", got)
	}
}

func TestSupervisor_RestartRateLimited(t *testing.T) {
	// The rate-limit branch returns before touching the pipeline manager, so a
	// nil manager is safe here.
	s := newSupervisor()
	s.MonitorPipeline("st1")
	h := s.monitoredPipelines["st1"]
	h.RestartCount = maxRestartsInWindow
	h.LastRestart = time.Now()

	s.restartPipeline("st1", "test")

	if got := s.monitoredPipelines["st1"].RestartCount; got != maxRestartsInWindow {
		t.Errorf("RestartCount = %d, want %d unchanged (rate limited)", got, maxRestartsInWindow)
	}
}

func TestSupervisor_PerformHealthCheck(t *testing.T) {
	pm := NewPipelineManager(&Config{}, zerolog.Nop())
	if _, err := pm.CreatePipeline(context.Background(), "st1", "mt1", nil, nil); err != nil {
		t.Fatalf("CreatePipeline() error: %v", err)
	}
	s := NewSupervisor(&Config{}, zerolog.Nop(), pm)
	s.MonitorPipeline("st1")

	s.performHealthCheck() // iterates monitored stations and checks each

	if _, ok := s.GetHealth("st1"); !ok {
		t.Error("healthy station should remain monitored after a health check pass")
	}
}
