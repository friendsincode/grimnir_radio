/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package client

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// failEngine returns Success:false with an Error on the RPCs whose wrappers have
// a !resp.Success branch, so we can prove those wrappers surface the failure.
// The rest are inherited from fakeEngine (all Success:true).
type failEngine struct{ fakeEngine }

func (failEngine) LoadGraph(context.Context, *pb.LoadGraphRequest) (*pb.LoadGraphResponse, error) {
	return &pb.LoadGraphResponse{Success: false, Error: "bad graph"}, nil
}
func (failEngine) Play(context.Context, *pb.PlayRequest) (*pb.PlayResponse, error) {
	return &pb.PlayResponse{Success: false, Error: "missing file"}, nil
}
func (failEngine) Stop(context.Context, *pb.StopRequest) (*pb.StopResponse, error) {
	return &pb.StopResponse{Success: false, Error: "not running"}, nil
}
func (failEngine) Fade(context.Context, *pb.FadeRequest) (*pb.FadeResponse, error) {
	return &pb.FadeResponse{Success: false, Error: "fade denied"}, nil
}
func (failEngine) InsertEmergency(context.Context, *pb.InsertEmergencyRequest) (*pb.InsertEmergencyResponse, error) {
	return &pb.InsertEmergencyResponse{Success: false, Error: "recorder busy"}, nil
}

func startFailEngine(t *testing.T) (*Client, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	pb.RegisterMediaEngineServer(srv, failEngine{})
	go func() { _ = srv.Serve(lis) }()

	c := New(DefaultConfig(lis.Addr().String()), zerolog.Nop())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		srv.Stop()
		t.Fatalf("Connect: %v", err)
	}
	return c, func() { _ = c.Close(); srv.Stop() }
}

// TestClientWrappers_SurfaceEngineFailure guards #84: fakeEngine always returned
// Success:true, so the only real logic in these wrappers — the `if !resp.Success`
// branch that turns resp.Error into a Go error — was never exercised. A dropped
// check would silently swallow a failed engine op as success.
func TestClientWrappers_SurfaceEngineFailure(t *testing.T) {
	c, cleanup := startFailEngine(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := c.LoadGraph(ctx, "st", "mt", &pb.DSPGraph{}); err == nil || !strings.Contains(err.Error(), "bad graph") {
		t.Errorf("LoadGraph: err=%v, want it to surface 'bad graph'", err)
	}
	if _, err := c.Play(ctx, &pb.PlayRequest{}); err == nil || !strings.Contains(err.Error(), "missing file") {
		t.Errorf("Play: err=%v, want 'missing file'", err)
	}
	if err := c.Stop(ctx, "st", "mt", true); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("Stop: err=%v, want 'not running'", err)
	}
	if _, _, err := c.Fade(ctx, &pb.FadeRequest{}); err == nil || !strings.Contains(err.Error(), "fade denied") {
		t.Errorf("Fade: err=%v, want 'fade denied'", err)
	}
	if _, err := c.InsertEmergency(ctx, "st", "mt", &pb.SourceConfig{}); err == nil || !strings.Contains(err.Error(), "recorder busy") {
		t.Errorf("InsertEmergency: err=%v, want 'recorder busy'", err)
	}
}
