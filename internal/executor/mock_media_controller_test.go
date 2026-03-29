/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package executor

import (
	"context"

	"github.com/friendsincode/grimnir_radio/internal/models"
	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// mockMediaController implements MediaControllerIface for testing.
type mockMediaController struct {
	connected    bool
	loadGraphErr error
	playErr      error
	stopErr      error
	fadeErr      error
	emergencyErr error
	routeLiveErr error
	statusResp   *pb.StatusResponse
	statusErr    error
	streamFunc   func(ctx context.Context, intervalMs int32, cb func(*pb.TelemetryData) error) error
	// track calls
	playCalls int
	stopCalls int
	fadeCalls int
}

func (m *mockMediaController) IsConnected() bool { return m.connected }

func (m *mockMediaController) LoadGraph(ctx context.Context, graph *pb.DSPGraph) (string, error) {
	return "graph-handle", m.loadGraphErr
}

func (m *mockMediaController) Play(ctx context.Context, sourceID, path string, sourceType pb.SourceType, priority models.PriorityLevel, cuePoints *pb.CuePoints) (string, error) {
	m.playCalls++
	return "playback-id", m.playErr
}

func (m *mockMediaController) Stop(ctx context.Context, immediate bool) error {
	m.stopCalls++
	return m.stopErr
}

func (m *mockMediaController) Fade(ctx context.Context, nextSourceID, nextPath string, nextSourceType pb.SourceType, nextCuePoints *pb.CuePoints, fadeConfig *pb.FadeConfig) (string, error) {
	m.fadeCalls++
	return "fade-id", m.fadeErr
}

func (m *mockMediaController) InsertEmergency(ctx context.Context, sourceID, path string) (string, error) {
	return "emergency-id", m.emergencyErr
}

func (m *mockMediaController) RouteLive(ctx context.Context, inputURL, authToken string, applyProcessing bool) (string, error) {
	return "live-id", m.routeLiveErr
}

func (m *mockMediaController) GetStatus(ctx context.Context) (*pb.StatusResponse, error) {
	return m.statusResp, m.statusErr
}

func (m *mockMediaController) StreamTelemetry(ctx context.Context, intervalMs int32, callback func(*pb.TelemetryData) error) error {
	if m.streamFunc != nil {
		return m.streamFunc(ctx, intervalMs, callback)
	}
	return nil
}
