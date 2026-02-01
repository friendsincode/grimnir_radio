/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package logging

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Setup configures zerolog for the process.
func Setup(environment string) zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	level := zerolog.InfoLevel
	if environment == "development" {
		level = zerolog.DebugLevel
	}
	logger := log.Output(zerolog.ConsoleWriter{Out: os.Stdout}).Level(level)
	log.Logger = logger
	return logger
}
