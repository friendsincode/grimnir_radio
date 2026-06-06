/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/mem"
)

// ErrAuthUnavailable is returned when the control-plane gRPC roundtrip itself
// failed (network, deadline, server transient). Callers treat this as a hard
// reject — the fan-out never fail-opens.
var ErrAuthUnavailable = errors.New("grimnirfanout: control-plane auth unavailable")

// ErrAuthRejected is returned when the control plane refused the credential
// (NOT_FOUND / PERMISSION_DENIED). Callers reject the wire-level handshake.
var ErrAuthRejected = errors.New("grimnirfanout: credentials rejected")

// AuthClaims are the fields the fan-out keeps about a validated DJ session.
// Mirrors proto/grimnirradio/v1/djauth.proto's ValidateTokenResponse so the
// JSON-over-grpc wire format stays in lockstep with the proto contract. When
// `make proto` becomes available on the CI runner, the generated stubs slot
// in & this struct becomes the codec's intermediate form.
type AuthClaims struct {
	SessionID string
	StationID string
	Username  string
	Priority  int32
}

// ValidateTokenRequest is the wire form of the DJAuth.ValidateToken request.
// Hand-rolled mirror of the proto message; JSON-tagged for the custom codec.
type ValidateTokenRequest struct {
	Mount    string `json:"mount"`
	Token    string `json:"token"`
	Protocol string `json:"protocol"`
}

// ValidateTokenResponse is the wire form of the DJAuth.ValidateToken
// response. cache_ttl_seconds is normalized to int32 so the JSON & proto
// shapes match exactly.
type ValidateTokenResponse struct {
	SessionID       string `json:"session_id"`
	StationID       string `json:"station_id"`
	Username        string `json:"username"`
	Priority        int32  `json:"priority"`
	CacheTTLSeconds int32  `json:"cache_ttl_seconds"`
}

// djAuthValidateFullMethod is the gRPC service-path the client dials. Matches
// service grimnirradio.v1.DJAuth { rpc ValidateToken(...) returns (...); } in
// the .proto contract.
const djAuthValidateFullMethod = "/grimnirradio.v1.DJAuth/ValidateToken"

// djAuthCodecName is the content-subtype the grpc-go transport advertises in
// the `application/grpc+<name>` Content-Type header. We use "json" so a
// future swap to the protoc-generated stubs is a one-line dial-option flip
// (drop the ForceCodec, both sides default back to "proto").
const djAuthCodecName = "json"

// djAuthJSONCodecV2 satisfies google.golang.org/grpc/encoding.CodecV2 with a
// straight encoding/json round-trip. This sidesteps the protoc dependency
// while still using the standard grpc-go transport (HTTP/2, deadlines,
// status codes, metadata).
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

func (djAuthJSONCodecV2) Name() string { return djAuthCodecName }

// DJAuthClientConfig wires a DJAuthClient. Addr is the control-plane gRPC
// host:port; Timeout caps a single ValidateToken call; MaxTTL caps how long
// a positive verdict can stay cached (server can ask for less; never more).
// CacheSize defaults to 1024 entries.
type DJAuthClientConfig struct {
	Addr        string
	Timeout     time.Duration
	MaxTTL      time.Duration
	CacheSize   int
	DialOptions []grpc.DialOption
}

// DJAuthClient is the fan-out side of the DJAuth gRPC service. Maintains an
// LRU cache keyed by (normalized mount, token) so reconnects don't hit the
// control plane on every handshake.
type DJAuthClient struct {
	cc      *grpc.ClientConn
	timeout time.Duration
	maxTTL  time.Duration

	cache *lruTTLCache

	// now is overridable so tests can fast-forward without sleeping.
	now func() time.Time
}

// NewDJAuthClient dials the control-plane gRPC server & prepares the cache.
// Does not block on the first call: dial happens on the background pool, the
// first Validate inherits its result.
func NewDJAuthClient(cfg DJAuthClientConfig) (*DJAuthClient, error) {
	if cfg.Addr == "" {
		return nil, errors.New("DJAuthClient: Addr required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 3 * time.Second
	}
	if cfg.MaxTTL <= 0 {
		cfg.MaxTTL = 5 * time.Minute
	}
	if cfg.CacheSize <= 0 {
		cfg.CacheSize = 1024
	}

	dialOpts := append([]grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.ForceCodecV2(djAuthJSONCodecV2{})),
	}, cfg.DialOptions...)

	cc, err := grpc.NewClient(cfg.Addr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("DJAuthClient dial: %w", err)
	}
	return &DJAuthClient{
		cc:      cc,
		timeout: cfg.Timeout,
		maxTTL:  cfg.MaxTTL,
		cache:   newLRUTTLCache(cfg.CacheSize),
		now:     time.Now,
	}, nil
}

// Close drops the underlying gRPC connection. Safe to call on a half-built
// client (Close on nil cc is a no-op).
func (c *DJAuthClient) Close() error {
	if c == nil || c.cc == nil {
		return nil
	}
	return c.cc.Close()
}

