/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// fakeLeader is a hand-rolled stand-in for internal/leadership.Election that
// drives the Clock state machine deterministically. Tests poke isLeader to
// flip MASTER/SLAVE transitions.
type fakeLeader struct {
	mu        sync.Mutex
	isLeader  bool
	stopped   bool
	leaderCh  chan bool
	stopCalls atomic.Int32
}

func newFakeLeader() *fakeLeader {
	return &fakeLeader{leaderCh: make(chan bool, 8)}
}

func (f *fakeLeader) IsLeader() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.isLeader
}

func (f *fakeLeader) LeaderCh() <-chan bool { return f.leaderCh }

func (f *fakeLeader) Start(ctx context.Context) error { return nil }

func (f *fakeLeader) Stop() error {
	f.mu.Lock()
	f.stopped = true
	f.mu.Unlock()
	f.stopCalls.Add(1)
	return nil
}

func (f *fakeLeader) setLeader(v bool) {
	f.mu.Lock()
	f.isLeader = v
	f.mu.Unlock()
	f.leaderCh <- v
}

// fakeProvider stands in for *gstnet.NetTimeProvider so tests don't need
// libgstnet at the linker step.
type fakeProvider struct {
	closed atomic.Bool
}

func (p *fakeProvider) Close() error {
	p.closed.Store(true)
	return nil
}

func TestClock_Disabled_DoesNothing(t *testing.T) {
	c := NewClock(ClockConfig{Enabled: false}, zerolog.Nop())
	if c == nil {
		t.Fatal("NewClock returned nil for disabled config")
	}
	if c.IsMaster() {
		t.Error("disabled Clock should never be master")
	}
	if c.GstClock() != nil {
		t.Error("disabled Clock should return nil gst clock (caller uses GstSystemClock)")
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start on disabled Clock: %v", err)
	}
	if err := c.Stop(); err != nil {
		t.Fatalf("Stop on disabled Clock: %v", err)
	}
}

func TestClock_BecomesMaster_SpawnsProvider(t *testing.T) {
	leader := newFakeLeader()
	var spawned atomic.Int32
	var lastProvider atomic.Pointer[fakeProvider]

	c := NewClock(ClockConfig{
		Enabled: true,
		Region:  "test",
		Port:    19994,
	}, zerolog.Nop())
	c.leader = leader
	c.providerFn = func(port int) clockProvider {
		spawned.Add(1)
		fp := &fakeProvider{}
		lastProvider.Store(fp)
		return fp
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = c.Stop() }()

	leader.setLeader(true)

	waitFor(t, time.Second, func() bool { return c.IsMaster() })
	if spawned.Load() != 1 {
		t.Errorf("provider spawn count = %d, want 1", spawned.Load())
	}
}

func TestClock_LosesLease_KillsProvider(t *testing.T) {
	leader := newFakeLeader()
	var spawned atomic.Int32
	var lastProvider atomic.Pointer[fakeProvider]

	c := NewClock(ClockConfig{
		Enabled: true,
		Region:  "test",
		Port:    19995,
	}, zerolog.Nop())
	c.leader = leader
	c.providerFn = func(port int) clockProvider {
		spawned.Add(1)
		fp := &fakeProvider{}
		lastProvider.Store(fp)
		return fp
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = c.Stop() }()

	leader.setLeader(true)
	waitFor(t, time.Second, func() bool { return c.IsMaster() })

	fp := lastProvider.Load()
	if fp == nil {
		t.Fatal("provider not spawned")
	}

	leader.setLeader(false)
	waitFor(t, time.Second, func() bool { return !c.IsMaster() })

	if !fp.closed.Load() {
		t.Error("provider not closed after losing leadership")
	}
}

func TestClock_ReacquireLease_SpawnsNewProvider(t *testing.T) {
	leader := newFakeLeader()
	var spawned atomic.Int32

	c := NewClock(ClockConfig{
		Enabled: true,
		Region:  "test",
		Port:    19996,
	}, zerolog.Nop())
	c.leader = leader
	c.providerFn = func(port int) clockProvider {
		spawned.Add(1)
		return &fakeProvider{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = c.Stop() }()

	leader.setLeader(true)
	waitFor(t, time.Second, func() bool { return c.IsMaster() })

	leader.setLeader(false)
	waitFor(t, time.Second, func() bool { return !c.IsMaster() })

	leader.setLeader(true)
	waitFor(t, time.Second, func() bool { return c.IsMaster() })

	if spawned.Load() != 2 {
		t.Errorf("provider spawn count = %d, want 2 (acquire + reacquire)", spawned.Load())
	}
}

func TestClock_Stop_ClosesProvider(t *testing.T) {
	leader := newFakeLeader()
	var lastProvider atomic.Pointer[fakeProvider]

	c := NewClock(ClockConfig{
		Enabled: true,
		Region:  "test",
		Port:    19997,
	}, zerolog.Nop())
	c.leader = leader
	c.providerFn = func(port int) clockProvider {
		fp := &fakeProvider{}
		lastProvider.Store(fp)
		return fp
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	leader.setLeader(true)
	waitFor(t, time.Second, func() bool { return c.IsMaster() })

	if err := c.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	fp := lastProvider.Load()
	if fp == nil || !fp.closed.Load() {
		t.Error("provider not closed on Stop")
	}
	if leader.stopCalls.Load() != 1 {
		t.Errorf("leader.Stop called %d times, want 1", leader.stopCalls.Load())
	}
}

func TestClock_LeaseKey_IncludesRegion(t *testing.T) {
	key := netClockLeaseKey("dfw1")
	want := "grimnir-netclock-master-dfw1"
	if key != want {
		t.Errorf("netClockLeaseKey(dfw1) = %q, want %q", key, want)
	}
}

// waitFor polls cond at 10ms intervals until it returns true or timeout fires.
// Marks the calling test as failed if the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition never satisfied within %s", timeout)
}
