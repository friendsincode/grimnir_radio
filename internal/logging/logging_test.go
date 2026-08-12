/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package logging

import (
	"testing"

	"github.com/rs/zerolog"
)

// clearLevelEnv removes every level variable so a developer machine that
// happens to export one cannot decide the result.
func clearLevelEnv(t *testing.T) {
	t.Helper()
	for _, k := range LevelEnvKeys {
		t.Setenv(k, "")
	}
}

// Without an explicit level the environment name still decides, which is the
// behaviour every existing caller relies on.
func TestLevelFor_EnvironmentDefaults(t *testing.T) {
	cases := []struct {
		environment string
		want        zerolog.Level
	}{
		{"development", zerolog.DebugLevel},
		{"production", zerolog.InfoLevel},
		{"", zerolog.InfoLevel},
		{"staging", zerolog.InfoLevel},
	}
	for _, tc := range cases {
		t.Run(tc.environment, func(t *testing.T) {
			clearLevelEnv(t)
			if got := levelFor(tc.environment); got != tc.want {
				t.Fatalf("levelFor(%q) = %v, want %v", tc.environment, got, tc.want)
			}
		})
	}
}

// docker-compose.yml has set GRIMNIR_LOG_LEVEL on the container for a long time
// while no Go code read it. It now wins over the environment default, which is
// what makes it possible to quiet a development box or drop prod to warn
// without touching the environment name.
func TestLevelFor_ExplicitLevelWins(t *testing.T) {
	for _, key := range LevelEnvKeys {
		t.Run(key, func(t *testing.T) {
			clearLevelEnv(t)
			t.Setenv(key, "warn")
			if got := levelFor("development"); got != zerolog.WarnLevel {
				t.Fatalf("%s=warn in development gave %v, want warn", key, got)
			}
		})
	}
}

// GRIMNIR_LOG_LEVEL is checked before RLM_LOG_LEVEL.
func TestLevelFor_PrecedenceBetweenKeys(t *testing.T) {
	clearLevelEnv(t)
	t.Setenv("GRIMNIR_LOG_LEVEL", "error")
	t.Setenv("RLM_LOG_LEVEL", "debug")
	if got := levelFor("production"); got != zerolog.ErrorLevel {
		t.Fatalf("levelFor = %v, want error (GRIMNIR_LOG_LEVEL should win)", got)
	}
}

// A typo in a log knob must not take the process down or silently flip the
// level to something unexpected; it falls through to the environment default.
func TestLevelFor_UnparseableFallsBackToEnvironment(t *testing.T) {
	clearLevelEnv(t)
	t.Setenv("GRIMNIR_LOG_LEVEL", "verbose")
	if got := levelFor("production"); got != zerolog.InfoLevel {
		t.Fatalf("levelFor with bad level = %v, want info", got)
	}

	clearLevelEnv(t)
	t.Setenv("GRIMNIR_LOG_LEVEL", "verbose")
	if got := levelFor("development"); got != zerolog.DebugLevel {
		t.Fatalf("levelFor with bad level in development = %v, want debug", got)
	}
}

// Setup wires the resolved level onto the returned logger, so the resolution
// above is not merely internal bookkeeping.
func TestSetup_AppliesResolvedLevel(t *testing.T) {
	clearLevelEnv(t)
	t.Setenv("GRIMNIR_LOG_LEVEL", "warn")
	if got := Setup("development").GetLevel(); got != zerolog.WarnLevel {
		t.Fatalf("Setup logger level = %v, want warn", got)
	}
}
