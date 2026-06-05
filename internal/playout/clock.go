/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-gst/go-gst/gst"
	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/gstnet"
	"github.com/friendsincode/grimnir_radio/internal/leadership"
)

// defaultSlaveSyncTimeout caps how long GstClock() will block waiting for the
// NetClientClock to sync to the master. Past this point we fall back to nil
// (caller uses GstSystemClock) and log a warning. Five seconds chosen because
// the gstnet integration test shows sync at ~150ms on localhost; anything
// over a few seconds on a real network indicates a misconfiguration or a
// genuinely unreachable master, and we'd rather start the pipeline late-bound
// than block a director tick forever.
const defaultSlaveSyncTimeout = 5 * time.Second

// ClockConfig is the subset of process config the NetClock state machine
// consumes. Constructed from internal/config in the binary main.
type ClockConfig struct {
	Enabled    bool
	Region     string
	Port       int
	MasterAddr string

	// SyncTimeout caps the blocking wait in GstClock() while the slave's
	// NetClientClock is dialing the master. Zero means defaultSlaveSyncTimeout.
	SyncTimeout time.Duration

	// Redis settings used when Enabled is true. Forwarded to the leadership
	// package for the Redis-lease election.
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	InstanceID    string
}

// clockProvider is the small interface the Clock uses to talk to the GStreamer
// net-time provider. Real implementation is *gstnet.NetTimeProvider; tests
// substitute a fake to avoid linking libgstnet in unit tests.
type clockProvider interface {
	Close() error
}

// clockClient is the small interface the Clock uses to talk to a
// NetClientClock that's syncing to a remote master. Real implementation wraps
// *gstnet.NetClientClock; tests substitute a fake.
type clockClient interface {
	WaitForSync(timeout time.Duration) bool
	GstClock() *gst.Clock
	Close() error
}

// clockLeader is the contract the Clock relies on from the leadership package.
// Defined as an interface so tests can swap in a deterministic fake.
type clockLeader interface {
	Start(ctx context.Context) error
	Stop() error
	IsLeader() bool
	LeaderCh() <-chan bool
}

// Clock owns the NetClock master-election lifecycle for one process. On Start
// it spins a goroutine that drives the state machine:
//
//	not-elected --(leader gained)--> MASTER (spawns NetTimeProvider)
//	MASTER     --(leader lost)----> SLAVE  (closes provider)
//	SLAVE      --(leader gained)--> MASTER (spawns new provider)
//
// When NetClockEnabled=false the Clock is a no-op: Start/Stop succeed, IsMaster
// always returns false, GstClock returns nil. Callers treat nil-gst-clock as
// "use the default GstSystemClock" (current behavior for single-instance
// deploys; preserves backward compatibility).
type Clock struct {
	cfg    ClockConfig
	logger zerolog.Logger

	// leader, providerFn, clientFn are exposed for tests; production code
	// populates them from internal/leadership and internal/gstnet
	// respectively.
	leader     clockLeader
	providerFn func(port int) clockProvider
	clientFn   func(addr string, port int) clockClient

	mu       sync.Mutex
	provider clockProvider
	isMaster bool
	gstClock *gst.Clock // master holds a strong ref to its own clock

	// Slave-side state. client is constructed lazily on the first GstClock()
	// call after demotion (or process start, when we begin as not-elected).
	// synced gates the blocking wait so concurrent GstClock() callers all
	// see the same outcome instead of each re-running WaitForSync. warnedNoAddr
	// throttles the "MasterAddr empty" log to one line per slave epoch.
	client       clockClient
	synced       bool
	warnedNoAddr bool
	warnedNoSync bool

	cancel context.CancelFunc
	done   chan struct{}
}

// NewClock returns a Clock with default production wiring. Tests overwrite the
// leader and providerFn fields after construction.
func NewClock(cfg ClockConfig, logger zerolog.Logger) *Clock {
	c := &Clock{
		cfg:    cfg,
		logger: logger.With().Str("component", "netclock").Logger(),
	}
	if cfg.Enabled {
		// Default provider factory wraps the real CGo NetTimeProvider. The
		// master clock is a fresh GstSystemClock; we hold a strong reference
		// in Clock.gstClock so GC doesn't free it while the provider is alive.
		c.providerFn = func(port int) clockProvider {
			sysClock := gst.ObtainSystemClock()
			if sysClock == nil {
				return nil
			}
			masterClock := sysClock.Clock
			c.mu.Lock()
			c.gstClock = masterClock
			c.mu.Unlock()
			p := gstnet.NewNetTimeProvider(masterClock, "0.0.0.0", port)
			if p == nil {
				return nil
			}
			return p
		}
		// Default client factory wraps the real CGo NetClientClock. Returns
		// nil on failure; the caller treats that as "fall back to nil
		// gst.Clock" so pipelines use GstSystemClock instead of crashing.
		c.clientFn = func(addr string, port int) clockClient {
			cc := gstnet.NewNetClientClock("grimnir-slave", addr, port)
			if cc == nil {
				return nil
			}
			return &netClientWrapper{cc: cc}
		}
	}
	return c
}

