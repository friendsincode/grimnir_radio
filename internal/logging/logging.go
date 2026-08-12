/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package logging

import (
	"io"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// LevelEnvKeys are the variable names that set the log level explicitly, in
// precedence order. docker-compose.yml has passed GRIMNIR_LOG_LEVEL to the
// container for a long time while nothing read it, so the knob looked set and
// did nothing; an explicit level now wins over the environment default.
var LevelEnvKeys = []string{"GRIMNIR_LOG_LEVEL", "RLM_LOG_LEVEL"}

// levelFor resolves the log level: an explicit, parseable level variable wins,
// otherwise development gets debug and everything else gets info. An
// unparseable value falls through to the environment default rather than
// failing startup, since losing the process over a typo in a log knob is worse
// than logging at the wrong level.
func levelFor(environment string) zerolog.Level {
	for _, k := range LevelEnvKeys {
		raw := strings.ToLower(strings.TrimSpace(os.Getenv(k)))
		if raw == "" {
			continue
		}
		if lvl, err := zerolog.ParseLevel(raw); err == nil {
			return lvl
		}
	}
	if environment == "development" {
		return zerolog.DebugLevel
	}
	return zerolog.InfoLevel
}

// Setup configures zerolog for the process.
func Setup(environment string) zerolog.Logger {
	return SetupWithWriter(environment, nil)
}

// SetupWithWriter configures zerolog with an additional writer (e.g., for log buffer).
func SetupWithWriter(environment string, additionalWriter io.Writer) zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	level := levelFor(environment)

	// Console writer for human-readable output
	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout}

	var writer io.Writer = consoleWriter
	if additionalWriter != nil {
		// JSON writer for the buffer (machine-readable)
		jsonWriter := os.Stdout // zerolog will use this for JSON format
		// Multi-writer: console for display, JSON for buffer
		multiWriter := zerolog.MultiLevelWriter(consoleWriter, additionalWriter)
		writer = multiWriter
		_ = jsonWriter // not used directly, additionalWriter captures JSON
	}

	logger := zerolog.New(writer).With().Timestamp().Logger().Level(level)
	log.Logger = logger
	return logger
}
