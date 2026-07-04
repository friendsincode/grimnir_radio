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

	"github.com/go-gst/go-gst/gst"
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

// fakeClient stands in for *gstnet.NetClientClock so tests don't need
// libgstnet. waitDelay simulates network sync latency; syncOK gates the
// final outcome.
type fakeClient struct {
	addr      string
	port      int
	waitDelay time.Duration
	syncOK    bool
	waitCalls atomic.Int32
	closed    atomic.Bool
}

func (c *fakeClient) WaitForSync(timeout time.Duration) bool {
	c.waitCalls.Add(1)
	if c.waitDelay > 0 {
		if c.waitDelay > timeout {
			time.Sleep(timeout)
			return false
		}
		time.Sleep(c.waitDelay)
	}
	return c.syncOK
}

func (c *fakeClient) GstClock() *gst.Clock {
	// Returning a non-nil sentinel without actually constructing a
	// gst.Clock would require linking GStreamer; tests that need to
	// distinguish "got the client clock" from "got nil" verify via
	// waitCalls and closed instead. The test that exercises GstClock()
	// return value checks for nil-vs-non-nil only when the production
	// gst factory is in play (see TestClock_SlaveSync_ReturnsClient).
	return nil
}

func (c *fakeClient) Close() error {
	c.closed.Store(true)
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

	// demote() flips isMaster under the mutex & closes the provider after
	// releasing it (deliberately, so Close never blocks the leader loop), so
	// !IsMaster is observable a beat before the close lands. The invariant is
	// "eventually closed", not "closed before the flag flips".
	waitFor(t, time.Second, func() bool { return fp.closed.Load() })
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

func TestClock_Slave_NoMasterAddr_ReturnsNil(t *testing.T) {
	leader := newFakeLeader()
	c := NewClock(ClockConfig{
		Enabled:    true,
		Region:     "test",
		Port:       19998,
		MasterAddr: "",
	}, zerolog.Nop())
	c.leader = leader
	c.providerFn = func(int) clockProvider { return &fakeProvider{} }
	var clientSpawns atomic.Int32
	c.clientFn = func(string, int) clockClient {
		clientSpawns.Add(1)
		return &fakeClient{syncOK: true}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = c.Stop() }()

	// Never elected master; MasterAddr empty -> GstClock() returns nil and
	// no client is constructed.
	if got := c.GstClock(); got != nil {
		t.Errorf("GstClock() = %v, want nil", got)
	}
	if clientSpawns.Load() != 0 {
		t.Errorf("client spawned %d times despite empty MasterAddr; want 0", clientSpawns.Load())
	}
}

func TestClock_Slave_BadMasterAddr_ReturnsNil(t *testing.T) {
	c := NewClock(ClockConfig{
		Enabled:    true,
		Region:     "test",
		Port:       19999,
		MasterAddr: "not-a-host-port",
	}, zerolog.Nop())
	c.leader = newFakeLeader()
	c.providerFn = func(int) clockProvider { return &fakeProvider{} }
	var clientSpawns atomic.Int32
	c.clientFn = func(string, int) clockClient {
		clientSpawns.Add(1)
		return &fakeClient{syncOK: true}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = c.Stop() }()

	if got := c.GstClock(); got != nil {
		t.Errorf("GstClock() = %v, want nil", got)
	}
	if clientSpawns.Load() != 0 {
		t.Errorf("client spawned %d times on bad MasterAddr; want 0", clientSpawns.Load())
	}
}

func TestClock_Slave_SyncTimeout_ReturnsNil(t *testing.T) {
	c := NewClock(ClockConfig{
		Enabled:     true,
		Region:      "test",
		Port:        20000,
		MasterAddr:  "127.0.0.1:9094",
		SyncTimeout: 50 * time.Millisecond,
	}, zerolog.Nop())
	c.leader = newFakeLeader()
	c.providerFn = func(int) clockProvider { return &fakeProvider{} }
	fc := &fakeClient{waitDelay: 200 * time.Millisecond, syncOK: false}
	c.clientFn = func(addr string, port int) clockClient {
		fc.addr = addr
		fc.port = port
		return fc
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = c.Stop() }()

	start := time.Now()
	got := c.GstClock()
	elapsed := time.Since(start)

	if got != nil {
		t.Errorf("GstClock() = %v, want nil on timeout", got)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("GstClock() blocked for %s; expected to respect 50ms SyncTimeout", elapsed)
	}
	if fc.addr != "127.0.0.1" || fc.port != 9094 {
		t.Errorf("client got addr=%q port=%d; want 127.0.0.1:9094", fc.addr, fc.port)
	}
}

func TestClock_Slave_SyncOK_CachesClock(t *testing.T) {
	c := NewClock(ClockConfig{
		Enabled:     true,
		Region:      "test",
		Port:        20001,
		MasterAddr:  "127.0.0.1:9094",
		SyncTimeout: 500 * time.Millisecond,
	}, zerolog.Nop())
	c.leader = newFakeLeader()
	c.providerFn = func(int) clockProvider { return &fakeProvider{} }
	fc := &fakeClient{syncOK: true}
	var spawns atomic.Int32
	c.clientFn = func(string, int) clockClient {
		spawns.Add(1)
		return fc
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = c.Stop() }()

	// First call: triggers WaitForSync, fake reports synced -> returns the
	// fake's GstClock (nil for the fake, but the import-side cost of a real
	// clock is exercised separately).
	_ = c.GstClock()
	if fc.waitCalls.Load() != 1 {
		t.Errorf("WaitForSync called %d times after first GstClock(); want 1", fc.waitCalls.Load())
	}
	// Second call: should NOT re-sync.
	_ = c.GstClock()
	if fc.waitCalls.Load() != 1 {
		t.Errorf("WaitForSync called %d times after second GstClock(); want 1 (cached)", fc.waitCalls.Load())
	}
	if spawns.Load() != 1 {
		t.Errorf("client constructed %d times; want 1", spawns.Load())
	}
}

func TestClock_Promotion_ClosesSlaveClient(t *testing.T) {
	leader := newFakeLeader()
	c := NewClock(ClockConfig{
		Enabled:    true,
		Region:     "test",
		Port:       20002,
		MasterAddr: "127.0.0.1:9094",
	}, zerolog.Nop())
	c.leader = leader
	c.providerFn = func(int) clockProvider { return &fakeProvider{} }
	fc := &fakeClient{syncOK: true}
	c.clientFn = func(string, int) clockClient { return fc }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = c.Stop() }()

	// Stand up the slave client.
	_ = c.GstClock()
	if fc.closed.Load() {
		t.Fatal("client closed before promotion")
	}

	// Promote to master; slave client should be released.
	leader.setLeader(true)
	waitFor(t, time.Second, func() bool { return c.IsMaster() })
	waitFor(t, time.Second, func() bool { return fc.closed.Load() })
}

// TestClock_Disabled_GstClock confirms that the disabled-clock fast path
// short-circuits in GstClock() too, not just at the top-level Start/Stop.
// Belt-and-suspenders test guarding the "NETCLOCK_ENABLED=false ->
// behavior identical to today" invariant called out in the plan's self-review.
func TestClock_Disabled_GstClock(t *testing.T) {
	c := NewClock(ClockConfig{Enabled: false, MasterAddr: "127.0.0.1:9094"}, zerolog.Nop())
	c.clientFn = func(string, int) clockClient {
		t.Fatal("clientFn should never run on a disabled Clock")
		return nil
	}
	if got := c.GstClock(); got != nil {
		t.Errorf("disabled GstClock() = %v, want nil", got)
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
