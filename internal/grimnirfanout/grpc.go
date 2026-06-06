/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"
	"time"

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

// SessionMgrStatusProvider adapts a SessionMgr into a StatusProvider. The
// per-protocol counters come straight from SessionMgr.CountByProtocol; the
// engine reachability bits stay false here & get filled in by a future chunk
// that owns the engine health probes.
type SessionMgrStatusProvider struct {
	version string
	mgr     *SessionMgr
	uptime  func() time.Duration
}

// NewSessionMgrStatusProvider wires a SessionMgr into a StatusProvider.
// uptime returns the process uptime; main.go captures the start time in a
// closure (so the provider doesn't have to import time-of-start state).
func NewSessionMgrStatusProvider(version string, mgr *SessionMgr, uptime func() time.Duration) *SessionMgrStatusProvider {
	if uptime == nil {
		uptime = func() time.Duration { return 0 }
	}
	return &SessionMgrStatusProvider{version: version, mgr: mgr, uptime: uptime}
}

// Status implements StatusProvider.
func (p *SessionMgrStatusProvider) Status() Status {
	return Status{
		Version:             p.version,
		UptimeSeconds:       int64(p.uptime().Seconds()),
		ActiveSessions:      int64(p.mgr.Count()),
		HarborSessionCount:  int64(p.mgr.CountByProtocol(ProtocolHarbor)),
		RTPSessionCount:     int64(p.mgr.CountByProtocol(ProtocolRTP)),
		SRTSessionCount:     int64(p.mgr.CountByProtocol(ProtocolSRT)),
		WebRTCSessionCount:  int64(p.mgr.CountByProtocol(ProtocolWebRTC)),
		TotalSessionsServed: p.mgr.TotalSessionsServed(),
		// EngineAReachable / EngineBReachable wired by a later chunk; the
		// engine health-probe lives outside the session manager.
	}
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
