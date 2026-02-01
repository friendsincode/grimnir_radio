/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/mediaengine/dsp"
	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

func TestPipelineManager_CreatePipeline(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &Config{
		GStreamerBin: "gst-launch-1.0",
	}

	pm := NewPipelineManager(cfg, logger)

	ctx := context.Background()
	graph := &dsp.Graph{
		ID:       "test-graph",
		Pipeline: "audioconvert ! audioresample",
	}

	outputConfig := &EncoderConfig{
		OutputType: OutputTypeTest,
		Format:     AudioFormatMP3,
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}

	pipeline, err := pm.CreatePipeline(ctx, "station-123", "mount-456", graph, outputConfig)
	if err != nil {
		t.Fatalf("CreatePipeline() failed: %v", err)
	}

	if pipeline.StationID != "station-123" {
		t.Errorf("StationID = %s, want station-123", pipeline.StationID)
	}
	if pipeline.MountID != "mount-456" {
		t.Errorf("MountID = %s, want mount-456", pipeline.MountID)
	}
	if pipeline.State != pb.PlaybackState_PLAYBACK_STATE_IDLE {
		t.Errorf("Initial state = %s, want IDLE", pipeline.State)
	}
	if pipeline.Graph != graph {
		t.Error("Graph not set correctly")
	}
	if pipeline.OutputConfig != outputConfig {
		t.Error("OutputConfig not set correctly")
	}
	if pipeline.crossfadeMgr == nil {
		t.Error("CrossfadeManager not initialized")
	}
}

func TestPipelineManager_CreatePipeline_Duplicate(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &Config{}
	pm := NewPipelineManager(cfg, logger)

	ctx := context.Background()
	graph := &dsp.Graph{}
	outputConfig := &EncoderConfig{
		OutputType: OutputTypeTest,
		Format:     AudioFormatMP3,
		Bitrate:    128,
	}

	// Create first pipeline
	pipeline1, err := pm.CreatePipeline(ctx, "station-123", "mount-456", graph, outputConfig)
	if err != nil {
		t.Fatalf("First CreatePipeline() failed: %v", err)
	}

	// Try to create duplicate
	pipeline2, err := pm.CreatePipeline(ctx, "station-123", "mount-456", graph, outputConfig)
	if err != nil {
		t.Fatalf("Second CreatePipeline() failed: %v", err)
	}

	// Should return existing pipeline
	if pipeline1 != pipeline2 {
		t.Error("CreatePipeline() should return existing pipeline for duplicate")
	}
}

func TestPipelineManager_GetPipeline(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &Config{}
	pm := NewPipelineManager(cfg, logger)

	ctx := context.Background()
	graph := &dsp.Graph{}
	outputConfig := &EncoderConfig{
		OutputType: OutputTypeTest,
		Format:     AudioFormatMP3,
		Bitrate:    128,
	}

	// Create pipeline
	_, err := pm.CreatePipeline(ctx, "station-123", "mount-456", graph, outputConfig)
	if err != nil {
		t.Fatalf("CreatePipeline() failed: %v", err)
	}

	// Get pipeline
	pipeline, err := pm.GetPipeline("station-123")
	if err != nil {
		t.Fatalf("GetPipeline() failed: %v", err)
	}

	if pipeline.StationID != "station-123" {
		t.Errorf("Retrieved StationID = %s, want station-123", pipeline.StationID)
	}

	// Get non-existent pipeline
	_, err = pm.GetPipeline("non-existent")
	if err == nil {
		t.Error("GetPipeline() should return error for non-existent station")
	}
}

func TestPipelineManager_DestroyPipeline(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &Config{}
	pm := NewPipelineManager(cfg, logger)

	ctx := context.Background()
	graph := &dsp.Graph{}
	outputConfig := &EncoderConfig{
		OutputType: OutputTypeTest,
		Format:     AudioFormatMP3,
		Bitrate:    128,
	}

	// Create pipeline
	_, err := pm.CreatePipeline(ctx, "station-123", "mount-456", graph, outputConfig)
	if err != nil {
		t.Fatalf("CreatePipeline() failed: %v", err)
	}

	// Destroy pipeline
	err = pm.DestroyPipeline("station-123")
	if err != nil {
		t.Fatalf("DestroyPipeline() failed: %v", err)
	}

	// Verify pipeline is gone
	_, err = pm.GetPipeline("station-123")
	if err == nil {
		t.Error("GetPipeline() should fail after DestroyPipeline()")
	}

	// Destroy non-existent pipeline
	err = pm.DestroyPipeline("non-existent")
	if err == nil {
		t.Error("DestroyPipeline() should return error for non-existent station")
	}
}

