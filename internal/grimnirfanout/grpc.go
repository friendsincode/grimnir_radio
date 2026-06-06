/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"

	pb "github.com/friendsincode/grimnir_radio/proto/grimnirfanout/v1"
)

// Status is the snapshot returned by the gRPC GetStatus call. The
// StatusProvider interface lets the gRPC server be tested in isolation from
// the session manager (which doesn't exist yet; Chunk 2). Mirrors the
// edgeencoder.Status pattern.
type Status struct {
	Version             string
	UptimeSeconds       int64
	ActiveSessions      int64
	HarborSessionCount  int64
	RTPSessionCount     int64
	SRTSessionCount     int64
	WebRTCSessionCount  int64
	TotalSessionsServed int64
	EngineAReachable    bool
	EngineBReachable    bool
}

// StatusProvider is implemented by whatever owns live session/engine state.
// The gRPC server queries it on every call.
type StatusProvider interface {
	Status() Status
}

// GRPCServer implements pb.GrimnirFanoutServer.
type GRPCServer struct {
	pb.UnimplementedGrimnirFanoutServer
	provider StatusProvider
}

// NewGRPCServer constructs a GRPCServer that delegates Status queries to the
// provided StatusProvider.
func NewGRPCServer(provider StatusProvider) *GRPCServer {
	return &GRPCServer{provider: provider}
}

// GetStatus returns the fan-out's current status from the underlying provider.
func (s *GRPCServer) GetStatus(_ context.Context, _ *pb.StatusRequest) (*pb.StatusResponse, error) {
	st := s.provider.Status()
	return &pb.StatusResponse{
		Version:             st.Version,
		UptimeSeconds:       st.UptimeSeconds,
		ActiveSessions:      st.ActiveSessions,
		HarborSessionCount:  st.HarborSessionCount,
		RtpSessionCount:     st.RTPSessionCount,
		SrtSessionCount:     st.SRTSessionCount,
		WebrtcSessionCount:  st.WebRTCSessionCount,
		TotalSessionsServed: st.TotalSessionsServed,
		EngineAReachable:    st.EngineAReachable,
		EngineBReachable:    st.EngineBReachable,
	}, nil
}
