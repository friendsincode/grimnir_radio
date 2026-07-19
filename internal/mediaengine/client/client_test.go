/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("localhost:9091")
	if cfg.Address != "localhost:9091" {
		t.Errorf("Address = %q", cfg.Address)
	}
	if cfg.MaxRetries != 3 || cfg.RetryInterval != 2*time.Second || cfg.ConnectionTimeout != 10*time.Second {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestNewClient_NotConnected(t *testing.T) {
	c := New(DefaultConfig("localhost:9091"), zerolog.Nop())
	if c.addr != "localhost:9091" {
		t.Errorf("addr = %q", c.addr)
	}
	if c.IsConnected() {
		t.Error("a fresh client must not report connected")
	}
	// Close with no underlying connection is a no-op that returns nil.
	if err := c.Close(); err != nil {
		t.Errorf("Close() on unconnected client = %v, want nil", err)
	}
}

// Every RPC wrapper guards on a nil client, so on an unconnected client they
// all return a "not connected" error instead of panicking on the nil gRPC stub.
func TestRPCMethods_NotConnected(t *testing.T) {
	c := New(DefaultConfig("localhost:9091"), zerolog.Nop())
	ctx := context.Background()

	checks := map[string]func() error{
		"LoadGraph":       func() error { _, err := c.LoadGraph(ctx, "s", "m", &pb.DSPGraph{}); return err },
		"Play":            func() error { _, err := c.Play(ctx, &pb.PlayRequest{}); return err },
		"Stop":            func() error { return c.Stop(ctx, "s", "m", true) },
		"Fade":            func() error { _, _, err := c.Fade(ctx, &pb.FadeRequest{}); return err },
		"InsertEmergency": func() error { _, err := c.InsertEmergency(ctx, "s", "m", &pb.SourceConfig{}); return err },
		"RouteLive":       func() error { _, err := c.RouteLive(ctx, &RouteLiveRequest{}); return err },
		"GetStatus":       func() error { _, err := c.GetStatus(ctx, "s", "m"); return err },
		"StreamTelemetry": func() error { return c.StreamTelemetry(ctx, "s", "m", 100, nil) },
		"AnalyzeMedia":    func() error { _, err := c.AnalyzeMedia(ctx, "/f.mp3"); return err },
		"ExtractArtwork":  func() error { _, err := c.ExtractArtwork(ctx, "/f.mp3", 0, 0, "jpeg", 0); return err },
		"GenerateWaveform": func() error {
			_, err := c.GenerateWaveform(ctx, "/f.mp3", 10, pb.WaveformType(0))
			return err
		},
		"StartRecording": func() error { return c.StartRecording(ctx, &StartRecordingRequest{}) },
		"StopRecording":  func() error { _, err := c.StopRecording(ctx, "s", "r"); return err },
	}

	for name, call := range checks {
		if err := call(); err == nil {
			t.Errorf("%s on an unconnected client: expected an error, got nil", name)
		}
	}
}

func TestRetry_SucceedsFirstTry(t *testing.T) {
	c := New(DefaultConfig("x"), zerolog.Nop())
	calls := 0
	err := c.Retry(context.Background(), func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("Retry() = %v, want nil", err)
	}
	if calls != 1 {
		t.Errorf("operation called %d times, want 1", calls)
	}
}

func TestRetry_DoesNotRetryTerminalCodes(t *testing.T) {
	c := New(DefaultConfig("x"), zerolog.Nop())
	calls := 0
	want := status.Error(codes.InvalidArgument, "bad")
	err := c.Retry(context.Background(), func() error {
		calls++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("Retry() = %v, want the InvalidArgument error", err)
	}
	if calls != 1 {
		t.Errorf("operation called %d times, want 1 (no retry on InvalidArgument)", calls)
	}
}

func TestRetry_HonorsContextCancellation(t *testing.T) {
	c := New(DefaultConfig("x"), zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel up front so the backoff wait returns immediately

	calls := 0
	err := c.Retry(ctx, func() error {
		calls++
		return status.Error(codes.Unavailable, "retryable")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Retry() = %v, want context.Canceled", err)
	}
	// One attempt runs, then the cancelled context short-circuits the backoff.
	if calls != 1 {
		t.Errorf("operation called %d times, want 1", calls)
	}
}