func TestPipeline_buildPlaybackPipeline(t *testing.T) {
	logger := zerolog.Nop()

	tests := []struct {
		name         string
		track        *Track
		graph        *dsp.Graph
		outputConfig *EncoderConfig
		wantContains []string
	}{
		{
			name: "media file with DSP and MP3 output",
			track: &Track{
				SourceType: pb.SourceType_SOURCE_TYPE_MEDIA,
				Path:       "/media/test.mp3",
			},
			graph: &dsp.Graph{
				Pipeline: "rgvolume pre-amp=0.0",
			},
			outputConfig: &EncoderConfig{
				OutputType: OutputTypeTest,
				Format:     AudioFormatMP3,
				Bitrate:    192,
				SampleRate: 44100,
				Channels:   2,
			},
			wantContains: []string{
				"filesrc",
				"location=/media/test.mp3",
				"decodebin",
				"rgvolume",
				"lamemp3enc",
				"bitrate=192",
				"fakesink",
			},
		},
		{
			name: "webstream with AAC output",
			track: &Track{
				SourceType: pb.SourceType_SOURCE_TYPE_WEBSTREAM,
				Path:       "http://example.com/stream.mp3",
			},
			graph: nil,
			outputConfig: &EncoderConfig{
				OutputType: OutputTypeTest,
				Format:     AudioFormatAAC,
				Bitrate:    128,
				SampleRate: 48000,
				Channels:   2,
			},
			wantContains: []string{
				"souphttpsrc",
				"location=http://example.com/stream.mp3",
				"decodebin",
				"avenc_aac",
				"fakesink",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline := &Pipeline{
				Graph:        tt.graph,
				OutputConfig: tt.outputConfig,
				logger:       logger,
			}

			pipelineStr, err := pipeline.buildPlaybackPipeline(tt.track)
			if err != nil {
				t.Fatalf("buildPlaybackPipeline() failed: %v", err)
			}

			for _, want := range tt.wantContains {
				if !contains(pipelineStr, want) {
					t.Errorf("Pipeline missing %q: %s", want, pipelineStr)
				}
			}

			t.Logf("Pipeline: %s", pipelineStr)
		})
	}
}

func TestPipeline_buildEmergencyPipeline(t *testing.T) {
	logger := zerolog.Nop()

	outputConfig := &EncoderConfig{
		OutputType: OutputTypeTest,
		Format:     AudioFormatMP3,
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}

	pipeline := &Pipeline{
		OutputConfig: outputConfig,
		logger:       logger,
	}

	track := &Track{
		SourceType: pb.SourceType_SOURCE_TYPE_MEDIA,
		Path:       "/media/emergency.wav",
	}

	pipelineStr, err := pipeline.buildEmergencyPipeline(track)
	if err != nil {
		t.Fatalf("buildEmergencyPipeline() failed: %v", err)
	}

	// Emergency pipeline should bypass DSP but still encode
	if contains(pipelineStr, "rgvolume") {
		t.Error("Emergency pipeline should not contain DSP elements")
	}
	if !contains(pipelineStr, "lamemp3enc") {
		t.Error("Emergency pipeline should contain encoder")
	}

	t.Logf("Emergency pipeline: %s", pipelineStr)
}

