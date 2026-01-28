/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package mediaengine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/friendsincode/grimnir_radio/internal/mediaengine/dsp"
	"github.com/friendsincode/grimnir_radio/internal/telemetry"
	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// Service implements the MediaEngine gRPC service.
type Service struct {
	pb.UnimplementedMediaEngineServer

	cfg             *Config
	logger          zerolog.Logger
	pipelineManager *PipelineManager
	dspBuilder      *dsp.Builder
	supervisor      *Supervisor
	liveInputMgr    *LiveInputManager
	webstreamMgr    *WebstreamManager
	analyzer        *Analyzer

	mu       sync.RWMutex
	stations map[string]*StationEngine // station_id -> engine
	graphs   map[string]*dsp.Graph    // graph_handle -> graph
	uptime   time.Time
}

// Config contains media engine configuration.
type Config struct {
	GRPCBind     string
	GRPCPort     int
	LogLevel     string
	GStreamerBin string
}

// StationEngine manages playback for a single station.
type StationEngine struct {
	StationID     string
	MountID       string
	State         pb.PlaybackState
	CurrentSource *pb.SourceConfig
	GraphHandle   string
	StartedAt     time.Time
	Position      int64
	Duration      int64

	// Telemetry
	AudioLevelL   float32
	AudioLevelR   float32
	LoudnessLUFS  float32
	BufferDepthMS int64
	UnderrunCount int64

	// Internal state
	mu sync.RWMutex
}

// New creates a new media engine service.
func New(cfg *Config, logger zerolog.Logger) *Service {
	pipelineManager := NewPipelineManager(cfg, logger)
	supervisor := NewSupervisor(cfg, logger, pipelineManager)
	liveInputMgr := NewLiveInputManager(logger)
	webstreamMgr := NewWebstreamManager(logger)

	svc := &Service{
		cfg:             cfg,
		logger:          logger,
		pipelineManager: pipelineManager,
		dspBuilder:      dsp.NewBuilder(logger),
		supervisor:      supervisor,
		liveInputMgr:    liveInputMgr,
		webstreamMgr:    webstreamMgr,
		analyzer:        NewAnalyzer(logger),
		stations:        make(map[string]*StationEngine),
		graphs:          make(map[string]*dsp.Graph),
		uptime:          time.Now(),
	}

	// Start supervisor
	supervisor.Start()

	return svc
}

// LoadGraph loads a DSP processing graph configuration.
func (s *Service) LoadGraph(ctx context.Context, req *pb.LoadGraphRequest) (*pb.LoadGraphResponse, error) {
	startTime := time.Now()

	s.logger.Info().
		Str("station_id", req.StationId).
		Str("mount_id", req.MountId).
		Int("node_count", len(req.Graph.Nodes)).
		Msg("loading DSP graph")

	// Build DSP graph from protobuf
	graph, err := s.dspBuilder.Build(req.Graph)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to build DSP graph")
		telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "load_graph", "failure").Inc()
		return &pb.LoadGraphResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to build graph: %v", err),
		}, nil
	}

	graphHandle := uuid.NewString()
	graph.ID = graphHandle

	s.mu.Lock()
	s.graphs[graphHandle] = graph
	engine := s.getOrCreateStation(req.StationId, req.MountId)
	engine.GraphHandle = graphHandle
	s.mu.Unlock()

	// Create pipeline with graph
	// TODO: Get output config from request
	// For now, use default (test sink)
	_, err = s.pipelineManager.CreatePipeline(ctx, req.StationId, req.MountId, graph, nil)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to create pipeline")
		telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "load_graph", "failure").Inc()
		return &pb.LoadGraphResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create pipeline: %v", err),
		}, nil
	}

	// Start monitoring pipeline
	s.supervisor.MonitorPipeline(req.StationId)

	s.logger.Info().
		Str("graph_handle", graphHandle).
		Str("pipeline", graph.Pipeline).
		Msg("DSP graph loaded successfully")

	// Track metrics
	duration := time.Since(startTime).Seconds()
	telemetry.MediaEngineOperationDuration.WithLabelValues(req.StationId, req.MountId, "load_graph").Observe(duration)
	telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "load_graph", "success").Inc()
	telemetry.MediaEngineActivePipelines.WithLabelValues(req.StationId, req.MountId).Set(1)

	return &pb.LoadGraphResponse{
		GraphHandle: graphHandle,
		Success:     true,
	}, nil
}

