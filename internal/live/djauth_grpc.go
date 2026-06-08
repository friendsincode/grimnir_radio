/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package live

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// djAuthFullMethod must match the value the fan-out client dials. See
// internal/grimnirfanout/auth.go's djAuthValidateFullMethod.
const djAuthFullMethod = "/grimnirradio.v1.DJAuth/ValidateToken"

// djAuthCacheTTL is the verdict TTL the server hands back. The fan-out then
// clamps to its own process-wide MaxTTL. Five minutes is short enough that a
// revocation-event miss self-heals within a single bus reconnect cycle.
const djAuthCacheTTL = 5 * time.Minute

// wireValidateRequest/Response are hand-rolled mirrors of the protoc-generated
// types. They share the JSON codec the fan-out side already uses, so swapping
// in generated stubs later is a one-line change on both sides.
type wireValidateRequest struct {
	Mount    string `json:"mount"`
	Token    string `json:"token"`
	Protocol string `json:"protocol"`
}

type wireValidateResponse struct {
	SessionID       string `json:"session_id"`
	StationID       string `json:"station_id"`
	Username        string `json:"username"`
	Priority        int32  `json:"priority"`
	CacheTTLSeconds int32  `json:"cache_ttl_seconds"`
}

// DJAuthJSONCodec returns the codec a *grpc.Server must be built with so
// the DJAuth wire format matches the fan-out client. Use as
// `grpc.NewServer(grpc.ForceServerCodecV2(live.DJAuthJSONCodec()))`.
func DJAuthJSONCodec() djAuthJSONCodecV2 { return djAuthJSONCodecV2{} }

// djAuthJSONCodecV2 mirrors the fan-out's codec so a single wire format
// works both sides. Lives here so cmd/grimnirradio can register the server
// without importing internal/grimnirfanout (cyclic).
type djAuthJSONCodecV2 struct{}

func (djAuthJSONCodecV2) Marshal(v any) (mem.BufferSlice, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return mem.BufferSlice{mem.SliceBuffer(b)}, nil
}

func (djAuthJSONCodecV2) Unmarshal(data mem.BufferSlice, v any) error {
	return json.Unmarshal(data.Materialize(), v)
}

func (djAuthJSONCodecV2) Name() string { return "json" }

// djAuthServer is the in-process server interface protoc-gen-go-grpc would
// emit. *Service satisfies it via ValidateToken below.
type djAuthServer interface {
	ValidateToken(ctx context.Context, req *wireValidateRequest) (*wireValidateResponse, error)
}

