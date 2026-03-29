/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ── detectContentType ─────────────────────────────────────────────────────────

func TestDetectContentType(t *testing.T) {
	cases := []struct {
		filename string
		want     string
	}{
		{"track.mp3", "audio/mpeg"},
		{"track.MP3", "application/octet-stream"}, // extension check is case-sensitive
		{"track.flac", "audio/flac"},
		{"track.ogg", "audio/ogg"},
		{"track.oga", "audio/ogg"},
		{"track.m4a", "audio/mp4"},
		{"track.wav", "audio/wav"},
		{"track.aac", "audio/aac"},
		{"track.opus", "audio/opus"},
		{"track.wma", "application/octet-stream"},
		{"track.audio", "application/octet-stream"},
		{"document.pdf", "application/octet-stream"},
		{"noextension", "application/octet-stream"},
	}

	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			got := detectContentType(tc.filename)
			if got != tc.want {
				t.Errorf("detectContentType(%q) = %q, want %q", tc.filename, got, tc.want)
			}
		})
	}
}

// ── Service URL / CheckStorageAccess ─────────────────────────────────────────

func TestService_URL(t *testing.T) {
	root := t.TempDir()
	logger := zerolog.Nop()
	storage := NewFilesystemStorage(root, logger)
	svc := &Service{storage: storage, mediaRoot: root, logger: logger}

	path := "station1/ab/cd/track.mp3"
	got := svc.URL(path)
	if got != path {
		t.Errorf("Service.URL() = %q, want %q", got, path)
	}
}

func TestService_CheckStorageAccess_ValidDir(t *testing.T) {
	root := t.TempDir()
	logger := zerolog.Nop()
	storage := NewFilesystemStorage(root, logger)
	svc := &Service{storage: storage, mediaRoot: root, logger: logger}

	if err := svc.CheckStorageAccess(); err != nil {
		t.Errorf("CheckStorageAccess() unexpected error: %v", err)
	}
}

func TestService_CheckStorageAccess_InvalidDir(t *testing.T) {
	logger := zerolog.Nop()
	storage := NewFilesystemStorage("/nonexistent/path/xyz", logger)
	svc := &Service{storage: storage, mediaRoot: "/nonexistent/path/xyz", logger: logger}

	if err := svc.CheckStorageAccess(); err == nil {
		t.Error("CheckStorageAccess() should fail for non-existent directory")
	}
}

// ── Service.InitOrphanScanner / GetOrphanScanner ──────────────────────────────

func TestService_InitAndGetOrphanScanner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.OrphanMedia{}, &models.MediaItem{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	root := t.TempDir()
	logger := zerolog.Nop()
	storage := NewFilesystemStorage(root, logger)
	svc := &Service{storage: storage, mediaRoot: root, logger: logger}

	if svc.GetOrphanScanner() != nil {
		t.Error("GetOrphanScanner() should return nil before Init")
	}

	svc.InitOrphanScanner(db)

	if svc.GetOrphanScanner() == nil {
		t.Error("GetOrphanScanner() should return non-nil after Init")
	}
}

// ── Service orphan delegation (nil scanner returns error) ─────────────────────

func TestService_OrphanMethods_NilScannerError(t *testing.T) {
	root := t.TempDir()
	logger := zerolog.Nop()
	storage := NewFilesystemStorage(root, logger)
	svc := &Service{storage: storage, mediaRoot: root, logger: logger}
	ctx := context.Background()

	if _, err := svc.ScanForOrphans(ctx); err == nil {
		t.Error("ScanForOrphans() should error with nil scanner")
	}
	if _, _, err := svc.GetOrphans(ctx, 1, 25); err == nil {
		t.Error("GetOrphans() should error with nil scanner")
	}
	if _, err := svc.GetOrphanByHash(ctx, "hash"); err == nil {
		t.Error("GetOrphanByHash() should error with nil scanner")
	}
	if _, err := svc.GetOrphanByID(ctx, "id"); err == nil {
		t.Error("GetOrphanByID() should error with nil scanner")
	}
	if _, err := svc.AdoptOrphan(ctx, "id", "station"); err == nil {
		t.Error("AdoptOrphan() should error with nil scanner")
	}
	if err := svc.DeleteOrphan(ctx, "id", false); err == nil {
		t.Error("DeleteOrphan() should error with nil scanner")
	}
	if _, err := svc.BulkAdoptOrphans(ctx, []string{}, "station"); err == nil {
		t.Error("BulkAdoptOrphans() should error with nil scanner")
	}
	if _, err := svc.BulkDeleteOrphans(ctx, []string{}, false); err == nil {
		t.Error("BulkDeleteOrphans() should error with nil scanner")
	}
	if _, err := svc.GetAllOrphanIDs(ctx); err == nil {
		t.Error("GetAllOrphanIDs() should error with nil scanner")
	}
	if _, _, err := svc.GetOrphanStats(ctx); err == nil {
		t.Error("GetOrphanStats() should error with nil scanner")
	}
}

