/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gates

import (
	"context"
	"fmt"
	"strings"
)

// HealthProbe probes one node's overall health (control plane, mediaengine,
// edge-encoder, fan-out). Implementations live in internal/grimnirdeploy/probe;
// the gate just consumes the verdict.
type HealthProbe interface {
	Probe(ctx context.Context, host string) error
}

// HealthGate aborts unless every named host probes healthy. Design Section 6
// requires both nodes healthy before any rolling deploy; this enforces it.
type HealthGate struct {
	P     HealthProbe
	Hosts []string
}

// NewHealthGate constructs a HealthGate.
func NewHealthGate(p HealthProbe, hosts []string) *HealthGate {
	return &HealthGate{P: p, Hosts: hosts}
}

// Name returns the gate identifier.
func (g *HealthGate) Name() string { return "both-nodes-healthy" }

// Evaluate probes every host and aggregates failures into one Aborted.
func (g *HealthGate) Evaluate(ctx context.Context) error {
	var bad []string
	for _, h := range g.Hosts {
		if err := g.P.Probe(ctx, h); err != nil {
			bad = append(bad, fmt.Sprintf("%s: %v", h, err))
		}
	}
	if len(bad) > 0 {
		return &Aborted{Gate: g.Name(), Reason: fmt.Sprintf("unhealthy nodes: %s",
			strings.Join(bad, "; "))}
	}
	return nil
}
