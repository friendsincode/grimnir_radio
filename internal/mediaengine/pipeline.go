package mediaengine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/friendsincode/grimnir_radio/internal/mediaengine/dsp"
	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// Pipeline represents an active GStreamer pipeline
type Pipeline struct {
	ID          string
	StationID   string
	MountID     string
	Graph       *dsp.Graph
	State       pb.PlaybackState
	CurrentTrack *Track
	NextTrack    *Track
	OutputConfig *EncoderConfig

	process         *GStreamerProcess
	crossfadeMgr    *CrossfadeManager
	mu              sync.RWMutex
	logger          zerolog.Logger
	cancelFunc      context.CancelFunc
	telemetry       *TelemetryCollector
}

// Track represents a media source being played
type Track struct {
	SourceID   string
	SourceType pb.SourceType
	Path       string
	CuePoints  *pb.CuePoints
	StartedAt  time.Time
	Position   time.Duration
	Duration   time.Duration
	Metadata   map[string]string
}

// TelemetryCollector collects real-time audio metrics
type TelemetryCollector struct {
	mu            sync.RWMutex
	AudioLevelL   float32
	AudioLevelR   float32
	PeakLevelL    float32
	PeakLevelR    float32
	LoudnessLUFS  float32
	MomentaryLUFS float32
	ShortTermLUFS float32
	BufferDepthMS int64
	BufferFillPct int32
	UnderrunCount int64
}

// PipelineManager manages GStreamer pipelines for media playback
type PipelineManager struct {
	cfg       *Config
	logger    zerolog.Logger
	pipelines map[string]*Pipeline // stationID -> pipeline
	mu        sync.RWMutex
}

// NewPipelineManager creates a new pipeline manager
func NewPipelineManager(cfg *Config, logger zerolog.Logger) *PipelineManager {
	return &PipelineManager{
		cfg:       cfg,
		logger:    logger,
		pipelines: make(map[string]*Pipeline),
	}
}

// CreatePipeline creates a new GStreamer pipeline for a station
func (pm *PipelineManager) CreatePipeline(ctx context.Context, stationID, mountID string, graph *dsp.Graph, outputConfig *EncoderConfig) (*Pipeline, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Check if pipeline already exists
	if existing, ok := pm.pipelines[stationID]; ok {
		pm.logger.Warn().
			Str("station_id", stationID).
			Str("state", existing.State.String()).
			Msg("pipeline already exists for station")
		return existing, nil
	}

	_, cancel := context.WithCancel(ctx)

	pipelineLogger := pm.logger.With().Str("pipeline_id", stationID+"-"+mountID).Logger()

	// Set default output config if not provided
	if outputConfig == nil {
		outputConfig = &EncoderConfig{
			OutputType: OutputTypeTest, // Use test sink by default
			Format:     AudioFormatMP3,
			Bitrate:    128,
			SampleRate: 44100,
			Channels:   2,
		}
	}

	pipeline := &Pipeline{
		ID:           stationID + "-" + mountID,
		StationID:    stationID,
		MountID:      mountID,
		Graph:        graph,
		OutputConfig: outputConfig,
		State:        pb.PlaybackState_PLAYBACK_STATE_IDLE,
		logger:       pipelineLogger,
		cancelFunc:   cancel,
		telemetry:    &TelemetryCollector{},
		crossfadeMgr: NewCrossfadeManager(stationID, mountID, pipelineLogger),
	}

	pm.pipelines[stationID] = pipeline

	pm.logger.Info().
		Str("station_id", stationID).
		Str("mount_id", mountID).
		Str("output_type", string(outputConfig.OutputType)).
		Str("format", string(outputConfig.Format)).
		Msg("created pipeline")

	return pipeline, nil
}

// GetPipeline retrieves an existing pipeline
func (pm *PipelineManager) GetPipeline(stationID string) (*Pipeline, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pipeline, ok := pm.pipelines[stationID]
	if !ok {
		return nil, fmt.Errorf("pipeline not found for station %s", stationID)
	}

	return pipeline, nil
}

// DestroyPipeline stops and removes a pipeline
func (pm *PipelineManager) DestroyPipeline(stationID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pipeline, ok := pm.pipelines[stationID]
	if !ok {
		return fmt.Errorf("pipeline not found for station %s", stationID)
	}

	// Stop the pipeline
	if err := pipeline.Stop(); err != nil {
		pm.logger.Error().Err(err).Str("station_id", stationID).Msg("failed to stop pipeline")
	}

	delete(pm.pipelines, stationID)

	pm.logger.Info().Str("station_id", stationID).Msg("destroyed pipeline")

	return nil
}

