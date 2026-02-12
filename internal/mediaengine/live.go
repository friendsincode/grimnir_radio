/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
	"github.com/rs/zerolog"
)

// LiveInput represents an active live input stream.
type LiveInput struct {
	SessionID   string
	StationID   string
	MountID     string
	SourceURL   string // Input URL (e.g., "icecast://source:pass@localhost:8000/live")
	Connected   bool
	ConnectedAt time.Time

	// GStreamer elements
	Pipeline string // GStreamer pipeline string
	Process  *GStreamerProcess

	logger zerolog.Logger
}

// LiveInputManager handles multiple concurrent live inputs.
type LiveInputManager struct {
	mu     sync.RWMutex
	inputs map[string]*LiveInput // key: session_id
	logger zerolog.Logger
}

const defaultWebRTCRTPPort = 5006

// NewLiveInputManager creates a new live input manager.
func NewLiveInputManager(logger zerolog.Logger) *LiveInputManager {
	return &LiveInputManager{
		inputs: make(map[string]*LiveInput),
		logger: logger.With().Str("component", "live_input_manager").Logger(),
	}
}

// RouteLive routes a live input stream through the DSP graph.
func (lim *LiveInputManager) RouteLive(ctx context.Context, req *pb.RouteLiveRequest) (*pb.RouteLiveResponse, error) {
	lim.mu.Lock()
	defer lim.mu.Unlock()

	sessionID := req.SessionId
	if sessionID == "" {
		return nil, fmt.Errorf("session_id required")
	}

	// Check if already routed
	if existing, exists := lim.inputs[sessionID]; exists {
		if existing.Connected {
			return &pb.RouteLiveResponse{
				Success:   true,
				SessionId: sessionID,
				Message:   "already connected",
			}, nil
		}
	}

	// Build live input pipeline
	// Input source depends on the type specified
	var sourcePipeline string
	inputURL := strings.TrimSpace(req.InputUrl)
	if inputURL == "" && req.Input != nil {
		// Backward compatibility for callers still using the legacy field.
		inputURL = strings.TrimSpace(req.Input.InputUrl)
	}
	switch req.InputType {
	case pb.LiveInputType_LIVE_INPUT_TYPE_ICECAST:
		// Icecast source client (harbor-style)
		// Format: souphttpsrc location=http://source:pass@host:port/mount
		if inputURL == "" {
			return nil, fmt.Errorf("input_url required for ICECAST input")
		}
		sourcePipeline = fmt.Sprintf("souphttpsrc location=\"%s\"", inputURL)

	case pb.LiveInputType_LIVE_INPUT_TYPE_RTP:
		// RTP input
		port := int(req.Port)
		if port <= 0 {
			port = 5004
		}
		sourcePipeline = fmt.Sprintf("udpsrc port=%d ! application/x-rtp", port)

	case pb.LiveInputType_LIVE_INPUT_TYPE_SRT:
		// SRT input (Secure Reliable Transport)
		if inputURL == "" {
			return nil, fmt.Errorf("input_url required for SRT input")
		}
		sourcePipeline = fmt.Sprintf("srtsrc uri=\"%s\"", inputURL)

	case pb.LiveInputType_LIVE_INPUT_TYPE_WEBRTC:
		// WebRTC ingest contract:
		// a signaling/ingest component must bridge browser WebRTC audio into RTP.
		// Media engine consumes that RTP stream here.
		rtpHost, rtpPort := resolveWebRTCRTPBridge(inputURL, int(req.Port))
		inputURL = fmt.Sprintf("udp://%s:%d", rtpHost, rtpPort)
		sourcePipeline = fmt.Sprintf(
			"udpsrc address=%s port=%d caps=\"application/x-rtp,media=audio,encoding-name=OPUS,payload=111,clock-rate=48000\" ! rtpopusdepay ! opusdec",
			rtpHost,
			rtpPort,
		)

	default:
		return nil, fmt.Errorf("unsupported input type: %v", req.InputType)
	}

	// Add decoder
	sourcePipeline += " ! decodebin"

	// If DSP graph handle provided, route through it
	if req.DspGraphHandle != "" {
		// DSP graph would be applied here
		// For now, just pass through with basic processing
		sourcePipeline += " ! audioconvert ! audioresample"
	}

	// Add fade in if requested
	if req.FadeInMs > 0 {
		fadeInSec := float64(req.FadeInMs) / 1000.0
		sourcePipeline += fmt.Sprintf(" ! volume volume=0.0 ! volumeenvelope attack=%.3f", fadeInSec)
	}

	// Create live input record
	liveInput := &LiveInput{
		SessionID:   sessionID,
		StationID:   req.StationId,
		MountID:     req.MountId,
		SourceURL:   inputURL,
		Connected:   true,
		ConnectedAt: time.Now(),
		Pipeline:    sourcePipeline,
		logger:      lim.logger.With().Str("session_id", sessionID).Logger(),
	}

	// Store the input
	lim.inputs[sessionID] = liveInput

	lim.logger.Info().
		Str("session_id", sessionID).
		Str("station_id", req.StationId).
		Str("mount_id", req.MountId).
		Str("input_type", req.InputType.String()).
		Msg("live input routed")

	return &pb.RouteLiveResponse{
		Success:   true,
		SessionId: sessionID,
		Message:   "live input routed successfully",
	}, nil
}

