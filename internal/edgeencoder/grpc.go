/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"context"
	"fmt"
	"time"

	pb "github.com/friendsincode/grimnir_radio/proto/edgeencoder/v1"
	mepb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
	// Engine-divergence detection (issue #236). Phase 1 reports the RTP-
	// timestamp comparison only; an audio-fingerprint follow-up is tracked
	// separately. The detector observes & reports; it does NOT pin or force-
	// switch on its own.
	DivergenceDetected       bool
	DivergenceCount          int64
	LastDivergenceSecondsAgo int64 // -1 when no divergence has occurred
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
		Version:                  st.Version,
		UptimeSeconds:            st.UptimeSeconds,
		ActiveInput:              st.ActiveInput,
		InputAHealthy:            st.InputAHealthy,
		InputBHealthy:            st.InputBHealthy,
		ListenerCount:            st.ListenerCount,
		SwitchCount:              st.SwitchCount,
		DivergenceDetected:       st.DivergenceDetected,
		DivergenceCount:          st.DivergenceCount,
		LastDivergenceSecondsAgo: st.LastDivergenceSecondsAgo,
	}, nil
}

// EngineHealthSubscriber polls an engine's GetStatus gRPC method on a ticker
// and flips the associated InputHealth's gRPC gate based on the result. After
// failuresThreshold consecutive failures the gate flips to false; on a single
// successful response the gate flips back to true. Transitions are reported
// via the optional onTransition callback so callers can log them at INFO.
type EngineHealthSubscriber struct {
	addr              string
	health            *InputHealth
	tick              time.Duration
	failuresThreshold int
	onTransition      func(healthy bool, err error)
}

// NewEngineHealthSubscriber constructs a subscriber. addr is the engine's
// gRPC host:port, health is the InputHealth whose gRPC gate gets toggled,
// tick is the poll interval, and failuresThreshold is the number of
// consecutive GetStatus errors required before the gate flips to unhealthy.
func NewEngineHealthSubscriber(addr string, health *InputHealth, tick time.Duration, failuresThreshold int) *EngineHealthSubscriber {
	return &EngineHealthSubscriber{
		addr:              addr,
		health:            health,
		tick:              tick,
		failuresThreshold: failuresThreshold,
	}
}

// SetTransitionCallback registers a callback fired only on healthy<->unhealthy
// transitions. nil disables it. Safe to call before Run.
func (s *EngineHealthSubscriber) SetTransitionCallback(cb func(healthy bool, err error)) {
	s.onTransition = cb
}

// Run blocks until ctx is cancelled. Dials the configured engine once and
// reuses the connection for every poll; grpc.NewClient handles reconnect on
// transient failures so per-call errors get surfaced via GetStatus.
func (s *EngineHealthSubscriber) Run(ctx context.Context) error {
	conn, err := grpc.NewClient(s.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial %s: %w", s.addr, err)
	}
	defer func() { _ = conn.Close() }()
	client := mepb.NewMediaEngineClient(conn)

	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	failures := 0
	// lastReportedHealthy nil = no transition reported yet; we treat the
	// initial state as "unknown" so the first definitive verdict (healthy on
	// first success, or unhealthy after N failures) is reported.
	var lastReportedHealthy *bool

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			cctx, cancel := context.WithTimeout(ctx, s.tick/2)
			_, callErr := client.GetStatus(cctx, &mepb.StatusRequest{})
			cancel()
			if callErr != nil {
				failures++
				if failures >= s.failuresThreshold {
					s.health.SetGRPCHealthy(false)
					if lastReportedHealthy == nil || *lastReportedHealthy {
						f := false
						lastReportedHealthy = &f
						if s.onTransition != nil {
							s.onTransition(false, callErr)
						}
					}
				}
				continue
			}
			failures = 0
			s.health.SetGRPCHealthy(true)
			if lastReportedHealthy == nil || !*lastReportedHealthy {
				t := true
				lastReportedHealthy = &t
				if s.onTransition != nil {
					s.onTransition(true, nil)
				}
			}
		}
	}
}
