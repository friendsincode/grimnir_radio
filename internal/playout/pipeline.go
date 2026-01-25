/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package playout

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

    "github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/rs/zerolog"
)

// Pipeline manages a GStreamer-based playout process for a mount.
type Pipeline struct {
	cfg    *config.Config
	logger zerolog.Logger

	mu      sync.Mutex
	cmd     *exec.Cmd
	mountID string
	stdout  io.ReadCloser
	done    chan struct{} // signals when the process has exited
}

// NewPipeline constructs a pipeline for a mount.
func NewPipeline(cfg *config.Config, mountID string, logger zerolog.Logger) *Pipeline {
	return &Pipeline{cfg: cfg, mountID: mountID, logger: logger}
}

// Start launches the gst pipeline with the provided launch string.
func (p *Pipeline) Start(ctx context.Context, launch string) error {
	return p.StartWithOutput(ctx, launch, nil)
}

// StartWithOutput launches the pipeline and optionally captures stdout.
// If outputHandler is provided, stdout is piped to it; otherwise discarded.
func (p *Pipeline) StartWithOutput(ctx context.Context, launch string, outputHandler func(io.Reader)) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil && p.done != nil {
		select {
		case <-p.done:
			// Previous process has exited, ok to start new one
		default:
			return fmt.Errorf("pipeline already running")
		}
	}

	// Use shell to properly parse the GStreamer pipeline string
	shellCmd := fmt.Sprintf("%s -e %s", p.cfg.GStreamerBin, launch)
	cmd := exec.CommandContext(ctx, "sh", "-c", shellCmd)
	cmd.Stderr = nil

	if outputHandler != nil {
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("create stdout pipe: %w", err)
		}
		p.stdout = stdout
	} else {
		cmd.Stdout = nil
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	p.cmd = cmd
	p.done = make(chan struct{})

	// Handle stdout if provided
	if outputHandler != nil && p.stdout != nil {
		go outputHandler(p.stdout)
	}

	// Single goroutine to wait for process completion
	go func(done chan struct{}, c *exec.Cmd) {
		err := c.Wait()
		close(done)
		if err != nil {
			p.logger.Debug().Err(err).Str("mount", p.mountID).Msg("gstreamer pipeline exited")
		} else {
			p.logger.Info().Str("mount", p.mountID).Msg("gstreamer pipeline stopped")
		}
	}(p.done, cmd)

	return nil
}

// StartWithDualOutput launches a pipeline that outputs to two streams (HQ and LQ).
// Uses extra file descriptors (fd=3 for HQ, fd=4 for LQ) to capture both outputs.
func (p *Pipeline) StartWithDualOutput(ctx context.Context, launch string, hqHandler, lqHandler func(io.Reader)) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil && p.done != nil {
		select {
		case <-p.done:
			// Previous process has exited, ok to start new one
		default:
			return fmt.Errorf("pipeline already running")
		}
	}

	// Create pipes for HQ (fd=3) and LQ (fd=4) outputs
	hqReader, hqWriter, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create HQ pipe: %w", err)
	}

	lqReader, lqWriter, err := os.Pipe()
	if err != nil {
		hqReader.Close()
		hqWriter.Close()
		return fmt.Errorf("create LQ pipe: %w", err)
	}

	// Use shell to properly parse the GStreamer pipeline string
	shellCmd := fmt.Sprintf("%s -e %s", p.cfg.GStreamerBin, launch)
	cmd := exec.CommandContext(ctx, "sh", "-c", shellCmd)
	cmd.Stderr = nil
	cmd.Stdout = nil

	// Pass extra file descriptors: fd=3 for HQ, fd=4 for LQ
	cmd.ExtraFiles = []*os.File{hqWriter, lqWriter}

	if err := cmd.Start(); err != nil {
		hqReader.Close()
		hqWriter.Close()
		lqReader.Close()
		lqWriter.Close()
		return fmt.Errorf("start pipeline: %w", err)
	}

	// Close write ends in parent process (GStreamer has them now)
	hqWriter.Close()
	lqWriter.Close()

	p.cmd = cmd
	p.done = make(chan struct{})

	// Handle HQ output
	if hqHandler != nil {
		go func() {
			hqHandler(hqReader)
			hqReader.Close()
		}()
	} else {
		hqReader.Close()
	}

	// Handle LQ output
	if lqHandler != nil {
		go func() {
			lqHandler(lqReader)
			lqReader.Close()
		}()
	} else {
		lqReader.Close()
	}

	// Single goroutine to wait for process completion
	go func(done chan struct{}, c *exec.Cmd) {
		err := c.Wait()
		close(done)
		if err != nil {
			p.logger.Debug().Err(err).Str("mount", p.mountID).Msg("gstreamer pipeline exited")
		} else {
			p.logger.Info().Str("mount", p.mountID).Msg("gstreamer pipeline stopped")
		}
	}(p.done, cmd)

	return nil
}

// Stop terminates the running pipeline.
func (p *Pipeline) Stop() error {
	p.mu.Lock()
	cmd := p.cmd
	done := p.done
	p.mu.Unlock()

	if cmd == nil || done == nil {
		return nil
	}

	// Check if already exited
	select {
	case <-done:
		return nil
	default:
	}

	// Send interrupt signal
	if cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
	}

	// Wait for process to exit or timeout
	select {
	case <-time.After(5 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done
	case <-done:
		// Process exited normally
	}

	return nil
}
