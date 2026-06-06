/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gates

import (
	"context"
	"testing"
	"time"
)

func TestPolicyAutoAlwaysPasses(t *testing.T) {
	g := NewPolicyGate("auto", "0 4 * * SUN", false, false, now)
	if err := g.Evaluate(context.Background()); err != nil {
		t.Errorf("auto should pass; got %v", err)
	}
}

func TestPolicyManualRequiresGoFlag(t *testing.T) {
	g := NewPolicyGate("manual", "", false, false, now)
	if !IsAborted(g.Evaluate(context.Background())) {
		t.Error("manual without --go should abort")
	}
	g2 := NewPolicyGate("manual", "", false, true, now)
	if err := g2.Evaluate(context.Background()); err != nil {
		t.Errorf("manual with --go should pass; got %v", err)
	}
}

func TestPolicyWindowOutOfWindowAborts(t *testing.T) {
	// Run on a Tuesday at 14:00; window is Sunday 04:00.
	clock := func() time.Time { return time.Date(2026, 6, 9, 14, 0, 0, 0, time.UTC) }
	g := NewPolicyGate("window", "0 4 * * SUN", false, false, clock)
	if !IsAborted(g.Evaluate(context.Background())) {
		t.Error("out-of-window should abort")
	}
}

func TestPolicyWindowHotfixOverrideBypasses(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 6, 9, 14, 0, 0, 0, time.UTC) }
	g := NewPolicyGate("window", "0 4 * * SUN", true, false, clock)
	if err := g.Evaluate(context.Background()); err != nil {
		t.Errorf("hotfix should override window; got %v", err)
	}
}

func TestPolicyWindowInsideWindowPasses(t *testing.T) {
	// Sunday 04:30 UTC = inside 04:00 window (within an hour).
	clock := func() time.Time { return time.Date(2026, 6, 7, 4, 30, 0, 0, time.UTC) }
	g := NewPolicyGate("window", "0 4 * * SUN", false, false, clock)
	if err := g.Evaluate(context.Background()); err != nil {
		t.Errorf("inside window should pass; got %v", err)
	}
}

func now() time.Time { return time.Now().UTC() }
