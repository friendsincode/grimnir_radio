/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package analyzer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// newSvc builds an analyzer over in-memory sqlite with no media engine
// configured, which is the state every gRPC-free code path runs in.
func newSvc(t *testing.T) (*Service, *gorm.DB, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.AnalysisJob{}, &models.MediaItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	workDir := t.TempDir()
	return New(db, workDir, zerolog.Nop()), db, workDir
}

func bg() context.Context { return context.Background() }

// seedMediaFile creates both the DB row and the file on disk so performAnalysis
// gets past its file-wait loop without sleeping.
func seedMediaFile(t *testing.T, db *gorm.DB, workDir, id, name string) *models.MediaItem {
	t.Helper()
	if err := os.WriteFile(filepath.Join(workDir, name), []byte("not really audio"), 0o600); err != nil {
		t.Fatalf("write media file: %v", err)
	}
	m := &models.MediaItem{ID: id, StationID: "st1", Path: name, Title: strings.TrimSuffix(name, filepath.Ext(name))}
	if err := db.Create(m).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}
	return m
}

// ---------------------------------------------------------------------------
// constructors
// ---------------------------------------------------------------------------

func TestNew_WithoutMediaEngine(t *testing.T) {
	s, _, _ := newSvc(t)
	if s.mediaEngineClient != nil {
		t.Fatal("deprecated New must not build a media engine client")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := s.TestMediaEngine(bg()); !errors.Is(err, ErrMediaEngineNotConfigured) {
		t.Fatalf("TestMediaEngine = %v, want ErrMediaEngineNotConfigured", err)
	}
}

func TestNewWithConfig_EmptyAddressSkipsClient(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	s := NewWithConfig(db, t.TempDir(), zerolog.Nop(), Config{})
	if s.mediaEngineClient != nil {
		t.Fatal("client built despite an empty gRPC address")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// ---------------------------------------------------------------------------
// queue
// ---------------------------------------------------------------------------

func TestEnqueue_CreatesPendingJob(t *testing.T) {
	s, db, _ := newSvc(t)

	jobID, err := s.Enqueue(bg(), "media-1")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if jobID == "" {
		t.Fatal("empty job ID")
	}

	var job models.AnalysisJob
	if err := db.First(&job, "id = ?", jobID).Error; err != nil {
		t.Fatalf("load job: %v", err)
	}
	if job.Status != "pending" {
		t.Fatalf("status = %q, want pending", job.Status)
	}
	if job.MediaID != "media-1" {
		t.Fatalf("media = %q, want media-1", job.MediaID)
	}
}

func TestEnqueue_PropagatesDBError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// No AutoMigrate: the analysis_jobs table does not exist.
	s := New(db, t.TempDir(), zerolog.Nop())
	if _, err := s.Enqueue(bg(), "media-1"); err == nil {
		t.Fatal("enqueue succeeded against a missing table")
	}
}

// Jobs are claimed oldest-first, and claiming flips the row to running so a
// second worker can't pick up the same job.
func TestNextPendingJob_ClaimsOldestFirst(t *testing.T) {
	s, db, _ := newSvc(t)

	base := time.Now().Add(-time.Hour)
	db.Create(&models.AnalysisJob{ID: "newer", MediaID: "m2", Status: "pending", CreatedAt: base.Add(30 * time.Minute)})
	db.Create(&models.AnalysisJob{ID: "older", MediaID: "m1", Status: "pending", CreatedAt: base})
	db.Create(&models.AnalysisJob{ID: "done", MediaID: "m0", Status: "complete", CreatedAt: base.Add(-time.Hour)})

	job, err := s.nextPendingJob(bg())
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if job == nil {
		t.Fatal("no job claimed")
	}
	if job.ID != "older" {
		t.Fatalf("claimed %q, want the oldest pending job", job.ID)
	}
	if job.Status != "running" {
		t.Fatalf("returned status = %q, want running", job.Status)
	}

	var row models.AnalysisJob
	db.First(&row, "id = ?", "older")
	if row.Status != "running" {
		t.Fatalf("persisted status = %q, want running", row.Status)
	}

	// The next claim moves on to the newer job, then the queue drains to nil.
	job, err = s.nextPendingJob(bg())
	if err != nil {
		t.Fatalf("second next: %v", err)
	}
	if job == nil || job.ID != "newer" {
		t.Fatalf("second claim = %+v, want newer", job)
	}

	job, err = s.nextPendingJob(bg())
	if err != nil {
		t.Fatalf("third next: %v", err)
	}
	if job != nil {
		t.Fatalf("claimed %+v from an empty queue, want nil", job)
	}
}

func TestNextPendingJob_EmptyQueue(t *testing.T) {
	s, _, _ := newSvc(t)
	job, err := s.nextPendingJob(bg())
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if job != nil {
		t.Fatalf("got %+v, want nil", job)
	}
}

// ---------------------------------------------------------------------------
// failure handling
// ---------------------------------------------------------------------------

func TestFailJob_MarksJobAndMedia(t *testing.T) {
	s, db, workDir := newSvc(t)
	m := seedMediaFile(t, db, workDir, "media-1", "track.mp3")
	db.Create(&models.AnalysisJob{ID: "job-1", MediaID: m.ID, Status: "running"})

	s.failJob(bg(), "job-1", m.ID, errors.New("decoder exploded"))

	var job models.AnalysisJob
	db.First(&job, "id = ?", "job-1")
	if job.Status != "failed" {
		t.Fatalf("job status = %q, want failed", job.Status)
	}
	if job.Error != "decoder exploded" {
		t.Fatalf("job error = %q", job.Error)
	}

	var media models.MediaItem
	db.First(&media, "id = ?", m.ID)
	if media.AnalysisState != models.AnalysisFailed {
		t.Fatalf("media analysis state = %q, want failed", media.AnalysisState)
	}
}

// An empty media ID must not turn into a bare UPDATE across every media item.
func TestFailJob_EmptyMediaIDLeavesLibraryAlone(t *testing.T) {
	s, db, workDir := newSvc(t)
	seedMediaFile(t, db, workDir, "media-1", "track.mp3")
	db.Create(&models.AnalysisJob{ID: "job-1", Status: "running"})

	s.failJob(bg(), "job-1", "", errors.New("no media"))

	var media models.MediaItem
	db.First(&media, "id = ?", "media-1")
	if media.AnalysisState == models.AnalysisFailed {
		t.Fatal("unrelated media item was marked failed")
	}
}

func TestProcessJob_MissingMediaFailsJob(t *testing.T) {
	s, db, _ := newSvc(t)
	job := &models.AnalysisJob{ID: "job-1", MediaID: "ghost", Status: "running"}
	db.Create(job)

	if err := s.processJob(bg(), job); err == nil {
		t.Fatal("processJob succeeded for a missing media item")
	}

	var row models.AnalysisJob
	db.First(&row, "id = ?", "job-1")
	if row.Status != "failed" {
		t.Fatalf("job status = %q, want failed", row.Status)
	}
}

// With the file present but no media engine configured, the job fails with a
// clear reason rather than hanging in running forever.
func TestProcessJob_WithoutMediaEngine(t *testing.T) {
	s, db, workDir := newSvc(t)
	m := seedMediaFile(t, db, workDir, "media-1", "track.mp3")
	job := &models.AnalysisJob{ID: "job-1", MediaID: m.ID, Status: "running"}
	db.Create(job)

	err := s.processJob(bg(), job)
	if !errors.Is(err, ErrMediaEngineNotConfigured) {
		t.Fatalf("processJob = %v, want ErrMediaEngineNotConfigured", err)
	}

	var row models.AnalysisJob
	db.First(&row, "id = ?", "job-1")
	if row.Status != "failed" {
		t.Fatalf("job status = %q, want failed", row.Status)
	}
	if !strings.Contains(row.Error, "not configured") {
		t.Fatalf("job error = %q, want it to name the missing media engine", row.Error)
	}

	var media models.MediaItem
	db.First(&media, "id = ?", m.ID)
	if media.AnalysisState != models.AnalysisFailed {
		t.Fatalf("media state = %q, want failed", media.AnalysisState)
	}
}

// ---------------------------------------------------------------------------
// performAnalysis file wait
// ---------------------------------------------------------------------------

func TestPerformAnalysis_FilePresentButNoEngine(t *testing.T) {
	s, db, workDir := newSvc(t)
	m := seedMediaFile(t, db, workDir, "media-1", "track.mp3")

	if _, err := s.performAnalysis(bg(), m); !errors.Is(err, ErrMediaEngineNotConfigured) {
		t.Fatalf("err = %v, want ErrMediaEngineNotConfigured", err)
	}
}

// The file-wait loop retries on ENOENT; a cancelled context has to break it
// instead of burning the full 5 attempts.
func TestPerformAnalysis_MissingFileHonoursCancellation(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx, cancel := context.WithCancel(bg())
	cancel()

	start := time.Now()
	_, err := s.performAnalysis(ctx, &models.MediaItem{ID: "m1", Path: "never-written.mp3"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("waited %v before honouring cancellation", elapsed)
	}
}

// A stat error that is not "does not exist" aborts immediately: retrying would
// never fix a path whose parent is a regular file.
func TestPerformAnalysis_NonExistenceStatErrorFailsFast(t *testing.T) {
	s, _, workDir := newSvc(t)
	if err := os.WriteFile(filepath.Join(workDir, "blocker"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	start := time.Now()
	_, err := s.performAnalysis(bg(), &models.MediaItem{ID: "m1", Path: "blocker/track.mp3"})
	if err == nil {
		t.Fatal("expected a stat error")
	}
	if errors.Is(err, ErrMediaEngineNotConfigured) {
		t.Fatalf("err = %v, want the stat failure to short-circuit first", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("retried a permanent stat error for %v", elapsed)
	}
}

// ---------------------------------------------------------------------------
// status reporting
// ---------------------------------------------------------------------------

func TestGetMediaEngineStatus_NotConfigured(t *testing.T) {
	s, _, _ := newSvc(t)
	status := s.GetMediaEngineStatus(bg())

	if status.Configured {
		t.Fatal("reported as configured")
	}
	if status.Connected {
		t.Fatal("reported as connected")
	}
	if status.Error == "" {
		t.Fatal("expected an explanatory error string")
	}
}

func TestGetMediaEngineStatus_ConfiguredButClientMissing(t *testing.T) {
	s, _, _ := newSvc(t)
	s.cfg = Config{MediaEngineGRPCAddr: "127.0.0.1:65000"}

	status := s.GetMediaEngineStatus(bg())
	if !status.Configured {
		t.Fatal("should report configured once an address is set")
	}
	if status.Address != "127.0.0.1:65000" {
		t.Fatalf("address = %q", status.Address)
	}
	if status.Connected {
		t.Fatal("reported connected with no client")
	}
	if !strings.Contains(status.Error, "not initialized") {
		t.Fatalf("error = %q, want it to name the uninitialized client", status.Error)
	}
}

// ---------------------------------------------------------------------------
// run loop
// ---------------------------------------------------------------------------

func TestRun_StopsOnContextCancel(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx, cancel := context.WithCancel(bg())
	cancel()

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}