// Play starts playback of a source
func (p *Pipeline) Play(ctx context.Context, source *pb.SourceConfig, cuePoints *pb.CuePoints) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.State == pb.PlaybackState_PLAYBACK_STATE_PLAYING {
		p.logger.Warn().Msg("pipeline already playing")
		return fmt.Errorf("pipeline already playing")
	}

	track := &Track{
		SourceID:   source.SourceId,
		SourceType: source.Type,
		Path:       source.Path,
		CuePoints:  cuePoints,
		StartedAt:  time.Now(),
		Metadata:   source.Metadata,
	}

	// Build GStreamer pipeline command
	pipelineStr, err := p.buildPlaybackPipeline(track)
	if err != nil {
		return fmt.Errorf("failed to build pipeline: %w", err)
	}

	p.logger.Info().
		Str("source_id", track.SourceID).
		Str("pipeline", pipelineStr).
		Msg("starting playback")

	// Create GStreamer process
	processID := fmt.Sprintf("%s-%s-playback", p.StationID, p.MountID)
	p.process = NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       processID,
		Pipeline: pipelineStr,
		LogLevel: "info",
		OnStateChange: func(state ProcessState) {
			p.logger.Debug().
				Str("process_state", string(state)).
				Msg("playback process state changed")
		},
		OnTelemetry: func(gstTelem *GStreamerTelemetry) {
			// Update pipeline telemetry from GStreamer output
			p.telemetry.mu.Lock()
			p.telemetry.AudioLevelL = gstTelem.AudioLevelL
			p.telemetry.AudioLevelR = gstTelem.AudioLevelR
			p.telemetry.PeakLevelL = gstTelem.PeakLevelL
			p.telemetry.PeakLevelR = gstTelem.PeakLevelR
			p.telemetry.BufferDepthMS = gstTelem.BufferDepthMS
			p.telemetry.BufferFillPct = gstTelem.BufferFillPct
			p.telemetry.UnderrunCount = gstTelem.UnderrunCount
			p.telemetry.mu.Unlock()

			// Update track position
			if p.CurrentTrack != nil {
				p.CurrentTrack.Position = gstTelem.CurrentPosition
			}
		},
		OnExit: func(exitCode int, err error) {
			if err != nil {
				p.logger.Error().
					Err(err).
					Int("exit_code", exitCode).
					Msg("playback process exited with error")

				// Update state to idle on error
				p.mu.Lock()
				p.State = pb.PlaybackState_PLAYBACK_STATE_IDLE
				p.mu.Unlock()
			} else {
				p.logger.Info().Msg("playback process completed normally")
			}
		},
	}, p.logger)

	// Start the process
	if err := p.process.Start(pipelineStr); err != nil {
		return fmt.Errorf("failed to start playback process: %w", err)
	}

	p.CurrentTrack = track
	p.State = pb.PlaybackState_PLAYBACK_STATE_PLAYING

	p.logger.Info().
		Int("pid", p.process.GetPID()).
		Msg("playback started successfully")

	return nil
}

// Stop stops playback
func (p *Pipeline) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.State == pb.PlaybackState_PLAYBACK_STATE_IDLE {
		return nil
	}

	p.logger.Info().Msg("stopping playback")

	// Stop crossfade mixer if running
	if p.crossfadeMgr != nil {
		if err := p.crossfadeMgr.Stop(); err != nil {
			p.logger.Error().Err(err).Msg("failed to stop crossfade manager")
		}
	}

	// Stop GStreamer process
	if p.process != nil {
		if err := p.process.Stop(); err != nil {
			p.logger.Error().Err(err).Msg("failed to stop playback process gracefully")
			// Try force kill
			if killErr := p.process.Kill(); killErr != nil {
				p.logger.Error().Err(killErr).Msg("failed to kill playback process")
			}
		}
		p.process = nil
	}

	p.CurrentTrack = nil
	p.NextTrack = nil
	p.State = pb.PlaybackState_PLAYBACK_STATE_IDLE

	return nil
}

