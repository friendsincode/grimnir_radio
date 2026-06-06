/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package autorollback

import (
	"context"
	"fmt"
	"time"
)

// Monitor polls Prometheus for the duration of the soak window & returns a
// structured Verdict. Constructed inline by the caller (cmd_deploy.go) so
// the field set is the configuration surface — no separate Options struct.
//
// Concurrency: Observe is single-goroutine; it ticks, queries each rule in
// sequence, then sleeps until the next tick. Querier implementations MUST
// be safe for sequential reuse but need not be goroutine-safe.
type Monitor struct {
	// Querier hits Prometheus. Nil yields an Inconclusive verdict immediately.
	Querier Querier
	// Rules is evaluated on every tick. Nil/empty yields Pass immediately
	// (the soak window completes as a no-op).
	Rules []Rule
	// Window is the total soak duration. Observe returns when the window
	// elapses, the context cancels, or a rule triggers Rollback.
	Window time.Duration
	// TickInterval is how often the Monitor re-queries Prometheus. A
	// shorter tick finds problems faster but loads Prometheus harder; 15s
	// is reasonable in production, ms-scale in tests.
	TickInterval time.Duration
	// Now is the clock. Tests inject a fixed clock; production passes
	// time.Now. Nil falls back to time.Now.
	Now func() time.Time
}

// Observe runs the poll loop until the soak window completes or a rule
// triggers Rollback. Always returns a Verdict (never nil); the caller MUST
// check Decision before acting. Observe never panics; query errors are
// counted, not propagated.
func (m *Monitor) Observe(ctx context.Context) Verdict {
	now := m.Now
	if now == nil {
		now = time.Now
	}
	v := Verdict{WindowStarted: now()}

	if m.Querier == nil {
		v.Decision = DecisionInconclusive
		v.Reason = "autorollback: no Prometheus querier configured"
		v.WindowEnded = now()
		return v
	}
	if len(m.Rules) == 0 {
		v.Decision = DecisionPass
		v.Reason = "autorollback: no rules configured; soak window is a no-op"
		v.WindowEnded = now()
		return v
	}
	if m.TickInterval <= 0 {
		m.TickInterval = 15 * time.Second
	}
	if m.Window <= 0 {
		v.Decision = DecisionPass
		v.Reason = "autorollback: zero-duration soak window"
		v.WindowEnded = now()
		return v
	}

	// Per-rule consecutive-breach counter. Indexed by rule name.
	breaches := make(map[string]int, len(m.Rules))

	deadline := v.WindowStarted.Add(m.Window)
	tick := time.NewTicker(m.TickInterval)
	defer tick.Stop()

	// First evaluation runs immediately rather than waiting one tick.
	if verdict, done := m.evalOnce(ctx, &v, breaches, now()); done {
		v.WindowEnded = now()
		return verdict
	}

	for {
		select {
		case <-ctx.Done():
			v.WindowEnded = now()
			if v.Decision == 0 {
				v.Decision = DecisionInconclusive
				v.Reason = fmt.Sprintf("autorollback: context cancelled before window completed: %v", ctx.Err())
			}
			return v
		case <-tick.C:
			if !now().Before(deadline) {
				v.WindowEnded = now()
				return finalizeVerdict(v)
			}
			if verdict, done := m.evalOnce(ctx, &v, breaches, now()); done {
				v.WindowEnded = now()
				return verdict
			}
			if !now().Before(deadline) {
				v.WindowEnded = now()
				return finalizeVerdict(v)
			}
		}
	}
}

// evalOnce queries every rule at the given tick timestamp, appends samples
// to the verdict, updates breach counters, & returns (verdict, true) if a
// rule has tripped the ConsecutiveBreaches threshold. Otherwise returns
// (zero verdict, false) & lets the caller continue polling.
func (m *Monitor) evalOnce(ctx context.Context, v *Verdict, breaches map[string]int, at time.Time) (Verdict, bool) {
	v.TicksObserved++
	for _, r := range m.Rules {
		val, err := m.Querier.Query(ctx, r.Query, at)
		sample := Sample{Tick: at, RuleName: r.Name, Value: val}
		if err != nil {
			sample.Err = err.Error()
			v.QueryErrors++
			v.Samples = append(v.Samples, sample)
			// A query error does NOT count as a breach; we don't want a
			// flaky Prometheus to roll deploys back. The Inconclusive
			// fallback in finalizeVerdict catches sustained errors.
			breaches[r.Name] = 0
			continue
		}
		breached := r.Compare.breached(val, r.Threshold)
		sample.Breached = breached
		v.Samples = append(v.Samples, sample)
		if breached {
			breaches[r.Name]++
			need := r.ConsecutiveBreaches
			if need < 1 {
				need = 1
			}
			if breaches[r.Name] >= need {
				out := *v
				out.Decision = DecisionRollback
				out.TriggeringRule = r.Name
				out.Reason = fmt.Sprintf("%s: %s (value=%.4f threshold=%.4f after %d consecutive breaches)",
					r.Name, r.Description, val, r.Threshold, breaches[r.Name])
				return out, true
			}
		} else {
			breaches[r.Name] = 0
		}
	}
	return Verdict{}, false
}

// finalizeVerdict decides Pass vs Inconclusive at the end of a clean window.
// If more than half of the tick-rule evaluations errored we can't tell what
// the new build did; return Inconclusive so the caller can extend the
// window or fail-open per policy.
func finalizeVerdict(v Verdict) Verdict {
	totalEvals := v.TicksObserved * countDistinctRules(v.Samples)
	if totalEvals > 0 && v.QueryErrors*2 > totalEvals {
		v.Decision = DecisionInconclusive
		v.Reason = fmt.Sprintf("autorollback: %d/%d Prometheus queries failed; cannot verdict",
			v.QueryErrors, totalEvals)
		return v
	}
	// In the all-errors edge case where totalEvals is 0 (no rules ran
	// successfully), we still want Inconclusive rather than a silent Pass.
	if v.QueryErrors > 0 && totalEvals == 0 {
		v.Decision = DecisionInconclusive
		v.Reason = "autorollback: all Prometheus queries failed; cannot verdict"
		return v
	}
	v.Decision = DecisionPass
	v.Reason = fmt.Sprintf("autorollback: %d ticks observed; no rule breached its dwell threshold", v.TicksObserved)
	return v
}

// countDistinctRules returns the number of unique rule names in samples. The
// Monitor's evalOnce records one sample per (tick, rule) so this is also
// the per-tick rule count.
func countDistinctRules(samples []Sample) int {
	if len(samples) == 0 {
		return 0
	}
	seen := make(map[string]struct{})
	for _, s := range samples {
		seen[s.RuleName] = struct{}{}
	}
	return len(seen)
}
