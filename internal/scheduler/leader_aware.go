package scheduler

import (
	"context"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/leadership"
	"github.com/rs/zerolog"
)

// LeaderAwareScheduler wraps a scheduler and only runs when this instance is the leader
type LeaderAwareScheduler struct {
	scheduler *Service
	election  *leadership.Election
	logger    zerolog.Logger

	// Internal state
	ctx        context.Context
	cancelFunc context.CancelFunc
	schedulerRunning bool
}

// NewLeaderAware creates a leader-aware scheduler wrapper
func NewLeaderAware(scheduler *Service, election *leadership.Election, logger zerolog.Logger) *LeaderAwareScheduler {
	return &LeaderAwareScheduler{
		scheduler:        scheduler,
		election:         election,
		logger:           logger.With().Str("component", "leader_aware_scheduler").Logger(),
		schedulerRunning: false,
	}
}

// Start begins monitoring leadership status and manages scheduler lifecycle
func (las *LeaderAwareScheduler) Start(ctx context.Context) error {
	las.ctx = ctx

	las.logger.Info().Msg("starting leader-aware scheduler")

	// Start leader election
	if err := las.election.Start(ctx); err != nil {
		return err
	}

	// Monitor leadership changes
	go las.monitorLeadership()

	return nil
}

// Stop stops the leader-aware scheduler and releases leadership
func (las *LeaderAwareScheduler) Stop() error {
	las.logger.Info().Msg("stopping leader-aware scheduler")

	// Stop scheduler if running
	if las.schedulerRunning && las.cancelFunc != nil {
		las.cancelFunc()
		las.schedulerRunning = false
	}

	// Stop election
	return las.election.Stop()
}

// monitorLeadership watches for leadership changes and starts/stops scheduler accordingly
func (las *LeaderAwareScheduler) monitorLeadership() {
	leaderCh := las.election.LeaderCh()

	// Check initial leadership status
	if las.election.IsLeader() {
		las.startScheduler()
	}

	for {
		select {
		case <-las.ctx.Done():
			return
		case isLeader := <-leaderCh:
			if isLeader {
				las.logger.Info().Msg("became leader, starting scheduler")
				las.startScheduler()
			} else {
				las.logger.Warn().Msg("lost leadership, stopping scheduler")
				las.stopScheduler()
			}
		}
	}
}

// startScheduler starts the scheduler in a goroutine
func (las *LeaderAwareScheduler) startScheduler() {
	if las.schedulerRunning {
		las.logger.Warn().Msg("scheduler already running")
		return
	}

	ctx, cancel := context.WithCancel(las.ctx)
	las.cancelFunc = cancel
	las.schedulerRunning = true

	go func() {
		las.logger.Info().Msg("scheduler started")
		if err := las.scheduler.Run(ctx); err != nil && err != context.Canceled {
			las.logger.Error().Err(err).Msg("scheduler error")
		}
		las.schedulerRunning = false
		las.logger.Info().Msg("scheduler stopped")
	}()
}

// stopScheduler stops the running scheduler
func (las *LeaderAwareScheduler) stopScheduler() {
	if !las.schedulerRunning {
		return
	}

	if las.cancelFunc != nil {
		las.cancelFunc()
		las.cancelFunc = nil
	}

	// Wait briefly for scheduler to stop
	time.Sleep(100 * time.Millisecond)
	las.schedulerRunning = false
}

// IsLeader returns whether this instance is the leader
func (las *LeaderAwareScheduler) IsLeader() bool {
	return las.election.IsLeader()
}
