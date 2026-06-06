/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gates

import (
	"context"
	"fmt"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/pause"
)

// PauseReader is the subset of pause.Client the gate needs. The deploy command
// binds the region into the implementation it passes (see the adapter in
// cmd_deploy.go) so the gate stays region-agnostic.
type PauseReader interface {
	Read(ctx context.Context) (*pause.State, error)
}

// PauseGate aborts when the emergency-pause Redis key is set.
type PauseGate struct{ R PauseReader }

// NewPauseGate constructs a PauseGate.
func NewPauseGate(r PauseReader) *PauseGate { return &PauseGate{R: r} }

// Name returns the gate identifier used in audit logs + Aborted messages.
func (g *PauseGate) Name() string { return "emergency-pause" }

// Evaluate reads the pause state; returns Aborted if a pause is set, nil
// otherwise, or a wrapped transport error if Redis is unreachable.
func (g *PauseGate) Evaluate(ctx context.Context) error {
	s, err := g.R.Read(ctx)
	if err != nil {
		return fmt.Errorf("read pause state: %w", err)
	}
	if s != nil {
		return &Aborted{Gate: g.Name(), Reason: fmt.Sprintf("pause set by %s at %s: %s",
			s.Operator, s.TS.Format("2006-01-02T15:04:05Z"), s.Reason)}
	}
	return nil
}
