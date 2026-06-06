/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"context"

	"github.com/friendsincode/grimnir_radio/internal/notify"
)

// NotifierPoster adapts a notify.Notifier to the audit Poster interface so
// the audit Wrapper can route its start / complete / failed / panic events
// through the typed Notifier built in internal/notify. Priorities above
// PriorityHigh map to Tier2 (operator page); everything else maps to Tier1
// (audit firehose). The topic and tags arguments are ignored — Notifier
// picks the right topic per-tier internally.
type NotifierPoster struct {
	n notify.Notifier
}

// NewNotifierPoster wraps a Notifier as a Poster. A nil Notifier yields a
// nil Poster so call sites can pass FromEnv() through without an extra
// nil-check; RecorderImpl already treats a nil Poster as a silent no-op.
func NewNotifierPoster(n notify.Notifier) *NotifierPoster {
	if n == nil {
		return nil
	}
	return &NotifierPoster{n: n}
}

// Post forwards to Tier2 for PriorityHigh+ events (failures, panics) and
// Tier1 for everything else (deploy started, deploy completed). The topic
// and tags slots are intentionally unused; Notifier owns topic routing.
func (p *NotifierPoster) Post(ctx context.Context, _ string, title, message string, priority Priority, _ ...string) error {
	if p == nil || p.n == nil {
		return nil
	}
	if priority >= PriorityHigh {
		return p.n.Tier2(ctx, title, message)
	}
	return p.n.Tier1(ctx, title, message)
}
