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
