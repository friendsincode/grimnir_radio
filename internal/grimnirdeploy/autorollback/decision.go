/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package autorollback turns the deploy soak window from a passive
// time.Sleep into an active poll loop. During the configured soak duration
// the Monitor queries Prometheus on a fixed tick & decides whether the new
// version is misbehaving badly enough to roll back, holding steady, or in a
// gray zone that warrants extending the soak window.
//
// The package is intentionally framework-light: rules are plain PromQL
// strings paired with a numeric threshold + comparison, the Querier surface
// is small enough to satisfy with httptest, and the Monitor returns a
// structured Verdict so cmd_deploy.go can audit-log + ntfy on the same
// signal it acts on.
package autorollback

import "time"

// Decision is the three-way verdict the Monitor returns at the end of a soak
// window. Pass means every rule stayed within bounds across the window;
// Rollback means at least one rule breached its threshold for the required
// dwell time; Inconclusive means Prometheus was unreachable or returned
// errors often enough that we can't tell — the caller's choice is "extend
// the window" or "fail-open & accept the deploy".
type Decision int

const (
	// DecisionPass means the soak window completed cleanly. Every rule
	// stayed within bounds for every tick that returned data.
	DecisionPass Decision = iota
	// DecisionRollback means at least one rule breached its threshold for
	// the configured number of consecutive ticks. The caller MUST roll back.
	DecisionRollback
	// DecisionInconclusive means Prometheus was unavailable or returned
	// errors on more than half the ticks. The caller decides whether to
	// extend the window or treat it as a soft pass.
	DecisionInconclusive
)

// String returns the lowercase decision name (used in audit + ntfy payloads).
func (d Decision) String() string {
	switch d {
	case DecisionPass:
		return "pass"
	case DecisionRollback:
		return "rollback"
	case DecisionInconclusive:
		return "inconclusive"
	default:
		return "unknown"
	}
}

// Verdict is the full result of a soak-window observation. Reason is
// human-readable & ends up in the audit notes + ntfy body; TriggeringRule
// is the rule name that pushed us into Rollback (empty for Pass /
// Inconclusive); Samples is the per-rule history for post-mortem.
type Verdict struct {
	Decision       Decision
	Reason         string
	TriggeringRule string
	WindowStarted  time.Time
	WindowEnded    time.Time
	Samples        []Sample
	QueryErrors    int // total Prometheus query failures across all ticks
	TicksObserved  int // total ticks attempted
}

// Sample is one (tick, rule, value) observation. Kept on the Verdict so the
// post-mortem audit row can describe what happened without a second
// Prometheus query.
type Sample struct {
	Tick     time.Time
	RuleName string
	Value    float64
	Breached bool
	Err      string // non-empty when the query failed for this rule on this tick
}
