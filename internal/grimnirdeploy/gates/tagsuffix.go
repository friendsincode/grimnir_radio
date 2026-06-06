/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gates

import (
	"context"
	"strings"
)

// TagSuffixGate enforces the design's tag suffix conventions:
//
//	-hold:   skip auto entirely (always abort)
//	-hotfix: override window policy
//
// Effect on PolicyGate is communicated via the OverridesPolicy method, read
// by the deploy command after gate evaluation.
type TagSuffixGate struct {
	tag    string
	policy string
}

// NewTagSuffixGate constructs a TagSuffixGate. The policy argument is kept
// for future audit messages even though Evaluate only acts on the suffix.
func NewTagSuffixGate(tag, policy string) *TagSuffixGate {
	return &TagSuffixGate{tag: tag, policy: policy}
}

// Name returns the gate identifier.
func (g *TagSuffixGate) Name() string { return "tag-suffix" }

// OverridesPolicy reports whether the tag suffix overrides the deploy policy
// (e.g., -hotfix bypasses window restrictions). The PolicyGate consults this.
func (g *TagSuffixGate) OverridesPolicy() bool { return strings.HasSuffix(g.tag, "-hotfix") }

// Evaluate aborts on the -hold suffix; everything else passes.
func (g *TagSuffixGate) Evaluate(ctx context.Context) error {
	if strings.HasSuffix(g.tag, "-hold") {
		return &Aborted{Gate: g.Name(), Reason: "-hold suffix; deploys disabled for this tag"}
	}
	return nil
}
