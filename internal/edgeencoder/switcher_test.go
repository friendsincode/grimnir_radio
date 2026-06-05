/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeSwapper struct {
	mu      sync.Mutex
	current string
}

func (f *fakeSwapper) ActiveInput() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current
}

func (f *fakeSwapper) SetActiveInput(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = name
	return nil
}

func TestSwitcher_StaysOnHealthyInput(t *testing.T) {
	a := NewInputHealth(100 * time.Millisecond)
	b := NewInputHealth(100 * time.Millisecond)
	a.RecordPacket()
	b.RecordPacket()
	swap := &fakeSwapper{current: "A"}
	sw := NewSwitcher(a, b, swap, 10*time.Millisecond, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sw.Run(ctx)
	if swap.ActiveInput() != "A" {
		t.Errorf("active = %q, want A (no switch with both healthy)", swap.ActiveInput())
	}
}

func TestSwitcher_FailsOverWhenActiveDies(t *testing.T) {
	a := NewInputHealth(50 * time.Millisecond)
	b := NewInputHealth(50 * time.Millisecond)
	a.RecordPacket()
	b.RecordPacket()
	swap := &fakeSwapper{current: "A"}
	sw := NewSwitcher(a, b, swap, 10*time.Millisecond, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	go func() {
		// Keep B alive; let A go stale
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				b.RecordPacket()
			}
		}
	}()

	sw.Run(ctx)
	if swap.ActiveInput() != "B" {
		t.Errorf("active = %q, want B (should have switched after A went stale)", swap.ActiveInput())
	}
}

func TestSwitcher_NoSwitchIfOtherAlsoDead(t *testing.T) {
	a := NewInputHealth(50 * time.Millisecond)
	b := NewInputHealth(50 * time.Millisecond)
	// Neither input ever records a packet → both unhealthy
	swap := &fakeSwapper{current: "A"}
	sw := NewSwitcher(a, b, swap, 10*time.Millisecond, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sw.Run(ctx)
	if swap.ActiveInput() != "A" {
		t.Errorf("active = %q, want A (both dead, should keep current)", swap.ActiveInput())
	}
	if sw.SwitchCount() != 0 {
		t.Errorf("SwitchCount = %d, want 0 (no switch should have happened)", sw.SwitchCount())
	}
}

func TestSwitcher_SwitchCountIncrements(t *testing.T) {
	a := NewInputHealth(50 * time.Millisecond)
	b := NewInputHealth(50 * time.Millisecond)
	a.RecordPacket()
	b.RecordPacket()
	swap := &fakeSwapper{current: "A"}
	sw := NewSwitcher(a, b, swap, 10*time.Millisecond, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				b.RecordPacket() // only B stays alive
			}
		}
	}()

	sw.Run(ctx)
	if sw.SwitchCount() < 1 {
		t.Errorf("SwitchCount = %d, want >= 1 after one expected switch", sw.SwitchCount())
	}
}
