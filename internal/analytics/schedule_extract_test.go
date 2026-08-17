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

// These exercise the raw-SQL methods that use PostgreSQL EXTRACT(DOW/HOUR ...),
// which sqlite could not run — now covered on the dbtest Postgres harness.

func TestGetTimeSlotPerformance(t *testing.T) {
	svc, db := newScheduleAnalytics(t)
	date := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC) // a Monday
	db.Create(&models.Show{ID: dbtest.UUID("sh1"), StationID: dbtest.UUID("st1"), Name: "Show"})
	seedHourly(t, db, dbtest.UUID("a1"), dbtest.UUID("sh1"), date, 9, 40, 70)
	seedHourly(t, db, dbtest.UUID("a2"), dbtest.UUID("sh1"), date, 9, 60, 90) // same DOW+hour, aggregates
	seedHourly(t, db, dbtest.UUID("a3"), dbtest.UUID("sh1"), date, 20, 10, 15)

	slots, err := svc.GetTimeSlotPerformance(context.Background(), dbtest.UUID("st1"), date, date.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("GetTimeSlotPerformance: %v", err)
	}
	if len(slots) != 2 {
		t.Fatalf("expected 2 time slots (hours 9 and 20), got %d: %+v", len(slots), slots)
	}
	byHour := map[int]models.TimeSlotPerformance{}
	for _, s := range slots {
		byHour[s.Hour] = s
	}
	if h9 := byHour[9]; h9.PeakListeners != 90 || h9.SampleCount != 2 {
		t.Fatalf("hour 9 slot wrong: %+v", h9)
	}
}

func TestGetBestTimeSlots_SortedAndLimited(t *testing.T) {
	svc, db := newScheduleAnalytics(t)
	date := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -3)
	db.Create(&models.Show{ID: dbtest.UUID("sh1"), StationID: dbtest.UUID("st1"), Name: "Show"})
	seedHourly(t, db, dbtest.UUID("a1"), dbtest.UUID("sh1"), date, 8, 20, 30)
	seedHourly(t, db, dbtest.UUID("a2"), dbtest.UUID("sh1"), date, 9, 80, 95)
	seedHourly(t, db, dbtest.UUID("a3"), dbtest.UUID("sh1"), date, 10, 50, 60)

	best, err := svc.GetBestTimeSlots(context.Background(), dbtest.UUID("st1"), 2)
	if err != nil {
		t.Fatalf("GetBestTimeSlots: %v", err)
	}
	if len(best) != 2 {
		t.Fatalf("expected top 2 slots, got %d", len(best))
	}
	if best[0].AvgListeners < best[1].AvgListeners {
		t.Fatalf("slots not sorted desc by avg: %+v", best)
	}
	if best[0].Hour != 9 {
		t.Fatalf("top slot should be hour 9 (avg 80), got hour %d", best[0].Hour)
	}
}

func TestAggregateDaily_RollsUpAndUpserts(t *testing.T) {
	svc, db := newScheduleAnalytics(t)
	date := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	db.Create(&models.Show{ID: dbtest.UUID("sh1"), StationID: dbtest.UUID("st1"), Name: "Show"})
	seedHourly(t, db, dbtest.UUID("a1"), dbtest.UUID("sh1"), date, 9, 40, 70)
	seedHourly(t, db, dbtest.UUID("a2"), dbtest.UUID("sh1"), date, 10, 60, 90)

	if err := svc.AggregateDaily(context.Background(), dbtest.UUID("st1"), date); err != nil {
		t.Fatalf("AggregateDaily: %v", err)
	}

	var station models.ScheduleAnalyticsDaily
	if err := db.Where("station_id = ? AND scope = ?", dbtest.UUID("st1"), "station").First(&station).Error; err != nil {
		t.Fatalf("no station rollup: %v", err)
	}
	if station.PeakListeners != 90 || station.HoursCovered != 2 {
		t.Fatalf("station rollup wrong: %+v", station)
	}

	// Re-running upserts (ON CONFLICT), not duplicates.
	if err := svc.AggregateDaily(context.Background(), dbtest.UUID("st1"), date); err != nil {
		t.Fatalf("AggregateDaily rerun: %v", err)
	}
	var n int64
	db.Model(&models.ScheduleAnalyticsDaily{}).Where("station_id = ? AND scope = ?", dbtest.UUID("st1"), "station").Count(&n)
	if n != 1 {
		t.Fatalf("station rollup duplicated on rerun: %d rows", n)
	}
}

func TestBackfillDaily_CoversRange(t *testing.T) {
	svc, db := newScheduleAnalytics(t)
	d1 := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	d2 := d1.AddDate(0, 0, 1)
	db.Create(&models.Show{ID: dbtest.UUID("sh1"), StationID: dbtest.UUID("st1"), Name: "Show"})
	seedHourly(t, db, dbtest.UUID("a1"), dbtest.UUID("sh1"), d1, 9, 40, 70)
	seedHourly(t, db, dbtest.UUID("a2"), dbtest.UUID("sh1"), d2, 9, 50, 80)

	if err := svc.BackfillDaily(context.Background(), dbtest.UUID("st1"), d1, d2); err != nil {
		t.Fatalf("BackfillDaily: %v", err)
	}
	var days int64
	db.Model(&models.ScheduleAnalyticsDaily{}).Where("station_id = ? AND scope = ?", dbtest.UUID("st1"), "station").Count(&days)
	if days != 2 {
		t.Fatalf("expected station rollups for 2 days, got %d", days)
	}
}

func TestGetSchedulingSuggestions_Runs(t *testing.T) {
	svc, db := newScheduleAnalytics(t)
	date := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -3)
	db.Create(&models.Show{ID: dbtest.UUID("sh1"), StationID: dbtest.UUID("st1"), Name: "Show"})
	// A high-traffic slot with no regular programming triggers an "add_show" suggestion.
	seedHourly(t, db, dbtest.UUID("a1"), dbtest.UUID("sh1"), date, 9, 90, 120)

	suggestions, err := svc.GetSchedulingSuggestions(context.Background(), dbtest.UUID("st1"))
	if err != nil {
		t.Fatalf("GetSchedulingSuggestions: %v", err)
	}
	// The exact set depends on trend math; at minimum it must run and return a slice.
	if len(suggestions) == 0 {
		t.Fatal("expected a (possibly empty) suggestions slice, got nil")
	}
}
