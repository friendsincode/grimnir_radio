/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Codes pulled out so the auth client can compare without importing the
// status package at every call-site (keeps auth.go's `import` block short).
const (
	codesUnavailable      = codes.Unavailable
	codesDeadlineExceeded = codes.DeadlineExceeded
)

func grpcStatusOf(err error) codes.Code {
	return status.Code(err)
}

// djAuthServer is the minimal server-side interface for the DJAuth service.
// Mirrors what protoc-gen-go-grpc would emit for the .proto contract; lives
// here so the fake test server & a future real handler share one shape.
//
// When `make proto` is wired into a non-sandboxed CI runner, the generated
// `DJAuthServer` interface drops in; this hand-rolled mirror gets removed in
// the same commit that registers the generated `RegisterDJAuthServer` shim.
type djAuthServer interface {
	ValidateToken(ctx context.Context, req *ValidateTokenRequest) (*ValidateTokenResponse, error)
}

// djAuthServiceDesc matches what protoc-gen-go-grpc would emit, hand-rolled
// so the test fake can register itself against a real *grpc.Server.
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

// djAuthValidateHandler is the per-RPC server-side dispatcher. Matches the
// signature grpc.ServiceDesc expects; calls the registered server impl.
func djAuthValidateHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(ValidateTokenRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(djAuthServer).ValidateToken(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: djAuthValidateFullMethod,
	}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(djAuthServer).ValidateToken(ctx, req.(*ValidateTokenRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// registerDJAuthFakeServer wires a server impl onto a *grpc.Server. Only the
// test fake currently uses this; once the generated stubs exist, callers
// switch to the proper `grimnirradiov1.RegisterDJAuthServer`.
func registerDJAuthFakeServer(s *grpc.Server, srv djAuthServer) {
	s.RegisterService(&djAuthServiceDesc, srv)
}
