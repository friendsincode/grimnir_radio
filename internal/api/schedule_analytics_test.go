/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/analytics"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newScheduleAnalyticsAPITest(t *testing.T) (*ScheduleAnalyticsAPI, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.ScheduleAnalytics{},
		&models.ScheduleAnalyticsDaily{},
		&models.ShowInstance{},
		&models.Show{},
		&models.Station{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	base := &API{db: db, logger: zerolog.Nop()}
	svc := analytics.NewScheduleAnalyticsService(db, zerolog.Nop())
	return NewScheduleAnalyticsAPI(base, svc), db
}

func TestScheduleAnalyticsAPI_MissingStationID(t *testing.T) {
	a, _ := newScheduleAnalyticsAPITest(t)

	handlers := []struct {
		name    string
		handler http.HandlerFunc
		method  string
	}{
		{"aggregate-daily", a.handleAggregateDaily, "POST"},
		{"show-performance", a.handleShowPerformance, "GET"},
		{"time-slot-performance", a.handleTimeSlotPerformance, "GET"},
		{"best-slots", a.handleBestTimeSlots, "GET"},
		{"suggestions", a.handleSchedulingSuggestions, "GET"},
	}

	for _, h := range handlers {
		t.Run(h.name, func(t *testing.T) {
			req := httptest.NewRequest(h.method, "/", nil)
			rr := httptest.NewRecorder()
			h.handler(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("%s: got %d, want 400", h.name, rr.Code)
			}
		})
	}
}

func TestScheduleAnalyticsAPI_AggregateDaily(t *testing.T) {
	a, _ := newScheduleAnalyticsAPITest(t)

	// Valid request with no data in range → 200
	req := httptest.NewRequest("POST", "/?station_id=s1&start=2025-01-01&end=2025-01-02", nil)
	rr := httptest.NewRecorder()
	a.handleAggregateDaily(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("aggregate daily: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", resp["status"])
	}
	if resp["start"] != "2025-01-01" {
		t.Fatalf("expected start=2025-01-01, got %v", resp["start"])
	}
}

func TestScheduleAnalyticsAPI_AggregateDailyDateRange(t *testing.T) {
	a, _ := newScheduleAnalyticsAPITest(t)

	// Reversed start/end → should be swapped
	req := httptest.NewRequest("POST", "/?station_id=s1&start=2025-01-10&end=2025-01-01", nil)
	rr := httptest.NewRecorder()
	a.handleAggregateDaily(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("aggregate daily reversed: got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	// Handler swaps reversed dates
	if resp["start"] != "2025-01-01" {
		t.Fatalf("expected start=2025-01-01 after swap, got %v", resp["start"])
	}

	// No date params → uses yesterday default (no error)
	req = httptest.NewRequest("POST", "/?station_id=s1", nil)
	rr = httptest.NewRecorder()
	a.handleAggregateDaily(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("aggregate daily default: got %d, want 200", rr.Code)
	}
}

func TestScheduleAnalyticsAPI_ShowPerformance(t *testing.T) {
	a, _ := newScheduleAnalyticsAPITest(t)

	req := httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr := httptest.NewRecorder()
	a.handleShowPerformance(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("show performance: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["start"]; !ok {
		t.Fatal("expected start key in response")
	}
}

func TestScheduleAnalyticsAPI_ShowPerformanceWithDateRange(t *testing.T) {
	a, _ := newScheduleAnalyticsAPITest(t)

	req := httptest.NewRequest("GET", "/?station_id=s1&start=2025-01-01&end=2025-01-31", nil)
	rr := httptest.NewRecorder()
	a.handleShowPerformance(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("show performance with range: got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["start"] != "2025-01-01" {
		t.Fatalf("expected start=2025-01-01, got %v", resp["start"])
	}
	if resp["end"] != "2025-01-31" {
		t.Fatalf("expected end=2025-01-31, got %v", resp["end"])
	}
}

// Note: handleTimeSlotPerformance, handleBestTimeSlots, and handleSchedulingSuggestions
// use EXTRACT(DOW FROM date) which is PostgreSQL-specific syntax and not supported by SQLite.
// The missing-station-id guard is covered by TestScheduleAnalyticsAPI_MissingStationID above.
// Full integration tests for these handlers run against PostgreSQL in the integration test suite.
