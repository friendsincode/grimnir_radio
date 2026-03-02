/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// ActiveRecording tracks state for a single in-progress recording.
type ActiveRecording struct {
	RecordingID string
	StationID   string
	MountID     string
	OutputPath  string
	Codec       string // "flac" or "opus"
	SampleRate  int32
	Channels    int32
	Bitrate     int32 // opus only (kbps)

	process   *GStreamerProcess
	startedAt time.Time
	mu        sync.Mutex
}

// RecordingManager manages active recordings per station.
// It spawns a secondary GStreamer pipeline that reads from the broadcast
// pipeline's output via a tee element and encodes to a file.
type RecordingManager struct {
	logger     zerolog.Logger
	cfg        *Config
	recordings map[string]*ActiveRecording // recording_id -> active recording
	byStation  map[string]string           // station_id -> recording_id
	mu         sync.RWMutex
}

// NewRecordingManager creates a new recording manager.
func NewRecordingManager(cfg *Config, logger zerolog.Logger) *RecordingManager {
	return &RecordingManager{
		logger:     logger.With().Str("component", "recording-manager").Logger(),
		cfg:        cfg,
		recordings: make(map[string]*ActiveRecording),
		byStation:  make(map[string]string),
	}
}

// StartRecording starts recording audio for a station.
// It launches a separate GStreamer pipeline that captures the station's
// audio monitor output and encodes it to the specified file.
func (rm *RecordingManager) StartRecording(ctx context.Context, rec *ActiveRecording) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Check for existing recording on this station
	if existingID, ok := rm.byStation[rec.StationID]; ok {
		return fmt.Errorf("station %s already has active recording %s", rec.StationID, existingID)
	}

	// Ensure output directory exists
	dir := filepath.Dir(rec.OutputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create recording directory: %w", err)
	}

	// Build the recording pipeline string
	pipeline, err := rm.buildRecordingPipeline(rec)
	if err != nil {
		return fmt.Errorf("build recording pipeline: %w", err)
	}

	rm.logger.Info().
		Str("recording_id", rec.RecordingID).
		Str("station_id", rec.StationID).
		Str("output_path", rec.OutputPath).
		Str("codec", rec.Codec).
		Str("pipeline", pipeline).
		Msg("starting recording pipeline")

	// Launch GStreamer process
	proc := NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       rec.RecordingID,
		Pipeline: pipeline,
		OnExit: func(exitCode int, exitErr error) {
			rm.logger.Info().
				Str("recording_id", rec.RecordingID).
				Int("exit_code", exitCode).
				Err(exitErr).
				Msg("recording pipeline exited")
		},
	}, rm.logger)

	if err := proc.Start(pipeline); err != nil {
		return fmt.Errorf("start recording process: %w", err)
	}

	rec.process = proc
	rec.startedAt = time.Now()
	rm.recordings[rec.RecordingID] = rec
	rm.byStation[rec.StationID] = rec.RecordingID

	return nil
}

// StopRecording stops an active recording and returns file info.
func (rm *RecordingManager) StopRecording(recordingID string) (sizeBytes int64, durationMs int64, err error) {
	rm.mu.Lock()
	rec, ok := rm.recordings[recordingID]
	if !ok {
		rm.mu.Unlock()
		return 0, 0, fmt.Errorf("recording %s not found", recordingID)
	}
	delete(rm.recordings, recordingID)
	delete(rm.byStation, rec.StationID)
	rm.mu.Unlock()

	rec.mu.Lock()
	defer rec.mu.Unlock()

	// Stop the pipeline gracefully
	if rec.process != nil {
		rm.logger.Info().
			Str("recording_id", recordingID).
			Msg("stopping recording pipeline")
		rec.process.Stop()
	}

	// Calculate duration from wall clock
	durationMs = time.Since(rec.startedAt).Milliseconds()

	// Get file size
	if info, statErr := os.Stat(rec.OutputPath); statErr == nil {
		sizeBytes = info.Size()
	}

	rm.logger.Info().
		Str("recording_id", recordingID).
		Int64("size_bytes", sizeBytes).
		Int64("duration_ms", durationMs).
		Msg("recording stopped")

	return sizeBytes, durationMs, nil
}

// IsRecording returns true if the station has an active recording.
func (rm *RecordingManager) IsRecording(stationID string) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	_, ok := rm.byStation[stationID]
	return ok
}

// GetRecordingID returns the active recording ID for a station, or "".
func (rm *RecordingManager) GetRecordingID(stationID string) string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.byStation[stationID]
}

// StopAll stops all active recordings (used during shutdown).
func (rm *RecordingManager) StopAll() {
	rm.mu.Lock()
	ids := make([]string, 0, len(rm.recordings))
	for id := range rm.recordings {
		ids = append(ids, id)
	}
	rm.mu.Unlock()

	for _, id := range ids {
		if _, _, err := rm.StopRecording(id); err != nil {
			rm.logger.Error().Err(err).Str("recording_id", id).Msg("failed to stop recording during shutdown")
		}
	}
}

// buildRecordingPipeline constructs the GStreamer pipeline for recording.
//
// The recording pipeline uses pulsesrc to capture the station's audio
// monitor output, then encodes to FLAC or Opus and writes to a file.
//
// Pipeline: audiotestsrc (placeholder) ! audioconvert ! encoder ! filesink
//
// In Phase 3, the actual implementation uses a monitor source from the
// broadcast pipeline. For now, we use a simple file-based approach where
// the broadcast pipeline's tee element feeds the recording leg.
func (rm *RecordingManager) buildRecordingPipeline(rec *ActiveRecording) (string, error) {
	// Source: read from a UNIX domain socket or TCP port that the broadcast
	// pipeline's tee writes to. For simplicity, we use a separate approach:
	// the broadcast pipeline adds a tee element that writes raw audio to a
	// shared file descriptor, and the recording pipeline reads from it.
	//
	// Practical approach: use GStreamer's interpipeline sink/src elements
	// or, more simply, build the recording as part of the main pipeline.
	//
	// For the initial implementation, we build a standalone pipeline that
	// captures from the system's audio monitor (pulsesrc/alsasrc).
	// In production, this will be replaced with the tee approach.

	var encoder string
	switch rec.Codec {
	case "opus":
		bitrate := rec.Bitrate
		if bitrate <= 0 {
			bitrate = 192
		}
		encoder = fmt.Sprintf("audioconvert ! audioresample ! opusenc bitrate=%d000 ! oggmux", bitrate)
	case "flac":
		encoder = "audioconvert ! audioresample ! flacenc"
	default:
		encoder = "audioconvert ! audioresample ! flacenc" // default to FLAC
	}

	sampleRate := rec.SampleRate
	if sampleRate <= 0 {
		sampleRate = 44100
	}
	channels := rec.Channels
	if channels <= 0 {
		channels = 2
	}

	// Use interaudiosrc to receive audio from the broadcast pipeline's
	// interaudiosink (identified by station channel name).
	// This requires the broadcast pipeline to add:
	//   tee name=t ! queue ! [broadcast encoder]
	//                t. ! queue ! interaudiosink channel={station_id}-rec
	channelName := fmt.Sprintf("%s-rec", rec.StationID)
	source := fmt.Sprintf(
		"interaudiosrc channel=%s ! audio/x-raw,rate=%d,channels=%d",
		channelName, sampleRate, channels,
	)

	sink := fmt.Sprintf("filesink location=%s", rec.OutputPath)

	pipeline := fmt.Sprintf("%s ! %s ! %s", source, encoder, sink)
	return pipeline, nil
}
