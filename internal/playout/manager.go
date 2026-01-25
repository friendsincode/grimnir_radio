/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package playout

import (
	"context"
	"io"
	"sync"

    "github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/rs/zerolog"
)

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

// EnsurePipelineWithOutput starts a pipeline with optional stdout capture.
func (m *Manager) EnsurePipelineWithOutput(ctx context.Context, mountID string, launch string, outputHandler func(io.Reader)) error {
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
func (m *Manager) EnsurePipelineWithDualOutput(ctx context.Context, mountID string, launch string, hqHandler, lqHandler func(io.Reader)) error {
	m.mu.Lock()
	pipeline, ok := m.pipelines[mountID]
	if !ok {
		pipeline = NewPipeline(m.cfg, mountID, m.logger)
		m.pipelines[mountID] = pipeline
	}
	m.mu.Unlock()

	return pipeline.StartWithDualOutput(ctx, launch, hqHandler, lqHandler)
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
