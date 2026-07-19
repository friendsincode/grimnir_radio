/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"testing"

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
