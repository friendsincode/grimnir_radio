/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-gst/go-gst/gst"
	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/gstnet"
	"github.com/friendsincode/grimnir_radio/internal/leadership"
)

// ClockConfig is the subset of process config the NetClock state machine
// consumes. Constructed from internal/config in the binary main.
type ClockConfig struct {
	Enabled    bool
	Region     string
	Port       int
	MasterAddr string

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

	// leader and providerFn are exposed for tests; production code populates
	// them from internal/leadership and internal/gstnet respectively.
	leader     clockLeader
	providerFn func(port int) clockProvider

	mu       sync.Mutex
	provider clockProvider
	isMaster bool
	gstClock *gst.Clock // master holds a strong ref to its own clock

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
	}
	return c
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
// On the master this is the system clock backing the NetTimeProvider; on a
// slave (Chunk 3 wires this) it'll be a NetClientClock. Returns nil when the
// Clock is disabled or hasn't reached steady state — callers treat nil as
// "use default" (today's GstSystemClock behavior).
func (c *Clock) GstClock() *gst.Clock {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gstClock
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
	c.mu.Unlock()

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
	wasMaster := c.isMaster
	c.provider = nil
	c.isMaster = false
	c.gstClock = nil
	c.mu.Unlock()

	if prov != nil {
		if err := prov.Close(); err != nil {
			c.logger.Warn().Err(err).Msg("error closing NetTimeProvider")
		}
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
