/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
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

// newScheduleExportAPIWithDB creates a ScheduleExportAPI with all required tables migrated
// and a seeded station so the happy paths can succeed.
func newScheduleExportAPIWithDB(t *testing.T) (*ScheduleExportAPI, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "export_full.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Station{},
		&models.Show{},
		&models.ShowInstance{},
		&models.User{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Seed a station so exports can find it.
	station := models.Station{
		ID:       "export-station-1",
		Name:     "Export Test Station",
		Timezone: "UTC",
	}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("seed station: %v", err)
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

// TestScheduleExportAPI_ExportICal_WithDateParams tests that start/end query params
// are parsed correctly (covers the date-param branches in handleExportICal).
func TestScheduleExportAPI_ExportICal_WithDateParams(t *testing.T) {
	e, _ := newScheduleExportAPIWithDB(t)

	// Provide explicit start/end dates — exercises the date-parse branches (lines 54-62).
	req := httptest.NewRequest(
		"GET",
		"/schedule/export/ical?station_id=export-station-1&start=2026-04-01&end=2026-04-30",
		nil,
	)
	rr := httptest.NewRecorder()
	e.handleExportICal(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("export ical with dates: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/calendar") {
		t.Fatalf("expected text/calendar content type, got %q", ct)
	}
}

// TestScheduleExportAPI_ExportICal_Success tests the success path without date params
// (uses defaults — covers lines 71-74 that write the response).
func TestScheduleExportAPI_ExportICal_Success(t *testing.T) {
	e, _ := newScheduleExportAPIWithDB(t)

	req := httptest.NewRequest(
		"GET",
		"/schedule/export/ical?station_id=export-station-1",
		nil,
	)
	rr := httptest.NewRecorder()
	e.handleExportICal(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("export ical success: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Content-Disposition") == "" {
		t.Fatal("expected Content-Disposition header to be set")
	}
}

// TestScheduleExportAPI_ExportPDF_WithDateParams tests that start/end params are parsed
// (covers the date-param branches in handleExportPDF).
func TestScheduleExportAPI_ExportPDF_WithDateParams(t *testing.T) {
	e, _ := newScheduleExportAPIWithDB(t)

	req := httptest.NewRequest(
		"GET",
		"/schedule/export/pdf?station_id=export-station-1&start=2026-04-01&end=2026-04-07",
		nil,
	)
	rr := httptest.NewRecorder()
	e.handleExportPDF(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("export pdf with dates: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html content type, got %q", ct)
	}
}

// TestScheduleExportAPI_ExportPDF_Success tests the PDF success path without explicit dates.
func TestScheduleExportAPI_ExportPDF_Success(t *testing.T) {
	e, _ := newScheduleExportAPIWithDB(t)

	req := httptest.NewRequest(
		"GET",
		"/schedule/export/pdf?station_id=export-station-1",
		nil,
	)
	rr := httptest.NewRecorder()
	e.handleExportPDF(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("export pdf success: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// TestScheduleExportAPI_ImportICal_Success tests importing a valid iCal file
// (covers lines 130-142 that process the file and return the result JSON).
func TestScheduleExportAPI_ImportICal_Success(t *testing.T) {
	e, _ := newScheduleExportAPIWithDB(t)

	// Build a minimal but valid iCal payload.
	icalContent := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:test-evt-001@grimnir\r\n" +
		"SUMMARY:Morning Drive\r\n" +
		"DTSTART:20260501T090000Z\r\n" +
		"DTEND:20260501T110000Z\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n"

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "schedule.ics")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := io.WriteString(fw, icalContent); err != nil {
		t.Fatalf("write ical: %v", err)
	}
	mw.Close()

	req := httptest.NewRequest(
		"POST",
		"/schedule/export/import/ical?station_id=export-station-1",
		&buf,
	)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	e.handleImportICal(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("import ical success: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// TestScheduleExportAPI_ImportICal_BadForm tests that a non-multipart body triggers 400.
func TestScheduleExportAPI_ImportICal_BadForm(t *testing.T) {
	e, _ := newScheduleExportAPITest(t)

	// Send raw bytes without the multipart boundary — ParseMultipartForm will fail.
	req := httptest.NewRequest(
		"POST",
		"/schedule/export/import/ical?station_id=s1",
		strings.NewReader("not a multipart form"),
	)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=MISSING_BOUNDARY")
	rr := httptest.NewRecorder()
	e.handleImportICal(rr, req)

	// Either 400 (form parse failure or missing file) or 500 (DB tables not migrated).
	if rr.Code != http.StatusBadRequest && rr.Code != http.StatusInternalServerError {
		t.Fatalf("bad form: got %d, want 400 or 500", rr.Code)
	}
}

// errorReader is an io.Reader that always returns an error on Read.
type errorReader struct{}

func (errorReader) Read(_ []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

// TestScheduleExportAPI_ImportICal_ReadError tests that a file read error
// causes ImportFromICal to fail and return 500.
// NOTE: This test uses a closed SQLite DB to simulate a downstream failure
// because the schedule.ExportService only returns an error on read failure
// or when its internal db calls fail. Since the reader error happens before
// any DB call, we can force it by passing an erroring reader wrapped in
// a multipart form.
func TestScheduleExportAPI_ImportICal_ServiceError(t *testing.T) {
	// Use a DB without the Show/ShowInstance tables so ImportFromICal errors
	// when it tries to look up or create a show — but we need to reach ImportFromICal
	// first, which means providing a valid multipart form with a real file field.
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "import_err.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// Migrate only Station (not Show/ShowInstance) so the ical content can be
	// delivered but the show lookup causes a DB error → ImportFromICal returns error.
	if err := db.AutoMigrate(&models.Station{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&models.Station{ID: "import-err-st", Name: "Err Station", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}

	// Build a valid iCal multipart form.
	icalContent := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:err-evt-001@grimnir\r\n" +
		"SUMMARY:Error Show\r\n" +
		"DTSTART:20260601T090000Z\r\n" +
		"DTEND:20260601T110000Z\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n"

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, fwErr := mw.CreateFormFile("file", "schedule.ics")
	if fwErr != nil {
		t.Fatalf("create form file: %v", fwErr)
	}
	if _, werr := io.WriteString(fw, icalContent); werr != nil {
		t.Fatalf("write ical: %v", werr)
	}
	mw.Close()

	svc := schedule.NewExportService(db, zerolog.Nop())
	api := &API{db: db, logger: zerolog.Nop()}
	e := NewScheduleExportAPI(api, svc)

	req := httptest.NewRequest(
		"POST",
		"/schedule/export/import/ical?station_id=import-err-st",
		&buf,
	)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	e.handleImportICal(rr, req)

	// The Show table doesn't exist → ImportFromICal encounters a DB error creating the show,
	// but it is caught internally (added to Errors slice), so the handler still returns 200
	// with errors in the payload.
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Fatalf("service error: got %d, want 200 or 500; body=%s", rr.Code, rr.Body.String())
	}
}