// Play starts playback of a media source.
func (s *Service) Play(ctx context.Context, req *pb.PlayRequest) (*pb.PlayResponse, error) {
	startTime := time.Now()

	s.logger.Info().
		Str("station_id", req.StationId).
		Str("mount_id", req.MountId).
		Str("source_type", req.Source.Type.String()).
		Str("source_id", req.Source.SourceId).
		Msg("starting playback")

	// Get pipeline
	pipeline, err := s.pipelineManager.GetPipeline(req.StationId)
	if err != nil {
		s.logger.Error().Err(err).Msg("pipeline not found")
		telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "play", "failure").Inc()
		return &pb.PlayResponse{
			Success: false,
			Error:   fmt.Sprintf("pipeline not found: %v", err),
		}, nil
	}

	// Start playback
	if err := pipeline.Play(ctx, req.Source, req.CuePoints); err != nil {
		s.logger.Error().Err(err).Msg("failed to start playback")
		telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "play", "failure").Inc()
		return &pb.PlayResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to start playback: %v", err),
		}, nil
	}

	// Update station engine state
	s.mu.Lock()
	engine := s.getOrCreateStation(req.StationId, req.MountId)
	engine.mu.Lock()
	oldState := engine.State
	engine.State = pb.PlaybackState_PLAYBACK_STATE_PLAYING
	engine.CurrentSource = req.Source
	engine.StartedAt = time.Now()
	engine.mu.Unlock()
	s.mu.Unlock()

	playbackID := uuid.NewString()

	// Track metrics
	duration := time.Since(startTime).Seconds()
	telemetry.MediaEngineOperationDuration.WithLabelValues(req.StationId, req.MountId, "play").Observe(duration)
	telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "play", "success").Inc()

	// Track state change
	if oldState != pb.PlaybackState_PLAYBACK_STATE_PLAYING {
		telemetry.MediaEnginePlaybackState.WithLabelValues(req.StationId, req.MountId).Set(float64(pb.PlaybackState_PLAYBACK_STATE_PLAYING))
	}

	return &pb.PlayResponse{
		Success:    true,
		PlaybackId: playbackID,
	}, nil
}

// Stop halts playback.
func (s *Service) Stop(ctx context.Context, req *pb.StopRequest) (*pb.StopResponse, error) {
	startTime := time.Now()

	s.logger.Info().
		Str("station_id", req.StationId).
		Str("mount_id", req.MountId).
		Bool("immediate", req.Immediate).
		Msg("stopping playback")

	// Get pipeline
	pipeline, err := s.pipelineManager.GetPipeline(req.StationId)
	if err != nil {
		s.logger.Warn().Err(err).Msg("pipeline not found")
		telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "stop", "failure").Inc()
		return &pb.StopResponse{
			Success: false,
			Error:   fmt.Sprintf("pipeline not found: %v", err),
		}, nil
	}

	// Stop pipeline
	if err := pipeline.Stop(); err != nil {
		s.logger.Error().Err(err).Msg("failed to stop pipeline")
		telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "stop", "failure").Inc()
		return &pb.StopResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to stop: %v", err),
		}, nil
	}

	// Update station engine state
	s.mu.Lock()
	engine := s.getStation(req.StationId)
	if engine != nil {
		engine.mu.Lock()
		engine.State = pb.PlaybackState_PLAYBACK_STATE_IDLE
		engine.CurrentSource = nil
		engine.mu.Unlock()

		// Track state change
		telemetry.MediaEnginePlaybackState.WithLabelValues(req.StationId, req.MountId).Set(float64(pb.PlaybackState_PLAYBACK_STATE_IDLE))
	}
	s.mu.Unlock()

	// Track metrics
	duration := time.Since(startTime).Seconds()
	telemetry.MediaEngineOperationDuration.WithLabelValues(req.StationId, req.MountId, "stop").Observe(duration)
	telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "stop", "success").Inc()

	return &pb.StopResponse{
		Success: true,
	}, nil
}

