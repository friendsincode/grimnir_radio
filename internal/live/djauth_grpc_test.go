/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package live

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// seedMountAndSession sets up a station + mount + un-used live session token.
// Returns the chosen mount path ("/live") & the token.
func seedMountAndSession(t *testing.T, svc *Service, mountName string) (mountPath, token string) {
	t.Helper()
	station := &models.Station{ID: uuid.NewString(), Name: "Test"}
	if err := svc.db.Create(station).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	mount := &models.Mount{
		ID:        uuid.NewString(),
		StationID: station.ID,
		Name:      mountName, // e.g. "live"
		Format:    "mp3",
	}
	if err := svc.db.Create(mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}
	tok, err := svc.GenerateToken(context.Background(), GenerateTokenRequest{
		StationID: station.ID,
		MountID:   mount.ID,
		UserID:    uuid.NewString(),
		Username:  "dj-test",
		Priority:  models.PriorityLiveScheduled,
		ExpiresIn: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("seed token: %v", err)
	}
	return "/" + mountName, tok
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	db := setupTestDB(t)
	// Mount model is not part of the shared setup; AutoMigrate it here.
	if err := db.AutoMigrate(&models.Mount{}); err != nil {
		t.Fatalf("migrate mount: %v", err)
	}
	pSvc := priority.NewService(db, nil, zerolog.Nop())
	bus := events.NewBus()
	return NewService(db, pSvc, bus, zerolog.Nop())
}

// TestDJAuth_ValidatesGoodToken: a seeded, un-used token resolves to a
// ValidateResponse with the correct (station_id, username, priority).
func TestDJAuth_ValidatesGoodToken(t *testing.T) {
	svc := newTestService(t)
	mountPath, token := seedMountAndSession(t, svc, "live")

	resp, err := svc.ValidateDJToken(context.Background(), DJAuthRequest{
		Mount:    mountPath,
		Token:    token,
		Protocol: "harbor",
	})
	if err != nil {
		t.Fatalf("ValidateDJToken err = %v, want nil", err)
	}
	if resp.Username != "dj-test" {
		t.Errorf("Username = %q, want dj-test", resp.Username)
	}
	if resp.Priority != int32(models.PriorityLiveScheduled) {
		t.Errorf("Priority = %d, want %d", resp.Priority, models.PriorityLiveScheduled)
	}
	if resp.SessionID == "" {
		t.Error("SessionID is empty; want UUID")
	}
	if resp.StationID == "" {
		t.Error("StationID is empty; want station UUID")
	}
	if resp.CacheTTLSeconds <= 0 {
		t.Errorf("CacheTTLSeconds = %d, want > 0", resp.CacheTTLSeconds)
	}
}

// TestDJAuth_RejectsBadToken: an unknown token returns a PermissionDenied /
// NotFound gRPC status.
func TestDJAuth_RejectsBadToken(t *testing.T) {
	svc := newTestService(t)
	_, _ = seedMountAndSession(t, svc, "live")

	_, err := svc.ValidateDJToken(context.Background(), DJAuthRequest{
		Mount:    "/live",
		Token:    "definitely-not-a-real-token",
		Protocol: "harbor",
	})
	if err == nil {
		t.Fatal("ValidateDJToken err = nil, want non-nil")
	}
	c := status.Code(err)
	if c != codes.NotFound && c != codes.PermissionDenied {
		t.Errorf("status code = %v, want NotFound or PermissionDenied", c)
	}
}

// TestDJAuth_RejectsMountMismatch: token is real but the client claims the
// wrong mount path. Server rejects with PermissionDenied.
func TestDJAuth_RejectsMountMismatch(t *testing.T) {
	svc := newTestService(t)
	_, token := seedMountAndSession(t, svc, "live")

	_, err := svc.ValidateDJToken(context.Background(), DJAuthRequest{
		Mount:    "/wrongmount",
		Token:    token,
		Protocol: "harbor",
	})
	if err == nil {
		t.Fatal("ValidateDJToken err = nil, want non-nil")
	}
	if c := status.Code(err); c != codes.PermissionDenied {
		t.Errorf("status code = %v, want PermissionDenied", c)
	}
}

// TestDJAuth_NormalizesMountPath: client sends "live" (no leading slash) &
// "/live"; both resolve to the same mount.
func TestDJAuth_NormalizesMountPath(t *testing.T) {
	svc := newTestService(t)
	_, token := seedMountAndSession(t, svc, "live")

	// With leading slash.
	if _, err := svc.ValidateDJToken(context.Background(), DJAuthRequest{
		Mount: "/live", Token: token,
	}); err != nil {
		t.Errorf("with slash: err = %v, want nil", err)
	}
	// Without.
	if _, err := svc.ValidateDJToken(context.Background(), DJAuthRequest{
		Mount: "live", Token: token,
	}); err != nil {
		t.Errorf("no slash: err = %v, want nil", err)
	}
}

// TestDJAuth_EmptyInputs: empty mount or empty token => InvalidArgument.
func TestDJAuth_EmptyInputs(t *testing.T) {
	svc := newTestService(t)

	cases := []DJAuthRequest{
		{Mount: "", Token: "x"},
		{Mount: "/live", Token: ""},
	}
	for _, in := range cases {
		_, err := svc.ValidateDJToken(context.Background(), in)
		if err == nil {
			t.Errorf("empty input %+v: err = nil, want InvalidArgument", in)
			continue
		}
		if c := status.Code(err); c != codes.InvalidArgument {
			t.Errorf("empty input %+v: code = %v, want InvalidArgument", in, c)
		}
	}
}

// TestDJAuth_PublishRevoke fires the dj.auth.revoke event so a subscriber
// (the fan-out's AuthRevocationSubscriber) drops its cached entry.
func TestDJAuth_PublishRevoke(t *testing.T) {
	svc := newTestService(t)
	sub := svc.bus.Subscribe(EventDJAuthRevoke)
	defer svc.bus.Unsubscribe(EventDJAuthRevoke, sub)

	svc.PublishDJAuthRevoke("/live", "tok-xyz")

	select {
	case p := <-sub:
		if p["mount"] != "/live" || p["token"] != "tok-xyz" {
			t.Errorf("payload = %+v, want mount=/live token=tok-xyz", p)
		}
	case <-time.After(time.Second):
		t.Fatal("no event delivered within 1s")
	}
}

// Empty token => the "purge all" form of the revoke event.
func TestDJAuth_PublishRevoke_AllForm(t *testing.T) {
	svc := newTestService(t)
	sub := svc.bus.Subscribe(EventDJAuthRevoke)
	defer svc.bus.Unsubscribe(EventDJAuthRevoke, sub)

	svc.PublishDJAuthRevoke("", "")
	select {
	case p := <-sub:
		all, _ := p["all"].(bool)
		if !all {
			t.Errorf("payload = %+v, want {all:true}", p)
		}
	case <-time.After(time.Second):
		t.Fatal("no event delivered within 1s")
	}
}

// Integration: a real gRPC server bound on loopback receives a real
// ValidateToken RPC from a hand-rolled client (mirrors the fanout-side
// DJAuthClient wire format).
func TestDJAuth_GRPCServer_RoundTrip(t *testing.T) {
	svc := newTestService(t)
	mountPath, token := seedMountAndSession(t, svc, "live")

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	g := grpc.NewServer(grpc.ForceServerCodecV2(djAuthJSONCodecV2{}))
	RegisterDJAuthServer(g, svc)
	go func() { _ = g.Serve(lis) }()
	t.Cleanup(g.Stop)

	// Build a minimal client that speaks the same JSON-codec contract.
	cc, err := grpc.NewClient(
		lis.Addr().String(),
		insecureCreds(),
		grpc.WithDefaultCallOptions(grpc.ForceCodecV2(djAuthJSONCodecV2{})),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = cc.Close() })

	req := &wireValidateRequest{Mount: mountPath, Token: token, Protocol: "harbor"}
	resp := &wireValidateResponse{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := cc.Invoke(ctx, "/grimnirradio.v1.DJAuth/ValidateToken", req, resp); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Username != "dj-test" {
		t.Errorf("Username = %q", resp.Username)
	}
	if resp.SessionID == "" {
		t.Error("SessionID empty over the wire")
	}
}
