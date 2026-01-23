package mediaengine

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// GStreamerProcess manages a single GStreamer process with monitoring
type GStreamerProcess struct {
	id       string
	cmd      *exec.Cmd
	logger   zerolog.Logger
	ctx      context.Context
	cancel   context.CancelFunc

	// Process state
	mu         sync.RWMutex
	state      ProcessState
	startTime  time.Time
	exitCode   int
	exitError  error

	// Output monitoring
	stdout     io.ReadCloser
	stderr     io.ReadCloser
	outputDone chan struct{}

	// Telemetry extracted from GStreamer output
	telemetry  *GStreamerTelemetry

	// Callbacks
	onStateChange func(ProcessState)
	onTelemetry   func(*GStreamerTelemetry)
	onExit        func(exitCode int, err error)
}

// ProcessState represents the current state of a GStreamer process
type ProcessState string

const (
	ProcessStateIdle     ProcessState = "idle"
	ProcessStateStarting ProcessState = "starting"
	ProcessStateRunning  ProcessState = "running"
	ProcessStateStopping ProcessState = "stopping"
	ProcessStateStopped  ProcessState = "stopped"
	ProcessStateFailed   ProcessState = "failed"
)

// GStreamerTelemetry contains metrics extracted from GStreamer output
type GStreamerTelemetry struct {
	mu sync.RWMutex

	// Audio levels (from level element)
	AudioLevelL   float32
	AudioLevelR   float32
	PeakLevelL    float32
	PeakLevelR    float32

	// Buffer status
	BufferFillPct int32
	BufferDepthMS int64

	// Errors and warnings
	UnderrunCount int64
	OverrunCount  int64
	LastWarning   string
	LastError     string

	// State
	CurrentPosition time.Duration
	PipelineState   string // NULL, READY, PAUSED, PLAYING

	// Performance
	CPUPercent    float32
	MemoryMB      int64
}

// GStreamerProcessConfig contains configuration for launching GStreamer
type GStreamerProcessConfig struct {
	ID              string
	Pipeline        string
	LogLevel        string // "none", "error", "warning", "info", "debug"
	OnStateChange   func(ProcessState)
	OnTelemetry     func(*GStreamerTelemetry)
	OnExit          func(exitCode int, err error)
}

// Regular expressions for parsing GStreamer output
var (
	// State changes: "Setting pipeline to PAUSED"
	stateChangeRegex = regexp.MustCompile(`Setting pipeline to (\w+)`)

	// Audio levels from level element: "level0: rms=0.123456, peak=0.234567"
	audioLevelRegex = regexp.MustCompile(`level.*?rms=([-0-9.]+).*?peak=([-0-9.]+)`)

	// Buffer status: "queue name=buffer0 ... current-level-buffers=50 max-size-buffers=200"
	bufferStatusRegex = regexp.MustCompile(`queue.*?current-level-buffers=(\d+).*?max-size-buffers=(\d+)`)

	// Position: "0:00:23.456789"
	positionRegex = regexp.MustCompile(`(\d+):(\d+):(\d+)\.(\d+)`)

	// Errors: "ERROR: from element"
	errorRegex = regexp.MustCompile(`ERROR:(.+)`)

	// Warnings: "WARNING: from element"
	warningRegex = regexp.MustCompile(`WARNING:(.+)`)

	// Underruns: "queue is empty"
	underrunRegex = regexp.MustCompile(`queue.*?is empty|underrun`)
)

// NewGStreamerProcess creates a new GStreamer process manager
func NewGStreamerProcess(ctx context.Context, cfg GStreamerProcessConfig, logger zerolog.Logger) *GStreamerProcess {
	procCtx, cancel := context.WithCancel(ctx)

	return &GStreamerProcess{
		id:            cfg.ID,
		logger:        logger.With().Str("gst_process", cfg.ID).Logger(),
		ctx:           procCtx,
		cancel:        cancel,
		state:         ProcessStateIdle,
		telemetry:     &GStreamerTelemetry{},
		outputDone:    make(chan struct{}),
		onStateChange: cfg.OnStateChange,
		onTelemetry:   cfg.OnTelemetry,
		onExit:        cfg.OnExit,
	}
}