// Fade initiates a crossfade between sources.
func (s *Service) Fade(ctx context.Context, req *pb.FadeRequest) (*pb.FadeResponse, error) {
	startTime := time.Now()

	s.logger.Info().
		Str("station_id", req.StationId).
		Str("mount_id", req.MountId).
		Str("next_source", req.NextSource.SourceId).
		Msg("starting crossfade")

	// Get pipeline
	pipeline, err := s.pipelineManager.GetPipeline(req.StationId)
	if err != nil {
		s.logger.Error().Err(err).Msg("pipeline not found")
		telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "fade", "failure").Inc()
		return &pb.FadeResponse{
			Success: false,
			Error:   fmt.Sprintf("pipeline not found: %v", err),
		}, nil
	}

	// Start crossfade
	if err := pipeline.Fade(ctx, req.NextSource, req.NextCuePoints, req.FadeConfig); err != nil {
		s.logger.Error().Err(err).Msg("failed to start crossfade")
		telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "fade", "failure").Inc()
		return &pb.FadeResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to fade: %v", err),
		}, nil
	}

	// Update station engine state
	s.mu.Lock()
	engine := s.getOrCreateStation(req.StationId, req.MountId)
	engine.mu.Lock()
	engine.State = pb.PlaybackState_PLAYBACK_STATE_FADING
	engine.mu.Unlock()
	s.mu.Unlock()

	fadeID := uuid.NewString()

	// Estimate duration based on fade config
	estimatedDuration := int64(0)
	if req.FadeConfig != nil {
		estimatedDuration = int64(req.FadeConfig.FadeOutMs + req.FadeConfig.FadeInMs)
	}

	// Track metrics
	duration := time.Since(startTime).Seconds()
	telemetry.MediaEngineOperationDuration.WithLabelValues(req.StationId, req.MountId, "fade").Observe(duration)
	telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "fade", "success").Inc()
	telemetry.MediaEnginePlaybackState.WithLabelValues(req.StationId, req.MountId).Set(float64(pb.PlaybackState_PLAYBACK_STATE_FADING))

	return &pb.FadeResponse{
		Success:             true,
		FadeId:              fadeID,
		EstimatedDurationMs: estimatedDuration,
	}, nil
}

// InsertEmergency immediately plays emergency content.
func (s *Service) InsertEmergency(ctx context.Context, req *pb.InsertEmergencyRequest) (*pb.InsertEmergencyResponse, error) {
	startTime := time.Now()

	s.logger.Warn().
		Str("station_id", req.StationId).
		Str("mount_id", req.MountId).
		Str("source_id", req.Source.SourceId).
		Msg("inserting emergency broadcast")

	// Get pipeline
	pipeline, err := s.pipelineManager.GetPipeline(req.StationId)
	if err != nil {
		s.logger.Error().Err(err).Msg("pipeline not found")
		telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "emergency", "failure").Inc()
		return &pb.InsertEmergencyResponse{
			Success: false,
			Error:   fmt.Sprintf("pipeline not found: %v", err),
		}, nil
	}

	// Insert emergency content
	if err := pipeline.InsertEmergency(ctx, req.Source); err != nil {
		s.logger.Error().Err(err).Msg("failed to insert emergency")
		telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "emergency", "failure").Inc()
		return &pb.InsertEmergencyResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to insert emergency: %v", err),
		}, nil
	}

	// Update station engine state
	s.mu.Lock()
	engine := s.getOrCreateStation(req.StationId, req.MountId)
	engine.mu.Lock()
	engine.State = pb.PlaybackState_PLAYBACK_STATE_PLAYING
	engine.CurrentSource = req.Source
	engine.mu.Unlock()
	s.mu.Unlock()

	emergencyID := uuid.NewString()

	// Track metrics
	duration := time.Since(startTime).Seconds()
	telemetry.MediaEngineOperationDuration.WithLabelValues(req.StationId, req.MountId, "emergency").Observe(duration)
	telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "emergency", "success").Inc()
	telemetry.MediaEnginePlaybackState.WithLabelValues(req.StationId, req.MountId).Set(float64(pb.PlaybackState_PLAYBACK_STATE_PLAYING))

	return &pb.InsertEmergencyResponse{
		Success:     true,
		EmergencyId: emergencyID,
	}, nil
}

// RouteLive routes a live input stream.
func (s *Service) RouteLive(ctx context.Context, req *pb.RouteLiveRequest) (*pb.RouteLiveResponse, error) {
	startTime := time.Now()

	s.logger.Info().
		Str("station_id", req.StationId).
		Str("mount_id", req.MountId).
		Str("session_id", req.SessionId).
		Str("input_type", req.InputType.String()).
		Msg("routing live input")

	// Route through live input manager
	resp, err := s.liveInputMgr.RouteLive(ctx, req)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to route live input")
		telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "route_live", "failure").Inc()
		return &pb.RouteLiveResponse{
			Success: false,
			Message: fmt.Sprintf("failed to route live: %v", err),
		}, status.Error(codes.Internal, err.Error())
	}

	// Update station engine state to indicate live playback
	s.mu.Lock()
	engine := s.getOrCreateStation(req.StationId, req.MountId)
	engine.mu.Lock()
	engine.State = pb.PlaybackState_PLAYBACK_STATE_PLAYING
	engine.mu.Unlock()
	s.mu.Unlock()

	// Track metrics
	duration := time.Since(startTime).Seconds()
	telemetry.MediaEngineOperationDuration.WithLabelValues(req.StationId, req.MountId, "route_live").Observe(duration)
	telemetry.MediaEngineOperations.WithLabelValues(req.StationId, req.MountId, "route_live", "success").Inc()
	telemetry.MediaEnginePlaybackState.WithLabelValues(req.StationId, req.MountId).Set(float64(pb.PlaybackState_PLAYBACK_STATE_PLAYING))

	return resp, nil
}

