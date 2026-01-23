/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package scheduler

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/clock"
	"github.com/friendsincode/grimnir_radio/internal/scheduler/state"
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
	"github.com/rs/zerolog"
)

func TestNew(t *testing.T) {
	logger := zerolog.Nop()

	tests := []struct {
		name              string
		lookahead         time.Duration
		expectedLookahead time.Duration
	}{
		{
			name:              "zero lookahead defaults to 24h",
			lookahead:         0,
			expectedLookahead: 24 * time.Hour,
		},
		{
			name:              "negative lookahead defaults to 24h",
			lookahead:         -1 * time.Hour,
			expectedLookahead: 24 * time.Hour,
		},
		{
			name:              "positive lookahead is preserved",
			lookahead:         48 * time.Hour,
			expectedLookahead: 48 * time.Hour,
		},
		{
			name:              "custom lookahead is preserved",
			lookahead:         12 * time.Hour,
			expectedLookahead: 12 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create minimal dependencies (nil db is ok for this test)
			planner := clock.NewPlanner(nil, logger)
			engine := smartblock.New(nil, logger)
			stateStore := state.NewStore()

			svc := New(nil, planner, engine, stateStore, tt.lookahead, logger)

			if svc.lookahead != tt.expectedLookahead {
				t.Errorf("New() lookahead = %v, want %v", svc.lookahead, tt.expectedLookahead)
			}
		})
	}
}

func TestServiceFields(t *testing.T) {
	logger := zerolog.Nop()
	planner := clock.NewPlanner(nil, logger)
	engine := smartblock.New(nil, logger)
	stateStore := state.NewStore()
	lookahead := 48 * time.Hour

	svc := New(nil, planner, engine, stateStore, lookahead, logger)

	if svc.planner == nil {
		t.Error("New() planner is nil")
	}
	if svc.engine == nil {
		t.Error("New() engine is nil")
	}
	if svc.stateStore == nil {
		t.Error("New() stateStore is nil")
	}
	if svc.lookahead != lookahead {
		t.Errorf("New() lookahead = %v, want %v", svc.lookahead, lookahead)
	}
}
