/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package gates implements the pre-flight checks every cluster-mutating
// grimnir-deploy subcommand runs before any side effect. Each gate is a
// small focused type with one Evaluate method.
package gates

import (
	"context"
	"errors"
	"fmt"
)

// Aborted is returned by a gate to refuse the operation. Distinct from a
// transport error: an Aborted means "the gate worked correctly and the
// operation is denied"; a regular error means "the gate could not decide."
type Aborted struct {
	Gate   string
	Reason string
}

func (a *Aborted) Error() string {
	return fmt.Sprintf("aborted by %s gate: %s", a.Gate, a.Reason)
}

// IsAborted reports whether err is an Aborted.
func IsAborted(err error) bool {
	var a *Aborted
	return errors.As(err, &a)
}

// Gate evaluates one pre-flight condition.
type Gate interface {
	Name() string
	Evaluate(ctx context.Context) error
}

// RunAll runs every gate in order and returns the first non-nil result.
func RunAll(ctx context.Context, gates ...Gate) error {
	for _, g := range gates {
		if err := g.Evaluate(ctx); err != nil {
			return err
		}
	}
	return nil
}
