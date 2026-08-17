/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package analytics

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newScheduleAnalytics(t *testing.T) (*ScheduleAnalyticsService, *gorm.DB) {
	t.Helper()
	db := dbtest.Open(t,
		&models.Station{}, &models.Show{}, &models.ShowInstance{},
		&models.ScheduleAnalytics{}, &models.ScheduleAnalyticsDaily{},
	)
	// Station must exist before shows/instances/analytics reference it (FK).
	if err := db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "Test"}).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	return NewScheduleAnalyticsService(db, zerolog.Nop()), db
}

func TestDayName(t *testing.T) {
	want := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	for i, w := range want {
		if got := dayName(i); got != w {
			t.Fatalf("dayName(%d) = %q, want %q", i, got, w)
		}
	}
	if dayName(7) != "Unknown" || dayName(-1) != "Unknown" {
		t.Fatal("out-of-range day should be Unknown")
	}
}

func TestMin(t *testing.T) {
	if min(3, 5) != 3 || min(5, 3) != 3 || min(4, 4) != 4 {
		t.Fatal("min wrong")
	}
}

func TestRecordHourlyStats(t *testing.T) {
	svc, db := newScheduleAnalytics(t)
	date := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	// A show instance covering hour 9 links the analytics row to the show.
	db.Create(&models.Show{ID: dbtest.UUID("sh1"), StationID: dbtest.UUID("st1"), Name: "Morning"})
	db.Create(&models.ShowInstance{
		ID: dbtest.UUID("i1"), ShowID: dbtest.UUID("sh1"), StationID: dbtest.UUID("st1"),
		StartsAt: date.Add(9 * time.Hour), EndsAt: date.Add(11 * time.Hour), Status: models.ShowInstanceScheduled,
	})

	err := svc.RecordHourlyStats(context.Background(), dbtest.UUID("st1"), date, 9, HourlyStats{
		AvgListeners: 42, PeakListeners: 80, TuneIns: 10, TuneOuts: 5, TotalMinutes: 2520,
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	var row models.ScheduleAnalytics
	if err := db.First(&row, "station_id = ? AND hour = ?", dbtest.UUID("st1"), 9).Error; err != nil {
		t.Fatalf("row not created: %v", err)
	}
	if row.ShowID == nil || *row.ShowID != dbtest.UUID("sh1") {
		t.Fatalf("row should link to the covering show: %+v", row.ShowID)
	}
	if row.AvgListeners != 42 || row.PeakListeners != 80 {
		t.Fatalf("stats not stored: %+v", row)
	}
}

func seedHourly(t *testing.T, db *gorm.DB, id, showID string, date time.Time, hour, avg, peak int) {
	t.Helper()
	sid := showID
	row := &models.ScheduleAnalytics{
		ID: id, StationID: dbtest.UUID("st1"), ShowID: &sid, Date: date, Hour: hour,
		AvgListeners: avg, PeakListeners: peak, TuneIns: 3, TotalMinutes: avg * 60,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("seed hourly: %v", err)
	}
}

func TestGetShowPerformance(t *testing.T) {
	svc, db := newScheduleAnalytics(t)
	date := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	db.Create(&models.Show{ID: dbtest.UUID("sh1"), StationID: dbtest.UUID("st1"), Name: "Morning Drive"})
	seedHourly(t, db, dbtest.UUID("a1"), dbtest.UUID("sh1"), date, 9, 40, 70)
	seedHourly(t, db, dbtest.UUID("a2"), dbtest.UUID("sh1"), date, 10, 60, 90)

	perf, err := svc.GetShowPerformance(context.Background(), dbtest.UUID("st1"), date, date.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("show performance: %v", err)
	}
	if len(perf) != 1 {
		t.Fatalf("expected 1 show, got %d", len(perf))
	}
	if perf[0].ShowName != "Morning Drive" || perf[0].PeakListeners != 90 {
		t.Fatalf("performance row wrong: %+v", perf[0])
	}
}