// netClientWrapper adapts *gstnet.NetClientClock to the clockClient interface
// so the Clock state machine doesn't have to import gstnet's concrete type
// outside the default factory closure.
type netClientWrapper struct {
	cc *gstnet.NetClientClock
}

func (w *netClientWrapper) WaitForSync(timeout time.Duration) bool {
	return w.cc.WaitForSync(timeout)
}

func (w *netClientWrapper) GstClock() *gst.Clock {
	if w.cc == nil {
		return nil
	}
	return w.cc.Clock
}

func (w *netClientWrapper) Close() error {
	// gstnet.NetClientClock holds a GObject ref via gst.FromGstClockUnsafeFull,
	// which the gst.Clock finalizer releases on GC. We don't have an explicit
	// Unref API on the wrapper today; dropping the reference is enough.
	w.cc = nil
	return nil
}

// Start begins the election loop. Safe to call on a disabled Clock (no-op).
func (c *Clock) Start(ctx context.Context) error {
	if !c.cfg.Enabled {
		return nil
	}

	// Lazily construct the leader from cfg.Redis* if one wasn't injected.
	// Tests inject a fakeLeader before calling Start.
	if c.leader == nil {
		econf := leadership.DefaultConfig()
		econf.ElectionKey = netClockLeaseKey(c.cfg.Region)
		econf.RedisAddr = c.cfg.RedisAddr
		econf.RedisPassword = c.cfg.RedisPassword
		econf.RedisDB = c.cfg.RedisDB
		if c.cfg.InstanceID != "" {
			econf.InstanceID = c.cfg.InstanceID
		}
		e, err := leadership.NewElection(econf, c.logger)
		if err != nil {
			return fmt.Errorf("create netclock election: %w", err)
		}
		c.leader = e
	}

	loopCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.done = make(chan struct{})

	if err := c.leader.Start(loopCtx); err != nil {
		cancel()
		return fmt.Errorf("start netclock election: %w", err)
	}

	go c.loop(loopCtx)
	return nil
}

// Stop tears down the election and closes the provider if we're master.
// Idempotent.
func (c *Clock) Stop() error {
	if !c.cfg.Enabled {
		return nil
	}
	if c.cancel != nil {
		c.cancel()
	}
	if c.done != nil {
		<-c.done
	}
	if c.leader != nil {
		_ = c.leader.Stop()
	}
	c.demote()
	return nil
}

// IsMaster reports whether this process currently owns the master lease and
// is hosting the NetTimeProvider.
func (c *Clock) IsMaster() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isMaster
}

// GstClock returns the *gst.Clock that pipelines should bind via UseClock.
//
// On the master this is the system clock backing the NetTimeProvider.
//
// On a slave (Clock enabled, not currently master, MasterAddr set) this
// constructs a NetClientClock pointing at the master on first call and blocks
// up to SyncTimeout for it to sync. Subsequent calls return the same synced
// clock without re-syncing.
//
// Three slave failure modes all return nil (callers fall back to
// GstSystemClock, identical to today's behavior):
//   - MasterAddr is empty — logs a one-shot warning per slave epoch.
//   - MasterAddr doesn't parse as host:port — same warning, returns nil.
//   - WaitForSync times out — logs a one-shot warning; the next call retries.
//
// Returns nil when the Clock is disabled, since callers treat nil as the
// "use default" signal.
func (c *Clock) GstClock() *gst.Clock {
	if !c.cfg.Enabled {
		return nil
	}

	c.mu.Lock()
	if c.isMaster {
		gc := c.gstClock
		c.mu.Unlock()
		return gc
	}
	// Slave path. If we already have a synced client, return it without
	// re-running the blocking WaitForSync.
	if c.synced && c.client != nil {
		gc := c.client.GstClock()
		c.mu.Unlock()
		return gc
	}
	addr := c.cfg.MasterAddr
	if addr == "" {
		warned := c.warnedNoAddr
		c.warnedNoAddr = true
		c.mu.Unlock()
		if !warned {
			c.logger.Warn().Msg("NetClock slave: MasterAddr is empty; pipelines will use local GstSystemClock")
		}
		return nil
	}
	host, port, ok := splitHostPort(addr)
	if !ok {
		warned := c.warnedNoAddr
		c.warnedNoAddr = true
		c.mu.Unlock()
		if !warned {
			c.logger.Warn().Str("addr", addr).Msg("NetClock slave: MasterAddr does not parse as host:port; falling back to local clock")
		}
		return nil
	}
	// Construct the client lazily on the first ask. If construction fails
	// we leave c.client == nil and try again next call.
	client := c.client
	if client == nil {
		client = c.clientFn(host, port)
		if client == nil {
			c.mu.Unlock()
			c.logger.Warn().Str("addr", addr).Msg("NetClock slave: NetClientClock construction failed; falling back to local clock")
			return nil
		}
		c.client = client
	}
	timeout := c.cfg.SyncTimeout
	c.mu.Unlock()
	if timeout <= 0 {
		timeout = defaultSlaveSyncTimeout
	}

	// Blocking sync wait OUTSIDE the mutex so the leader-loop goroutine can
	// still flip promote/demote without deadlocking on us. Concurrent
	// GstClock() callers will both enter WaitForSync; that's harmless —
	// gst_clock_wait_for_sync is reentrant and idempotent.
	syncStart := time.Now()
	ok = client.WaitForSync(timeout)
	elapsed := time.Since(syncStart)

	c.mu.Lock()
	defer c.mu.Unlock()
	// If a promotion happened during the wait, prefer the master clock and
	// throw away the half-synced client.
	if c.isMaster {
		_ = client.Close()
		c.client = nil
		c.synced = false
		return c.gstClock
	}
	if !ok {
		warned := c.warnedNoSync
		c.warnedNoSync = true
		if !warned {
			c.logger.Warn().
				Str("addr", addr).
				Dur("elapsed", elapsed).
				Dur("timeout", timeout).
				Msg("NetClock slave: WaitForSync timed out; falling back to local clock for now")
		}
		return nil
	}
	c.synced = true
	c.warnedNoSync = false
	c.logger.Info().
		Str("addr", addr).
		Dur("elapsed", elapsed).
		Msg("NetClock slave: synced to master")
	return client.GstClock()
}

