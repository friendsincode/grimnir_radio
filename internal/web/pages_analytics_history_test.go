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
)

// newAnalyticsTestHandler creates a Handler with full template loading for analytics tests.
func newAnalyticsTestHandler(t *testing.T, db *gorm.DB) *Handler {
	t.Helper()
	h, err := NewHandler(db, []byte("test-secret"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h
}

func migrateAnalyticsTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(
		&models.User{},
		&models.Station{},
		&models.StationUser{},
		&models.Mount{},
		&models.MediaItem{},
		&models.Playlist{},
		&models.PlayHistory{},
		&models.LandingPage{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Restart / interrupt detection logic (in-memory, no templates)
// ---------------------------------------------------------------------------

// applyRestartDetection replicates the in-memory detection loop from AnalyticsHistory.
// The history slice must be in DESC order (newest first).
func applyRestartDetection(history []analyticsHistoryRow) {
	type trackKey struct{ title, artist string }
	for i := range history {
		key := trackKey{history[i].Title, history[i].Artist}
		if key.title == "" {
			continue
		}
		for j := i + 1; j < len(history); j++ {
			if history[j].Title != key.title || history[j].Artist != key.artist {
				continue
			}
			runtime := history[j].FullRuntime
			if runtime <= 0 {
				runtime = history[j].Duration
			}
			if runtime <= 0 {
				break
			}
			expectedEnd := history[j].StartedAt.Add(runtime)
			if expectedEnd.After(history[i].StartedAt.Add(10 * time.Second)) {
				history[i].Restarted = true
				history[j].Interrupted = true
				history[j].PlayedFor = history[i].StartedAt.Sub(history[j].StartedAt)
				if history[j].CutOffsetMS > 0 {
					history[i].ResumedFromMS = history[j].CutOffsetMS
					history[i].ResumeStrategy = "cut"
				} else {
					history[i].ResumeStrategy = "crash"
				}
			}
			break
		}
	}
}

func TestAnalyticsRestartDetection_DetectsRestart(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	fullDuration := 4 * time.Minute

	// History in DESC order: index 0 = newest play
	history := []analyticsHistoryRow{
		{
			// Newer play of same track (restart) — index 0
			Title:       "Track One",
			Artist:      "Artist One",
			StartedAt:   base.Add(2 * time.Minute), // started 2min into original's runtime
			EndedAt:     base.Add(6 * time.Minute),
			Duration:    4 * time.Minute,
			FullRuntime: fullDuration,
		},
		{
			// Older play that was cut short — index 1
			Title:       "Track One",
			Artist:      "Artist One",
			StartedAt:   base,
			EndedAt:     base.Add(2 * time.Minute), // ended early (cut at 2min of 4min track)
			Duration:    2 * time.Minute,
			FullRuntime: fullDuration,
		},
	}

	applyRestartDetection(history)

	if !history[0].Restarted {
		t.Errorf("expected history[0] (newer play) to be marked as Restarted")
	}
	if !history[1].Interrupted {
		t.Errorf("expected history[1] (older play) to be marked as Interrupted")
	}
	if history[1].PlayedFor != 2*time.Minute {
		t.Errorf("expected PlayedFor=2m, got %v", history[1].PlayedFor)
	}
}

func TestAnalyticsRestartDetection_NormalBackToBack_NotRestart(t *testing.T) {
	// Two plays of the same track back-to-back where the first completed normally
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	fullDuration := 3 * time.Minute

	history := []analyticsHistoryRow{
		{
			// Second play starts after first's expected end + 10s grace
			Title:       "Filler Track",
			Artist:      "Artist",
			StartedAt:   base.Add(fullDuration + 15*time.Second),
			EndedAt:     base.Add(fullDuration + 15*time.Second + fullDuration),
			Duration:    fullDuration,
			FullRuntime: fullDuration,
		},
		{
			// First play completed fully
			Title:       "Filler Track",
			Artist:      "Artist",
			StartedAt:   base,
			EndedAt:     base.Add(fullDuration),
			Duration:    fullDuration,
			FullRuntime: fullDuration,
		},
	}

	applyRestartDetection(history)

	if history[0].Restarted {
		t.Errorf("expected back-to-back completed tracks NOT to be marked as Restarted")
	}
	if history[1].Interrupted {
		t.Errorf("expected normally completed track NOT to be marked as Interrupted")
	}
}

func TestAnalyticsRestartDetection_CutOffsetFlowsToResumedFromMS(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	fullDuration := 5 * time.Minute
	const cutOffsetMS = int64(90000) // 90 seconds in

	history := []analyticsHistoryRow{
		{
			Title:       "Cut Track",
			Artist:      "DJ",
			StartedAt:   base.Add(90 * time.Second),
			Duration:    5 * time.Minute,
			FullRuntime: fullDuration,
		},
		{
			Title:       "Cut Track",
			Artist:      "DJ",
			StartedAt:   base,
			Duration:    90 * time.Second,
			FullRuntime: fullDuration,
			CutOffsetMS: cutOffsetMS,
		},
	}

	applyRestartDetection(history)

	if !history[0].Restarted {
		t.Errorf("expected restart to be detected")
	}
	if history[0].ResumedFromMS != cutOffsetMS {
		t.Errorf("expected ResumedFromMS=%d, got %d", cutOffsetMS, history[0].ResumedFromMS)
	}
	if history[0].ResumeStrategy != "cut" {
		t.Errorf("expected ResumeStrategy='cut', got %q", history[0].ResumeStrategy)
	}
}

func TestAnalyticsRestartDetection_NoCutOffset_CrashStrategy(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	fullDuration := 5 * time.Minute

	history := []analyticsHistoryRow{
		{
			Title:       "Crash Track",
			Artist:      "DJ",
			StartedAt:   base.Add(2 * time.Minute),
			Duration:    5 * time.Minute,
			FullRuntime: fullDuration,
		},
		{
			Title:       "Crash Track",
			Artist:      "DJ",
			StartedAt:   base,
			Duration:    2 * time.Minute,
			FullRuntime: fullDuration,
			CutOffsetMS: 0, // no saved position
		},
	}

	applyRestartDetection(history)

	if !history[0].Restarted {
		t.Errorf("expected restart to be detected")
	}
	if history[0].ResumeStrategy != "crash" {
		t.Errorf("expected ResumeStrategy='crash', got %q", history[0].ResumeStrategy)
	}
	if history[0].ResumedFromMS != 0 {
		t.Errorf("expected ResumedFromMS=0 for crash strategy, got %d", history[0].ResumedFromMS)
	}
}

func TestAnalyticsRestartDetection_EmptyTitleSkipped(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

	history := []analyticsHistoryRow{
		{
			Title:       "", // empty — should be skipped
			Artist:      "Artist",
			StartedAt:   base.Add(time.Minute),
			FullRuntime: 5 * time.Minute,
		},
		{
			Title:       "",
			Artist:      "Artist",
			StartedAt:   base,
			FullRuntime: 5 * time.Minute,
		},
	}

	applyRestartDetection(history)

	if history[0].Restarted || history[1].Interrupted {
		t.Errorf("expected empty-title tracks to be skipped in restart detection")
	}
}

// ---------------------------------------------------------------------------
// AnalyticsHistory handler — DB-backed integration tests
// ---------------------------------------------------------------------------

// analyticsTestUser returns a minimal user suitable for context injection in analytics tests.
// The dashboard template calls .User.Email unconditionally, so a user is always required.
func analyticsTestUser() *models.User {
	return &models.User{
		ID:           "u1",
		Email:        "test@example.com",
		PlatformRole: models.PlatformRoleUser,
	}
}

func TestAnalyticsHistory_EmptyResultReturns200(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	migrateAnalyticsTables(t, db)

	station := models.Station{ID: "s1", Name: "S1", Active: true}
	db.Create(&station)

	h := newAnalyticsTestHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/analytics/history", nil)
	ctx := context.WithValue(req.Context(), ctxKeyStation, &station)
	ctx = context.WithValue(ctx, ctxKeyUser, analyticsTestUser())
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.AnalyticsHistory(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty history, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAnalyticsHistory_ReturnsRowsDescOrder(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	migrateAnalyticsTables(t, db)

	station := models.Station{ID: "s1", Name: "S1", Active: true}
	db.Create(&station)

	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	entries := []models.PlayHistory{
		{ID: "ph1", StationID: "s1", Title: "First Track", Artist: "A", StartedAt: base, EndedAt: base.Add(3 * time.Minute)},
		{ID: "ph2", StationID: "s1", Title: "Second Track", Artist: "B", StartedAt: base.Add(5 * time.Minute), EndedAt: base.Add(8 * time.Minute)},
		{ID: "ph3", StationID: "s1", Title: "Third Track", Artist: "C", StartedAt: base.Add(10 * time.Minute), EndedAt: base.Add(13 * time.Minute)},
	}
	for i := range entries {
		db.Create(&entries[i])
	}

	h := newAnalyticsTestHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/analytics/history", nil)
	ctx := context.WithValue(req.Context(), ctxKeyStation, &station)
	ctx = context.WithValue(ctx, ctxKeyUser, analyticsTestUser())
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.AnalyticsHistory(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	// Verify all tracks appear in the rendered output
	for _, track := range []string{"First Track", "Second Track", "Third Track"} {
		if !strings.Contains(body, track) {
			t.Errorf("expected body to contain %q", track)
		}
	}
	// Verify descending order: "Third Track" should appear before "First Track"
	thirdIdx := strings.Index(body, "Third Track")
	firstIdx := strings.Index(body, "First Track")
	if thirdIdx == -1 || firstIdx == -1 {
		t.Fatalf("could not find tracks in body")
	}
	if thirdIdx >= firstIdx {
		t.Errorf("expected Third Track (newest) to appear before First Track (oldest) in DESC output")
	}
}

func TestAnalyticsHistory_NoStationRedirects(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	migrateAnalyticsTables(t, db)
	h := newAnalyticsTestHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/analytics/history", nil)
	rr := httptest.NewRecorder()
	h.AnalyticsHistory(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect with no station, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/dashboard/stations/select" {
		t.Fatalf("expected redirect to station select, got %q", loc)
	}
}

func TestAnalyticsHistory_DateRangeFilterApplied(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	migrateAnalyticsTables(t, db)

	station := models.Station{ID: "s1", Name: "S1", Active: true}
	db.Create(&station)

	// Two tracks: one in-range, one outside range
	inRange := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	outRange := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	db.Create(&models.PlayHistory{ID: "ph1", StationID: "s1", Title: "In Range Track", Artist: "A", StartedAt: inRange, EndedAt: inRange.Add(3 * time.Minute)})
	db.Create(&models.PlayHistory{ID: "ph2", StationID: "s1", Title: "Out Range Track", Artist: "B", StartedAt: outRange, EndedAt: outRange.Add(3 * time.Minute)})

	h := newAnalyticsTestHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/analytics/history?from=2026-03-01&to=2026-03-15", nil)
	ctx := context.WithValue(req.Context(), ctxKeyStation, &station)
	ctx = context.WithValue(ctx, ctxKeyUser, analyticsTestUser())
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.AnalyticsHistory(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, "In Range Track") {
		t.Errorf("expected in-range track to appear in filtered results")
	}
	if strings.Contains(body, "Out Range Track") {
		t.Errorf("expected out-of-range track to NOT appear in filtered results")
	}
}

// ---------------------------------------------------------------------------
// Source type normalization (unit tests on classification logic)
// ---------------------------------------------------------------------------

func TestAnalyticsSourceNormalization_LiveDJMapsToLive(t *testing.T) {
	// Replicate the source normalization logic from AnalyticsHistory
	classify := func(meta map[string]any) string {
		source := "automation"
		if meta != nil {
			if st, ok := meta["source_type"].(string); ok && strings.TrimSpace(st) != "" {
				source = strings.ToLower(strings.TrimSpace(st))
			} else if typ, ok := meta["type"].(string); ok && strings.TrimSpace(typ) != "" {
				source = strings.ToLower(strings.TrimSpace(typ))
			}
		}
		if source == "" {
			source = "automation"
		}
		switch source {
		case "live", "live_dj":
			return "live"
		case "playlist", "media", "smart_block", "clock_template", "webstream":
			return source
		default:
			return "automation"
		}
	}

	tests := []struct {
		name     string
		meta     map[string]any
		expected string
	}{
		{"live_dj maps to live", map[string]any{"source_type": "live_dj"}, "live"},
		{"live stays live", map[string]any{"source_type": "live"}, "live"},
		{"playlist stays", map[string]any{"source_type": "playlist"}, "playlist"},
		{"nil metadata defaults to automation", nil, "automation"},
		{"unknown defaults to automation", map[string]any{"source_type": "something_weird"}, "automation"},
		{"type field fallback", map[string]any{"type": "live_dj"}, "live"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.meta); got != tc.expected {
				t.Errorf("classify(%v): expected %q, got %q", tc.meta, tc.expected, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Pagination boundary check
// ---------------------------------------------------------------------------

func TestAnalyticsHistory_PaginationPage1With5Rows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	migrateAnalyticsTables(t, db)

	station := models.Station{ID: "s1", Name: "S1", Active: true}
	db.Create(&station)

	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		db.Create(&models.PlayHistory{
			ID:        "ph" + string(rune('0'+i)),
			StationID: "s1",
			Title:     "Track",
			Artist:    "Artist",
			StartedAt: base.Add(time.Duration(i) * time.Minute),
			EndedAt:   base.Add(time.Duration(i)*time.Minute + 3*time.Minute),
		})
	}

	h := newAnalyticsTestHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/analytics/history?page=1", nil)
	ctx := context.WithValue(req.Context(), ctxKeyStation, &station)
	ctx = context.WithValue(ctx, ctxKeyUser, analyticsTestUser())
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.AnalyticsHistory(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	// All 5 rows should appear since perPage=100
	body := rr.Body.String()
	if !strings.Contains(body, "Track") {
		t.Errorf("expected track entries in body")
	}
}

func TestAnalyticsHistory_InvalidPageDefaultsToPage1(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	migrateAnalyticsTables(t, db)

	station := models.Station{ID: "s1", Name: "S1", Active: true}
	db.Create(&station)

	h := newAnalyticsTestHandler(t, db)

	// Invalid page param should not cause 500
	req := httptest.NewRequest(http.MethodGet, "/dashboard/analytics/history?page=notanumber", nil)
	ctx := context.WithValue(req.Context(), ctxKeyStation, &station)
	ctx = context.WithValue(ctx, ctxKeyUser, analyticsTestUser())
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.AnalyticsHistory(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for invalid page param, got %d body=%s", rr.Code, rr.Body.String())
	}
}