// djAuthServiceDesc registers the service against a *grpc.Server. The method
// table & names match what protoc-gen-go-grpc would emit; the dispatcher
// shape matches grpc-go's expected ServiceDesc contract.
var djAuthServiceDesc = grpc.ServiceDesc{
	ServiceName: "grimnirradio.v1.DJAuth",
	HandlerType: (*djAuthServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "ValidateToken",
			Handler:    djAuthValidateHandler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/grimnirradio/v1/djauth.proto",
}

func djAuthValidateHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(wireValidateRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(djAuthServer).ValidateToken(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: djAuthFullMethod}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(djAuthServer).ValidateToken(ctx, req.(*wireValidateRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// RegisterDJAuthServer wires a *Service onto a *grpc.Server. Used by
// cmd/grimnirradio/main.go to expose the DJAuth gRPC endpoint.
func RegisterDJAuthServer(s *grpc.Server, svc *Service) {
	s.RegisterService(&djAuthServiceDesc, svc)
}

// ValidateToken is the on-wire RPC entry point. It wraps the existing
// AuthorizeSource path so the in-process auth logic isn't duplicated.
func (s *Service) ValidateToken(ctx context.Context, req *wireValidateRequest) (*wireValidateResponse, error) {
	in := DJAuthRequest{Mount: req.Mount, Token: req.Token, Protocol: req.Protocol}
	resp, err := s.ValidateDJToken(ctx, in)
	if err != nil {
		return nil, err
	}
	return &wireValidateResponse{
		SessionID:       resp.SessionID,
		StationID:       resp.StationID,
		Username:        resp.Username,
		Priority:        resp.Priority,
		CacheTTLSeconds: resp.CacheTTLSeconds,
	}, nil
}

// normalizeMountPath strips a leading slash so the value lines up with what's
// stored in models.Mount.Name. Mirrors the fan-out client's normalizeMount so
// "/live" and "live" both resolve.
func normalizeMountPath(m string) string {
	m = strings.TrimSpace(m)
	if m == "" {
		return ""
	}
	m = strings.TrimPrefix(m, "/")
	m = strings.TrimSuffix(m, "/")
	// Strip a trailing format suffix the way Harbor does, so /live.mp3 also
	// resolves to mount "live".
	if i := strings.LastIndex(m, "."); i > 0 {
		// only treat short suffixes as format hints
		if len(m)-i <= 5 {
			m = m[:i]
		}
	}
	return m
}

// insecureCreds is exposed so the in-package round-trip test can dial without
// pulling in google.golang.org/grpc/credentials/insecure at the call site.
func insecureCreds() grpc.DialOption {
	return grpc.WithTransportCredentials(insecure.NewCredentials())
}

// ValidateDJToken resolves (mount, token) to the underlying live session and
// returns the claims a fan-out wants to cache. Wraps AuthorizeSource — does
// not duplicate auth logic. Returns gRPC status errors so the server-side
// dispatcher serializes the right codes onto the wire.
func (s *Service) ValidateDJToken(ctx context.Context, req DJAuthRequest) (*DJAuthResponse, error) {
	if req.Mount == "" || req.Token == "" {
		return nil, status.Error(codes.InvalidArgument, "mount and token are required")
	}

	mountName := normalizeMountPath(req.Mount)
	if mountName == "" {
		return nil, status.Error(codes.InvalidArgument, "mount required")
	}

	// Resolve session by token first; the token is the strongest identifier
	// (uniquely indexed) so we don't need station_id from the client.
	var session models.LiveSession
	err := s.db.WithContext(ctx).Where("token = ?", req.Token).First(&session).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, "unknown token")
		}
		return nil, status.Errorf(codes.Internal, "session lookup: %v", err)
	}

	// Resolve mount by id, then confirm the client's claimed mount path
	// matches what the token was minted against.
	var mount models.Mount
	if err := s.db.WithContext(ctx).First(&mount, "id = ?", session.MountID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, "mount missing for session")
		}
		return nil, status.Errorf(codes.Internal, "mount lookup: %v", err)
	}
	if mount.Name != mountName {
		return nil, status.Error(codes.PermissionDenied, "mount mismatch")
	}

	// Delegate to the existing v1 auth helper. This is the single source of
	// truth — adds the same logging, the same future hardening (expiry,
	// one-time-use enforcement) without forking the auth tree.
	ok, err := s.AuthorizeSource(ctx, session.StationID, session.MountID, req.Token)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidToken):
			return nil, status.Error(codes.NotFound, "invalid or expired token")
		case errors.Is(err, ErrTokenAlreadyUsed):
			return nil, status.Error(codes.PermissionDenied, "token already used")
		case errors.Is(err, ErrUnauthorized):
			return nil, status.Error(codes.PermissionDenied, "unauthorized")
		default:
			return nil, status.Errorf(codes.Internal, "authorize: %v", err)
		}
	}
	if !ok {
		return nil, status.Error(codes.PermissionDenied, "authorization rejected")
	}

	return &DJAuthResponse{
		SessionID:       session.ID,
		StationID:       session.StationID,
		Username:        session.Username,
		Priority:        int32(session.Priority),
		CacheTTLSeconds: int32(djAuthCacheTTL / time.Second),
	}, nil
}

// PublishDJAuthRevoke fires the dj.auth.revoke event so any fan-out cache
// subscriber drops its cached entry. Used by the admin "end session" /
// disconnect path. Idempotent: missing/empty token publishes the all-purge
// payload so a control-plane restart can be signalled by sending {all:true}.
func (s *Service) PublishDJAuthRevoke(mount, token string) {
	if s == nil || s.bus == nil {
		return
	}
	payload := events.Payload{}
	if token == "" {
		payload["all"] = true
	} else {
		payload["mount"] = mount
		payload["token"] = token
	}
	s.bus.Publish(EventDJAuthRevoke, payload)
}

// EventDJAuthRevoke is the event-bus type name the fan-out's
// AuthRevocationSubscriber listens on. Mirrors
// internal/grimnirfanout/auth_revocation.go's constant — string-equal so the
// in-process bus & a future Redis pub/sub bridge route the same channel.
const EventDJAuthRevoke events.EventType = "dj.auth.revoke"
