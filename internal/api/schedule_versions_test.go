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
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newScheduleVersionsTest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.ScheduleVersion{},
		&models.ScheduleEntry{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{db: db, logger: zerolog.Nop()}, db
}

func TestScheduleVersionsAPI_ListAndGet(t *testing.T) {
	a, db := newScheduleVersionsTest(t)

	// List versions requires station_id
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleVersionsList(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("list without station_id: got %d, want 400", rr.Code)
	}

	// List with station_id (empty)
	req = httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr = httptest.NewRecorder()
	a.handleVersionsList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list versions: got %d, want 200", rr.Code)
	}
	var listResp map[string]any
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	versions, _ := listResp["versions"].([]any)
	if len(versions) != 0 {
		t.Fatalf("expected 0 versions, got %d", len(versions))
	}

	// Seed a version directly
	v := models.ScheduleVersion{
		ID:            "ver-1",
		StationID:     "s1",
		VersionNumber: 1,
		SnapshotData: map[string]any{
			"entries":     []any{},
			"range_start": time.Now().UTC().Format(time.RFC3339),
			"range_end":   time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339),
		},
		ChangeSummary: "Initial version",
		ChangeType:    "create",
	}
	db.Create(&v) //nolint:errcheck

	// List with version present
	req = httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr = httptest.NewRecorder()
	a.handleVersionsList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list versions after seed: got %d, want 200", rr.Code)
	}
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	versions, _ = listResp["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(versions))
	}
	total, _ := listResp["total"].(float64)
	if total != 1 {
		t.Fatalf("expected total=1, got %v", total)
	}

	// Get version by ID
	req = httptest.NewRequest("GET", "/ver-1", nil)
	req = withChiParam(req, "versionID", "ver-1")
	rr = httptest.NewRecorder()
	a.handleVersionsGet(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get version: got %d, want 200", rr.Code)
	}

	// Get non-existent version
	req = httptest.NewRequest("GET", "/missing", nil)
	req = withChiParam(req, "versionID", "nonexistent")
	rr = httptest.NewRecorder()
	a.handleVersionsGet(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get missing version: got %d, want 404", rr.Code)
	}
}

