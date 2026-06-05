/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"context"
	"sync/atomic"
	"time"
)

// Swapper is implemented by Pipeline. Decoupled for testability.
type Swapper interface {
	ActiveInput() string
	SetActiveInput(name string) error
}

// Switcher polls per-input health on a tick and switches the active input
// when the current one goes unhealthy. Hysteresis (consecutive failing ticks
// required before switch) prevents flapping under brief instability.
type Switcher struct {
	a, b        *InputHealth
	swap        Swapper
	tick        time.Duration
	hysteresisN int
	switchCount atomic.Int64
}

// NewSwitcher constructs a Switcher. `tick` is the polling interval (e.g.,
// 50ms in production, faster in tests). `hysteresisN` is the number of
// consecutive ticks the active input must be unhealthy before the switch
// fires (e.g., 2 ticks at 50ms = 100ms confirmation window).
func NewSwitcher(a, b *InputHealth, swap Swapper, tick time.Duration, hysteresisN int) *Switcher {
	return &Switcher{
		a:           a,
		b:           b,
		swap:        swap,
		tick:        tick,
		hysteresisN: hysteresisN,
	}
}

// Run blocks until ctx is cancelled, polling health and switching as needed.
// Safe to run in a goroutine.
func (s *Switcher) Run(ctx context.Context) {
	t := time.NewTicker(s.tick)
	defer t.Stop()
	failingTicks := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			active := s.swap.ActiveInput()
			activeHealthy := (active == "A" && s.a.IsHealthy()) || (active == "B" && s.b.IsHealthy())
			otherHealthy := (active == "A" && s.b.IsHealthy()) || (active == "B" && s.a.IsHealthy())

			if activeHealthy {
				failingTicks = 0
				continue
			}
			failingTicks++
			if failingTicks < s.hysteresisN {
				continue
			}
			if !otherHealthy {
				// No good choice; stay on the (unhealthy) current input.
				continue
			}
			next := "B"
			if active == "B" {
				next = "A"
			}
			_ = s.swap.SetActiveInput(next)
			s.switchCount.Add(1)
			failingTicks = 0
		}
	}
}

// SwitchCount returns total times the switcher has fired SetActiveInput.
// Used for telemetry / status reporting.
func (s *Switcher) SwitchCount() int64 {
	return s.switchCount.Load()
}
