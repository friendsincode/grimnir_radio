/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"sync"
	"testing"
	"time"
)

func TestSession_NewSessionDefaults(t *testing.T) {
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	s := newSessionWithDeps("sess-1", ProtocolHarbor, now)

	if s.ID != "sess-1" {
		t.Errorf("ID = %q, want sess-1", s.ID)
	}
	if s.Protocol != ProtocolHarbor {
		t.Errorf("Protocol = %v, want ProtocolHarbor", s.Protocol)
	}
	if !s.StartedAt.Equal(now) {
		t.Errorf("StartedAt = %v, want %v", s.StartedAt, now)
	}
	if got := s.State(); got != SessionIdle {
		t.Errorf("initial State = %v, want SessionIdle", got)
	}
}

func TestSession_StateTransitions(t *testing.T) {
	s := newSessionWithDeps("sess-2", ProtocolRTP, time.Now())

	if err := s.transitionTo(SessionAuthenticating); err != nil {
		t.Fatalf("Idle -> Authenticating: %v", err)
	}
	if err := s.transitionTo(SessionActive); err != nil {
		t.Fatalf("Authenticating -> Active: %v", err)
	}
	if err := s.transitionTo(SessionEnded); err != nil {
		t.Fatalf("Active -> Ended: %v", err)
	}
	// No transitions out of Ended.
	if err := s.transitionTo(SessionActive); err == nil {
		t.Error("transition out of Ended should fail")
	}
}

func TestSession_TelemetryAccumulates(t *testing.T) {
	s := newSessionWithDeps("sess-3", ProtocolHarbor, time.Now())
	s.recordBytesIn(1024)
	s.recordBytesIn(2048)
	s.markPacket(time.Date(2026, 6, 5, 12, 0, 1, 0, time.UTC))

	tel := s.Telemetry()
	if tel.BytesIn != 3072 {
		t.Errorf("BytesIn = %d, want 3072", tel.BytesIn)
	}
	if tel.LastPacketAt.IsZero() {
		t.Error("LastPacketAt is zero, want set")
	}
}

func TestSession_AttachPipeline(t *testing.T) {
	gstInit()
	p, err := NewPipeline(PipelineConfig{Engines: []string{"127.0.0.1:65000"}})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	defer p.Stop()
	s := newSessionWithDeps("sess-pipe", ProtocolHarbor, time.Now())
	s.AttachPipeline(p)
	if s.Pipeline != p {
		t.Errorf("Session.Pipeline = %v, want %v", s.Pipeline, p)
	}
}

func TestSessionMgr_AddGetRemove(t *testing.T) {
	mgr := NewSessionMgr()
	if mgr.Count() != 0 {
		t.Fatalf("empty Count = %d, want 0", mgr.Count())
	}
	s := newSessionWithDeps("a", ProtocolHarbor, time.Now())
	mgr.Add(s)
	if mgr.Count() != 1 {
		t.Errorf("Count after Add = %d, want 1", mgr.Count())
	}
	got, ok := mgr.Get("a")
	if !ok || got != s {
		t.Errorf("Get(a) = %v, %v; want session, true", got, ok)
	}
	mgr.Remove("a")
	if mgr.Count() != 0 {
		t.Errorf("Count after Remove = %d, want 0", mgr.Count())
	}
	if _, ok := mgr.Get("a"); ok {
		t.Error("Get after Remove returned ok=true")
	}
}

func TestSessionMgr_ListIsSnapshot(t *testing.T) {
	mgr := NewSessionMgr()
	s1 := newSessionWithDeps("a", ProtocolHarbor, time.Now())
	s2 := newSessionWithDeps("b", ProtocolRTP, time.Now())
	mgr.Add(s1)
	mgr.Add(s2)
	list := mgr.List()
	if len(list) != 2 {
		t.Errorf("List length = %d, want 2", len(list))
	}
	// Mutating the returned list must not affect the manager.
	list = list[:0]
	_ = list
	if mgr.Count() != 2 {
		t.Errorf("Count after caller mutates list = %d, want 2", mgr.Count())
	}
}

func TestSessionMgr_CountsByProtocol(t *testing.T) {
	mgr := NewSessionMgr()
	mgr.Add(newSessionWithDeps("a", ProtocolHarbor, time.Now()))
	mgr.Add(newSessionWithDeps("b", ProtocolHarbor, time.Now()))
	mgr.Add(newSessionWithDeps("c", ProtocolRTP, time.Now()))
	mgr.Add(newSessionWithDeps("d", ProtocolSRT, time.Now()))
	mgr.Add(newSessionWithDeps("e", ProtocolWebRTC, time.Now()))

	if got := mgr.CountByProtocol(ProtocolHarbor); got != 2 {
		t.Errorf("Harbor count = %d, want 2", got)
	}
	if got := mgr.CountByProtocol(ProtocolRTP); got != 1 {
		t.Errorf("RTP count = %d, want 1", got)
	}
	if got := mgr.CountByProtocol(ProtocolSRT); got != 1 {
		t.Errorf("SRT count = %d, want 1", got)
	}
	if got := mgr.CountByProtocol(ProtocolWebRTC); got != 1 {
		t.Errorf("WebRTC count = %d, want 1", got)
	}
}

func TestSessionMgr_TotalSessionsServed(t *testing.T) {
	mgr := NewSessionMgr()
	mgr.Add(newSessionWithDeps("a", ProtocolHarbor, time.Now()))
	mgr.Add(newSessionWithDeps("b", ProtocolRTP, time.Now()))
	mgr.Remove("a")
	mgr.Remove("a") // idempotent: shouldn't double-count
	mgr.Add(newSessionWithDeps("c", ProtocolSRT, time.Now()))

	if got := mgr.TotalSessionsServed(); got != 3 {
		t.Errorf("TotalSessionsServed = %d, want 3", got)
	}
	if got := mgr.Count(); got != 2 {
		t.Errorf("active Count = %d, want 2", got)
	}
}

func TestSessionMgr_ConcurrentAccess(t *testing.T) {
	mgr := NewSessionMgr()
	const N = 200
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			id := genID(i)
			s := newSessionWithDeps(id, ProtocolHarbor, time.Now())
			mgr.Add(s)
			_, _ = mgr.Get(id)
			_ = mgr.Count()
			mgr.Remove(id)
		}(i)
	}
	wg.Wait()
	if got := mgr.Count(); got != 0 {
		t.Errorf("Count after Add/Remove churn = %d, want 0", got)
	}
}

func TestNewIDGenerator_DeterministicWithSeed(t *testing.T) {
	g1 := NewSeededIDGenerator(42)
	g2 := NewSeededIDGenerator(42)
	if g1.NewID() != g2.NewID() {
		t.Error("seeded generators should produce same first ID")
	}
	if g1.NewID() == g1.NewID() {
		t.Error("subsequent IDs from same generator should differ")
	}
}

// genID is just a deterministic distinct string for the concurrent test.
func genID(i int) string {
	const hex = "0123456789abcdef"
	b := make([]byte, 8)
	for j := 0; j < 8; j++ {
		b[j] = hex[(i>>uint(j*4))&0xf]
	}
	return string(b)
}
