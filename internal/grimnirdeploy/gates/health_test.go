/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gates

import (
	"context"
	"testing"
)

type fakeHealthProbe struct {
	results map[string]error
}

func (f *fakeHealthProbe) Probe(ctx context.Context, host string) error {
	return f.results[host]
}

func TestHealthGatePassesWhenAllUp(t *testing.T) {
	p := &fakeHealthProbe{results: map[string]error{"local": nil, "node-2": nil}}
	g := NewHealthGate(p, []string{"local", "node-2"})
	if err := g.Evaluate(context.Background()); err != nil {
		t.Errorf("all up; got %v", err)
	}
}

func TestHealthGateAbortsWhenOneDown(t *testing.T) {
	p := &fakeHealthProbe{results: map[string]error{"local": nil, "node-2": context.DeadlineExceeded}}
	g := NewHealthGate(p, []string{"local", "node-2"})
	if !IsAborted(g.Evaluate(context.Background())) {
		t.Error("one down should abort")
	}
}
