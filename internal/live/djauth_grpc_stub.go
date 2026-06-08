/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package live

// This file used to hold a stub that returned ErrDJAuthGRPCNotWired. The real
// gRPC server lives in djauth_grpc.go (v2.0.0-rc.6 — audit warning W-1).
// Types kept here for callers that import the in-process Go shape without
// depending on the wire codec.

// DJAuthRequest is the in-process Go mirror of grimnirradio.v1
// .ValidateTokenRequest. Lives in package live so callers can use the
// auth helper without depending on the gRPC codec types.
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
