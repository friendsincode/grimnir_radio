package mediaengine

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/rs/zerolog"

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

	cmd             *exec.Cmd
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
func (pm *PipelineManager) CreatePipeline(ctx context.Context, stationID, mountID string, graph *dsp.Graph) (*Pipeline, error) {
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

	pipeline := &Pipeline{
		ID:           stationID + "-" + mountID,
		StationID:    stationID,
		MountID:      mountID,
		Graph:        graph,
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

	// Start GStreamer process
	// Note: In production, we'd use proper GStreamer bindings
	// For now, this is a placeholder showing the structure
	p.CurrentTrack = track
	p.State = pb.PlaybackState_PLAYBACK_STATE_PLAYING

	// TODO: Actually launch gst-launch process here
	// cmd := exec.CommandContext(ctx, p.cfg.GStreamerBin, pipelineStr)
	// p.cmd = cmd
	// if err := cmd.Start(); err != nil {
	//     return fmt.Errorf("failed to start pipeline: %w", err)
	// }

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
	if p.cmd != nil && p.cmd.Process != nil {
		if err := p.cmd.Process.Kill(); err != nil {
			p.logger.Error().Err(err).Msg("failed to kill pipeline process")
		}
		p.cmd = nil
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
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}

	track := &Track{
		SourceID:   source.SourceId,
		SourceType: source.Type,
		Path:       source.Path,
		StartedAt:  time.Now(),
		Metadata:   source.Metadata,
	}

	p.CurrentTrack = track
	p.State = pb.PlaybackState_PLAYBACK_STATE_PLAYING

	// TODO: Start emergency playback immediately
	// This should bypass all processing and go straight to output

	return nil
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

	data := &pb.TelemetryData{
		StationId:       p.StationID,
		MountId:         p.MountID,
		AudioLevelL:     p.telemetry.AudioLevelL,
		AudioLevelR:     p.telemetry.AudioLevelR,
		PeakLevelL:      p.telemetry.PeakLevelL,
		PeakLevelR:      p.telemetry.PeakLevelR,
		LoudnessLufs:    p.telemetry.LoudnessLUFS,
		MomentaryLufs:   p.telemetry.MomentaryLUFS,
		ShortTermLufs:   p.telemetry.ShortTermLUFS,
		BufferDepthMs:   p.telemetry.BufferDepthMS,
		BufferFillPercent: p.telemetry.BufferFillPct,
		UnderrunCount:   p.telemetry.UnderrunCount,
		State:           p.State,
	}

	if p.CurrentTrack != nil {
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

	// Add output (placeholder - would route to encoder/streamer)
	output := " ! autoaudiosink"

	pipeline := source + dspChain + output

	return pipeline, nil
}
