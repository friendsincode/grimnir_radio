/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"

	"github.com/friendsincode/grimnir_radio/internal/events"
)

// EventDJAuthRevoke is published by the control plane when a live-input
// token is invalidated mid-session (the user clicks "End Session", the
// admin revokes the DJ's access, the token's natural expiry hits inside
// the fan-out's cache window). Payload shape:
//
//	{"mount": "/live", "token": "abcdef..."}    // single token
//	{"all": true}                                // purge entire cache
//
// The grimnirradio control plane fires this on its own event.Bus
// (in-process for single-node deploys, Redis pub/sub for HA). The fan-out
// subscribes via NewAuthRevocationSubscriber.
const EventDJAuthRevoke events.EventType = "dj.auth.revoke"

// AuthRevocationSubscriber watches the event bus for revocation messages &
// evicts the matching DJAuthClient cache entry. Construct with
// NewAuthRevocationSubscriber, then call Run(ctx) in a goroutine.
type AuthRevocationSubscriber struct {
	client *DJAuthClient
	bus    *events.Bus
}

// NewAuthRevocationSubscriber wires the subscriber. Doesn't start consuming;
// call Run(ctx).
func NewAuthRevocationSubscriber(c *DJAuthClient, bus *events.Bus) *AuthRevocationSubscriber {
	return &AuthRevocationSubscriber{client: c, bus: bus}
}

// Run subscribes to the bus & processes events until ctx is cancelled.
// Returns after Unsubscribe so callers can wait on it cleanly.
func (s *AuthRevocationSubscriber) Run(ctx context.Context) {
	sub := s.bus.Subscribe(EventDJAuthRevoke)
	defer s.bus.Unsubscribe(EventDJAuthRevoke, sub)

	for {
		select {
		case <-ctx.Done():
			return
		case payload, ok := <-sub:
			if !ok {
				return
			}
			s.handle(payload)
		}
	}
}

// handle extracts (mount, token) — or the "all" flag — from a payload &
// applies it to the cache. Silent on malformed payloads so a single bad
// publisher doesn't break the subscriber loop.
func (s *AuthRevocationSubscriber) handle(p events.Payload) {
	if all, ok := p["all"].(bool); ok && all {
		s.client.RevokeAll()
		return
	}
	mount, mok := p["mount"].(string)
	token, tok := p["token"].(string)
	if !mok || !tok || mount == "" || token == "" {
		return
	}
	s.client.Revoke(mount, token)
}
