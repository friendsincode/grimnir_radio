/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// fakeDJAuthServer is a hand-rolled gRPC server that implements the
// DJAuth.ValidateToken contract using the JSON codec the fan-out client also
// uses. We don't depend on protoc-generated code so the test suite is self
// contained; once `make proto` is plumbed into CI on a non-sandboxed runner,
// we can swap to the generated stubs without touching the test logic.
type fakeDJAuthServer struct {
	mu       sync.Mutex
	calls    []ValidateTokenRequest
	verdict  func(req ValidateTokenRequest) (*ValidateTokenResponse, error)
	callsN   int32
	failNext int32 // when >0, the next N calls return Unavailable.
}

func (f *fakeDJAuthServer) ValidateToken(ctx context.Context, req *ValidateTokenRequest) (*ValidateTokenResponse, error) {
	atomic.AddInt32(&f.callsN, 1)
	f.mu.Lock()
	f.calls = append(f.calls, *req)
	verdict := f.verdict
	f.mu.Unlock()
	if atomic.LoadInt32(&f.failNext) > 0 {
		atomic.AddInt32(&f.failNext, -1)
		return nil, status.Error(codes.Unavailable, "fake server: transient")
	}
	if verdict == nil {
		return &ValidateTokenResponse{
			SessionID:       "sess-" + req.Token,
			StationID:       "station-1",
			Username:        "dj-" + req.Token,
			Priority:        1,
			CacheTTLSeconds: 60,
		}, nil
	}
	return verdict(*req)
}

func (f *fakeDJAuthServer) callCount() int {
	return int(atomic.LoadInt32(&f.callsN))
}

func (f *fakeDJAuthServer) lastCall() (ValidateTokenRequest, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return ValidateTokenRequest{}, false
	}
	return f.calls[len(f.calls)-1], true
}

// startFakeDJAuthServer binds a real TCP socket so the production client &
// the in-test server exercise the same grpc-go transport (HTTP/2 + the JSON
// codec). Returns the address & a stop func.
func startFakeDJAuthServer(t *testing.T, srv *fakeDJAuthServer) (string, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	g := grpc.NewServer(grpc.ForceServerCodecV2(djAuthJSONCodecV2{}))
	registerDJAuthFakeServer(g, srv)
	go func() { _ = g.Serve(lis) }()
	return lis.Addr().String(), func() {
		g.Stop()
	}
}

