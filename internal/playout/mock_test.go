/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"io"
	"os"
)

// mockPipeline implements PipelineInterface.
type mockPipeline struct {
	done    chan struct{}
	stopErr error
}

func newMockPipeline() *mockPipeline {
	ch := make(chan struct{})
	return &mockPipeline{done: ch}
}

// newDeadMockPipeline returns a pipeline that is present but has already exited:
// its Done() channel is pre-closed, modeling a GStreamer process that self-exited
// (EOS/crash) and lingers in the manager's map until StopPipeline runs.
func newDeadMockPipeline() *mockPipeline {
	ch := make(chan struct{})
	close(ch)
	return &mockPipeline{done: ch}
}

func (m *mockPipeline) Done() <-chan struct{} { return m.done }
func (m *mockPipeline) Stop() error {
	select {
	case <-m.done:
	default:
		close(m.done)
	}
	return m.stopErr
}

// mockManager implements ManagerInterface.
type mockManager struct {
	pipelines   map[string]*mockPipeline
	ensureErr   error
	stopErr     error
	stdinWriter io.WriteCloser
	stopCalls   int // number of StopPipeline invocations (preemption assertions)
}

func newMockManager() *mockManager {
	return &mockManager{pipelines: make(map[string]*mockPipeline)}
}

func (m *mockManager) EnsurePipeline(ctx context.Context, mountID, launch string) error {
	if _, ok := m.pipelines[mountID]; !ok {
		m.pipelines[mountID] = newMockPipeline()
	}
	return m.ensureErr
}

func (m *mockManager) EnsurePipelineWithOutput(ctx context.Context, mountID, launch string, outputHandler func(io.Reader)) error {
	if _, ok := m.pipelines[mountID]; !ok {
		m.pipelines[mountID] = newMockPipeline()
	}
	return m.ensureErr
}

func (m *mockManager) EnsurePipelineWithDualOutput(ctx context.Context, mountID, launch string, seekFile *os.File, hqHandler, lqHandler func(io.Reader)) error {
	if _, ok := m.pipelines[mountID]; !ok {
		m.pipelines[mountID] = newMockPipeline()
	}
	return m.ensureErr
}

func (m *mockManager) EnsurePipelineWithDualOutputAndInput(ctx context.Context, mountID, launch string, hqHandler, lqHandler func(io.Reader)) (io.WriteCloser, error) {
	if _, ok := m.pipelines[mountID]; !ok {
		m.pipelines[mountID] = newMockPipeline()
	}
	return m.stdinWriter, m.ensureErr
}

func (m *mockManager) GetPipeline(mountID string) PipelineInterface {
	p, ok := m.pipelines[mountID]
	if !ok {
		return nil
	}
	return p
}

func (m *mockManager) StopPipeline(mountID string) error {
	m.stopCalls++
	delete(m.pipelines, mountID)
	return m.stopErr
}

func (m *mockManager) Shutdown() error { return nil }
