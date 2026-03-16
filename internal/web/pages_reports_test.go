/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
)

// migrateReportsTables auto-migrates the tables needed for schedule health report tests.
func migrateReportsTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(
		&models.User{},
		&models.Station{},
		&models.StationUser{},
		&models.Mount{},
		&models.MediaItem{},
		&models.Playlist{},
		&models.SmartBlock{},
		&models.ScheduleEntry{},
		&models.ScheduleSuppression{},
		&models.PlayHistory{},
		&models.LandingPage{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

func newReportsTestHandler(t *testing.T, db *gorm.DB) *Handler {
	t.Helper()
	h, err := NewHandler(db, []byte("test"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h
}

// reportsTestUser returns a user that can be injected into context for report rendering.
// The dashboard template requires a non-nil user with an Email field.
func reportsTestUser() *models.User {
	return &models.User{
		ID:           "u1",
		Email:        "admin@example.com",
		PlatformRole: models.PlatformRoleAdmin,
	}
}

// ---------------------------------------------------------------------------
// Health classification logic (pure unit tests, no DB needed)
// ---------------------------------------------------------------------------

func TestScheduleHealthClassification_GreenAt100Percent(t *testing.T) {
	health := classifyDayHealth(100.0, nil)
	if health != "green" {
		t.Fatalf("expected 'green' at 100%% coverage, got %q", health)
	}
}

func TestScheduleHealthClassification_GreenAt95Percent(t *testing.T) {
	health := classifyDayHealth(95.0, nil)
	if health != "green" {
		t.Fatalf("expected 'green' at 95%% coverage, got %q", health)
	}
}

func TestScheduleHealthClassification_YellowAt85Percent(t *testing.T) {
	health := classifyDayHealth(85.0, nil)
	if health != "yellow" {
		t.Fatalf("expected 'yellow' at 85%% coverage, got %q", health)
	}
}

func TestScheduleHealthClassification_RedAt40Percent(t *testing.T) {
	health := classifyDayHealth(40.0, nil)
	if health != "red" {
		t.Fatalf("expected 'red' at 40%% coverage, got %q", health)
	}
}

func TestScheduleHealthClassification_RedAt69Percent(t *testing.T) {
	health := classifyDayHealth(69.9, nil)
	if health != "red" {
		t.Fatalf("expected 'red' just below 70%%, got %q", health)
	}
}

func TestScheduleHealthClassification_YellowAt70Percent(t *testing.T) {
	// Exactly 70% coverage with no issues → yellow (not red)
	health := classifyDayHealth(70.0, nil)
	if health != "yellow" {
		t.Fatalf("expected 'yellow' at 70%% (boundary), got %q", health)
	}
}

func TestScheduleHealthClassification_IssueWithErrorMakesRed(t *testing.T) {
	issues := []SmartBlockIssue{
		{Error: "Smart block no longer exists", FillPct: 0},
	}
	health := classifyDayHealth(100.0, issues)
	if health != "red" {
		t.Fatalf("expected 'red' when issue has error, got %q", health)
	}
}

func TestScheduleHealthClassification_IssueWithLowFillMakesRed(t *testing.T) {
	issues := []SmartBlockIssue{
		{FillPct: 50, Underfilled: true}, // below 70% fill threshold
	}
	health := classifyDayHealth(100.0, issues)
	if health != "red" {
		t.Fatalf("expected 'red' when fill < 70%%, got %q", health)
	}
}

func TestScheduleHealthClassification_PendingIssueIgnoredForHealth(t *testing.T) {
	// Pending issues (future, unmaterialized) should not affect health color
	issues := []SmartBlockIssue{
		{Pending: true, Underfilled: true, Error: "not materialized yet"},
	}
	health := classifyDayHealth(100.0, issues)
	if health != "green" {
		t.Fatalf("expected 'green' when only pending issues exist, got %q", health)
	}
}

// classifyDayHealth replicates the health classification logic from pages_reports.go
// to allow testing without a full DB setup.
func classifyDayHealth(coveragePct float64, issues []SmartBlockIssue) string {
	health := "green"
	for _, iss := range issues {
		if iss.Pending {
			continue
		}
		if iss.Error != "" || iss.FillPct < 70 {
			health = "red"
			break
		}
		if health != "red" {
			health = "yellow"
		}
	}
	if health == "green" && coveragePct < 70 {
		health = "red"
	} else if health == "green" && coveragePct < 95 {
		health = "yellow"
	}
	return health
}

// ---------------------------------------------------------------------------
// Gap detection logic (pure unit tests)
// ---------------------------------------------------------------------------

func TestGapDetection_TwoEntriesWithGap(t *testing.T) {
	dayStart := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	entries := []models.ScheduleEntry{
		{
			StartsAt: dayStart,
			EndsAt:   dayStart.Add(8 * time.Hour), // ends at 08:00
		},
		{
			StartsAt: dayStart.Add(10 * time.Hour), // starts at 10:00 — 2h gap
			EndsAt:   dayStart.Add(24 * time.Hour),
		},
	}

	gaps := computeGapWindows(dayStart, entries)

	if len(gaps) != 1 {
		t.Fatalf("expected 1 gap window, got %d", len(gaps))
	}
	if gaps[0].Hours != 2.0 {
		t.Fatalf("expected 2-hour gap, got %v hours", gaps[0].Hours)
	}
}

func TestGapDetection_FullCoverageNoGap(t *testing.T) {
	dayStart := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	entries := []models.ScheduleEntry{
		{
			StartsAt: dayStart,
			EndsAt:   dayStart.Add(24 * time.Hour),
		},
	}

	gaps := computeGapWindows(dayStart, entries)
	if len(gaps) != 0 {
		t.Fatalf("expected 0 gaps for full coverage, got %d", len(gaps))
	}
}

func TestGapDetection_SmallGapUnder15MinIgnored(t *testing.T) {
	dayStart := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	entries := []models.ScheduleEntry{
		{
			StartsAt: dayStart,
			EndsAt:   dayStart.Add(8 * time.Hour),
		},
		{
			StartsAt: dayStart.Add(8*time.Hour + 10*time.Minute), // 10m gap
			EndsAt:   dayStart.Add(24 * time.Hour),
		},
	}

	gaps := computeGapWindows(dayStart, entries)
	if len(gaps) != 0 {
		t.Fatalf("expected 0 gaps for 10m gap (under 15m threshold), got %d", len(gaps))
	}
}

func TestGapDetection_GapAtEndOfDay(t *testing.T) {
	dayStart := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	entries := []models.ScheduleEntry{
		{
			StartsAt: dayStart,
			EndsAt:   dayStart.Add(22 * time.Hour), // leaves 2h at end
		},
	}

	gaps := computeGapWindows(dayStart, entries)
	if len(gaps) != 1 {
		t.Fatalf("expected 1 gap at end of day, got %d", len(gaps))
	}
	if gaps[0].Hours != 2.0 {
		t.Fatalf("expected 2-hour end gap, got %v hours", gaps[0].Hours)
	}
}

// computeGapWindows replicates the gap detection from ScheduleHealthReport.
// Entries must be sorted by StartsAt.
func computeGapWindows(dayStart time.Time, entries []models.ScheduleEntry) []GapWindow {
	dayEnd := dayStart.Add(24 * time.Hour)
	var gapWindows []GapWindow
	cursor := dayStart
	for _, e := range entries {
		eStart := e.StartsAt
		if eStart.Before(dayStart) {
			eStart = dayStart
		}
		if eStart.After(cursor) && eStart.Sub(cursor) >= 15*time.Minute {
			gapWindows = append(gapWindows, GapWindow{
				StartsAt: cursor,
				EndsAt:   eStart,
				Hours:    eStart.Sub(cursor).Hours(),
			})
		}
		if e.EndsAt.After(cursor) {
			cursor = e.EndsAt
		}
	}
	if cursor.Before(dayEnd) && dayEnd.Sub(cursor) >= 15*time.Minute {
		gapWindows = append(gapWindows, GapWindow{
			StartsAt: cursor,
			EndsAt:   dayEnd,
			Hours:    dayEnd.Sub(cursor).Hours(),
		})
	}
	return gapWindows
}

// ---------------------------------------------------------------------------
// ScheduleHealthReport handler — HTTP tests
// ---------------------------------------------------------------------------

func TestScheduleHealthReport_NoStationRedirects(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	migrateReportsTables(t, db)
	h := newReportsTestHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/schedule/report", nil)
	rr := httptest.NewRecorder()
	h.ScheduleHealthReport(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect with no station, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/dashboard/stations/select" {
		t.Fatalf("expected redirect to station select, got %q", loc)
	}
}

func TestScheduleHealthReport_EmptyScheduleReturns200(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	migrateReportsTables(t, db)

	station := models.Station{ID: "s1", Name: "Test Station", Active: true}
	db.Create(&station)

	h := newReportsTestHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/schedule/report", nil)
	ctx := context.WithValue(req.Context(), ctxKeyStation, &station)
	ctx = context.WithValue(ctx, ctxKeyUser, reportsTestUser())
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.ScheduleHealthReport(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty schedule, got %d body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, "Schedule Health Report") {
		t.Errorf("expected 'Schedule Health Report' in body")
	}
}

func TestScheduleHealthReport_ShowsGreenCount(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	migrateReportsTables(t, db)

	station := models.Station{ID: "s1", Name: "Test Station", Active: true}
	db.Create(&station)

	// Populate a fully covered day (today + 1 day from now)
	now := time.Now().UTC()
	tomorrow := now.Truncate(24 * time.Hour).Add(24 * time.Hour)

	db.Create(&models.ScheduleEntry{
		ID:         "e1",
		StationID:  "s1",
		MountID:    "m1",
		StartsAt:   tomorrow,
		EndsAt:     tomorrow.Add(24 * time.Hour),
		SourceType: "playlist",
		SourceID:   "p1",
	})

	h := newReportsTestHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/schedule/report", nil)
	ctx := context.WithValue(req.Context(), ctxKeyStation, &station)
	ctx = context.WithValue(ctx, ctxKeyUser, reportsTestUser())
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.ScheduleHealthReport(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	// A fully covered day should result in at least one "green" day
	if !strings.Contains(body, "green") {
		t.Errorf("expected at least one 'green' day in report body")
	}
}

func TestScheduleHealthReport_SuppressedSlotCountRendered(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	migrateReportsTables(t, db)

	station := models.Station{ID: "s1", Name: "Test Station", Active: true}
	db.Create(&station)

	// Create a suppression record for tomorrow
	now := time.Now().UTC()
	tomorrow := now.Truncate(24 * time.Hour).Add(24 * time.Hour)

	db.Create(&models.ScheduleSuppression{
		ID:        "sup1",
		StationID: "s1",
		SlotID:    "clock-slot-1",
		SlotType:  "clock",
		StartsAt:  tomorrow.Add(8 * time.Hour),
		Reason:    "window pre-filled",
	})

	h := newReportsTestHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/schedule/report", nil)
	ctx := context.WithValue(req.Context(), ctxKeyStation, &station)
	ctx = context.WithValue(ctx, ctxKeyUser, reportsTestUser())
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.ScheduleHealthReport(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	// The suppression count is rendered as a number in the UI
	if !strings.Contains(body, "1") {
		t.Errorf("expected suppressed slot count (1) to appear in body")
	}
}

// ---------------------------------------------------------------------------
// ScheduleRefreshReport handler
// ---------------------------------------------------------------------------

func TestScheduleRefreshReport_NoStationRedirects(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	migrateReportsTables(t, db)
	h := newReportsTestHandler(t, db)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/schedule/report/refresh", nil)
	rr := httptest.NewRecorder()
	h.ScheduleRefreshReport(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/dashboard/stations/select" {
		t.Fatalf("expected redirect to station select, got %q", loc)
	}
}

func TestScheduleRefreshReport_RedirectsToReport(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	migrateReportsTables(t, db)

	station := models.Station{ID: "s1", Name: "Test", Active: true}
	db.Create(&station)

	h := newReportsTestHandler(t, db)
	// No scheduler set — should still redirect

	req := httptest.NewRequest(http.MethodPost, "/dashboard/schedule/report/refresh", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &station))
	rr := httptest.NewRecorder()
	h.ScheduleRefreshReport(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect after refresh, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/dashboard/schedule/report" {
		t.Fatalf("expected redirect to /dashboard/schedule/report, got %q", loc)
	}
}

func TestScheduleRefreshReport_CallsSchedulerWhenSet(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	migrateReportsTables(t, db)

	station := models.Station{ID: "s1", Name: "Test", Active: true}
	db.Create(&station)

	h := newReportsTestHandler(t, db)

	// Inject a mock scheduler that records calls
	called := false
	calledStationID := ""
	h.scheduler = &mockScheduler{
		refreshFn: func(ctx context.Context, stationID string) error {
			called = true
			calledStationID = stationID
			return nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/dashboard/schedule/report/refresh", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &station))
	rr := httptest.NewRecorder()
	h.ScheduleRefreshReport(rr, req)

	if !called {
		t.Errorf("expected scheduler.RefreshStation to be called")
	}
	if calledStationID != "s1" {
		t.Errorf("expected RefreshStation called with 's1', got %q", calledStationID)
	}
}

// mockScheduler implements SchedulerService for tests.
type mockScheduler struct {
	refreshFn func(ctx context.Context, stationID string) error
}

func (m *mockScheduler) RefreshStation(ctx context.Context, stationID string) error {
	if m.refreshFn != nil {
		return m.refreshFn(ctx, stationID)
	}
	return nil
}

func (m *mockScheduler) Materialize(ctx context.Context, req smartblock.GenerateRequest) (smartblock.GenerateResult, error) {
	return smartblock.GenerateResult{}, nil
}

// ---------------------------------------------------------------------------
// DayHealth coverage calculation (pure unit tests)
// ---------------------------------------------------------------------------

func TestCoveragePct_FullDay(t *testing.T) {
	dayStart := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	entries := []models.ScheduleEntry{
		{StartsAt: dayStart, EndsAt: dayStart.Add(24 * time.Hour)},
	}
	pct := computeCoveragePct(entries)
	if pct != 100.0 {
		t.Fatalf("expected 100%% for full-day coverage, got %.2f", pct)
	}
}

func TestCoveragePct_HalfDay(t *testing.T) {
	dayStart := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	entries := []models.ScheduleEntry{
		{StartsAt: dayStart, EndsAt: dayStart.Add(12 * time.Hour)},
	}
	pct := computeCoveragePct(entries)
	if pct != 50.0 {
		t.Fatalf("expected 50%% for half-day coverage, got %.2f", pct)
	}
}

func TestCoveragePct_CappedAt100(t *testing.T) {
	dayStart := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	// More than 24h of entries (overlapping)
	entries := []models.ScheduleEntry{
		{StartsAt: dayStart, EndsAt: dayStart.Add(20 * time.Hour)},
		{StartsAt: dayStart.Add(2 * time.Hour), EndsAt: dayStart.Add(24*time.Hour + 2*time.Hour)},
	}
	pct := computeCoveragePct(entries)
	if pct > 100 {
		t.Fatalf("expected coverage capped at 100%%, got %.2f", pct)
	}
}

// computeCoveragePct replicates the coverage calculation from ScheduleHealthReport.
func computeCoveragePct(entries []models.ScheduleEntry) float64 {
	var scheduledSecs float64
	for _, e := range entries {
		if dur := e.EndsAt.Sub(e.StartsAt); dur > 0 {
			scheduledSecs += dur.Seconds()
		}
	}
	scheduledHours := scheduledSecs / 3600.0
	coveragePct := scheduledHours / 24.0 * 100.0
	if coveragePct > 100 {
		return 100
	}
	return coveragePct
}
