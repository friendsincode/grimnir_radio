/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

func TestOutputConfigFromGraph_Defaults(t *testing.T) {
	// A nil graph, and a graph with no output node, both yield the MP3/test default.
	for _, g := range []*pb.DSPGraph{nil, {Nodes: []*pb.DSPNode{{Type: pb.NodeType_NODE_TYPE_INPUT}}}} {
		cfg := outputConfigFromGraph(g)
		if cfg.OutputType != OutputTypeTest || cfg.Format != AudioFormatMP3 {
			t.Errorf("defaults: got type=%v format=%v", cfg.OutputType, cfg.Format)
		}
		if cfg.Bitrate != 128 || cfg.SampleRate != 44100 || cfg.Channels != 2 {
			t.Errorf("defaults: got bitrate=%d rate=%d ch=%d", cfg.Bitrate, cfg.SampleRate, cfg.Channels)
		}
	}
}

func TestOutputConfigFromGraph_MapsParams(t *testing.T) {
	graph := &pb.DSPGraph{Nodes: []*pb.DSPNode{
		{Type: pb.NodeType_NODE_TYPE_OUTPUT, Params: map[string]string{
			"output_type": "http",
			"format":      "opus",
			"bitrate":     "256",
			"sample_rate": "48000",
			"channels":    "1",
			"output_url":  "http://sink/x",
		}},
	}}
	cfg := outputConfigFromGraph(graph)
	if cfg.OutputType != OutputTypeHTTP {
		t.Errorf("OutputType = %v, want http", cfg.OutputType)
	}
	if cfg.Format != AudioFormatOpus {
		t.Errorf("Format = %v, want opus", cfg.Format)
	}
	if cfg.Bitrate != 256 || cfg.SampleRate != 48000 || cfg.Channels != 1 {
		t.Errorf("got bitrate=%d rate=%d ch=%d", cfg.Bitrate, cfg.SampleRate, cfg.Channels)
	}
	if cfg.OutputURL != "http://sink/x" {
		t.Errorf("OutputURL = %q", cfg.OutputURL)
	}
}

func TestOutputConfigFromGraph_RTPAndFilePathFallback(t *testing.T) {
	graph := &pb.DSPGraph{Nodes: []*pb.DSPNode{
		{Type: pb.NodeType_NODE_TYPE_OUTPUT, Params: map[string]string{
			"output_type": "rtp",
			"rtp_host":    "127.0.0.1",
			"rtp_port":    "5004",
			"pt":          "111",
			"file_path":   "/tmp/out.mp3", // used when output_url is absent
		}},
	}}
	cfg := outputConfigFromGraph(graph)
	if cfg.OutputType != OutputTypeRTP {
		t.Errorf("OutputType = %v, want rtp", cfg.OutputType)
	}
	if cfg.RTPHost != "127.0.0.1" || cfg.RTPPort != 5004 || cfg.RTPPayloadType != 111 {
		t.Errorf("rtp fields: host=%q port=%d pt=%d", cfg.RTPHost, cfg.RTPPort, cfg.RTPPayloadType)
	}
	if cfg.OutputURL != "/tmp/out.mp3" {
		t.Errorf("OutputURL fallback = %q, want /tmp/out.mp3", cfg.OutputURL)
	}
}

func TestGetSourceID(t *testing.T) {
	if got := getSourceID(nil); got != "" {
		t.Errorf("getSourceID(nil) = %q, want empty", got)
	}
	if got := getSourceID(&pb.SourceConfig{SourceId: "src-9"}); got != "src-9" {
		t.Errorf("getSourceID = %q, want src-9", got)
	}
}

func TestService_NewAndShutdown(t *testing.T) {
	svc := New(&Config{}, zerolog.Nop())
	if svc == nil {
		t.Fatal("New() returned nil")
	}
	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error: %v", err)
	}
}

func TestService_GetOrCreateStation(t *testing.T) {
	svc := New(&Config{}, zerolog.Nop())
	defer func() { _ = svc.Shutdown(context.Background()) }()

	if svc.getStation("none") != nil {
		t.Error("getStation should return nil for unknown station")
	}
	e1 := svc.getOrCreateStation("st1", "mt1")
	e2 := svc.getOrCreateStation("st1", "mt1")
	if e1 != e2 {
		t.Error("getOrCreateStation should return the same engine on repeat")
	}
	if e1.MountID != "mt1" {
		t.Errorf("engine MountID = %q, want mt1", e1.MountID)
	}
}

func TestService_GetStatus(t *testing.T) {
	svc := New(&Config{}, zerolog.Nop())
	defer func() { _ = svc.Shutdown(context.Background()) }()

	resp, err := svc.GetStatus(context.Background(), &pb.StatusRequest{StationId: "unknown"})
	if err != nil {
		t.Fatalf("GetStatus() error: %v", err)
	}
	if resp.Running {
		t.Error("expected Running=false for unknown station")
	}

	svc.getOrCreateStation("st1", "mt1")
	resp, err = svc.GetStatus(context.Background(), &pb.StatusRequest{StationId: "st1"})
	if err != nil {
		t.Fatalf("GetStatus() error: %v", err)
	}
	if !resp.Running {
		t.Error("expected Running=true for known station")
	}
}
