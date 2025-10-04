package playout

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

    "github.com/example/grimnirradio/internal/config"
	"github.com/rs/zerolog"
)

// Pipeline manages a GStreamer-based playout process for a mount.
type Pipeline struct {
	cfg    *config.Config
	logger zerolog.Logger

	mu      sync.Mutex
	cmd     *exec.Cmd
	mountID string
}

// NewPipeline constructs a pipeline for a mount.
func NewPipeline(cfg *config.Config, mountID string, logger zerolog.Logger) *Pipeline {
	return &Pipeline{cfg: cfg, mountID: mountID, logger: logger}
}

// Start launches the gst pipeline with the provided launch string.
func (p *Pipeline) Start(ctx context.Context, launch string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil && p.cmd.ProcessState == nil {
		return fmt.Errorf("pipeline already running")
	}

	cmd := exec.CommandContext(ctx, p.cfg.GStreamerBin, "-e", launch)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return err
	}

	p.cmd = cmd
	go func() {
		err := cmd.Wait()
		if err != nil {
			p.logger.Error().Err(err).Str("mount", p.mountID).Msg("gstreamer pipeline exited")
		} else {
			p.logger.Info().Str("mount", p.mountID).Msg("gstreamer pipeline stopped")
		}
	}()

	return nil
}

// Stop terminates the running pipeline.
func (p *Pipeline) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd == nil || p.cmd.ProcessState != nil {
		return nil
	}

	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()

	select {
	case <-time.After(5 * time.Second):
		if err := p.cmd.Process.Kill(); err != nil {
			return err
		}
		<-done
	case err := <-done:
		if err != nil {
			return err
		}
	}

	p.cmd = nil
	return nil
}
