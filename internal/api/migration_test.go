package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/migration"
)

func TestHandleGetMigrationJob_IncludesAnomalyReport(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&migration.Job{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	job := &migration.Job{
		ID:         "job-1",
		SourceType: migration.SourceTypeAzuraCast,
		Status:     migration.JobStatusCompleted,
		Progress:   migration.Progress{Phase: "completed", StartTime: time.Now()},
		AnomalyReport: &migration.AnomalyReport{
			GeneratedAt: time.Now(),
			Total:       3,
			ByClass: map[migration.AnomalyClass]migration.AnomalyBucket{
				migration.AnomalyClassDuration:     {Count: 2, Examples: []string{"media_duration_zero (2)"}},
				migration.AnomalyClassMissingLinks: {Count: 1, Examples: []string{"media_not_found (1)"}},
			},
		},
	}
	if err := db.Create(job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}

	h := &MigrationHandler{
		service: migration.NewService(db, events.NewBus(), zerolog.Nop()),
		logger:  zerolog.Nop(),
	}

	req := httptest.NewRequest("GET", "/migrations/job-1", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", "job-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	rr := httptest.NewRecorder()

	h.handleGetMigrationJob(rr, req)
	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp GetMigrationJobResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Job == nil || resp.Job.AnomalyReport == nil {
		t.Fatalf("expected anomaly report in response")
	}
	if resp.Job.AnomalyReport.Total != 3 {
		t.Fatalf("expected anomaly total 3, got %d", resp.Job.AnomalyReport.Total)
	}
}