// Fade initiates a crossfade to a new source
func (p *Pipeline) Fade(ctx context.Context, nextSource *pb.SourceConfig, cuePoints *pb.CuePoints, fadeConfig *pb.FadeConfig) error {
	p.mu.Lock()
	currentTrack := p.CurrentTrack
	p.mu.Unlock()

	if currentTrack == nil {
		return fmt.Errorf("cannot fade when no track is playing")
	}

	if p.State != pb.PlaybackState_PLAYBACK_STATE_PLAYING {
		return fmt.Errorf("cannot fade when not playing (state: %v)", p.State)
	}

	nextTrack := &Track{
		SourceID:   nextSource.SourceId,
		SourceType: nextSource.Type,
		Path:       nextSource.Path,
		CuePoints:  cuePoints,
		Metadata:   nextSource.Metadata,
	}

	p.logger.Info().
		Str("current_source", currentTrack.SourceID).
		Str("next_source", nextTrack.SourceID).
		Int32("fade_in_ms", fadeConfig.FadeInMs).
		Int32("fade_out_ms", fadeConfig.FadeOutMs).
		Msg("starting crossfade")

	// Update pipeline state
	p.mu.Lock()
	p.NextTrack = nextTrack
	p.State = pb.PlaybackState_PLAYBACK_STATE_FADING
	p.mu.Unlock()

	// Use CrossfadeManager for actual crossfade with cue-point awareness
	if err := p.crossfadeMgr.StartCrossfade(ctx, currentTrack, nextTrack, fadeConfig); err != nil {
		p.mu.Lock()
		p.State = pb.PlaybackState_PLAYBACK_STATE_PLAYING // Revert state on error
		p.NextTrack = nil
		p.mu.Unlock()
		return fmt.Errorf("crossfade failed: %w", err)
	}

	// Monitor crossfade completion and update pipeline state
	go p.monitorCrossfadeCompletion()

	return nil
}

// monitorCrossfadeCompletion waits for crossfade to finish and updates state
func (p *Pipeline) monitorCrossfadeCompletion() {
	// Poll fade state until complete
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			state := p.crossfadeMgr.GetFadeState()
			if state == FadeStateIdle {
				// Crossfade completed
				p.mu.Lock()
				p.CurrentTrack = p.crossfadeMgr.GetCurrentTrack()
				p.NextTrack = nil
				p.State = pb.PlaybackState_PLAYBACK_STATE_PLAYING
				p.mu.Unlock()

				p.logger.Info().
					Str("current_track", p.CurrentTrack.SourceID).
					Msg("crossfade completed, transition successful")
				return
			}
		}
	}
}

// InsertEmergency immediately preempts current playback
func (p *Pipeline) InsertEmergency(ctx context.Context, source *pb.SourceConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.logger.Warn().
		Str("emergency_source", source.SourceId).
		Msg("inserting emergency broadcast")

	// Stop current playback immediately
	if p.process != nil {
		// Force kill for immediate preemption
		if err := p.process.Kill(); err != nil {
			p.logger.Error().Err(err).Msg("failed to kill current process")
		}
		p.process = nil
	}

	// Stop crossfade mixer if running
	if p.crossfadeMgr != nil {
		if err := p.crossfadeMgr.Stop(); err != nil {
			p.logger.Error().Err(err).Msg("failed to stop crossfade during emergency")
		}
	}

	track := &Track{
		SourceID:   source.SourceId,
		SourceType: source.Type,
		Path:       source.Path,
		StartedAt:  time.Now(),
		Metadata:   source.Metadata,
	}

	// Build emergency pipeline (bypasses DSP for minimal latency)
	pipelineStr, err := p.buildEmergencyPipeline(track)
	if err != nil {
		return fmt.Errorf("failed to build emergency pipeline: %w", err)
	}

	// Create emergency GStreamer process
	processID := fmt.Sprintf("%s-%s-emergency", p.StationID, p.MountID)
	p.process = NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       processID,
		Pipeline: pipelineStr,
		LogLevel: "warning", // Minimal logging for emergency
		OnStateChange: func(state ProcessState) {
			p.logger.Info().
				Str("process_state", string(state)).
				Msg("emergency process state changed")
		},
		OnExit: func(exitCode int, err error) {
			if err != nil {
				p.logger.Error().
					Err(err).
					Int("exit_code", exitCode).
					Msg("emergency process exited with error")
			} else {
				p.logger.Info().Msg("emergency broadcast completed")
			}

			// Return to idle state after emergency
			p.mu.Lock()
			p.State = pb.PlaybackState_PLAYBACK_STATE_IDLE
			p.CurrentTrack = nil
			p.mu.Unlock()
		},
	}, p.logger)

	// Start emergency playback immediately
	if err := p.process.Start(pipelineStr); err != nil {
		return fmt.Errorf("failed to start emergency playback: %w", err)
	}

	p.CurrentTrack = track
	p.State = pb.PlaybackState_PLAYBACK_STATE_PLAYING

	p.logger.Warn().
		Int("pid", p.process.GetPID()).
		Msg("emergency broadcast started")

	return nil
}

