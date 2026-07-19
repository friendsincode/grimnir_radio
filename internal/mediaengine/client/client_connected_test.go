/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package client

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// fakeEngine is a minimal in-process MediaEngine server. It returns canned
// success responses so the client's connected code paths (Connect plus each RPC
// wrapper's request build and response handling) run without GStreamer.
type fakeEngine struct {
	pb.UnimplementedMediaEngineServer
}

func (fakeEngine) LoadGraph(context.Context, *pb.LoadGraphRequest) (*pb.LoadGraphResponse, error) {
	return &pb.LoadGraphResponse{Success: true, GraphHandle: "h1"}, nil
}
func (fakeEngine) Play(context.Context, *pb.PlayRequest) (*pb.PlayResponse, error) {
	return &pb.PlayResponse{Success: true, PlaybackId: "p1"}, nil
}
func (fakeEngine) Stop(context.Context, *pb.StopRequest) (*pb.StopResponse, error) {
	return &pb.StopResponse{Success: true}, nil
}
func (fakeEngine) Fade(context.Context, *pb.FadeRequest) (*pb.FadeResponse, error) {
	return &pb.FadeResponse{Success: true, FadeId: "f1", EstimatedDurationMs: 100}, nil
}
func (fakeEngine) InsertEmergency(context.Context, *pb.InsertEmergencyRequest) (*pb.InsertEmergencyResponse, error) {
	return &pb.InsertEmergencyResponse{Success: true, EmergencyId: "e1"}, nil
}
func (fakeEngine) RouteLive(context.Context, *pb.RouteLiveRequest) (*pb.RouteLiveResponse, error) {
	return &pb.RouteLiveResponse{Success: true, SessionId: "s1"}, nil
}
func (fakeEngine) GetStatus(context.Context, *pb.StatusRequest) (*pb.StatusResponse, error) {
	return &pb.StatusResponse{Running: true}, nil
}
func (fakeEngine) AnalyzeMedia(context.Context, *pb.AnalyzeMediaRequest) (*pb.AnalyzeMediaResponse, error) {
	return &pb.AnalyzeMediaResponse{Success: true}, nil
}
func (fakeEngine) ExtractArtwork(context.Context, *pb.ExtractArtworkRequest) (*pb.ExtractArtworkResponse, error) {
	return &pb.ExtractArtworkResponse{}, nil
}
func (fakeEngine) GenerateWaveform(context.Context, *pb.GenerateWaveformRequest) (*pb.GenerateWaveformResponse, error) {
	return &pb.GenerateWaveformResponse{}, nil
}
func (fakeEngine) StartRecording(context.Context, *pb.StartRecordingRequest) (*pb.StartRecordingResponse, error) {
	return &pb.StartRecordingResponse{Success: true}, nil
}
func (fakeEngine) StopRecording(context.Context, *pb.StopRecordingRequest) (*pb.StopRecordingResponse, error) {
	return &pb.StopRecordingResponse{Success: true, RecordingId: "r1", FileSizeBytes: 10, DurationMs: 20}, nil
}
func (fakeEngine) StreamTelemetry(_ *pb.TelemetryRequest, stream pb.MediaEngine_StreamTelemetryServer) error {
	// Send two updates then return; the client loop should see both and then EOF.
	for i := 0; i < 2; i++ {
		if err := stream.Send(&pb.TelemetryData{}); err != nil {
			return err
		}
	}
	return nil
}

// startFakeEngine starts the fake server on a random port and returns a
// connected client plus a cleanup func.
func startFakeEngine(t *testing.T) (*Client, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	pb.RegisterMediaEngineServer(srv, fakeEngine{})
	go func() { _ = srv.Serve(lis) }()

	c := New(DefaultConfig(lis.Addr().String()), zerolog.Nop())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		srv.Stop()
		t.Fatalf("Connect: %v", err)
	}

	cleanup := func() {
		_ = c.Close()
		srv.Stop()
	}
	return c, cleanup
}

