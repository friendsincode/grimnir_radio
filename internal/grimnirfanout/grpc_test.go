/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"
	"net"
	"testing"
	"time"

	pb "github.com/friendsincode/grimnir_radio/proto/grimnirfanout/v1"
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
		version:             "test-version",
		activeSessions:      2,
		harborSessions:      1,
		rtpSessions:         1,
		totalSessionsServed: 17,
		engineAReachable:    true,
		engineBReachable:    false,
	})
	grpcServer := grpc.NewServer()
	pb.RegisterGrimnirFanoutServer(grpcServer, srv)
	go func() { _ = grpcServer.Serve(lis) }()
	defer grpcServer.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	client := pb.NewGrimnirFanoutClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := client.GetStatus(ctx, &pb.StatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Version != "test-version" {
		t.Errorf("Version = %q, want test-version", resp.Version)
	}
	if resp.ActiveSessions != 2 {
		t.Errorf("ActiveSessions = %d, want 2", resp.ActiveSessions)
	}
	if resp.HarborSessionCount != 1 {
		t.Errorf("HarborSessionCount = %d, want 1", resp.HarborSessionCount)
	}
	if resp.RtpSessionCount != 1 {
		t.Errorf("RtpSessionCount = %d, want 1", resp.RtpSessionCount)
	}
	if resp.TotalSessionsServed != 17 {
		t.Errorf("TotalSessionsServed = %d, want 17", resp.TotalSessionsServed)
	}
	if !resp.EngineAReachable {
		t.Error("EngineAReachable = false, want true")
	}
	if resp.EngineBReachable {
		t.Error("EngineBReachable = true, want false")
	}
}

type fakeStatusProvider struct {
	version             string
	activeSessions      int64
	harborSessions      int64
	rtpSessions         int64
	srtSessions         int64
	webrtcSessions      int64
	totalSessionsServed int64
	engineAReachable    bool
	engineBReachable    bool
}

func (f *fakeStatusProvider) Status() Status {
	return Status{
		Version:             f.version,
		ActiveSessions:      f.activeSessions,
		HarborSessionCount:  f.harborSessions,
		RTPSessionCount:     f.rtpSessions,
		SRTSessionCount:     f.srtSessions,
		WebRTCSessionCount:  f.webrtcSessions,
		TotalSessionsServed: f.totalSessionsServed,
		EngineAReachable:    f.engineAReachable,
		EngineBReachable:    f.engineBReachable,
	}
}
