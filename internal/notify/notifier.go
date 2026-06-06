/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package notify

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"
)

// Notifier is the minimal two-tier surface callers depend on. Tier1 is for
// informational events (deploy started, scan finished); Tier2 is for events
// that should wake an operator (soak failed, auto-rollback fired).
//
// PageAndRollback (tier-3) is reserved for the auto-rollback hook and goes
// through the Client directly; it intentionally isn't on this interface so
// callers can't fire the rollback ringtone by accident.
type Notifier interface {
	Tier1(ctx context.Context, title, body string) error
	Tier2(ctx context.Context, title, body string) error
}

// Tier1 satisfies Notifier by forwarding to Notify (audit topic, priority 3).
func (c *Client) Tier1(ctx context.Context, title, body string) error {
	return c.Notify(ctx, Message{Title: title, Body: body})
}

// Tier2 satisfies Notifier by forwarding to Page (page topic, priority 5).
func (c *Client) Tier2(ctx context.Context, title, body string) error {
	return c.Page(ctx, Message{Title: title, Body: body})
}

// NopNotifier is the no-op fallback returned by FromEnv when GRIMNIR_NTFY_URL
// is unset. Lets the binary run in dev / CI without paging anyone, while still
// satisfying every Notifier call site so the wiring code stays unconditional.
type NopNotifier struct{}

// Tier1 is a no-op.
func (NopNotifier) Tier1(_ context.Context, _, _ string) error { return nil }

// Tier2 is a no-op.
func (NopNotifier) Tier2(_ context.Context, _, _ string) error { return nil }

// FakeCall records one Tier1/Tier2 invocation against a FakeNotifier.
type FakeCall struct {
	Tier  int
	Title string
	Body  string
}

// FakeNotifier records every call to Tier1/Tier2 in order. Safe for
// concurrent use from many goroutines. Use in tests that want to assert
// notification side-effects without hitting the network.
type FakeNotifier struct {
	mu    sync.Mutex
	Calls []FakeCall
}

// Tier1 records a tier-1 call.
func (f *FakeNotifier) Tier1(_ context.Context, title, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, FakeCall{Tier: 1, Title: title, Body: body})
	return nil
}

// Tier2 records a tier-2 call.
func (f *FakeNotifier) Tier2(_ context.Context, title, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, FakeCall{Tier: 2, Title: title, Body: body})
	return nil
}

// fromEnvWarnOnce caps the "ntfy disabled" log line to one occurrence per
// process so the warning is visible at startup but doesn't spam every
// reconstruction of the notifier.
var fromEnvWarnOnce sync.Once

// FromEnv is the factory wired into binary startup. Reads GRIMNIR_NTFY_URL +
// per-topic tokens via LoadConfigFromEnv; returns a NopNotifier with a
// one-time warning log when the URL is unset so dev / CI environments don't
// need to fake ntfy. Returns a real *Client otherwise.
func FromEnv() Notifier {
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		fromEnvWarnOnce.Do(func() {
			log.Warn().Err(err).Msg("notify: ntfy disabled, alerts will not fire")
		})
		return NopNotifier{}
	}
	return NewClient(cfg)
}
