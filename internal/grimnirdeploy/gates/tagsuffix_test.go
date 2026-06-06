/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gates

import (
	"context"
	"testing"
)

func TestTagSuffixHoldAlwaysAborts(t *testing.T) {
	g := NewTagSuffixGate("v1.0.0-hold", "auto")
	if !IsAborted(g.Evaluate(context.Background())) {
		t.Error("hold suffix should always abort")
	}
}

func TestTagSuffixHotfixOverridesWindowPolicy(t *testing.T) {
	g := NewTagSuffixGate("v1.0.0-hotfix", "window")
	if err := g.Evaluate(context.Background()); err != nil {
		t.Errorf("hotfix should override window; got %v", err)
	}
	if !g.OverridesPolicy() {
		t.Error("hotfix tag should override policy")
	}
}

func TestTagSuffixBareTagDoesNothing(t *testing.T) {
	g := NewTagSuffixGate("v1.0.0", "auto")
	if err := g.Evaluate(context.Background()); err != nil {
		t.Errorf("bare tag should pass; got %v", err)
	}
	if g.OverridesPolicy() {
		t.Error("bare tag should not override policy")
	}
}
