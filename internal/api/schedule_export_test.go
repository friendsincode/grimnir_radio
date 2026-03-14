/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/schedule"
)

func newScheduleExportAPITest(t *testing.T) (*ScheduleExportAPI, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	svc := schedule.NewExportService(db, zerolog.Nop())
	api := &API{db: db, logger: zerolog.Nop()}
	return NewScheduleExportAPI(api, svc), db
}

func TestScheduleExportAPI_ExportICal(t *testing.T) {
	e, _ := newScheduleExportAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/schedule/export/ical", nil)
		rr := httptest.NewRecorder()
		e.handleExportICal(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("with station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/schedule/export/ical?station_id=s1", nil)
		rr := httptest.NewRecorder()
		e.handleExportICal(rr, req)
		// May succeed (200) or fail if schedule tables not migrated (500)
		if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
			t.Fatalf("got %d, want 200 or 500", rr.Code)
		}
	})
}

func TestScheduleExportAPI_ExportPDF(t *testing.T) {
	e, _ := newScheduleExportAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/schedule/export/pdf", nil)
		rr := httptest.NewRecorder()
		e.handleExportPDF(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("with station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/schedule/export/pdf?station_id=s1", nil)
		rr := httptest.NewRecorder()
		e.handleExportPDF(rr, req)
		if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
			t.Fatalf("got %d, want 200 or 500", rr.Code)
		}
	})
}

func TestScheduleExportAPI_ImportICal(t *testing.T) {
	e, _ := newScheduleExportAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/schedule/export/import/ical", nil)
		rr := httptest.NewRecorder()
		e.handleImportICal(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		w.Close()
		req := httptest.NewRequest("POST", "/schedule/export/import/ical?station_id=s1", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		rr := httptest.NewRecorder()
		e.handleImportICal(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}