// Start launches the GStreamer process
func (gp *GStreamerProcess) Start(pipeline string) error {
	gp.mu.Lock()
	defer gp.mu.Unlock()

	if gp.state != ProcessStateIdle {
		return fmt.Errorf("process already started (state: %s)", gp.state)
	}

	gp.setState(ProcessStateStarting)

	// Build gst-launch command
	// Use -v for verbose output (includes state changes and debug info)
	// Use -m for messages (includes errors and warnings)
	args := []string{"-v", "-m"}

	// Split pipeline into arguments
	// Note: This is a simplified approach. In production, consider using
	// proper shell parsing or just pass the whole pipeline as one arg
	pipelineArgs := strings.Fields(pipeline)
	args = append(args, pipelineArgs...)

	gp.cmd = exec.CommandContext(gp.ctx, "gst-launch-1.0", args...)

	// Capture stdout and stderr
	var err error
	gp.stdout, err = gp.cmd.StdoutPipe()
	if err != nil {
		gp.setState(ProcessStateFailed)
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	gp.stderr, err = gp.cmd.StderrPipe()
	if err != nil {
		gp.setState(ProcessStateFailed)
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := gp.cmd.Start(); err != nil {
		gp.setState(ProcessStateFailed)
		return fmt.Errorf("failed to start gst-launch: %w", err)
	}

	gp.startTime = time.Now()
	gp.setState(ProcessStateRunning)

	gp.logger.Info().
		Int("pid", gp.cmd.Process.Pid).
		Str("pipeline", pipeline).
		Msg("GStreamer process started")

	// Start output monitoring goroutines
	go gp.monitorStdout()
	go gp.monitorStderr()
	go gp.monitorProcess()

	return nil
}

// Stop gracefully stops the GStreamer process
func (gp *GStreamerProcess) Stop() error {
	gp.mu.Lock()

	if gp.state == ProcessStateStopped || gp.state == ProcessStateFailed {
		gp.mu.Unlock()
		return nil
	}

	gp.setState(ProcessStateStopping)
	gp.mu.Unlock()

	gp.logger.Info().Msg("stopping GStreamer process")

	// Send termination signal
	if gp.cmd != nil && gp.cmd.Process != nil {
		// Try graceful termination first (SIGTERM)
		if err := gp.cmd.Process.Signal(os.Interrupt); err != nil {
			gp.logger.Warn().Err(err).Msg("failed to send interrupt signal")
		}

		// Wait for graceful shutdown with timeout
		done := make(chan error, 1)
		go func() {
			done <- gp.cmd.Wait()
		}()

		select {
		case <-time.After(5 * time.Second):
			// Timeout - force kill
			gp.logger.Warn().Msg("graceful shutdown timeout, force killing")
			if err := gp.cmd.Process.Kill(); err != nil {
				gp.logger.Error().Err(err).Msg("failed to kill process")
				return err
			}
		case err := <-done:
			if err != nil {
				gp.logger.Debug().Err(err).Msg("process exited with error")
			}
		}
	}

	// Cancel context to stop monitoring goroutines
	gp.cancel()

	// Wait for output monitoring to complete
	<-gp.outputDone

	gp.mu.Lock()
	gp.setState(ProcessStateStopped)
	gp.mu.Unlock()

	gp.logger.Info().Msg("GStreamer process stopped")

	return nil
}

// Kill forcefully terminates the GStreamer process
func (gp *GStreamerProcess) Kill() error {
	gp.mu.Lock()
	defer gp.mu.Unlock()

	if gp.cmd != nil && gp.cmd.Process != nil {
		gp.logger.Warn().Msg("force killing GStreamer process")

		if err := gp.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process: %w", err)
		}
	}

	gp.setState(ProcessStateStopped)
	gp.cancel()

	return nil
}

// GetState returns the current process state
func (gp *GStreamerProcess) GetState() ProcessState {
	gp.mu.RLock()
	defer gp.mu.RUnlock()
	return gp.state
}

// GetPID returns the process ID
func (gp *GStreamerProcess) GetPID() int {
	gp.mu.RLock()
	defer gp.mu.RUnlock()

	if gp.cmd != nil && gp.cmd.Process != nil {
		return gp.cmd.Process.Pid
	}
	return 0
}

// GetTelemetry returns a copy of the current telemetry
func (gp *GStreamerProcess) GetTelemetry() *GStreamerTelemetry {
	gp.telemetry.mu.RLock()
	defer gp.telemetry.mu.RUnlock()

	// Return a copy to avoid race conditions
	return &GStreamerTelemetry{
		AudioLevelL:     gp.telemetry.AudioLevelL,
		AudioLevelR:     gp.telemetry.AudioLevelR,
		PeakLevelL:      gp.telemetry.PeakLevelL,
		PeakLevelR:      gp.telemetry.PeakLevelR,
		BufferFillPct:   gp.telemetry.BufferFillPct,
		BufferDepthMS:   gp.telemetry.BufferDepthMS,
		UnderrunCount:   gp.telemetry.UnderrunCount,
		OverrunCount:    gp.telemetry.OverrunCount,
		LastWarning:     gp.telemetry.LastWarning,
		LastError:       gp.telemetry.LastError,
		CurrentPosition: gp.telemetry.CurrentPosition,
		PipelineState:   gp.telemetry.PipelineState,
		CPUPercent:      gp.telemetry.CPUPercent,
		MemoryMB:        gp.telemetry.MemoryMB,
	}
}

// GetUptime returns how long the process has been running
func (gp *GStreamerProcess) GetUptime() time.Duration {
	gp.mu.RLock()
	defer gp.mu.RUnlock()

	if gp.startTime.IsZero() {
		return 0
	}
	return time.Since(gp.startTime)
}

// Internal methods

func (gp *GStreamerProcess) setState(state ProcessState) {
	gp.state = state
	gp.logger.Debug().Str("state", string(state)).Msg("process state changed")

	if gp.onStateChange != nil {
		go gp.onStateChange(state)
	}
}

func (gp *GStreamerProcess) monitorStdout() {
	scanner := bufio.NewScanner(gp.stdout)

	for scanner.Scan() {
		line := scanner.Text()
		gp.parseOutputLine(line, "stdout")
	}

	if err := scanner.Err(); err != nil {
		gp.logger.Error().Err(err).Msg("error reading stdout")
	}
}

func (gp *GStreamerProcess) monitorStderr() {
	defer close(gp.outputDone)

	scanner := bufio.NewScanner(gp.stderr)

	for scanner.Scan() {
		line := scanner.Text()
		gp.parseOutputLine(line, "stderr")
	}

	if err := scanner.Err(); err != nil {
		gp.logger.Error().Err(err).Msg("error reading stderr")
	}
}

func (gp *GStreamerProcess) monitorProcess() {
	err := gp.cmd.Wait()

	gp.mu.Lock()
	if err != nil {
		gp.exitError = err
		if exitErr, ok := err.(*exec.ExitError); ok {
			gp.exitCode = exitErr.ExitCode()
		} else {
			gp.exitCode = 1
		}
		gp.setState(ProcessStateFailed)
		gp.logger.Error().
			Err(err).
			Int("exit_code", gp.exitCode).
			Msg("GStreamer process exited with error")
	} else {
		gp.exitCode = 0
		gp.setState(ProcessStateStopped)
		gp.logger.Info().Msg("GStreamer process exited normally")
	}
	gp.mu.Unlock()

	if gp.onExit != nil {
		go gp.onExit(gp.exitCode, err)
	}
}

func (gp *GStreamerProcess) parseOutputLine(line, source string) {
	// Log the line at trace level for debugging
	gp.logger.Trace().
		Str("source", source).
		Str("line", line).
		Msg("gst output")

	// Parse state changes
	if matches := stateChangeRegex.FindStringSubmatch(line); matches != nil {
		gp.telemetry.mu.Lock()
		gp.telemetry.PipelineState = matches[1]
		gp.telemetry.mu.Unlock()

		gp.logger.Debug().
			Str("pipeline_state", matches[1]).
			Msg("GStreamer pipeline state changed")
	}

	// Parse audio levels
	if matches := audioLevelRegex.FindStringSubmatch(line); matches != nil {
		rms, _ := strconv.ParseFloat(matches[1], 32)
		peak, _ := strconv.ParseFloat(matches[2], 32)

		gp.telemetry.mu.Lock()
		// Convert RMS dB to linear (approximate)
		gp.telemetry.AudioLevelL = float32(rms)
		gp.telemetry.PeakLevelL = float32(peak)
		gp.telemetry.mu.Unlock()

		// Notify callback
		if gp.onTelemetry != nil {
			go gp.onTelemetry(gp.GetTelemetry())
		}
	}

	// Parse buffer status
	if matches := bufferStatusRegex.FindStringSubmatch(line); matches != nil {
		current, _ := strconv.Atoi(matches[1])
		max, _ := strconv.Atoi(matches[2])

		gp.telemetry.mu.Lock()
		if max > 0 {
			gp.telemetry.BufferFillPct = int32((current * 100) / max)
		}
		gp.telemetry.mu.Unlock()
	}

	// Parse position
	if matches := positionRegex.FindStringSubmatch(line); matches != nil {
		hours, _ := strconv.Atoi(matches[1])
		minutes, _ := strconv.Atoi(matches[2])
		seconds, _ := strconv.Atoi(matches[3])

		duration := time.Duration(hours)*time.Hour +
			time.Duration(minutes)*time.Minute +
			time.Duration(seconds)*time.Second

		gp.telemetry.mu.Lock()
		gp.telemetry.CurrentPosition = duration
		gp.telemetry.mu.Unlock()
	}

	// Parse errors
	if matches := errorRegex.FindStringSubmatch(line); matches != nil {
		gp.telemetry.mu.Lock()
		gp.telemetry.LastError = strings.TrimSpace(matches[1])
		gp.telemetry.mu.Unlock()

		gp.logger.Error().
			Str("error", matches[1]).
			Msg("GStreamer error")
	}

	// Parse warnings
	if matches := warningRegex.FindStringSubmatch(line); matches != nil {
		gp.telemetry.mu.Lock()
		gp.telemetry.LastWarning = strings.TrimSpace(matches[1])
		gp.telemetry.mu.Unlock()

		gp.logger.Warn().
			Str("warning", matches[1]).
			Msg("GStreamer warning")
	}

	// Detect underruns
	if underrunRegex.MatchString(line) {
		gp.telemetry.mu.Lock()
		gp.telemetry.UnderrunCount++
		gp.telemetry.mu.Unlock()

		gp.logger.Warn().
			Int64("underrun_count", gp.telemetry.UnderrunCount).
			Msg("buffer underrun detected")
	}
}
