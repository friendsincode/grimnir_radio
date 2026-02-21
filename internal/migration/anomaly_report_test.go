package migration

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
)

type fakeAnomalyImporter struct{}

func (f *fakeAnomalyImporter) Validate(context.Context, Options) error { return nil }
func (f *fakeAnomalyImporter) Analyze(context.Context, Options) (*Result, error) {
	return &Result{}, nil
}
func (f *fakeAnomalyImporter) Import(context.Context, Options, ProgressCallback) (*Result, error) {
	return &Result{
		Skipped: map[string]int{
			"media_duration_zero": 2,
			"media_deduplicated":  1,
			"media_not_found":     3,
		},
		Warnings: []string{
			"Duration verification: 2 imported media items have zero/missing duration",
			"No target station mapping found for source station 9",
		},
	}, nil
}

func TestBuildAnomalyReport_GroupsByClass(t *testing.T) {
	report := BuildAnomalyReport(&Result{
		Skipped: map[string]int{
			"media_duration_zero": 4,
			"media_deduplicated":  2,
			"media_not_found":     3,
		},
		Warnings: []string{
			"Duration verification warning",
			"No target station mapping found for source station 5",
		},
	})
	if report == nil {
		t.Fatalf("expected anomaly report")
	}
	if report.Total != 11 {
		t.Fatalf("expected total 11, got %d", report.Total)
	}
	if got := report.ByClass[AnomalyClassDuration].Count; got != 5 {
		t.Fatalf("expected duration count 5, got %d", got)
	}
	if got := report.ByClass[AnomalyClassDuplicateResolution].Count; got != 2 {
		t.Fatalf("expected duplicate count 2, got %d", got)
	}
	if got := report.ByClass[AnomalyClassMissingLinks].Count; got != 4 {
		t.Fatalf("expected missing links count 4, got %d", got)
	}
	if got := report.ByClass[AnomalyClassSkippedEntities].Count; got != 9 {
		t.Fatalf("expected skipped entities count 9, got %d", got)
	}
}

func TestServiceRunJob_PersistsAnomalyReport(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&Job{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	svc := NewService(db, events.NewBus(), zerolog.Nop())
	svc.RegisterImporter(SourceTypeAzuraCast, &fakeAnomalyImporter{})

	ctx := context.Background()
	job, err := svc.CreateJob(ctx, SourceTypeAzuraCast, Options{AzuraCastAPIURL: "https://example.invalid", AzuraCastAPIKey: "token"})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if err := svc.StartJob(ctx, job.ID); err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	completed := waitForJobStatus(t, svc, job.ID, JobStatusCompleted, 2*time.Second)
	if completed.AnomalyReport == nil {
		t.Fatalf("expected anomaly report to be persisted on completed job")
	}
	if completed.AnomalyReport.Total != 8 {
		t.Fatalf("expected total 8, got %d", completed.AnomalyReport.Total)
	}
}
