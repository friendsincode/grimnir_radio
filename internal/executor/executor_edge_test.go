/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package executor

import (
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestAllStatesCanReachEmergency(t *testing.T) {
	executor := &Executor{}

	states := []models.ExecutorStateEnum{
		models.ExecutorStateIdle,
		models.ExecutorStatePreloading,
		models.ExecutorStatePlaying,
		models.ExecutorStateFading,
		models.ExecutorStateLive,
	}

	for _, from := range states {
		t.Run(string(from)+"_to_emergency", func(t *testing.T) {
			if !executor.isValidTransition(from, models.ExecutorStateEmergency) {
				t.Errorf("expected %s → Emergency to be valid", from)
			}
		})
	}
}

func TestEmergencyExitPaths(t *testing.T) {
	executor := &Executor{}

	tests := []struct {
		to    models.ExecutorStateEnum
		valid bool
	}{
		{models.ExecutorStateIdle, true},
		{models.ExecutorStatePlaying, true},
		{models.ExecutorStateLive, true},
		{models.ExecutorStateFading, false},
		{models.ExecutorStatePreloading, false},
	}

	for _, tt := range tests {
		t.Run("emergency_to_"+string(tt.to), func(t *testing.T) {
			got := executor.isValidTransition(models.ExecutorStateEmergency, tt.to)
			if got != tt.valid {
				t.Errorf("Emergency → %s = %v, want %v", tt.to, got, tt.valid)
			}
		})
	}
}

func TestCompletePlaybackCycleChain(t *testing.T) {
	executor := &Executor{}

	chain := []models.ExecutorStateEnum{
		models.ExecutorStateIdle,
		models.ExecutorStatePreloading,
		models.ExecutorStatePlaying,
		models.ExecutorStateFading,
		models.ExecutorStatePlaying,
		models.ExecutorStateIdle,
	}

	for i := 1; i < len(chain); i++ {
		from := chain[i-1]
		to := chain[i]
		if !executor.isValidTransition(from, to) {
			t.Fatalf("step %d: %s → %s should be valid", i, from, to)
		}
	}
}

func TestLiveTakeoverDuringFade(t *testing.T) {
	executor := &Executor{}

	chain := []models.ExecutorStateEnum{
		models.ExecutorStateFading,
		models.ExecutorStateLive,
		models.ExecutorStatePlaying,
	}

	for i := 1; i < len(chain); i++ {
		from := chain[i-1]
		to := chain[i]
		if !executor.isValidTransition(from, to) {
			t.Fatalf("step %d: %s → %s should be valid", i, from, to)
		}
	}
}

func TestEmergencyDuringLive(t *testing.T) {
	executor := &Executor{}

	chain := []models.ExecutorStateEnum{
		models.ExecutorStateLive,
		models.ExecutorStateEmergency,
		models.ExecutorStateIdle,
	}

	for i := 1; i < len(chain); i++ {
		from := chain[i-1]
		to := chain[i]
		if !executor.isValidTransition(from, to) {
			t.Fatalf("step %d: %s → %s should be valid", i, from, to)
		}
	}
}

func TestAllInvalidTransitionsExhaustive(t *testing.T) {
	executor := &Executor{}

	// Map of all invalid transitions
	invalid := []struct {
		from, to models.ExecutorStateEnum
	}{
		{models.ExecutorStateIdle, models.ExecutorStateFading},
		{models.ExecutorStatePreloading, models.ExecutorStateFading},
		{models.ExecutorStateFading, models.ExecutorStatePreloading},
		{models.ExecutorStateLive, models.ExecutorStatePreloading},
		{models.ExecutorStateEmergency, models.ExecutorStateFading},
		{models.ExecutorStateEmergency, models.ExecutorStatePreloading},
	}

	for _, tt := range invalid {
		t.Run(string(tt.from)+"_to_"+string(tt.to), func(t *testing.T) {
			if executor.isValidTransition(tt.from, tt.to) {
				t.Errorf("%s → %s should be invalid", tt.from, tt.to)
			}
		})
	}
}
