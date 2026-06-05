/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"io"
	"os"
	"sync"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/go-gst/go-gst/gst"
	"github.com/rs/zerolog"
)

// ManagerInterface abstracts pipeline management for testing.
type ManagerInterface interface {
	EnsurePipeline(ctx context.Context, mountID, launch string) error
	EnsurePipelineWithOutput(ctx context.Context, mountID, launch string, outputHandler func(io.Reader)) error
	EnsurePipelineWithDualOutput(ctx context.Context, mountID, launch string, seekFile *os.File, hqHandler, lqHandler func(io.Reader)) error
	EnsurePipelineWithDualOutputAndInput(ctx context.Context, mountID, launch string, hqHandler, lqHandler func(io.Reader)) (io.WriteCloser, error)
	GetPipeline(mountID string) PipelineInterface
	StopPipeline(mountID string) error
	Shutdown() error
}

// Manager tracks pipelines per mount.
type Manager struct {
	cfg    *config.Config
	logger zerolog.Logger

	mu        sync.Mutex
	pipelines map[string]*Pipeline

	// netClock, when non-nil, supplies the *gst.Clock pipelines bind via
	// pipeline.UseClock() in Chunk 3. Set via WithManagerClock.
	netClock *Clock
}

// ManagerOption configures optional Manager dependencies.
type ManagerOption func(*Manager)

// WithManagerClock attaches a NetClock state machine so Chunk 3's pipeline
// wiring can bind master/slave clocks via pipeline.UseClock(). Safe to pass
// nil; Manager.Clock() returns nil in that case and pipelines fall back to
// the default GstSystemClock.
func WithManagerClock(c *Clock) ManagerOption {
	return func(m *Manager) {
		m.netClock = c
	}
}

// NewManager creates a playout manager.
func NewManager(cfg *config.Config, logger zerolog.Logger, opts ...ManagerOption) *Manager {
	m := &Manager{cfg: cfg, logger: logger, pipelines: make(map[string]*Pipeline)}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Clock returns the attached NetClock, or nil if none was injected.
func (m *Manager) Clock() *Clock {
	return m.netClock
}

// currentGstClock asks the attached NetClock for whichever *gst.Clock the
// next pipeline should bind via UseClock(). Returns nil when no Clock is
// attached or the Clock reports nil (disabled, slave-without-master-addr,
// sync-timeout), in which case the pipeline falls back to GstSystemClock —
// preserving today's behavior on single-instance deploys.
//
// Pulled out so all three EnsurePipeline* call sites pick up future
// behavior changes (e.g. blocking-vs-non-blocking modes) without
// duplicating policy.
func (m *Manager) currentGstClock() *gst.Clock {
	if m.netClock == nil {
		return nil
	}
	return m.netClock.GstClock()
}

// EnsurePipeline starts or reuses an existing pipeline.
func (m *Manager) EnsurePipeline(ctx context.Context, mountID string, launch string) error {
	return m.EnsurePipelineWithOutput(ctx, mountID, launch, nil)
}

// stopIfRunning ensures any in-flight gst process for this mount is fully
// terminated before a new pipeline is launched. Without this, the previous
// pipeline keeps writing into its output pipes (HQ/LQ fdsink) — and because
// the previous and new pipelines feed the SAME broadcast mount, listeners
// hear both tracks overlapping (the audible "echo"). It also prevents the
// leak of orphaned gst-launch processes (combined with Setpgid in pipeline.go,
// Stop() now kills the whole sh+gst process group, not just the shell).
//
// This used to return `"pipeline already running"` and bail; that bug forced
// the director to either skip the new track entirely or run two pipelines
// concurrently into the same mount, neither of which is acceptable for live
// broadcast.
func (m *Manager) stopIfRunning(mountID string) {
	m.mu.Lock()
	pipeline, ok := m.pipelines[mountID]
	m.mu.Unlock()
	if !ok || pipeline == nil {
		return
	}
	if err := pipeline.Stop(); err != nil {
		m.logger.Debug().Err(err).Str("mount", mountID).Msg("stop previous pipeline before re-ensure")
	}
}

// EnsurePipelineWithOutput starts a pipeline with optional stdout capture.
func (m *Manager) EnsurePipelineWithOutput(ctx context.Context, mountID string, launch string, outputHandler func(io.Reader)) error {
	m.stopIfRunning(mountID)
	m.mu.Lock()
	pipeline, ok := m.pipelines[mountID]
	if !ok {
		pipeline = NewPipeline(m.cfg, mountID, m.logger)
		m.pipelines[mountID] = pipeline
	}
	m.mu.Unlock()

	pipeline.SetClock(m.currentGstClock())
	return pipeline.StartWithOutput(ctx, launch, outputHandler)
}

// EnsurePipelineWithDualOutput starts a pipeline with HQ and LQ output streams.
// The pipeline should use fd=3 for HQ output and fd=4 for LQ output.
// If seekFile is non-nil it is passed as fd=5 for fdsrc-based positional seek.
func (m *Manager) EnsurePipelineWithDualOutput(ctx context.Context, mountID string, launch string, seekFile *os.File, hqHandler, lqHandler func(io.Reader)) error {
	m.stopIfRunning(mountID)
	m.mu.Lock()
	pipeline, ok := m.pipelines[mountID]
	if !ok {
		pipeline = NewPipeline(m.cfg, mountID, m.logger)
		m.pipelines[mountID] = pipeline
	}
	m.mu.Unlock()

	pipeline.SetClock(m.currentGstClock())
	return pipeline.StartWithDualOutput(ctx, launch, seekFile, hqHandler, lqHandler)
}

// EnsurePipelineWithDualOutputAndInput starts a pipeline that consumes stdin (fd=0) and emits HQ/LQ outputs.
func (m *Manager) EnsurePipelineWithDualOutputAndInput(ctx context.Context, mountID string, launch string, hqHandler, lqHandler func(io.Reader)) (io.WriteCloser, error) {
	m.stopIfRunning(mountID)
	m.mu.Lock()
	pipeline, ok := m.pipelines[mountID]
	if !ok {
		pipeline = NewPipeline(m.cfg, mountID, m.logger)
		m.pipelines[mountID] = pipeline
	}
	m.mu.Unlock()

	pipeline.SetClock(m.currentGstClock())
	return pipeline.StartWithDualOutputAndInput(ctx, launch, hqHandler, lqHandler)
}

// GetPipeline returns the pipeline for a mount, or nil if none exists.
func (m *Manager) GetPipeline(mountID string) PipelineInterface {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pipelines[mountID]
	if p == nil {
		return nil
	}
	return p
}

// StopPipeline stops the pipeline for a mount.
func (m *Manager) StopPipeline(mountID string) error {
	m.mu.Lock()
	pipeline, ok := m.pipelines[mountID]
	m.mu.Unlock()

	if !ok {
		return nil
	}

	return pipeline.Stop()
}

// Shutdown stops all pipelines and clears the map.
func (m *Manager) Shutdown() error {
	m.mu.Lock()
	pipelines := make(map[string]*Pipeline, len(m.pipelines))
	for k, v := range m.pipelines {
		pipelines[k] = v
	}
	m.pipelines = make(map[string]*Pipeline)
	m.mu.Unlock()

	for _, pipeline := range pipelines {
		if err := pipeline.Stop(); err != nil {
			return err
		}
	}
	return nil
}
