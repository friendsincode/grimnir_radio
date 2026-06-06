/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"context"
	"errors"
	"sync"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// LiveInputController records the current "DJ is live" state announced by
// the fan-out node. See proto/mediaengine/v1/liveinput.proto for the wire
// contract.
//
// The engine's GStreamer pipeline runs an always-on udpsrc/audiomixer
// branch (see internal/playout/director.go buildDualBroadcastPipeline);
// audiomixer auto-selects the louder input so this controller's flag does
// not gate the audio path itself. The flag is used for telemetry & for
// future priority-aware ducking decisions.
type LiveInputController struct {
	mu         sync.RWMutex
	active     bool
	sourceAddr string
	sessionID  string

	logger zerolog.Logger
}

// NewLiveInputController constructs a controller with sensible defaults.
func NewLiveInputController(logger zerolog.Logger) *LiveInputController {
	return &LiveInputController{
		logger: logger.With().Str("component", "live_input_controller").Logger(),
	}
}

// IsActive returns the last-known DJ-active state.
func (c *LiveInputController) IsActive() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.active
}

// SourceAddr returns the fan-out source_addr announced on the most recent
// SetLiveInput call (retained across active=false toggles).
func (c *LiveInputController) SourceAddr() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sourceAddr
}

// SessionID returns the DJ session_id announced on the most recent
// SetLiveInput call (retained across active=false toggles).
func (c *LiveInputController) SessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionID
}

// SetLiveInput implements the LiveInputControl/SetLiveInput RPC. The active
// bool rides BoolValue.value; source_addr & session_id ride gRPC metadata
// keys "x-grimnir-source-addr" & "x-grimnir-session-id".
func (c *LiveInputController) SetLiveInput(ctx context.Context, req *wrapperspb.BoolValue) (*emptypb.Empty, error) {
	if req == nil {
		return nil, errors.New("SetLiveInput: nil request")
	}

	var sourceAddr, sessionID string
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vs := md.Get("x-grimnir-source-addr"); len(vs) > 0 {
			sourceAddr = vs[0]
		}
		if vs := md.Get("x-grimnir-session-id"); len(vs) > 0 {
			sessionID = vs[0]
		}
	}

	c.mu.Lock()
	c.active = req.GetValue()
	if sourceAddr != "" {
		c.sourceAddr = sourceAddr
	}
	if sessionID != "" {
		c.sessionID = sessionID
	}
	logged := struct {
		active     bool
		sourceAddr string
		sessionID  string
	}{c.active, c.sourceAddr, c.sessionID}
	c.mu.Unlock()

	c.logger.Info().
		Bool("active", logged.active).
		Str("source_addr", logged.sourceAddr).
		Str("session_id", logged.sessionID).
		Msg("live input state updated")

	return &emptypb.Empty{}, nil
}

// liveInputServiceName is the wire name registered with the gRPC server.
// Matches proto/mediaengine/v1/liveinput.proto.
const liveInputServiceName = "mediaengine.v1.LiveInputControl"

// ServiceDesc returns a grpc.ServiceDesc the engine can hand to
// grpc.Server.RegisterService. Hand-rolled because we deliberately avoid
// regenerating mediaengine.pb.go for a single small RPC.
func (c *LiveInputController) ServiceDesc() *grpc.ServiceDesc {
	return &grpc.ServiceDesc{
		ServiceName: liveInputServiceName,
		HandlerType: (*liveInputServer)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "SetLiveInput",
				Handler:    setLiveInputHandler,
			},
		},
		Streams:  []grpc.StreamDesc{},
		Metadata: "proto/mediaengine/v1/liveinput.proto",
	}
}

// liveInputServer is the interface the grpc.ServiceDesc references. The
// concrete implementation is *LiveInputController.
type liveInputServer interface {
	SetLiveInput(context.Context, *wrapperspb.BoolValue) (*emptypb.Empty, error)
}

func setLiveInputHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(wrapperspb.BoolValue)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(liveInputServer).SetLiveInput(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/" + liveInputServiceName + "/SetLiveInput",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(liveInputServer).SetLiveInput(ctx, req.(*wrapperspb.BoolValue))
	}
	return interceptor(ctx, in, info, handler)
}