// splitHostPort accepts "host:port" and returns (host, port, true) when the
// port parses. Refusing IPv6 literals on purpose; ops can introduce bracket
// parsing if they need it.
func splitHostPort(addr string) (string, int, bool) {
	idx := strings.LastIndex(addr, ":")
	if idx <= 0 || idx == len(addr)-1 {
		return "", 0, false
	}
	host := addr[:idx]
	portStr := addr[idx+1:]
	var port int
	for _, ch := range portStr {
		if ch < '0' || ch > '9' {
			return "", 0, false
		}
		port = port*10 + int(ch-'0')
	}
	return host, port, true
}

// loop reacts to leader gain/loss events. Exits when ctx is cancelled.
func (c *Clock) loop(ctx context.Context) {
	defer close(c.done)
	leaderCh := c.leader.LeaderCh()
	for {
		select {
		case <-ctx.Done():
			return
		case isLeader, ok := <-leaderCh:
			if !ok {
				return
			}
			if isLeader {
				c.promote()
			} else {
				c.demote()
			}
		}
	}
}

func (c *Clock) promote() {
	c.mu.Lock()
	if c.isMaster {
		c.mu.Unlock()
		return
	}
	// Close any slave-side client; we own the clock now.
	prevClient := c.client
	c.client = nil
	c.synced = false
	c.warnedNoAddr = false
	c.warnedNoSync = false
	c.mu.Unlock()
	if prevClient != nil {
		_ = prevClient.Close()
	}

	provider := c.providerFn(c.cfg.Port)
	if provider == nil {
		c.logger.Error().
			Int("port", c.cfg.Port).
			Msg("failed to spawn NetTimeProvider; remaining in not-elected state")
		return
	}

	c.mu.Lock()
	c.provider = provider
	c.isMaster = true
	c.mu.Unlock()

	c.logger.Info().
		Str("region", c.cfg.Region).
		Int("port", c.cfg.Port).
		Msg("promoted to NetClock master; NetTimeProvider listening")
}

func (c *Clock) demote() {
	c.mu.Lock()
	prov := c.provider
	prevClient := c.client
	wasMaster := c.isMaster
	c.provider = nil
	c.isMaster = false
	c.gstClock = nil
	// Reset slave-side state too: if we're toggling SLAVE->SLAVE (e.g. Stop
	// during not-elected) we still want clean shutdown of any client we
	// stood up; if we're MASTER->SLAVE, the next GstClock() call will
	// construct a fresh client.
	c.client = nil
	c.synced = false
	c.warnedNoAddr = false
	c.warnedNoSync = false
	c.mu.Unlock()

	if prov != nil {
		if err := prov.Close(); err != nil {
			c.logger.Warn().Err(err).Msg("error closing NetTimeProvider")
		}
	}
	if prevClient != nil {
		_ = prevClient.Close()
	}
	if wasMaster {
		c.logger.Info().
			Str("region", c.cfg.Region).
			Msg("demoted from NetClock master; provider closed")
	}
}

// netClockLeaseKey is the Redis key used for the NetClock master lease.
// Distinct from the scheduler leader key so the two elections are independent.
func netClockLeaseKey(region string) string {
	return "grimnir-netclock-master-" + region
}
