/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestScheduleVersions_ListWithPagination(t *testing.T) {
	a, db := newScheduleVersionsTest(t)

	// Seed 3 versions
	for i := 1; i <= 3; i++ {
		v := models.ScheduleVersion{
			ID:            "ver-pg-" + string(rune('0'+i)),
			StationID:     "s1",
			VersionNumber: i,
			SnapshotData: map[string]any{
				"entries":     []any{},
				"range_start": time.Now().UTC().Format(time.RFC3339),
				"range_end":   time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339),
			},
			ChangeType: "create",
		}
		db.Create(&v) //nolint:errcheck
	}

	// List with limit=2&offset=1
	req := httptest.NewRequest("GET", "/?station_id=s1&limit=2&offset=1", nil)
	rr := httptest.NewRecorder()
	a.handleVersionsList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list with pagination: got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	versions, _ := resp["versions"].([]any)
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions with limit=2 offset=1, got %d", len(versions))
	}
	total, _ := resp["total"].(float64)
	if total != 3 {
		t.Fatalf("expected total=3, got %v", total)
	}
}

func TestScheduleVersions_GetMissingID(t *testing.T) {
	a, _ := newScheduleVersionsTest(t)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleVersionsGet(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("get missing id: got %d, want 400", rr.Code)
	}
}

func TestScheduleVersions_Restore_NotFound(t *testing.T) {
	a, _ := newScheduleVersionsTest(t)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{}")))
	req = withChiParam(req, "versionID", "nonexistent")
	rr := httptest.NewRecorder()
	a.handleVersionsRestore(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("restore not found: got %d, want 404", rr.Code)
	}
}

func TestScheduleVersions_Restore_MissingID(t *testing.T) {
	a, _ := newScheduleVersionsTest(t)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{}")))
	rr := httptest.NewRecorder()
	a.handleVersionsRestore(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("restore missing id: got %d, want 400", rr.Code)
	}
}

func TestScheduleVersions_Restore_WithEntries(t *testing.T) {
	a, db := newScheduleVersionsTest(t)

	now := time.Now().UTC()
	rangeEnd := now.Add(7 * 24 * time.Hour)

	// Seed an entry that will be cleared
	entry := models.ScheduleEntry{
		ID:         "entry-to-clear",
		StationID:  "s1",
		StartsAt:   now.Add(time.Hour),
		EndsAt:     now.Add(2 * time.Hour),
		SourceType: "smart_block",
		SourceID:   "sb-old",
	}
	db.Create(&entry) //nolint:errcheck

	// Seed a version with snapshot entries and range
	v := models.ScheduleVersion{
		ID:            "ver-restore",
		StationID:     "s1",
		VersionNumber: 1,
		SnapshotData: map[string]any{
			"entries": []map[string]any{
				{
					"id":          "snap-e1",
					"starts_at":   now.Add(time.Hour).Format(time.RFC3339),
					"ends_at":     now.Add(2 * time.Hour).Format(time.RFC3339),
					"source_type": "smart_block",
					"source_id":   "sb-restored",
				},
			},
			"range_start": now.Format(time.RFC3339),
			"range_end":   rangeEnd.Format(time.RFC3339),
		},
		ChangeType: "create",
	}
	db.Create(&v) //nolint:errcheck

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{}")))
	req = withChiParam(req, "versionID", v.ID)
	rr := httptest.NewRecorder()
	a.handleVersionsRestore(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("restore with entries: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["status"] != "restored" {
		t.Fatalf("expected status=restored, got %v", resp["status"])
	}
	restored, _ := resp["restored"].(float64)
	if restored != 1 {
		t.Fatalf("expected restored=1, got %v", restored)
	}
}

func TestScheduleVersions_Diff_MissingID(t *testing.T) {
	a, _ := newScheduleVersionsTest(t)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleVersionsDiff(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("diff missing id: got %d, want 400", rr.Code)
	}
}

func TestScheduleVersions_Diff_NotFound(t *testing.T) {
	a, _ := newScheduleVersionsTest(t)

	req := httptest.NewRequest("GET", "/nonexistent/diff", nil)
	req = withChiParam(req, "versionID", "nonexistent")
	rr := httptest.NewRecorder()
	a.handleVersionsDiff(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("diff not found: got %d, want 404", rr.Code)
	}
}