// buildEmergencyPipeline builds a minimal-latency pipeline for emergency broadcasts
func (p *Pipeline) buildEmergencyPipeline(track *Track) (string, error) {
	var source string

	switch track.SourceType {
	case pb.SourceType_SOURCE_TYPE_MEDIA:
		source = fmt.Sprintf("filesrc location=%s ! decodebin", track.Path)
	case pb.SourceType_SOURCE_TYPE_WEBSTREAM:
		source = fmt.Sprintf("souphttpsrc location=%s ! decodebin", track.Path)
	default:
		return "", fmt.Errorf("unsupported emergency source type: %v", track.SourceType)
	}

	// Emergency pipeline bypasses DSP for minimal latency
	// Still needs encoder/output to stream
	encoder := NewEncoderBuilder(*p.OutputConfig)
	outputChain, err := encoder.Build()
	if err != nil {
		return "", fmt.Errorf("build emergency output: %w", err)
	}

	pipeline := source + " ! " + outputChain

	return pipeline, nil
}

// RouteLive routes a live input stream
func (p *Pipeline) RouteLive(ctx context.Context, input *pb.LiveInputConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.logger.Info().
		Str("input_url", input.InputUrl).
		Bool("apply_processing", input.ApplyProcessing).
		Msg("routing live input")

	track := &Track{
		SourceID:   "live-" + input.InputUrl,
		SourceType: pb.SourceType_SOURCE_TYPE_LIVE,
		Path:       input.InputUrl,
		StartedAt:  time.Now(),
	}

	p.CurrentTrack = track
	p.State = pb.PlaybackState_PLAYBACK_STATE_PLAYING

	// TODO: Implement live input routing
	// 1. Use souphttpsrc or tcpserversrc for input
	// 2. Apply DSP graph if input.ApplyProcessing is true
	// 3. Route to output

	return nil
}

// GetTelemetry returns current telemetry data
func (p *Pipeline) GetTelemetry() *pb.TelemetryData {
	p.mu.RLock()
	defer p.mu.RUnlock()

	p.telemetry.mu.RLock()
	defer p.telemetry.mu.RUnlock()

	// Get current timestamp
	now := time.Now()
	timestamp := &timestamppb.Timestamp{
		Seconds: now.Unix(),
		Nanos:   int32(now.Nanosecond()),
	}

	data := &pb.TelemetryData{
		StationId:         p.StationID,
		MountId:           p.MountID,
		Timestamp:         timestamp,
		AudioLevelL:       p.telemetry.AudioLevelL,
		AudioLevelR:       p.telemetry.AudioLevelR,
		PeakLevelL:        p.telemetry.PeakLevelL,
		PeakLevelR:        p.telemetry.PeakLevelR,
		LoudnessLufs:      p.telemetry.LoudnessLUFS,
		MomentaryLufs:     p.telemetry.MomentaryLUFS,
		ShortTermLufs:     p.telemetry.ShortTermLUFS,
		BufferDepthMs:     p.telemetry.BufferDepthMS,
		BufferFillPercent: p.telemetry.BufferFillPct,
		UnderrunCount:     p.telemetry.UnderrunCount,
		State:             p.State,
	}

	// Add current track position and duration
	if p.CurrentTrack != nil {
		// Update position from GStreamer if available
		if p.process != nil {
			gstTelem := p.process.GetTelemetry()
			if gstTelem.CurrentPosition > 0 {
				p.CurrentTrack.Position = gstTelem.CurrentPosition
			}
		}

		data.PositionMs = int64(p.CurrentTrack.Position.Milliseconds())
		data.DurationMs = int64(p.CurrentTrack.Duration.Milliseconds())
	}

	return data
}

// buildPlaybackPipeline constructs a GStreamer pipeline for playback
func (p *Pipeline) buildPlaybackPipeline(track *Track) (string, error) {
	var source string

	switch track.SourceType {
	case pb.SourceType_SOURCE_TYPE_MEDIA:
		// File playback
		source = fmt.Sprintf("filesrc location=%s ! decodebin", track.Path)
	case pb.SourceType_SOURCE_TYPE_WEBSTREAM:
		// HTTP stream
		source = fmt.Sprintf("souphttpsrc location=%s ! decodebin", track.Path)
	case pb.SourceType_SOURCE_TYPE_LIVE:
		// Live input
		source = fmt.Sprintf("tcpserversrc port=8001 ! decodebin")
	default:
		return "", fmt.Errorf("unsupported source type: %v", track.SourceType)
	}

	// Add DSP graph processing
	var dspChain string
	if p.Graph != nil && p.Graph.Pipeline != "" {
		dspChain = " ! " + p.Graph.Pipeline
	}

	// Build encoder and output using EncoderBuilder
	encoder := NewEncoderBuilder(*p.OutputConfig)
	if err := encoder.ValidateConfig(); err != nil {
		return "", fmt.Errorf("invalid encoder config: %w", err)
	}

	outputChain, err := encoder.Build()
	if err != nil {
		return "", fmt.Errorf("build encoder output: %w", err)
	}

	pipeline := source + dspChain + " ! " + outputChain

	return pipeline, nil
}