func TestPipeline_GetTelemetry(t *testing.T) {
	logger := zerolog.Nop()

	pipeline := &Pipeline{
		StationID: "station-123",
		MountID:   "mount-456",
		State:     pb.PlaybackState_PLAYBACK_STATE_PLAYING,
		telemetry: &TelemetryCollector{
			AudioLevelL:   -12.5,
			AudioLevelR:   -13.0,
			PeakLevelL:    -3.0,
			PeakLevelR:    -3.5,
			BufferFillPct: 75,
			UnderrunCount: 2,
		},
		CurrentTrack: &Track{
			Position: 30 * time.Second,
			Duration: 180 * time.Second,
		},
		logger: logger,
	}

	telemetry := pipeline.GetTelemetry()

	if telemetry.StationId != "station-123" {
		t.Errorf("StationId = %s, want station-123", telemetry.StationId)
	}
	if telemetry.MountId != "mount-456" {
		t.Errorf("MountId = %s, want mount-456", telemetry.MountId)
	}
	if telemetry.State != pb.PlaybackState_PLAYBACK_STATE_PLAYING {
		t.Errorf("State = %s, want PLAYING", telemetry.State)
	}
	if telemetry.AudioLevelL != -12.5 {
		t.Errorf("AudioLevelL = %.2f, want -12.5", telemetry.AudioLevelL)
	}
	if telemetry.BufferFillPercent != 75 {
		t.Errorf("BufferFillPercent = %d, want 75", telemetry.BufferFillPercent)
	}
	if telemetry.UnderrunCount != 2 {
		t.Errorf("UnderrunCount = %d, want 2", telemetry.UnderrunCount)
	}
	if telemetry.PositionMs != 30000 {
		t.Errorf("PositionMs = %d, want 30000", telemetry.PositionMs)
	}
	if telemetry.DurationMs != 180000 {
		t.Errorf("DurationMs = %d, want 180000", telemetry.DurationMs)
	}
	if telemetry.Timestamp == nil {
		t.Error("Timestamp should not be nil")
	}
}

func TestPipeline_Stop(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	pipeline := &Pipeline{
		StationID: "station-123",
		MountID:   "mount-456",
		State:     pb.PlaybackState_PLAYBACK_STATE_PLAYING,
		CurrentTrack: &Track{
			SourceID: "track-789",
		},
		NextTrack:    nil,
		logger:       logger,
		crossfadeMgr: NewCrossfadeManager("station-123", "mount-456", logger),
	}

	err := pipeline.Stop()
	if err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}

	if pipeline.State != pb.PlaybackState_PLAYBACK_STATE_IDLE {
		t.Errorf("State after Stop() = %s, want IDLE", pipeline.State)
	}
	if pipeline.CurrentTrack != nil {
		t.Error("CurrentTrack should be nil after Stop()")
	}
	if pipeline.NextTrack != nil {
		t.Error("NextTrack should be nil after Stop()")
	}

	// Stop idempotent - should not error
	err = pipeline.Stop()
	if err != nil {
		t.Errorf("Second Stop() failed: %v", err)
	}

	_ = ctx // silence unused warning
}

func TestPipeline_ConcurrentTelemetryAccess(t *testing.T) {
	logger := zerolog.Nop()

	pipeline := &Pipeline{
		StationID: "station-123",
		MountID:   "mount-456",
		State:     pb.PlaybackState_PLAYBACK_STATE_PLAYING,
		telemetry: &TelemetryCollector{},
		logger:    logger,
	}

	done := make(chan bool)

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = pipeline.GetTelemetry()
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			pipeline.telemetry.mu.Lock()
			pipeline.telemetry.AudioLevelL = float32(i)
			pipeline.telemetry.mu.Unlock()
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Wait for both
	<-done
	<-done

	// No deadlock or panic = success
}

func BenchmarkPipeline_buildPlaybackPipeline(b *testing.B) {
	logger := zerolog.Nop()

	pipeline := &Pipeline{
		Graph: &dsp.Graph{
			Pipeline: "rgvolume pre-amp=0.0 ! audiodynamic mode=compressor",
		},
		OutputConfig: &EncoderConfig{
			OutputType: OutputTypeTest,
			Format:     AudioFormatMP3,
			Bitrate:    192,
			SampleRate: 44100,
			Channels:   2,
		},
		logger: logger,
	}

	track := &Track{
		SourceType: pb.SourceType_SOURCE_TYPE_MEDIA,
		Path:       "/media/test.mp3",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := pipeline.buildPlaybackPipeline(track)
		if err != nil {
			b.Fatalf("buildPlaybackPipeline() failed: %v", err)
		}
	}
}

func BenchmarkPipeline_GetTelemetry(b *testing.B) {
	logger := zerolog.Nop()

	pipeline := &Pipeline{
		StationID: "station-123",
		MountID:   "mount-456",
		State:     pb.PlaybackState_PLAYBACK_STATE_PLAYING,
		telemetry: &TelemetryCollector{
			AudioLevelL: -12.0,
			AudioLevelR: -13.0,
		},
		CurrentTrack: &Track{
			Position: 30 * time.Second,
			Duration: 180 * time.Second,
		},
		logger: logger,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pipeline.GetTelemetry()
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			len(s) > len(substr)+1 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
