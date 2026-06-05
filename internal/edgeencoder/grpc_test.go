/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"context"
	"net"
	"testing"
	"time"

	pb "github.com/friendsincode/grimnir_radio/proto/edgeencoder/v1"
	mepb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestGRPCGetStatus(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()

	srv := NewGRPCServer(&fakeStatusProvider{
		version:       "test-version",
		activeInput:   "A",
		inputAHealthy: true,
		inputBHealthy: false,
	})
	grpcServer := grpc.NewServer()
	pb.RegisterEdgeEncoderServer(grpcServer, srv)
	go grpcServer.Serve(lis)
	defer grpcServer.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	client := pb.NewEdgeEncoderClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := client.GetStatus(ctx, &pb.StatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Version != "test-version" {
		t.Errorf("Version = %q, want test-version", resp.Version)
	}
	if resp.ActiveInput != "A" {
		t.Errorf("ActiveInput = %q, want A", resp.ActiveInput)
	}
	if !resp.InputAHealthy {
		t.Error("InputAHealthy = false, want true")
	}
	if resp.InputBHealthy {
		t.Error("InputBHealthy = true, want false")
	}
}

type fakeStatusProvider struct {
	version       string
	activeInput   string
	inputAHealthy bool
	inputBHealthy bool
}

func (f *fakeStatusProvider) Status() Status {
	return Status{
		Version:       f.version,
		ActiveInput:   f.activeInput,
		InputAHealthy: f.inputAHealthy,
		InputBHealthy: f.inputBHealthy,
	}
}

func TestEngineHealthSubscriber_MarksUnhealthyAfterConsecutiveFailures(t *testing.T) {
	ih := NewInputHealth(100 * time.Millisecond)
	ih.RecordPacket() // packet side healthy; only gRPC gate matters

	// Start a mediaengine gRPC server, then stop it mid-test.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mockMEServer := grpc.NewServer()
	mepb.RegisterMediaEngineServer(mockMEServer, &fakeMediaEngine{})
	go func() { _ = mockMEServer.Serve(lis) }()

	addr := lis.Addr().String()
	sub := NewEngineHealthSubscriber(addr, ih, 50*time.Millisecond, 3)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = sub.Run(ctx) }()

	// Initially: subscription comes up, marks healthy after first success.
	// Keep packet timestamp fresh so we're testing the gRPC gate, not the
	// packet window.
	deadline := time.Now().Add(500 * time.Millisecond)
	healthySeen := false
	for time.Now().Before(deadline) {
		ih.RecordPacket()
		if ih.IsHealthy() {
			healthySeen = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !healthySeen {
		t.Error("expected healthy after subscriber's first success; got unhealthy")
	}

	// Kill the server; after 3 ticks of failure (150ms more), should mark unhealthy.
	mockMEServer.Stop()
	_ = lis.Close()

	deadline = time.Now().Add(1500 * time.Millisecond)
	gotUnhealthy := false
	for time.Now().Before(deadline) {
		ih.RecordPacket() // keep packet side fresh
		if !ih.IsHealthy() {
			gotUnhealthy = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !gotUnhealthy {
		t.Error("expected unhealthy after server died and 3 consecutive failures; got healthy")
	}
}

// fakeMediaEngine implements just enough of MediaEngineServer for the test.
type fakeMediaEngine struct {
	mepb.UnimplementedMediaEngineServer
}

func (f *fakeMediaEngine) GetStatus(_ context.Context, _ *mepb.StatusRequest) (*mepb.StatusResponse, error) {
	return &mepb.StatusResponse{Running: true}, nil
}