func resolveWebRTCRTPBridge(inputURL string, reqPort int) (host string, port int) {
	host = "127.0.0.1"
	port = defaultWebRTCRTPPort

	if parsed, err := url.Parse(strings.TrimSpace(inputURL)); err == nil {
		if h := strings.TrimSpace(parsed.Hostname()); h != "" {
			host = h
		}
		if p, err := strconv.Atoi(parsed.Port()); err == nil && p > 0 {
			port = p
		}
	}

	if reqPort > 0 {
		port = reqPort
	}

	return host, port
}

// DisconnectLive disconnects a live input.
func (lim *LiveInputManager) DisconnectLive(ctx context.Context, sessionID string) error {
	lim.mu.Lock()
	defer lim.mu.Unlock()

	input, exists := lim.inputs[sessionID]
	if !exists {
		return fmt.Errorf("live input session not found: %s", sessionID)
	}

	// Stop the pipeline if running
	if input.Process != nil {
		if err := input.Process.Stop(); err != nil {
			lim.logger.Error().Err(err).Str("session_id", sessionID).Msg("failed to stop live input pipeline")
		}
	}

	// Mark as disconnected
	input.Connected = false

	// Remove from active inputs
	delete(lim.inputs, sessionID)

	lim.logger.Info().
		Str("session_id", sessionID).
		Dur("duration", time.Since(input.ConnectedAt)).
		Msg("live input disconnected")

	return nil
}

// GetActiveInputs returns all active live inputs.
func (lim *LiveInputManager) GetActiveInputs() map[string]*LiveInput {
	lim.mu.RLock()
	defer lim.mu.RUnlock()

	// Return a copy to avoid race conditions
	inputs := make(map[string]*LiveInput, len(lim.inputs))
	for k, v := range lim.inputs {
		inputs[k] = v
	}

	return inputs
}

// GetInput retrieves a specific live input by session ID.
func (lim *LiveInputManager) GetInput(sessionID string) (*LiveInput, bool) {
	lim.mu.RLock()
	defer lim.mu.RUnlock()

	input, exists := lim.inputs[sessionID]
	return input, exists
}

// Shutdown stops all active live inputs.
func (lim *LiveInputManager) Shutdown() error {
	lim.mu.Lock()
	defer lim.mu.Unlock()

	lim.logger.Info().Int("count", len(lim.inputs)).Msg("shutting down live input manager")

	for sessionID, input := range lim.inputs {
		if input.Process != nil {
			if err := input.Process.Stop(); err != nil {
				lim.logger.Error().Err(err).Str("session_id", sessionID).Msg("failed to stop live input on shutdown")
			}
		}
	}

	// Clear all inputs
	lim.inputs = make(map[string]*LiveInput)

	return nil
}

// Note: GStreamerProcess is now implemented in gstreamer.go
