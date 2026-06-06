/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestLiveInputController_Defaults verifies a freshly constructed controller
// reports the DJ as not live and exposes empty source info.
func TestLiveInputController_Defaults(t *testing.T) {
	c := NewLiveInputController(zerolog.Nop())
	if c.IsActive() {
		t.Errorf("new controller IsActive() = true, want false")
	}
	if got := c.SourceAddr(); got != "" {
		t.Errorf("new controller SourceAddr() = %q, want empty", got)
	}
	if got := c.SessionID(); got != "" {
		t.Errorf("new controller SessionID() = %q, want empty", got)
	}
}

// TestLiveInputController_SetActiveToggles verifies SetLiveInput stores the
// active flag plus the metadata-supplied source_addr & session_id.
func TestLiveInputController_SetActiveToggles(t *testing.T) {
	c := NewLiveInputController(zerolog.Nop())

	md := metadata.New(map[string]string{
		"x-grimnir-source-addr": "10.10.0.7:9100",
		"x-grimnir-session-id":  "dj-session-42",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	if _, err := c.SetLiveInput(ctx, wrapperspb.Bool(true)); err != nil {
		t.Fatalf("SetLiveInput(true) error = %v", err)
	}
	if !c.IsActive() {
		t.Errorf("after SetLiveInput(true), IsActive() = false, want true")
	}
	if got := c.SourceAddr(); got != "10.10.0.7:9100" {
		t.Errorf("SourceAddr() = %q, want %q", got, "10.10.0.7:9100")
	}
	if got := c.SessionID(); got != "dj-session-42" {
		t.Errorf("SessionID() = %q, want %q", got, "dj-session-42")
	}

	if _, err := c.SetLiveInput(context.Background(), wrapperspb.Bool(false)); err != nil {
		t.Fatalf("SetLiveInput(false) error = %v", err)
	}
	if c.IsActive() {
		t.Errorf("after SetLiveInput(false), IsActive() = true, want false")
	}
	// SourceAddr & SessionID retain last-seen values so post-disconnect
	// telemetry still has something to report.
	if got := c.SourceAddr(); got != "10.10.0.7:9100" {
		t.Errorf("after disconnect SourceAddr() = %q, want last value %q", got, "10.10.0.7:9100")
	}
}

// TestLiveInputController_NilRequestErrors guards against accidental nil
// dereference.
func TestLiveInputController_NilRequestErrors(t *testing.T) {
	c := NewLiveInputController(zerolog.Nop())
	if _, err := c.SetLiveInput(context.Background(), nil); err == nil {
		t.Errorf("SetLiveInput(nil) returned nil error, want non-nil")
	}
}

// TestLiveInputController_ServiceDescIsRegisterable verifies the controller
// exposes a grpc.ServiceDesc so it can be wired into the engine's gRPC server.
func TestLiveInputController_ServiceDescIsRegisterable(t *testing.T) {
	c := NewLiveInputController(zerolog.Nop())
	desc := c.ServiceDesc()
	if desc == nil {
		t.Fatalf("ServiceDesc() returned nil")
	}
	if desc.ServiceName != "mediaengine.v1.LiveInputControl" {
		t.Errorf("ServiceName = %q, want %q", desc.ServiceName, "mediaengine.v1.LiveInputControl")
	}
	if len(desc.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(desc.Methods))
	}
	if desc.Methods[0].MethodName != "SetLiveInput" {
		t.Errorf("MethodName = %q, want SetLiveInput", desc.Methods[0].MethodName)
	}
}

// TestLiveInputController_EndToEndGRPC stands up the controller behind a
// real gRPC server on localhost, makes a client call using the same wire
// contract (BoolValue + metadata) the fan-out will use, and verifies the
// flag was set.
func TestLiveInputController_EndToEndGRPC(t *testing.T) {
	ctrl := NewLiveInputController(zerolog.Nop())

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	srv := grpc.NewServer()
	srv.RegisterService(ctrl.ServiceDesc(), ctrl)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.GracefulStop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ctx = metadata.AppendToOutgoingContext(ctx,
		"x-grimnir-source-addr", "10.20.30.40:9100",
		"x-grimnir-session-id", "sess-xyz",
	)

	out := new(emptypb.Empty)
	if err := conn.Invoke(ctx, "/mediaengine.v1.LiveInputControl/SetLiveInput", wrapperspb.Bool(true), out); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if !ctrl.IsActive() {
		t.Errorf("after wire-level SetLiveInput(true), IsActive() = false")
	}
	if got := ctrl.SourceAddr(); got != "10.20.30.40:9100" {
		t.Errorf("SourceAddr() = %q, want %q", got, "10.20.30.40:9100")
	}
	if got := ctrl.SessionID(); got != "sess-xyz" {
		t.Errorf("SessionID() = %q, want %q", got, "sess-xyz")
	}
}
