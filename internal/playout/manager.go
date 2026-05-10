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
}

// NewManager creates a playout manager.
func NewManager(cfg *config.Config, logger zerolog.Logger) *Manager {
	return &Manager{cfg: cfg, logger: logger, pipelines: make(map[string]*Pipeline)}
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
