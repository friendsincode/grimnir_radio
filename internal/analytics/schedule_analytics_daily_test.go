/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package analytics

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestGetShowPerformance_UsesDailyRollups covers the daily-rollup branch of
// GetShowPerformance (the existing test exercises only the hourly fallback).
func TestGetShowPerformance_UsesDailyRollups(t *testing.T) {
	svc, db := newScheduleAnalytics(t)
	date := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)

	db.Create(&models.Show{ID: dbtest.UUID("sh1"), StationID: dbtest.UUID("st1"), Name: "Evening Set"})
	db.Create(&models.ScheduleAnalyticsDaily{
		ID:        dbtest.UUID("d1"),
		StationID: dbtest.UUID("st1"), Date: date, Scope: "show", ShowID: dbtest.UUID("sh1"),
		InstanceCount: 3, AvgListeners: 50, PeakListeners: 80,
		TuneIns: 12, TotalListenerMinutes: 400,
	})

	perf, err := svc.GetShowPerformance(context.Background(), dbtest.UUID("st1"), date, date.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("GetShowPerformance: %v", err)
	}
	if len(perf) != 1 {
		t.Fatalf("expected 1 show from daily rollups, got %d", len(perf))
	}
	if perf[0].ShowName != "Evening Set" || perf[0].PeakListeners != 80 {
		t.Fatalf("daily rollup row wrong: %+v", perf[0])
	}
}
