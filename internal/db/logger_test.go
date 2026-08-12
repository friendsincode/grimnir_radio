/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package db

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// captureGormLog points the gorm writer at a buffer for the duration of a test.
func captureGormLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gormStdLogger.SetOutput(&buf)
	t.Cleanup(func() { gormStdLogger.SetOutput(os.Stdout) })
	return &buf
}

// A lookup that finds nothing is normal traffic, not an error. gorm logs
// ErrRecordNotFound regardless of log level unless told to ignore it, and on
// prod one polling query that usually misses wrote 104,492 of 930,783 lines in
// 3.5 hours, which is what pushed useful history out of the rotation window.
func TestConnect_DoesNotLogRecordNotFound(t *testing.T) {
	database, err := Connect(&config.Config{
		DBBackend: config.DatabaseSQLite,
		DBDSN:     ":memory:",
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := database.AutoMigrate(&models.ScheduleEntry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	buf := captureGormLog(t)

	var entry models.ScheduleEntry
	if err := database.First(&entry, "id = ?", "nope").Error; err == nil {
		t.Fatal("expected a miss on an empty table")
	}

	if got := buf.String(); strings.TrimSpace(got) != "" {
		t.Fatalf("a record-not-found miss wrote to the log:\n%s", got)
	}
}

// The writer must not prefix a blank line the way gorm's default one does.
func TestGormLogWriter_NoBlankLinePrefix(t *testing.T) {
	buf := captureGormLog(t)
	gormLogWriter{}.Printf("hello %s", "world")

	got := buf.String()
	if strings.HasPrefix(got, "\r\n") || strings.HasPrefix(got, "\n") {
		t.Fatalf("writer emitted a leading blank line: %q", got)
	}
	if !strings.Contains(got, "hello world") {
		t.Fatalf("writer dropped the message: %q", got)
	}
}