// StreamTelemetry streams real-time audio telemetry.
func (s *Service) StreamTelemetry(req *pb.TelemetryRequest, stream pb.MediaEngine_StreamTelemetryServer) error {
	s.logger.Debug().
		Str("station_id", req.StationId).
		Str("mount_id", req.MountId).
		Int32("interval_ms", req.IntervalMs).
		Msg("streaming telemetry")

	interval := time.Duration(req.IntervalMs) * time.Millisecond
	if interval < 100*time.Millisecond {
		interval = 1 * time.Second // Default interval
	}

	// Get pipeline
	pipeline, err := s.pipelineManager.GetPipeline(req.StationId)
	if err != nil {
		return status.Errorf(codes.NotFound, "pipeline not found: %v", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-ticker.C:
			// Get telemetry from pipeline
			telemetry := pipeline.GetTelemetry()

			if err := stream.Send(telemetry); err != nil {
				return status.Errorf(codes.Internal, "failed to send telemetry: %v", err)
			}
		}
	}
}

// GetStatus returns current engine status.
func (s *Service) GetStatus(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	s.mu.RLock()
	engine := s.getStation(req.StationId)
	s.mu.RUnlock()

	if engine == nil {
		return &pb.StatusResponse{
			Running: false,
		}, nil
	}

	engine.mu.RLock()
	defer engine.mu.RUnlock()

	metadata := make(map[string]string)
	if engine.CurrentSource != nil && engine.CurrentSource.Metadata != nil {
		metadata = engine.CurrentSource.Metadata
	}

	return &pb.StatusResponse{
		Running:         true,
		State:           engine.State,
		CurrentSourceId: getSourceID(engine.CurrentSource),
		UptimeSeconds:   int64(time.Since(s.uptime).Seconds()),
		GraphHandle:     engine.GraphHandle,
		Metadata:        metadata,
	}, nil
}

// AnalyzeMedia performs media analysis (metadata, loudness, cue points).
func (s *Service) AnalyzeMedia(ctx context.Context, req *pb.AnalyzeMediaRequest) (*pb.AnalyzeMediaResponse, error) {
	startTime := time.Now()

	s.logger.Info().
		Str("file_path", req.FilePath).
		Msg("analyzing media file")

	resp, err := s.analyzer.AnalyzeMedia(ctx, req.FilePath)
	if err != nil {
		s.logger.Error().Err(err).Msg("media analysis failed")
		telemetry.MediaEngineOperations.WithLabelValues("", "", "analyze_media", "failure").Inc()
		return &pb.AnalyzeMediaResponse{
			Success: false,
			Error:   fmt.Sprintf("analysis failed: %v", err),
		}, nil
	}

	// Track metrics
	duration := time.Since(startTime).Seconds()
	telemetry.MediaEngineOperationDuration.WithLabelValues("", "", "analyze_media").Observe(duration)

	status := "success"
	if !resp.Success {
		status = "failure"
	}
	telemetry.MediaEngineOperations.WithLabelValues("", "", "analyze_media", status).Inc()

	s.logger.Info().
		Int64("duration_ms", resp.DurationMs).
		Float32("loudness_lufs", resp.LoudnessLufs).
		Str("codec", resp.Codec).
		Msg("media analysis complete")

	return resp, nil
}

// ExtractArtwork extracts embedded album art from media.
func (s *Service) ExtractArtwork(ctx context.Context, req *pb.ExtractArtworkRequest) (*pb.ExtractArtworkResponse, error) {
	startTime := time.Now()

	s.logger.Debug().
		Str("file_path", req.FilePath).
		Int32("max_width", req.MaxWidth).
		Int32("max_height", req.MaxHeight).
		Str("format", req.Format).
		Msg("extracting artwork")

	resp, err := s.analyzer.ExtractArtwork(ctx, req)
	if err != nil {
		s.logger.Error().Err(err).Msg("artwork extraction failed")
		telemetry.MediaEngineOperations.WithLabelValues("", "", "extract_artwork", "failure").Inc()
		return &pb.ExtractArtworkResponse{
			Success: false,
			Error:   fmt.Sprintf("extraction failed: %v", err),
		}, nil
	}

	// Track metrics
	duration := time.Since(startTime).Seconds()
	telemetry.MediaEngineOperationDuration.WithLabelValues("", "", "extract_artwork").Observe(duration)

	status := "success"
	if !resp.Success {
		status = "failure"
	}
	telemetry.MediaEngineOperations.WithLabelValues("", "", "extract_artwork", status).Inc()

	if resp.Success {
		s.logger.Debug().
			Int("artwork_size", len(resp.ArtworkData)).
			Str("mime_type", resp.MimeType).
			Msg("artwork extracted")
	}

	return resp, nil
}

