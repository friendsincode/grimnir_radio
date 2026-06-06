/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gates

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// PolicyGate enforces the per-region deploy policy. The "window" policy means
// the deploy is allowed only inside a cron-expressed window each day/week.
// A window is "matched" if the cron schedule produces a fire-time within the
// past hour relative to now (i.e., we're inside that hour's deploy slot).
type PolicyGate struct {
	policy   string
	cronExpr string
	hotfix   bool
	goFlag   bool
	now      func() time.Time
}

// NewPolicyGate constructs a policy gate.
//
//	policy: "auto" | "window" | "manual"
//	cronExpr: standard 5-field cron expression, used when policy == "window"
//	hotfix: whether the tag carried the -hotfix suffix (bypasses window)
//	goFlag: whether the operator passed --go (required for manual)
//	now: clock injection for tests
func NewPolicyGate(policy, cronExpr string, hotfix, goFlag bool, now func() time.Time) *PolicyGate {
	return &PolicyGate{policy: policy, cronExpr: cronExpr, hotfix: hotfix, goFlag: goFlag, now: now}
}

// Name returns the gate identifier.
func (g *PolicyGate) Name() string { return "deploy-policy" }

// Evaluate honors the per-region policy: auto always passes; manual requires
// the --go flag; window matches the cron expression unless overridden by a
// -hotfix tag.
func (g *PolicyGate) Evaluate(ctx context.Context) error {
	switch g.policy {
	case "auto":
		return nil
	case "manual":
		if g.goFlag {
			return nil
		}
		return &Aborted{Gate: g.Name(), Reason: "policy=manual; pass --force-policy=manual --go to proceed"}
	case "window":
		if g.hotfix {
			return nil
		}
		sched, err := cron.ParseStandard(g.cronExpr)
		if err != nil {
			return fmt.Errorf("parse cron %q: %w", g.cronExpr, err)
		}
		now := g.now()
		next := sched.Next(now.Add(-time.Hour))
		// "Within window" = the next fire time from one hour ago is within the
		// current hour. This gives a one-hour deploy slot per cron tick.
		if !next.After(now) && now.Sub(next) <= time.Hour {
			return nil
		}
		return &Aborted{Gate: g.Name(), Reason: fmt.Sprintf("policy=window; outside %q (current %s)",
			g.cronExpr, now.Format("2006-01-02T15:04Z"))}
	default:
		return fmt.Errorf("unknown policy %q", g.policy)
	}
}
