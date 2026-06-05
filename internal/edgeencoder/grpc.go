/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"context"

	pb "github.com/friendsincode/grimnir_radio/proto/edgeencoder/v1"
)

// Status is the current operational state of the edge encoder, as seen by
// the gRPC GetStatus call. The StatusProvider interface lets the gRPC server
// be tested in isolation from the pipeline (which doesn't exist yet; Chunks 4+).
type Status struct {
	Version       string
	UptimeSeconds int64
	ActiveInput   string
	InputAHealthy bool
	InputBHealthy bool
	ListenerCount int64
	SwitchCount   int64
}

// StatusProvider is implemented by whatever owns the live state (pipeline +
// switcher + broadcast adapter, wired in later chunks). The gRPC server
// queries it on every call.
type StatusProvider interface {
	Status() Status
}

// GRPCServer implements pb.EdgeEncoderServer.
type GRPCServer struct {
	pb.UnimplementedEdgeEncoderServer
	provider StatusProvider
}

// NewGRPCServer constructs a GRPCServer that delegates Status queries to the
// provided StatusProvider.
func NewGRPCServer(provider StatusProvider) *GRPCServer {
	return &GRPCServer{provider: provider}
}

// GetStatus returns the encoder's current status from the underlying provider.
func (s *GRPCServer) GetStatus(_ context.Context, _ *pb.StatusRequest) (*pb.StatusResponse, error) {
	st := s.provider.Status()
	return &pb.StatusResponse{
		Version:       st.Version,
		UptimeSeconds: st.UptimeSeconds,
		ActiveInput:   st.ActiveInput,
		InputAHealthy: st.InputAHealthy,
		InputBHealthy: st.InputBHealthy,
		ListenerCount: st.ListenerCount,
		SwitchCount:   st.SwitchCount,
	}, nil
}
