/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package logging

import (
	"io"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Setup configures zerolog for the process.
func Setup(environment string) zerolog.Logger {
	return SetupWithWriter(environment, nil)
}

// SetupWithWriter configures zerolog with an additional writer (e.g., for log buffer).
func SetupWithWriter(environment string, additionalWriter io.Writer) zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	level := zerolog.InfoLevel
	if environment == "development" {
		level = zerolog.DebugLevel
	}

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