func TestScheduleVersionsAPI_Diff(t *testing.T) {
	a, db := newScheduleVersionsTest(t)

	now := time.Now().UTC()

	// Seed two versions
	v1 := models.ScheduleVersion{
		ID:            "ver-diff-1",
		StationID:     "s1",
		VersionNumber: 1,
		SnapshotData: map[string]any{
			"entries": []map[string]any{
				{
					"id":          "entry-a",
					"starts_at":   now.Format(time.RFC3339),
					"ends_at":     now.Add(time.Hour).Format(time.RFC3339),
					"source_type": "smart_block",
					"source_id":   "sb-1",
				},
			},
		},
		ChangeType: "create",
	}
	v2 := models.ScheduleVersion{
		ID:            "ver-diff-2",
		StationID:     "s1",
		VersionNumber: 2,
		SnapshotData: map[string]any{
			"entries": []map[string]any{
				{
					"id":          "entry-b",
					"starts_at":   now.Add(2 * time.Hour).Format(time.RFC3339),
					"ends_at":     now.Add(3 * time.Hour).Format(time.RFC3339),
					"source_type": "smart_block",
					"source_id":   "sb-2",
				},
			},
		},
		ChangeType: "update",
	}
	db.Create(&v1) //nolint:errcheck
	db.Create(&v2) //nolint:errcheck

	// Get diff for v2 (auto-compare to v1 as previous)
	req := httptest.NewRequest("GET", "/ver-diff-2/diff", nil)
	req = withChiParam(req, "versionID", "ver-diff-2")
	rr := httptest.NewRecorder()
	a.handleVersionsDiff(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("diff: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var diff models.VersionDiff
	json.NewDecoder(rr.Body).Decode(&diff) //nolint:errcheck
	if diff.FromVersion != 1 {
		t.Fatalf("expected from_version=1, got %d", diff.FromVersion)
	}
	if diff.ToVersion != 2 {
		t.Fatalf("expected to_version=2, got %d", diff.ToVersion)
	}

	// Diff for v1 (no previous version — everything is "added")
	req = httptest.NewRequest("GET", "/ver-diff-1/diff", nil)
	req = withChiParam(req, "versionID", "ver-diff-1")
	rr = httptest.NewRecorder()
	a.handleVersionsDiff(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("diff no previous: got %d, want 200", rr.Code)
	}
	json.NewDecoder(rr.Body).Decode(&diff) //nolint:errcheck
	if diff.FromVersion != 0 {
		t.Fatalf("expected from_version=0, got %d", diff.FromVersion)
	}
	if len(diff.Added) != 1 {
		t.Fatalf("expected 1 added entry, got %d", len(diff.Added))
	}
}

func TestScheduleVersionsAPI_CreateVersion(t *testing.T) {
	a, _ := newScheduleVersionsTest(t)

	// createScheduleVersion is called internally; test it directly
	err := a.createScheduleVersion(t.Context(), "s1", "test", "Test version")
	if err != nil {
		t.Fatalf("createScheduleVersion: %v", err)
	}

	// Verify version was persisted
	var count int64
	a.db.Model(&models.ScheduleVersion{}).Where("station_id = ?", "s1").Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 version record, got %d", count)
	}

	// Second call should bump version number
	err = a.createScheduleVersion(t.Context(), "s1", "test", "Second version")
	if err != nil {
		t.Fatalf("createScheduleVersion second: %v", err)
	}

	var latest models.ScheduleVersion
	a.db.Where("station_id = ?", "s1").Order("version_number DESC").First(&latest)
	if latest.VersionNumber != 2 {
		t.Fatalf("expected version_number=2, got %d", latest.VersionNumber)
	}
}

func TestScheduleVersionsAPI_Restore(t *testing.T) {
	a, _ := newScheduleVersionsTest(t)

	// Restore non-existent version → 404
	req := httptest.NewRequest("POST", "/versions/nonexistent/restore", nil)
	req = withChiParam(req, "versionID", "nonexistent-id")
	rr := httptest.NewRecorder()
	a.handleVersionsRestore(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("restore nonexistent: got %d, want 404", rr.Code)
	}

	// Create a version with valid snapshot data
	version := models.ScheduleVersion{
		ID:            "v-restore",
		StationID:     "s1",
		VersionNumber: 1,
		ChangeSummary: "restore-test",
		SnapshotData: map[string]any{
			"entries": []map[string]any{},
		},
	}
	if err := a.db.Create(&version).Error; err != nil {
		t.Fatalf("create version: %v", err)
	}

	req = httptest.NewRequest("POST", "/versions/v-restore/restore", nil)
	req = withChiParam(req, "versionID", "v-restore")
	rr = httptest.NewRecorder()
	a.handleVersionsRestore(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("restore: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["status"] != "restored" {
		t.Fatalf("expected status=restored, got %v", resp["status"])
	}
}

func TestScheduleVersions_HasChanges(t *testing.T) {
	now := time.Now()
	a := models.VersionSnapshotEntry{StartsAt: now, EndsAt: now.Add(time.Hour), SourceType: "smart_block", SourceID: "s1", MountID: "m1"}
	b := a

	if hasChanges(a, b) {
		t.Fatal("identical entries should have no changes")
	}

	b.SourceID = "s2"
	if !hasChanges(a, b) {
		t.Fatal("different source_id should report changes")
	}
}

func TestScheduleVersions_GetChanges(t *testing.T) {
	now := time.Now()
	a := models.VersionSnapshotEntry{StartsAt: now, SourceType: "smart_block", SourceID: "s1"}
	b := models.VersionSnapshotEntry{StartsAt: now, SourceType: "playlist", SourceID: "s2"}

	changes := getChanges(a, b)
	if _, ok := changes["source_type"]; !ok {
		t.Fatal("expected source_type in changes")
	}
	if _, ok := changes["source_id"]; !ok {
		t.Fatal("expected source_id in changes")
	}
	// starts_at same, so not in changes
	if _, ok := changes["starts_at"]; ok {
		t.Fatal("starts_at should not be in changes")
	}
}