// ── Service orphan delegation (live scanner) ──────────────────────────────────

func newServiceWithScanner(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.OrphanMedia{}, &models.MediaItem{}, &models.Station{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	root := t.TempDir()
	logger := zerolog.Nop()
	storage := NewFilesystemStorage(root, logger)
	svc := &Service{storage: storage, mediaRoot: root, logger: logger}
	svc.InitOrphanScanner(db)
	return svc, db
}

func TestService_ScanForOrphans_WithScanner(t *testing.T) {
	svc, _ := newServiceWithScanner(t)
	ctx := context.Background()

	result, err := svc.ScanForOrphans(ctx)
	if err != nil {
		t.Fatalf("ScanForOrphans() error: %v", err)
	}
	if result == nil {
		t.Fatal("ScanForOrphans() returned nil result")
	}
}

func TestService_GetOrphans_WithScanner(t *testing.T) {
	svc, _ := newServiceWithScanner(t)
	ctx := context.Background()

	_, total, err := svc.GetOrphans(ctx, 1, 25)
	if err != nil {
		t.Fatalf("GetOrphans() error: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
}

func TestService_GetOrphanByHash_WithScanner(t *testing.T) {
	svc, db := newServiceWithScanner(t)
	ctx := context.Background()

	// Insert an orphan directly.
	o := &models.OrphanMedia{
		ID:          uuid.New().String(),
		FilePath:    "svc/test.mp3",
		ContentHash: "svchash1",
		FileSize:    1,
		DetectedAt:  nowTime(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := svc.GetOrphanByHash(ctx, "svchash1")
	if err != nil {
		t.Fatalf("GetOrphanByHash() error: %v", err)
	}
	if got == nil {
		t.Fatal("GetOrphanByHash() returned nil")
	}
}

func TestService_GetOrphanByID_WithScanner(t *testing.T) {
	svc, db := newServiceWithScanner(t)
	ctx := context.Background()

	id := uuid.New().String()
	o := &models.OrphanMedia{
		ID:          id,
		FilePath:    "svc/byid.mp3",
		ContentHash: "svcbyid",
		FileSize:    1,
		DetectedAt:  nowTime(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := svc.GetOrphanByID(ctx, id)
	if err != nil {
		t.Fatalf("GetOrphanByID() error: %v", err)
	}
	if got == nil {
		t.Fatal("GetOrphanByID() returned nil")
	}
}

func TestService_AdoptOrphan_WithScanner(t *testing.T) {
	svc, db := newServiceWithScanner(t)
	ctx := context.Background()

	id := uuid.New().String()
	stationID := uuid.New().String()
	o := &models.OrphanMedia{
		ID:          id,
		FilePath:    "svc/adopt.mp3",
		ContentHash: "svcadopt",
		Title:       "SVC Track",
		FileSize:    1,
		DetectedAt:  nowTime(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	item, err := svc.AdoptOrphan(ctx, id, stationID)
	if err != nil {
		t.Fatalf("AdoptOrphan() error: %v", err)
	}
	if item.StationID != stationID {
		t.Errorf("StationID = %q, want %q", item.StationID, stationID)
	}
}

func TestService_DeleteOrphan_WithScanner(t *testing.T) {
	svc, db := newServiceWithScanner(t)
	ctx := context.Background()

	id := uuid.New().String()
	o := &models.OrphanMedia{
		ID:          id,
		FilePath:    "svc/del.mp3",
		ContentHash: "svcdel",
		FileSize:    1,
		DetectedAt:  nowTime(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.DeleteOrphan(ctx, id, false); err != nil {
		t.Fatalf("DeleteOrphan() error: %v", err)
	}
}

func TestService_BulkAdoptOrphans_WithScanner(t *testing.T) {
	svc, db := newServiceWithScanner(t)
	ctx := context.Background()
	stationID := uuid.New().String()

	ids := make([]string, 2)
	for i := range ids {
		id := uuid.New().String()
		ids[i] = id
		o := &models.OrphanMedia{
			ID:          id,
			FilePath:    fmt.Sprintf("svc/bulk-adopt-%d.mp3", i),
			ContentHash: id,
			FileSize:    1,
			DetectedAt:  nowTime(),
		}
		if err := db.Create(o).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	adopted, err := svc.BulkAdoptOrphans(ctx, ids, stationID)
	if err != nil {
		t.Fatalf("BulkAdoptOrphans() error: %v", err)
	}
	if adopted != 2 {
		t.Errorf("adopted = %d, want 2", adopted)
	}
}

func TestService_BulkDeleteOrphans_WithScanner(t *testing.T) {
	svc, db := newServiceWithScanner(t)
	ctx := context.Background()

	ids := make([]string, 2)
	for i := range ids {
		id := uuid.New().String()
		ids[i] = id
		o := &models.OrphanMedia{
			ID:          id,
			FilePath:    fmt.Sprintf("svc/bulk-del-%d.mp3", i),
			ContentHash: id,
			FileSize:    1,
			DetectedAt:  nowTime(),
		}
		if err := db.Create(o).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	deleted, err := svc.BulkDeleteOrphans(ctx, ids, false)
	if err != nil {
		t.Fatalf("BulkDeleteOrphans() error: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
}

func TestService_GetAllOrphanIDs_WithScanner(t *testing.T) {
	svc, db := newServiceWithScanner(t)
	ctx := context.Background()

	id := uuid.New().String()
	o := &models.OrphanMedia{
		ID:          id,
		FilePath:    "svc/ids.mp3",
		ContentHash: "svcids",
		FileSize:    1,
		DetectedAt:  nowTime(),
	}
	if err := db.Create(o).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	ids, err := svc.GetAllOrphanIDs(ctx)
	if err != nil {
		t.Fatalf("GetAllOrphanIDs() error: %v", err)
	}
	if len(ids) != 1 {
		t.Errorf("len(ids) = %d, want 1", len(ids))
	}
}

func TestService_GetOrphanStats_WithScanner(t *testing.T) {
	svc, _ := newServiceWithScanner(t)
	ctx := context.Background()

	count, size, err := svc.GetOrphanStats(ctx)
	if err != nil {
		t.Fatalf("GetOrphanStats() error: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if size != 0 {
		t.Errorf("size = %d, want 0", size)
	}
}

// ── Service.Store error path ──────────────────────────────────────────────────

// errorStorage is a Storage implementation that always fails.
type errorStorage struct{}

func (e *errorStorage) Store(_ context.Context, _, _ string, _ io.Reader) (string, error) {
	return "", fmt.Errorf("forced store error")
}
func (e *errorStorage) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("forced delete error")
}
func (e *errorStorage) URL(path string) string              { return path }
func (e *errorStorage) CheckAccess(_ context.Context) error { return fmt.Errorf("no access") }

func TestService_Store_PropagatesError(t *testing.T) {
	logger := zerolog.Nop()
	svc := &Service{storage: &errorStorage{}, mediaRoot: "/tmp", logger: logger}
	ctx := context.Background()

	_, err := svc.Store(ctx, "st", "mid", bytes.NewReader([]byte("data")))
	if err == nil {
		t.Error("Store() should return error when storage fails")
	}
}

func TestService_Delete_PropagatesError(t *testing.T) {
	logger := zerolog.Nop()
	svc := &Service{storage: &errorStorage{}, mediaRoot: "/tmp", logger: logger}
	ctx := context.Background()

	err := svc.Delete(ctx, "some/path.mp3")
	if err == nil {
		t.Error("Delete() should return error when storage fails")
	}
}

// ── S3Storage.URL (no network) ────────────────────────────────────────────────

func TestS3Storage_URL_AllBranches(t *testing.T) {
	cases := []struct {
		name          string
		endpoint      string
		publicBaseURL string
		usePathStyle  bool
		bucket        string
		region        string
		path          string
		wantContains  string
	}{
		{
			name:         "aws virtual-hosted",
			bucket:       "b1",
			region:       "eu-west-1",
			path:         "p/q.mp3",
			wantContains: "b1.s3.eu-west-1.amazonaws.com",
		},
		{
			name:         "aws path-style",
			bucket:       "b2",
			region:       "us-east-1",
			usePathStyle: true,
			path:         "p/q.mp3",
			wantContains: "s3.us-east-1.amazonaws.com/b2",
		},
		{
			name:         "custom endpoint path-style",
			endpoint:     "https://minio.local",
			bucket:       "b3",
			usePathStyle: true,
			path:         "p/q.mp3",
			wantContains: "minio.local/b3",
		},
		{
			name:         "custom endpoint virtual-hosted",
			endpoint:     "https://spaces.example.com",
			bucket:       "b4",
			usePathStyle: false,
			path:         "p/q.mp3",
			wantContains: "spaces.example.com/p/q.mp3",
		},
		{
			name:          "public base url",
			publicBaseURL: "https://cdn.example.com",
			bucket:        "b5",
			path:          "p/q.mp3",
			wantContains:  "cdn.example.com/p/q.mp3",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s3 := &S3Storage{
				bucket:        tc.bucket,
				region:        tc.region,
				endpoint:      tc.endpoint,
				publicBaseURL: tc.publicBaseURL,
				usePathStyle:  tc.usePathStyle,
				logger:        zerolog.Nop(),
			}
			url := s3.URL(tc.path)
			if tc.wantContains != "" && !containsStr(url, tc.wantContains) {
				t.Errorf("URL() = %q, want to contain %q", url, tc.wantContains)
			}
		})
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexStr(s, sub) >= 0)
}

func indexStr(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// nowTime returns the current time for use in test records.
func nowTime() time.Time {
	return time.Now()
}