// Validate consults the cache first; on miss, dials the control plane,
// stores the verdict (only positive ones), returns claims.
//
// Normalization: mount gets a leading slash if missing so "/live" and "live"
// collapse to one cache entry. Protocol is passed through for the audit log
// on the server side but is NOT part of the cache key (otherwise a DJ that
// reconnects on a different protocol re-roundtrips for no reason).
func (c *DJAuthClient) Validate(ctx context.Context, mount, token, protocol string) (AuthClaims, error) {
	mount = normalizeMount(mount)
	key := cacheKey(mount, token)

	if claims, ok := c.cache.get(key, c.now()); ok {
		return claims, nil
	}

	dialCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req := &ValidateTokenRequest{Mount: mount, Token: token, Protocol: protocol}
	resp := &ValidateTokenResponse{}
	if err := c.cc.Invoke(dialCtx, djAuthValidateFullMethod, req, resp); err != nil {
		// Network/transport-class failures get wrapped so callers can detect
		// "control plane down" separately from "credentials refused".
		if isTransportError(err) {
			return AuthClaims{}, fmt.Errorf("%w: %v", ErrAuthUnavailable, err)
		}
		return AuthClaims{}, err
	}

	claims := AuthClaims{
		SessionID: resp.SessionID,
		StationID: resp.StationID,
		Username:  resp.Username,
		Priority:  resp.Priority,
	}
	ttl := time.Duration(resp.CacheTTLSeconds) * time.Second
	if ttl > c.maxTTL {
		ttl = c.maxTTL
	}
	if ttl > 0 {
		c.cache.put(key, claims, c.now().Add(ttl))
	}
	return claims, nil
}

// Revoke evicts the cached entry for (mount, token), if any. Used by the
// event-bus subscriber in Task 7.2 when the control plane publishes a
// revocation. Idempotent: revoking a non-cached token is a silent no-op.
func (c *DJAuthClient) Revoke(mount, token string) {
	c.cache.delete(cacheKey(normalizeMount(mount), token))
}

// RevokeAll wipes the cache. Useful on a control-plane restart where the
// fanout can't be sure which tokens are still valid.
func (c *DJAuthClient) RevokeAll() {
	c.cache.purge()
}

// normalizeMount ensures a single leading slash & no trailing slash so cache
// keys don't fragment. Empty stays empty (caller's responsibility to reject).
func normalizeMount(m string) string {
	if m == "" {
		return ""
	}
	if !strings.HasPrefix(m, "/") {
		m = "/" + m
	}
	if len(m) > 1 && strings.HasSuffix(m, "/") {
		m = strings.TrimRight(m, "/")
	}
	return m
}

func cacheKey(mount, token string) string {
	// "\x00" separator: tokens are hex/base64, mounts are URL paths; neither
	// contains a NUL so the join is unambiguous.
	return mount + "\x00" + token
}

// isTransportError returns true for the gRPC status codes that indicate the
// control plane is unreachable rather than that it rejected the credential.
// We treat Unavailable + DeadlineExceeded as transport-class so callers can
// differentiate "ask again later" from "this token will never work."
func isTransportError(err error) bool {
	if err == nil {
		return false
	}
	s := grpcStatusOf(err)
	return s == codesUnavailable || s == codesDeadlineExceeded
}

// HarborAuthAdapter bridges the (mount, user, pass) -> claims interface
// HarborListener expects onto the (mount, token) -> AuthClaims interface
// DJAuthClient exposes. user is ignored (the token carries identity);
// pass IS the token.
type HarborAuthAdapter struct {
	client *DJAuthClient
}

// NewHarborAuthAdapter wraps a DJAuthClient as a HarborAuthenticator.
func NewHarborAuthAdapter(c *DJAuthClient) *HarborAuthAdapter {
	return &HarborAuthAdapter{client: c}
}

// Validate satisfies HarborAuthenticator. Returns AuthClaims (typed, not
// map[string]any) so downstream gets structured fields. Empty pass short
// circuits without dialing — saves the control plane a useless rejection.
func (a *HarborAuthAdapter) Validate(mount, user, pass string) (any, error) {
	if mount == "" || pass == "" {
		return nil, errors.New("empty credentials")
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.client.timeout)
	defer cancel()
	claims, err := a.client.Validate(ctx, mount, pass, "harbor")
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// ValidateWebRTC is the WebRTC ingress's entry point: token comes from the
// SDP offer JSON body, not Basic auth. Returns AuthClaims so the per-peer
// session carries structured identity.
func (c *DJAuthClient) ValidateWebRTC(ctx context.Context, mount, token string) (AuthClaims, error) {
	if mount == "" || token == "" {
		return AuthClaims{}, errors.New("empty credentials")
	}
	return c.Validate(ctx, mount, token, "webrtc")
}

// -- LRU+TTL cache (no external dep). ---------------------------------------

type lruEntry struct {
	key       string
	claims    AuthClaims
	expiresAt time.Time
}

type lruTTLCache struct {
	mu    sync.Mutex
	cap   int
	ll    *list.List
	index map[string]*list.Element
}

func newLRUTTLCache(capacity int) *lruTTLCache {
	return &lruTTLCache{
		cap:   capacity,
		ll:    list.New(),
		index: make(map[string]*list.Element, capacity),
	}
}

func (c *lruTTLCache) get(key string, now time.Time) (AuthClaims, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.index[key]
	if !ok {
		return AuthClaims{}, false
	}
	entry := el.Value.(*lruEntry)
	if !entry.expiresAt.After(now) {
		// Expired; drop & report miss so the caller re-dials.
		c.removeElement(el)
		return AuthClaims{}, false
	}
	c.ll.MoveToFront(el)
	return entry.claims, true
}

func (c *lruTTLCache) put(key string, claims AuthClaims, expiresAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.index[key]; ok {
		entry := el.Value.(*lruEntry)
		entry.claims = claims
		entry.expiresAt = expiresAt
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&lruEntry{key: key, claims: claims, expiresAt: expiresAt})
	c.index[key] = el
	if c.ll.Len() > c.cap {
		c.removeElement(c.ll.Back())
	}
}

func (c *lruTTLCache) delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.index[key]; ok {
		c.removeElement(el)
	}
}

func (c *lruTTLCache) purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll = list.New()
	c.index = make(map[string]*list.Element, c.cap)
}

func (c *lruTTLCache) removeElement(el *list.Element) {
	entry := el.Value.(*lruEntry)
	c.ll.Remove(el)
	delete(c.index, entry.key)
}
