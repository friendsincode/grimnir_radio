/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package analyzer

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func analysisGateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.SystemSettings{}, &models.AnalysisJob{}, &models.MediaItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := models.GetSystemSettings(db); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	return db
}

func setAnalysisEnabled(t *testing.T, db *gorm.DB, on bool) {
	t.Helper()
	if err := db.Model(&models.SystemSettings{}).Where("id = ?", 1).
		Update("analysis_enabled", on).Error; err != nil {
		t.Fatalf("set analysis_enabled=%v: %v", on, err)
	}
}

func seedPendingJob(t *testing.T, db *gorm.DB) string {
	t.Helper()
	job := models.AnalysisJob{ID: uuid.NewString(), MediaID: "no-such-media", Status: "pending"}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("seed job: %v", err)
	}
	return job.ID
}

func jobStatus(t *testing.T, db *gorm.DB, id string) string {
	t.Helper()
	var job models.AnalysisJob
	if err := db.First(&job, "id = ?", id).Error; err != nil {
		t.Fatalf("load job: %v", err)
	}
	return job.Status
}

// runBriefly runs the analyzer drain loop for a short window then cancels it.
func runBriefly(svc *Service) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = svc.Run(ctx)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done
}

// When analysis is disabled, the drain loop must leave pending jobs untouched.
func TestAnalyzerRun_SkipsPendingJobWhenDisabled(t *testing.T) {
	db := analysisGateTestDB(t)
	setAnalysisEnabled(t, db, false)
	id := seedPendingJob(t, db)

	runBriefly(New(db, t.TempDir(), zerolog.Nop()))

	if got := jobStatus(t, db, id); got != "pending" {
		t.Errorf("job status = %q, want %q (analysis disabled must not drain the queue)", got, "pending")
	}
}

// When analysis is enabled, the drain loop must pick the pending job up (it then
// fails because the media row is absent, but crucially it leaves "pending").
func TestAnalyzerRun_ProcessesPendingJobWhenEnabled(t *testing.T) {
	db := analysisGateTestDB(t)
	setAnalysisEnabled(t, db, true)
	id := seedPendingJob(t, db)

	runBriefly(New(db, t.TempDir(), zerolog.Nop()))

	if got := jobStatus(t, db, id); got == "pending" {
		t.Error("job status still \"pending\"; enabled analyzer should have drained it")
	}
}
