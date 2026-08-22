/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
)

// TestGetSystemSettings_Defaults guards #86: GetSystemSettings FirstOrCreates the
// singleton row (ID=1) that gates analysis, websockets and metrics platform-wide.
// Nothing asserted the created row carries the intended defaults rather than Go
// zero values, so the platform booting with those features silently off would go
// unnoticed. A second call must return the same row, not create a duplicate.
func TestGetSystemSettings_Defaults(t *testing.T) {
	db := dbtest.Open(t, &SystemSettings{})

	s, err := GetSystemSettings(db)
	if err != nil {
		t.Fatalf("GetSystemSettings: %v", err)
	}
	if !s.AnalysisEnabled || !s.WebsocketEnabled || !s.MetricsEnabled {
		t.Errorf("feature toggles = analysis:%v ws:%v metrics:%v, want all true",
			s.AnalysisEnabled, s.WebsocketEnabled, s.MetricsEnabled)
	}
	if s.SchedulerLookahead != "168h" {
		t.Errorf("SchedulerLookahead = %q, want 168h", s.SchedulerLookahead)
	}
	if s.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", s.LogLevel)
	}

	// Idempotent: no second row.
	if _, err := GetSystemSettings(db); err != nil {
		t.Fatalf("second GetSystemSettings: %v", err)
	}
	var n int64
	db.Model(&SystemSettings{}).Count(&n)
	if n != 1 {
		t.Fatalf("system_settings rows = %d, want 1", n)
	}
}