func TestConnectAndIsConnected(t *testing.T) {
	c, cleanup := startFakeEngine(t)
	defer cleanup()

	if !c.IsConnected() {
		t.Error("client should report connected after Connect")
	}
	// A second Connect on an already-connected client is a no-op that succeeds.
	if err := c.Connect(context.Background()); err != nil {
		t.Errorf("second Connect() = %v, want nil", err)
	}
}

func TestConnectedRPCWrappers(t *testing.T) {
	c, cleanup := startFakeEngine(t)
	defer cleanup()
	ctx := context.Background()

	if h, err := c.LoadGraph(ctx, "s", "m", &pb.DSPGraph{}); err != nil || h != "h1" {
		t.Errorf("LoadGraph = %q, %v; want h1, nil", h, err)
	}
	if id, err := c.Play(ctx, &pb.PlayRequest{}); err != nil || id != "p1" {
		t.Errorf("Play = %q, %v; want p1, nil", id, err)
	}
	if err := c.Stop(ctx, "s", "m", true); err != nil {
		t.Errorf("Stop = %v, want nil", err)
	}
	if fid, dur, err := c.Fade(ctx, &pb.FadeRequest{}); err != nil || fid != "f1" || dur != 100 {
		t.Errorf("Fade = %q, %d, %v; want f1, 100, nil", fid, dur, err)
	}
	if eid, err := c.InsertEmergency(ctx, "s", "m", &pb.SourceConfig{}); err != nil || eid != "e1" {
		t.Errorf("InsertEmergency = %q, %v; want e1, nil", eid, err)
	}
	if sid, err := c.RouteLive(ctx, &RouteLiveRequest{}); err != nil || sid != "s1" {
		t.Errorf("RouteLive = %q, %v; want s1, nil", sid, err)
	}
	if st, err := c.GetStatus(ctx, "s", "m"); err != nil || !st.Running {
		t.Errorf("GetStatus running=%v, %v; want true, nil", st.GetRunning(), err)
	}
	if _, err := c.AnalyzeMedia(ctx, "/f.mp3"); err != nil {
		t.Errorf("AnalyzeMedia = %v, want nil", err)
	}
	if _, err := c.ExtractArtwork(ctx, "/f.mp3", 0, 0, "jpeg", 0); err != nil {
		t.Errorf("ExtractArtwork = %v, want nil", err)
	}
	if _, err := c.GenerateWaveform(ctx, "/f.mp3", 10, pb.WaveformType(0)); err != nil {
		t.Errorf("GenerateWaveform = %v, want nil", err)
	}
	if err := c.StartRecording(ctx, &StartRecordingRequest{}); err != nil {
		t.Errorf("StartRecording = %v, want nil", err)
	}
	if res, err := c.StopRecording(ctx, "s", "r1"); err != nil || res.RecordingID != "r1" {
		t.Errorf("StopRecording = %+v, %v; want r1, nil", res, err)
	}
}

func TestConnectedStreamTelemetry(t *testing.T) {
	c, cleanup := startFakeEngine(t)
	defer cleanup()

	got := 0
	err := c.StreamTelemetry(context.Background(), "s", "m", 100, func(*pb.TelemetryData) error {
		got++
		return nil
	})
	if err != nil {
		t.Fatalf("StreamTelemetry() = %v, want nil", err)
	}
	if got != 2 {
		t.Errorf("callback fired %d times, want 2", got)
	}
}

func TestConnect_Failure(t *testing.T) {
	c := New(DefaultConfig("127.0.0.1:1"), zerolog.Nop())
	// A cancelled context makes the readiness wait fail fast instead of blocking
	// on the (unreachable) address for the full 5s.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Connect(ctx); err == nil {
		t.Error("Connect to an unreachable address with a cancelled context should error")
		_ = c.Close()
	}
}
