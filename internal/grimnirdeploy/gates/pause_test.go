/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gates

import (
	"context"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/pause"
)

type fakePauseReader struct{ state *pause.State }

func (f *fakePauseReader) Read(ctx context.Context) (*pause.State, error) {
	return f.state, nil
}

func TestPauseGatePassesWhenNoPause(t *testing.T) {
	g := NewPauseGate(&fakePauseReader{})
	if err := g.Evaluate(context.Background()); err != nil {
		t.Errorf("expected pass; got %v", err)
	}
}

func TestPauseGateAbortsWhenPauseSet(t *testing.T) {
	g := NewPauseGate(&fakePauseReader{state: &pause.State{Reason: "fixing"}})
	err := g.Evaluate(context.Background())
	if !IsAborted(err) {
		t.Errorf("expected Aborted; got %v", err)
	}
}
