package migration

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestAzuraCastStagedBackup_AnalyzingToStaged(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&Job{}, &models.StagedImport{}, &models.MediaItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	backupPath := createTestAzuraBackupTarGz(t)

	svc := NewService(db, events.NewBus(), zerolog.Nop())
	svc.RegisterImporter(SourceTypeAzuraCast, &AzuraCastImporter{
		db:     db,
		logger: zerolog.Nop(),
	})

	ctx := context.Background()
	job, err := svc.CreateStagedJob(ctx, SourceTypeAzuraCast, Options{
		StagedMode:          true,
		AzuraCastBackupPath: backupPath,
	})
	if err != nil {
		t.Fatalf("CreateStagedJob: %v", err)
	}
	if err := svc.StartStagedJob(ctx, job.ID); err != nil {
		t.Fatalf("StartStagedJob: %v", err)
	}

	waitForMigrationStatus(t, svc, job.ID, JobStatusStaged, 3*time.Second)
	updated, err := svc.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if updated.StagedImportID == nil || *updated.StagedImportID == "" {
		t.Fatalf("expected staged import id")
	}

	staged, err := svc.GetStagedImport(ctx, *updated.StagedImportID)
	if err != nil {
		t.Fatalf("GetStagedImport: %v", err)
	}
	if staged.Status != models.StagedImportStatusReady {
		t.Fatalf("expected staged status ready, got %s", staged.Status)
	}
	if len(staged.StagedMedia) != 2 {
		t.Fatalf("expected 2 staged media entries, got %d", len(staged.StagedMedia))
	}
}

func waitForMigrationStatus(t *testing.T, svc *Service, jobID string, status JobStatus, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := svc.GetJob(context.Background(), jobID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if job.Status == status {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	job, err := svc.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("GetJob final: %v", err)
	}
	t.Fatalf("timed out waiting for status %s (last %s)", status, job.Status)
}

func createTestAzuraBackupTarGz(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "azura-test-backup.tar.gz")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create tar.gz: %v", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	backupJSON := map[string]any{
		"stations": []map[string]any{
			{
				"id":          7,
				"name":        "Backup Station",
				"description": "From backup",
				"short_name":  "backup",
				"is_enabled":  true,
			},
		},
		"users": []map[string]any{{"id": 1, "email": "dj@example.com", "name": "DJ"}},
	}
	jsonBytes, _ := json.Marshal(backupJSON)
	writeTarEntry(t, tw, "backup.json", jsonBytes)

	writeTarEntry(t, tw, "media/track1.mp3", []byte("fake-mp3-data-1"))
	writeTarEntry(t, tw, "media/nested/track2.ogg", []byte("fake-ogg-data-2"))
	writeTarEntry(t, tw, "media/readme.txt", []byte("ignore-me"))

	return path
}

func writeTarEntry(t *testing.T, tw *tar.Writer, name string, data []byte) {
	t.Helper()
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o644,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write header %s: %v", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("write entry %s: %v", name, err)
	}
}