func newTestAuthClient(t *testing.T, addr string) *DJAuthClient {
	t.Helper()
	c, err := NewDJAuthClient(DJAuthClientConfig{
		Addr:    addr,
		Timeout: 2 * time.Second,
		MaxTTL:  5 * time.Minute,
		// insecure: tests run on loopback so no TLS.
		DialOptions: []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	})
	if err != nil {
		t.Fatalf("NewDJAuthClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// Task 7.1 — ValidateToken happy path: claims come back, cache stores them.
func TestDJAuthClient_Validate_HappyPath(t *testing.T) {
	srv := &fakeDJAuthServer{}
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()

	c := newTestAuthClient(t, addr)
	claims, err := c.Validate(context.Background(), "/live", "tok-1", "harbor")
	if err != nil {
		t.Fatalf("Validate err = %v, want nil", err)
	}
	if claims.SessionID != "sess-tok-1" {
		t.Errorf("SessionID = %q, want sess-tok-1", claims.SessionID)
	}
	if claims.StationID != "station-1" {
		t.Errorf("StationID = %q, want station-1", claims.StationID)
	}
	if srv.callCount() != 1 {
		t.Errorf("server calls = %d, want 1", srv.callCount())
	}
	if got, _ := srv.lastCall(); got.Mount != "/live" || got.Token != "tok-1" || got.Protocol != "harbor" {
		t.Errorf("server saw req = %+v, want mount=/live token=tok-1 protocol=harbor", got)
	}
}

// Task 7.1 — Cache hit: second Validate with same (mount, token) skips the
// gRPC roundtrip while inside the TTL window.
func TestDJAuthClient_Validate_CacheHit(t *testing.T) {
	srv := &fakeDJAuthServer{}
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()
	c := newTestAuthClient(t, addr)

	if _, err := c.Validate(context.Background(), "/live", "tok-x", "harbor"); err != nil {
		t.Fatalf("first Validate: %v", err)
	}
	if _, err := c.Validate(context.Background(), "/live", "tok-x", "harbor"); err != nil {
		t.Fatalf("second Validate: %v", err)
	}
	if srv.callCount() != 1 {
		t.Errorf("server calls = %d, want 1 (cache should absorb 2nd)", srv.callCount())
	}
}

// Different (mount, token) tuples must miss separately.
func TestDJAuthClient_Validate_DistinctKeys(t *testing.T) {
	srv := &fakeDJAuthServer{}
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()
	c := newTestAuthClient(t, addr)

	_, _ = c.Validate(context.Background(), "/live", "a", "harbor")
	_, _ = c.Validate(context.Background(), "/live", "b", "harbor") // different token
	_, _ = c.Validate(context.Background(), "/news", "a", "harbor") // different mount
	_, _ = c.Validate(context.Background(), "/live", "a", "webrtc") // different protocol => still same cache (protocol is audit-only)
	if srv.callCount() != 3 {
		t.Errorf("server calls = %d, want 3 (4th is cache hit on (/live, a))", srv.callCount())
	}
}

// TTL expiry: after the server-supplied TTL elapses, the next Validate must
// re-dial.
func TestDJAuthClient_Validate_TTLExpires(t *testing.T) {
	srv := &fakeDJAuthServer{
		verdict: func(req ValidateTokenRequest) (*ValidateTokenResponse, error) {
			return &ValidateTokenResponse{
				SessionID:       "sess",
				StationID:       "s",
				Username:        "dj",
				Priority:        1,
				CacheTTLSeconds: 1, // 1-second TTL
			}, nil
		},
	}
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()
	c := newTestAuthClient(t, addr)
	c.now = func() time.Time { return time.Unix(1000, 0) }

	if _, err := c.Validate(context.Background(), "/live", "k", "harbor"); err != nil {
		t.Fatalf("first Validate: %v", err)
	}
	// Jump past TTL.
	c.now = func() time.Time { return time.Unix(1005, 0) }
	if _, err := c.Validate(context.Background(), "/live", "k", "harbor"); err != nil {
		t.Fatalf("second Validate: %v", err)
	}
	if srv.callCount() != 2 {
		t.Errorf("server calls = %d, want 2 (cache should have expired)", srv.callCount())
	}
}

// MaxTTL clamps a generous server-supplied TTL down to the process-wide cap.
func TestDJAuthClient_Validate_MaxTTLClamp(t *testing.T) {
	srv := &fakeDJAuthServer{
		verdict: func(req ValidateTokenRequest) (*ValidateTokenResponse, error) {
			return &ValidateTokenResponse{
				SessionID:       "sess",
				StationID:       "s",
				Username:        "dj",
				CacheTTLSeconds: 3600, // 1h
			}, nil
		},
	}
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()

	c, err := NewDJAuthClient(DJAuthClientConfig{
		Addr:        addr,
		Timeout:     2 * time.Second,
		MaxTTL:      10 * time.Second, // cap
		DialOptions: []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	})
	if err != nil {
		t.Fatalf("NewDJAuthClient: %v", err)
	}
	defer c.Close()
	c.now = func() time.Time { return time.Unix(1000, 0) }
	if _, err := c.Validate(context.Background(), "/live", "k", "harbor"); err != nil {
		t.Fatalf("first Validate: %v", err)
	}
	// 11s later (past cap, well before server's 1h): cache must miss.
	c.now = func() time.Time { return time.Unix(1011, 0) }
	if _, err := c.Validate(context.Background(), "/live", "k", "harbor"); err != nil {
		t.Fatalf("second Validate: %v", err)
	}
	if srv.callCount() != 2 {
		t.Errorf("server calls = %d, want 2 (MaxTTL should cap)", srv.callCount())
	}
}

// PermissionDenied/NotFound from the server is bubbled up to the caller and
// NOT cached (so a token issued a moment later isn't blocked by a stale "no").
func TestDJAuthClient_Validate_NegativeNotCached(t *testing.T) {
	srv := &fakeDJAuthServer{
		verdict: func(req ValidateTokenRequest) (*ValidateTokenResponse, error) {
			return nil, status.Error(codes.PermissionDenied, "token revoked")
		},
	}
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()
	c := newTestAuthClient(t, addr)

	if _, err := c.Validate(context.Background(), "/live", "k", "harbor"); err == nil {
		t.Fatal("first Validate: err = nil, want non-nil")
	}
	if _, err := c.Validate(context.Background(), "/live", "k", "harbor"); err == nil {
		t.Fatal("second Validate: err = nil, want non-nil")
	}
	if srv.callCount() != 2 {
		t.Errorf("server calls = %d, want 2 (negatives must NOT be cached)", srv.callCount())
	}
}

// Mount normalization: "/live" and "live" (no slash) hit the same cache entry.
func TestDJAuthClient_Validate_MountNormalized(t *testing.T) {
	srv := &fakeDJAuthServer{}
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()
	c := newTestAuthClient(t, addr)

	_, _ = c.Validate(context.Background(), "live", "tok", "harbor")
	_, _ = c.Validate(context.Background(), "/live", "tok", "harbor")
	if srv.callCount() != 1 {
		t.Errorf("server calls = %d, want 1 (mount normalization)", srv.callCount())
	}
	if got, _ := srv.lastCall(); got.Mount != "/live" {
		t.Errorf("server saw mount = %q, want /live (normalized)", got.Mount)
	}
}

// Task 7.2 — Revoke event evicts the cached entry; next Validate re-dials.
func TestDJAuthClient_Revoke_EvictsCache(t *testing.T) {
	srv := &fakeDJAuthServer{}
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()
	c := newTestAuthClient(t, addr)

	if _, err := c.Validate(context.Background(), "/live", "tok-r", "harbor"); err != nil {
		t.Fatalf("first Validate: %v", err)
	}
	c.Revoke("/live", "tok-r")
	if _, err := c.Validate(context.Background(), "/live", "tok-r", "harbor"); err != nil {
		t.Fatalf("post-revoke Validate: %v", err)
	}
	if srv.callCount() != 2 {
		t.Errorf("server calls = %d, want 2 (revoke should have evicted)", srv.callCount())
	}
}

// Revoking a non-cached token is a no-op (idempotent).
func TestDJAuthClient_Revoke_Idempotent(t *testing.T) {
	srv := &fakeDJAuthServer{}
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()
	c := newTestAuthClient(t, addr)

	c.Revoke("/live", "never-seen") // must not panic
}

// HarborAdapter wraps a DJAuthClient so the existing HarborAuthenticator
// contract (mount, user, pass) is satisfied: user is ignored (the token
// carries identity), pass is the token.
func TestHarborAdapter_DelegatesToDJAuthClient(t *testing.T) {
	srv := &fakeDJAuthServer{}
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()
	c := newTestAuthClient(t, addr)

	adapter := NewHarborAuthAdapter(c)
	claims, err := adapter.Validate("/live", "ignored-user", "tok-h")
	if err != nil {
		t.Fatalf("Validate err = %v", err)
	}
	ac, ok := claims.(AuthClaims)
	if !ok {
		t.Fatalf("claims type = %T, want AuthClaims", claims)
	}
	if ac.SessionID != "sess-tok-h" {
		t.Errorf("SessionID = %q", ac.SessionID)
	}
	if got, _ := srv.lastCall(); got.Token != "tok-h" || got.Protocol != "harbor" {
		t.Errorf("server saw req = %+v", got)
	}
}

// HarborAdapter must reject empty creds without dialing the server (matches
// the previous AcceptAllAuthenticator behaviour the listener tests depend on).
func TestHarborAdapter_RejectsEmpty(t *testing.T) {
	srv := &fakeDJAuthServer{}
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()
	c := newTestAuthClient(t, addr)
	adapter := NewHarborAuthAdapter(c)

	if _, err := adapter.Validate("/live", "", ""); err == nil {
		t.Error("empty creds: err = nil, want non-nil")
	}
	if srv.callCount() != 0 {
		t.Errorf("server calls = %d, want 0 (empty creds short-circuit)", srv.callCount())
	}
}

// Server transient failure surfaces as an error (no fail-open).
func TestDJAuthClient_TransientServerErrorBubblesUp(t *testing.T) {
	srv := &fakeDJAuthServer{}
	atomic.StoreInt32(&srv.failNext, 1)
	addr, stop := startFakeDJAuthServer(t, srv)
	defer stop()
	c := newTestAuthClient(t, addr)

	_, err := c.Validate(context.Background(), "/live", "tok", "harbor")
	if err == nil {
		t.Fatal("Validate err = nil, want error on transient server failure")
	}
	if !errors.Is(err, ErrAuthUnavailable) && status.Code(err) != codes.Unavailable {
		t.Errorf("err = %v, want ErrAuthUnavailable or codes.Unavailable", err)
	}
}
