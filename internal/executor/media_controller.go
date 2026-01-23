/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package executor

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/mediaengine/client"
	"github.com/friendsincode/grimnir_radio/internal/models"
	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// MediaController manages communication with the media engine
type MediaController struct {
	client    *client.Client
	stationID string
	mountID   string
	logger    zerolog.Logger
}

// NewMediaController creates a new media controller
func NewMediaController(mediaClient *client.Client, stationID, mountID string, logger zerolog.Logger) *MediaController {
	return &MediaController{
		client:    mediaClient,
		stationID: stationID,
		mountID:   mountID,
		logger:    logger.With().Str("component", "media_controller").Str("station_id", stationID).Logger(),
	}
}

// LoadGraph loads a DSP processing graph into the media engine
func (mc *MediaController) LoadGraph(ctx context.Context, graph *pb.DSPGraph) (string, error) {
	if !mc.client.IsConnected() {
		return "", fmt.Errorf("media engine not connected")
	}

	graphHandle, err := mc.client.LoadGraph(ctx, mc.stationID, mc.mountID, graph)
	if err != nil {
		mc.logger.Error().Err(err).Msg("failed to load DSP graph")
		return "", fmt.Errorf("load graph: %w", err)
	}

	mc.logger.Info().Str("graph_handle", graphHandle).Msg("DSP graph loaded")
	return graphHandle, nil
}

// Play starts playback of a media source
func (mc *MediaController) Play(ctx context.Context, sourceID, path string, sourceType pb.SourceType, priority models.PriorityLevel, cuePoints *pb.CuePoints) (string, error) {
	if !mc.client.IsConnected() {
		return "", fmt.Errorf("media engine not connected")
	}

	req := &pb.PlayRequest{
		StationId: mc.stationID,
		MountId:   mc.mountID,
		Source: &pb.SourceConfig{
			Type:     sourceType,
			SourceId: sourceID,
			Path:     path,
			Metadata: make(map[string]string),
		},
		CuePoints: cuePoints,
		Priority:  int32(priority),
	}

	playbackID, err := mc.client.Play(ctx, req)
	if err != nil {
		mc.logger.Error().Err(err).Str("source_id", sourceID).Msg("failed to start playback")
		return "", fmt.Errorf("play: %w", err)
	}

	mc.logger.Info().
		Str("source_id", sourceID).
		Str("playback_id", playbackID).
		Msg("playback started")

	return playbackID, nil
}

// Stop halts playback
func (mc *MediaController) Stop(ctx context.Context, immediate bool) error {
	if !mc.client.IsConnected() {
		return fmt.Errorf("media engine not connected")
	}

	if err := mc.client.Stop(ctx, mc.stationID, mc.mountID, immediate); err != nil {
		mc.logger.Error().Err(err).Msg("failed to stop playback")
		return fmt.Errorf("stop: %w", err)
	}

	mc.logger.Info().Bool("immediate", immediate).Msg("playback stopped")
	return nil
}

// Fade initiates a crossfade to the next source
func (mc *MediaController) Fade(ctx context.Context, nextSourceID, nextPath string, nextSourceType pb.SourceType, nextCuePoints *pb.CuePoints, fadeConfig *pb.FadeConfig) (string, error) {
	if !mc.client.IsConnected() {
		return "", fmt.Errorf("media engine not connected")
	}

	req := &pb.FadeRequest{
		StationId: mc.stationID,
		MountId:   mc.mountID,
		NextSource: &pb.SourceConfig{
			Type:     nextSourceType,
			SourceId: nextSourceID,
			Path:     nextPath,
			Metadata: make(map[string]string),
		},
		NextCuePoints: nextCuePoints,
		FadeConfig:    fadeConfig,
	}

	fadeID, estimatedDuration, err := mc.client.Fade(ctx, req)
	if err != nil {
		mc.logger.Error().Err(err).Str("next_source_id", nextSourceID).Msg("failed to start crossfade")
		return "", fmt.Errorf("fade: %w", err)
	}

	mc.logger.Info().
		Str("next_source_id", nextSourceID).
		Str("fade_id", fadeID).
		Int64("estimated_duration_ms", estimatedDuration).
		Msg("crossfade started")

	return fadeID, nil
}

// InsertEmergency immediately plays emergency content
func (mc *MediaController) InsertEmergency(ctx context.Context, sourceID, path string) (string, error) {
	if !mc.client.IsConnected() {
		return "", fmt.Errorf("media engine not connected")
	}

	source := &pb.SourceConfig{
		Type:     pb.SourceType_SOURCE_TYPE_EMERGENCY,
		SourceId: sourceID,
		Path:     path,
		Metadata: make(map[string]string),
	}

	emergencyID, err := mc.client.InsertEmergency(ctx, mc.stationID, mc.mountID, source)
	if err != nil {
		mc.logger.Error().Err(err).Str("source_id", sourceID).Msg("failed to insert emergency")
		return "", fmt.Errorf("insert emergency: %w", err)
	}

	mc.logger.Warn().
		Str("source_id", sourceID).
		Str("emergency_id", emergencyID).
		Msg("emergency broadcast inserted")

	return emergencyID, nil
}

// RouteLive routes a live input stream
func (mc *MediaController) RouteLive(ctx context.Context, inputURL, authToken string, applyProcessing bool) (string, error) {
	if !mc.client.IsConnected() {
		return "", fmt.Errorf("media engine not connected")
	}

	input := &pb.LiveInputConfig{
		InputUrl:        inputURL,
		AuthToken:       authToken,
		BufferMs:        2000, // 2 second buffer
		ApplyProcessing: applyProcessing,
	}

	liveID, err := mc.client.RouteLive(ctx, mc.stationID, mc.mountID, input)
	if err != nil {
		mc.logger.Error().Err(err).Str("input_url", inputURL).Msg("failed to route live input")
		return "", fmt.Errorf("route live: %w", err)
	}

	mc.logger.Info().
		Str("input_url", inputURL).
		Str("live_id", liveID).
		Bool("apply_processing", applyProcessing).
		Msg("live input routed")

	return liveID, nil
}

// GetStatus returns current engine status
func (mc *MediaController) GetStatus(ctx context.Context) (*pb.StatusResponse, error) {
	if !mc.client.IsConnected() {
		return nil, fmt.Errorf("media engine not connected")
	}

	status, err := mc.client.GetStatus(ctx, mc.stationID, mc.mountID)
	if err != nil {
		mc.logger.Error().Err(err).Msg("failed to get status")
		return nil, fmt.Errorf("get status: %w", err)
	}

	return status, nil
}

// StreamTelemetry starts streaming telemetry and calls the callback for each update
func (mc *MediaController) StreamTelemetry(ctx context.Context, intervalMs int32, callback func(*pb.TelemetryData) error) error {
	if !mc.client.IsConnected() {
		return fmt.Errorf("media engine not connected")
	}

	mc.logger.Info().Int32("interval_ms", intervalMs).Msg("starting telemetry stream")

	err := mc.client.StreamTelemetry(ctx, mc.stationID, mc.mountID, intervalMs, callback)
	if err != nil {
		mc.logger.Error().Err(err).Msg("telemetry stream error")
		return fmt.Errorf("stream telemetry: %w", err)
	}

	return nil
}

// IsConnected returns whether the media engine is connected
func (mc *MediaController) IsConnected() bool {
	return mc.client.IsConnected()
}
