/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package executor

import (
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestIsValidTransition(t *testing.T) {
	executor := &Executor{}

	tests := []struct {
		name    string
		from    models.ExecutorStateEnum
		to      models.ExecutorStateEnum
		valid   bool
	}{
		// From Idle
		{"idle to preloading", models.ExecutorStateIdle, models.ExecutorStatePreloading, true},
		{"idle to playing", models.ExecutorStateIdle, models.ExecutorStatePlaying, true},
		{"idle to live", models.ExecutorStateIdle, models.ExecutorStateLive, true},
		{"idle to emergency", models.ExecutorStateIdle, models.ExecutorStateEmergency, true},
		{"idle to fading invalid", models.ExecutorStateIdle, models.ExecutorStateFading, false},

		// From Preloading
		{"preloading to idle", models.ExecutorStatePreloading, models.ExecutorStateIdle, true},
		{"preloading to playing", models.ExecutorStatePreloading, models.ExecutorStatePlaying, true},
		{"preloading to live", models.ExecutorStatePreloading, models.ExecutorStateLive, true},
		{"preloading to emergency", models.ExecutorStatePreloading, models.ExecutorStateEmergency, true},
		{"preloading to fading invalid", models.ExecutorStatePreloading, models.ExecutorStateFading, false},

		// From Playing
		{"playing to idle", models.ExecutorStatePlaying, models.ExecutorStateIdle, true},
		{"playing to preloading", models.ExecutorStatePlaying, models.ExecutorStatePreloading, true},
		{"playing to fading", models.ExecutorStatePlaying, models.ExecutorStateFading, true},
		{"playing to live", models.ExecutorStatePlaying, models.ExecutorStateLive, true},
		{"playing to emergency", models.ExecutorStatePlaying, models.ExecutorStateEmergency, true},

		// From Fading
		{"fading to playing", models.ExecutorStateFading, models.ExecutorStatePlaying, true},
		{"fading to live", models.ExecutorStateFading, models.ExecutorStateLive, true},
		{"fading to emergency", models.ExecutorStateFading, models.ExecutorStateEmergency, true},
		{"fading to idle invalid", models.ExecutorStateFading, models.ExecutorStateIdle, false},
		{"fading to preloading invalid", models.ExecutorStateFading, models.ExecutorStatePreloading, false},

		// From Live
		{"live to idle", models.ExecutorStateLive, models.ExecutorStateIdle, true},
		{"live to fading", models.ExecutorStateLive, models.ExecutorStateFading, true},
		{"live to playing", models.ExecutorStateLive, models.ExecutorStatePlaying, true},
		{"live to emergency", models.ExecutorStateLive, models.ExecutorStateEmergency, true},
		{"live to preloading invalid", models.ExecutorStateLive, models.ExecutorStatePreloading, false},

		// From Emergency
		{"emergency to idle", models.ExecutorStateEmergency, models.ExecutorStateIdle, true},
		{"emergency to playing", models.ExecutorStateEmergency, models.ExecutorStatePlaying, true},
		{"emergency to live", models.ExecutorStateEmergency, models.ExecutorStateLive, true},
		{"emergency to fading invalid", models.ExecutorStateEmergency, models.ExecutorStateFading, false},
		{"emergency to preloading invalid", models.ExecutorStateEmergency, models.ExecutorStatePreloading, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.isValidTransition(tt.from, tt.to)
			if result != tt.valid {
				t.Errorf("isValidTransition(%s, %s) = %v, want %v", tt.from, tt.to, result, tt.valid)
			}
		})
	}
}

func TestExecutorStateEnum(t *testing.T) {
	states := []models.ExecutorStateEnum{
		models.ExecutorStateIdle,
		models.ExecutorStatePreloading,
		models.ExecutorStatePlaying,
		models.ExecutorStateFading,
		models.ExecutorStateLive,
		models.ExecutorStateEmergency,
	}

	// Ensure all states are defined
	for _, state := range states {
		if state == "" {
			t.Error("ExecutorStateEnum should not be empty")
		}
	}

	// Ensure states are unique
	seen := make(map[models.ExecutorStateEnum]bool)
	for _, state := range states {
		if seen[state] {
			t.Errorf("Duplicate state found: %s", state)
		}
		seen[state] = true
	}
}

func TestTelemetryStruct(t *testing.T) {
	telemetry := Telemetry{
		AudioLevelL:   -12.5,
		AudioLevelR:   -13.2,
		LoudnessLUFS:  -23.0,
		BufferDepthMS: 5000,
	}

	if telemetry.AudioLevelL != -12.5 {
		t.Errorf("AudioLevelL = %v, want -12.5", telemetry.AudioLevelL)
	}

	if telemetry.AudioLevelR != -13.2 {
		t.Errorf("AudioLevelR = %v, want -13.2", telemetry.AudioLevelR)
	}

	if telemetry.LoudnessLUFS != -23.0 {
		t.Errorf("LoudnessLUFS = %v, want -23.0", telemetry.LoudnessLUFS)
	}

	if telemetry.BufferDepthMS != 5000 {
		t.Errorf("BufferDepthMS = %v, want 5000", telemetry.BufferDepthMS)
	}
}

func TestExecutorStateTransitionScenarios(t *testing.T) {
	executor := &Executor{}

	scenarios := []struct {
		name        string
		transitions []models.ExecutorStateEnum
		shouldPass  bool
	}{
		{
			name: "normal playback flow",
			transitions: []models.ExecutorStateEnum{
				models.ExecutorStateIdle,
				models.ExecutorStatePreloading,
				models.ExecutorStatePlaying,
				models.ExecutorStateFading,
				models.ExecutorStatePlaying,
				models.ExecutorStateIdle,
			},
			shouldPass: true,
		},
		{
			name: "live takeover",
			transitions: []models.ExecutorStateEnum{
				models.ExecutorStateIdle,
				models.ExecutorStatePlaying,
				models.ExecutorStateLive,
				models.ExecutorStatePlaying,
			},
			shouldPass: true,
		},
		{
			name: "emergency preemption",
			transitions: []models.ExecutorStateEnum{
				models.ExecutorStatePlaying,
				models.ExecutorStateEmergency,
				models.ExecutorStateIdle,
			},
			shouldPass: true,
		},
		{
			name: "invalid direct idle to fading",
			transitions: []models.ExecutorStateEnum{
				models.ExecutorStateIdle,
				models.ExecutorStateFading, // Invalid!
			},
			shouldPass: false,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			valid := true
			for i := 1; i < len(scenario.transitions); i++ {
				from := scenario.transitions[i-1]
				to := scenario.transitions[i]
				if !executor.isValidTransition(from, to) {
					valid = false
					break
				}
			}

			if valid != scenario.shouldPass {
				t.Errorf("scenario %s: got %v, want %v", scenario.name, valid, scenario.shouldPass)
			}
		})
	}
}