// GenerateWaveform generates peak/RMS waveform data for visualization.
func (s *Service) GenerateWaveform(ctx context.Context, req *pb.GenerateWaveformRequest) (*pb.GenerateWaveformResponse, error) {
	startTime := time.Now()

	s.logger.Debug().
		Str("file_path", req.FilePath).
		Int32("samples_per_second", req.SamplesPerSecond).
		Str("type", req.Type.String()).
		Msg("generating waveform")

	resp, err := s.analyzer.GenerateWaveform(ctx, req)
	if err != nil {
		s.logger.Error().Err(err).Msg("waveform generation failed")
		telemetry.MediaEngineOperations.WithLabelValues("", "", "generate_waveform", "failure").Inc()
		return &pb.GenerateWaveformResponse{
			Success: false,
			Error:   fmt.Sprintf("generation failed: %v", err),
		}, nil
	}

	// Track metrics
	duration := time.Since(startTime).Seconds()
	telemetry.MediaEngineOperationDuration.WithLabelValues("", "", "generate_waveform").Observe(duration)

	status := "success"
	if !resp.Success {
		status = "failure"
	}
	telemetry.MediaEngineOperations.WithLabelValues("", "", "generate_waveform", status).Inc()

	if resp.Success {
		s.logger.Debug().
			Int("peak_samples", len(resp.PeakLeft)).
			Int("rms_samples", len(resp.RmsLeft)).
			Int64("duration_ms", resp.DurationMs).
			Msg("waveform generated")
	}

	return resp, nil
}

// Shutdown gracefully shuts down the media engine.
func (s *Service) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("shutting down media engine")

	// Stop supervisor first
	s.supervisor.Stop()

	s.mu.Lock()
	stationIDs := make([]string, 0, len(s.stations))
	for stationID := range s.stations {
		stationIDs = append(stationIDs, stationID)
	}
	s.mu.Unlock()

	// Destroy all pipelines
	for _, stationID := range stationIDs {
		// Get mount ID before destroying
		s.mu.RLock()
		engine := s.getStation(stationID)
		mountID := ""
		if engine != nil {
			mountID = engine.MountID
		}
		s.mu.RUnlock()

		if err := s.pipelineManager.DestroyPipeline(stationID); err != nil {
			s.logger.Error().Err(err).Str("station_id", stationID).Msg("failed to destroy pipeline")
		} else if mountID != "" {
			// Clear active pipeline metric
			telemetry.MediaEngineActivePipelines.WithLabelValues(stationID, mountID).Set(0)
		}
	}

	// Update station engine states
	s.mu.Lock()
	for _, engine := range s.stations {
		engine.mu.Lock()
		engine.State = pb.PlaybackState_PLAYBACK_STATE_IDLE
		engine.CurrentSource = nil
		engine.mu.Unlock()
	}
	s.mu.Unlock()

	// Shutdown live input manager
	if err := s.liveInputMgr.Shutdown(); err != nil {
		s.logger.Error().Err(err).Msg("failed to shutdown live input manager")
	}

	// Shutdown webstream manager
	if err := s.webstreamMgr.Shutdown(); err != nil {
		s.logger.Error().Err(err).Msg("failed to shutdown webstream manager")
	}

	s.logger.Info().Msg("media engine shutdown complete")

	return nil
}

// Helper methods

func (s *Service) getOrCreateStation(stationID, mountID string) *StationEngine {
	engine := s.stations[stationID]
	if engine == nil {
		engine = &StationEngine{
			StationID: stationID,
			MountID:   mountID,
			State:     pb.PlaybackState_PLAYBACK_STATE_IDLE,
			StartedAt: time.Now(),
		}
		s.stations[stationID] = engine
	}
	return engine
}

func (s *Service) getStation(stationID string) *StationEngine {
	return s.stations[stationID]
}

func getSourceID(source *pb.SourceConfig) string {
	if source == nil {
		return ""
	}
	return source.SourceId
}
