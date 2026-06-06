/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package live

import (
	"context"
	"errors"
)

// TODO(chunk-7.x control-plane side):
//
// This file is the placeholder for the grimnirradio control plane's DJAuth
// gRPC service implementation. The fan-out binary already speaks
// grimnirradio.v1.DJAuth.ValidateToken via a JSON-over-grpc codec (see
// proto/grimnirradio/v1/djauth.proto + internal/grimnirfanout/auth.go).
//
// What's still missing:
//
//  1. cmd/grimnirradio doesn't expose a gRPC server. Either add one alongside
//     the existing HTTP API, or route the DJAuth.ValidateToken RPC through a
//     gRPC-over-HTTP-bridge if we want to keep one port.
//
//  2. Register a server impl that calls Service.AuthorizeSource (already in
//     this package; see service.go). The wire-level mapping is:
//
//        ValidateTokenRequest{ Mount, Token, Protocol }
//          ↓
//        Service.AuthorizeSource(ctx, stationID, mountID, token)
//        — where stationID is derived from Mount via the Mount model lookup,
//        and mountID is the Mount's primary key.
//          ↓
//        ValidateTokenResponse{
//          SessionID:       liveSession.ID,
//          StationID:       liveSession.StationID,
//          Username:        liveSession.Username,
//          Priority:        int32(liveSession.Priority),
//          CacheTTLSeconds: clamp(remaining-token-ttl, MaxFanoutCacheTTL),
//        }
//
//  3. Publish revocation events on the existing events.Bus with EventType
//     "dj.auth.revoke" (constant defined in internal/grimnirfanout/
//     auth_revocation.go as EventDJAuthRevoke). Payload:
//       {"mount": "/live", "token": "..."}   — single token
//       {"all": true}                        — purge all
//     The fan-out's AuthRevocationSubscriber listens on the same channel via
//     Redis pub/sub or in-process bus.
//
// Until #1 lands, the fan-out can't actually call this endpoint in
// production; the integration is exercised end-to-end by the fan-out's own
// unit tests against the fake server in auth_test.go.

// ErrDJAuthGRPCNotWired is returned by callers that try to use the gRPC
// server before it's plumbed into cmd/grimnirradio's main.go.
var ErrDJAuthGRPCNotWired = errors.New("live: DJAuth gRPC server not yet wired into control plane")

// DJAuthRequest is the in-process Go mirror of grimnirradio.v1
// .ValidateTokenRequest. Lives here so the control-plane authorization
// helper that future versions wire to gRPC can be unit-tested without
// reaching for the protoc-generated code.
type DJAuthRequest struct {
	Mount    string
	Token    string
	Protocol string
}

// DJAuthResponse is the in-process Go mirror of grimnirradio.v1
// .ValidateTokenResponse.
type DJAuthResponse struct {
	SessionID       string
	StationID       string
	Username        string
	Priority        int32
	CacheTTLSeconds int32
}

// ValidateDJToken is the function the future gRPC handler will call. Right
// now it returns ErrDJAuthGRPCNotWired so any accidental wiring lights up
// immediately; once the gRPC server is registered in cmd/grimnirradio/main.go,
// this becomes the integration point that joins Service.AuthorizeSource +
// LiveSession lookup into the response shape the fan-out cache expects.
func (s *Service) ValidateDJToken(ctx context.Context, req DJAuthRequest) (*DJAuthResponse, error) {
	// Intentionally NOT calling Service.AuthorizeSource here yet — the
	// (StationID, MountID) derivation from the wire-level Mount string needs
	// a Mount-table lookup we don't have in scope. The follow-up CL will:
	//   1. Resolve req.Mount -> models.Mount via s.db.
	//   2. Call s.AuthorizeSource(ctx, mount.StationID, mount.ID, req.Token).
	//   3. Look up the matching LiveSession & return its fields.
	return nil, ErrDJAuthGRPCNotWired
}
