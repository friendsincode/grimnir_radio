/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// makeAudioFixture writes a one-second stereo WAV via ffmpeg and returns its
// path. The test is skipped if ffmpeg is unavailable.
func makeAudioFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	path := filepath.Join(t.TempDir(), "fixture.wav")
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-ac", "2", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg fixture generation failed: %v\n%s", err, out)
	}
	return path
}

func loadedGraph() *pb.DSPGraph {
	return &pb.DSPGraph{
		Nodes: []*pb.DSPNode{
			{Id: "input", Type: pb.NodeType_NODE_TYPE_INPUT},
			{Id: "output", Type: pb.NodeType_NODE_TYPE_OUTPUT},
		},
		Connections: []*pb.DSPConnection{{FromNode: "input", ToNode: "output"}},
	}
}

func TestService_AnalyzerMethods(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns gst-discoverer/ffprobe")
	}
	file := makeAudioFixture(t)
	svc := New(&Config{}, zerolog.Nop())
	defer func() { _ = svc.Shutdown(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := svc.AnalyzeMedia(ctx, &pb.AnalyzeMediaRequest{FilePath: file}); err != nil {
		t.Errorf("AnalyzeMedia() error: %v", err)
	}
	// Artwork extraction runs its code path even though this fixture has no art.
	if _, err := svc.ExtractArtwork(ctx, &pb.ExtractArtworkRequest{FilePath: file, Format: "jpeg"}); err != nil {
		t.Errorf("ExtractArtwork() error: %v", err)
	}
	if _, err := svc.GenerateWaveform(ctx, &pb.GenerateWaveformRequest{FilePath: file, SamplesPerSecond: 10}); err != nil {
		t.Errorf("GenerateWaveform() error: %v", err)
	}
}

func TestService_PlaybackLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns gst-launch")
	}
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		t.Skip("gst-launch not available")
	}
	file := makeAudioFixture(t)
	svc := New(&Config{}, zerolog.Nop())
	defer func() { _ = svc.Shutdown(context.Background()) }()
	ctx := context.Background()

	if _, err := svc.LoadGraph(ctx, &pb.LoadGraphRequest{StationId: "st1", MountId: "mt1", Graph: loadedGraph()}); err != nil {
		t.Fatalf("LoadGraph() error: %v", err)
	}

	src := &pb.SourceConfig{SourceId: "s1", Type: pb.SourceType_SOURCE_TYPE_MEDIA, Path: file}
	if resp, err := svc.Play(ctx, &pb.PlayRequest{StationId: "st1", MountId: "mt1", Source: src}); err != nil || !resp.Success {
		t.Fatalf("Play() err=%v resp=%+v", err, resp)
	}

	next := &pb.SourceConfig{SourceId: "s2", Type: pb.SourceType_SOURCE_TYPE_MEDIA, Path: file}
	fadeReq := &pb.FadeRequest{StationId: "st1", MountId: "mt1", NextSource: next, FadeConfig: &pb.FadeConfig{FadeInMs: 100, FadeOutMs: 100}}
	if resp, err := svc.Fade(ctx, fadeReq); err != nil || !resp.Success {
		t.Errorf("Fade() err=%v resp=%+v", err, resp)
	}

	emg := &pb.SourceConfig{SourceId: "e1", Type: pb.SourceType_SOURCE_TYPE_MEDIA, Path: file}
	if resp, err := svc.InsertEmergency(ctx, &pb.InsertEmergencyRequest{StationId: "st1", MountId: "mt1", Source: emg}); err != nil || !resp.Success {
		t.Errorf("InsertEmergency() err=%v resp=%+v", err, resp)
	}

	if resp, err := svc.Stop(ctx, &pb.StopRequest{StationId: "st1", MountId: "mt1"}); err != nil || !resp.Success {
		t.Errorf("Stop() err=%v resp=%+v", err, resp)
	}
}

func TestService_RecordingLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns gst-launch")
	}
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		t.Skip("gst-launch not available")
	}
	svc := New(&Config{}, zerolog.Nop())
	defer func() { _ = svc.Shutdown(context.Background()) }()
	ctx := context.Background()

	out := filepath.Join(t.TempDir(), "rec.flac")
	start, err := svc.StartRecording(ctx, &pb.StartRecordingRequest{
		StationId: "st1", MountId: "mt1", RecordingId: "rec1", OutputPath: out, Codec: "flac",
	})
	if err != nil || !start.Success {
		t.Fatalf("StartRecording() err=%v resp=%+v", err, start)
	}

	stop, err := svc.StopRecording(ctx, &pb.StopRecordingRequest{StationId: "st1", RecordingId: "rec1"})
	if err != nil || !stop.Success {
		t.Errorf("StopRecording() err=%v resp=%+v", err, stop)
	}
}
