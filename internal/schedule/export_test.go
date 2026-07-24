/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package schedule

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

func TestEscapeUnescapeICalText_RoundTrip(t *testing.T) {
	cases := []string{
		"plain text",
		"comma, semicolon; and backslash \\",
		"line one\nline two",
		"Rock & Roll; Vol, 2\\",
	}
	for _, in := range cases {
		if got := unescapeICalText(escapeICalText(in)); got != in {
			t.Fatalf("round-trip failed for %q: got %q", in, got)
		}
	}
}

func TestEscapeICalText_KnownEscapes(t *testing.T) {
	got := escapeICalText("a;b,c\nd\\e")
	want := `a\;b\,c\nd\\e`
	if got != want {
		t.Fatalf("escape = %q, want %q", got, want)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Rock FM":         "rock-fm",
		"KJAZZ 88.1!":     "kjazz-881",
		"  Spaces  ":      "--spaces--",
		"Ünïcode Removed": "ncode-removed",
		"already-slug-9":  "already-slug-9",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Fatalf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSortStrings(t *testing.T) {
	s := []string{"2026-01-03", "2026-01-01", "2026-01-02"}
	sortStrings(s)
	want := []string{"2026-01-01", "2026-01-02", "2026-01-03"}
	for i := range want {
		if s[i] != want[i] {
			t.Fatalf("sorted[%d] = %s, want %s", i, s[i], want[i])
		}
	}
}

func TestFormatICalTime_IsUTCZulu(t *testing.T) {
	loc := time.FixedZone("EST", -5*3600)
	tm := time.Date(2026, 1, 2, 8, 30, 0, 0, loc) // 13:30 UTC
	if got := formatICalTime(tm); got != "20260102T133000Z" {
		t.Fatalf("formatICalTime = %q, want 20260102T133000Z", got)
	}
}

func TestParseICalTime(t *testing.T) {
	cases := map[string]time.Time{
		"20260102T133000Z":                 time.Date(2026, 1, 2, 13, 30, 0, 0, time.UTC),
		"20260102T133000":                  time.Date(2026, 1, 2, 13, 30, 0, 0, time.UTC),
		"20260102":                         time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		"TZID=America/NY:20260102T133000Z": time.Date(2026, 1, 2, 13, 30, 0, 0, time.UTC), // TZID stripped
	}
	for in, want := range cases {
		if got := parseICalTime(in); !got.Equal(want) {
			t.Fatalf("parseICalTime(%q) = %v, want %v", in, got, want)
		}
	}
	if got := parseICalTime("garbage"); !got.IsZero() {
		t.Fatalf("parseICalTime(garbage) = %v, want zero", got)
	}
}

func TestParseICalEvents(t *testing.T) {
	content := "BEGIN:VCALENDAR\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:abc@grimnir\r\n" +
		"SUMMARY:Morning Show\\, Live\r\n" +
		"DESCRIPTION:News\\; weather\r\n" +
		"DTSTART:20260102T090000Z\r\n" +
		"DTEND:20260102T100000Z\r\n" +
		"END:VEVENT\r\n" +
		"BEGIN:VEVENT\r\n" +
		"SUMMARY:Night Show\r\n" +
		"DTSTART:20260102T220000Z\r\n" +
		"DTEND:20260102T230000Z\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n"

	events := parseICalEvents(content)
	if len(events) != 2 {
		t.Fatalf("parsed %d events, want 2", len(events))
	}
	if events[0].UID != "abc@grimnir" {
		t.Fatalf("UID = %q", events[0].UID)
	}
	if events[0].Summary != "Morning Show, Live" {
		t.Fatalf("SUMMARY unescape failed: %q", events[0].Summary)
	}
	if events[0].Description != "News; weather" {
		t.Fatalf("DESCRIPTION unescape failed: %q", events[0].Description)
	}
	if !events[0].Start.Equal(time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("start = %v", events[0].Start)
	}
}

// ---------------------------------------------------------------------------
// DB-backed export / import
// ---------------------------------------------------------------------------

func newScheduleTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}, &models.User{}, &models.Show{}, &models.ShowInstance{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedShowInstance(t *testing.T, db *gorm.DB, stationID string, start time.Time) {
	t.Helper()
	show := models.Show{ID: "show1", StationID: stationID, Name: "Morning Show", Description: "News", Color: "#FF0000", DefaultDurationMinutes: 60, DTStart: start, Active: true}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	inst := models.ShowInstance{ID: "inst1", ShowID: show.ID, StationID: stationID, StartsAt: start, EndsAt: start.Add(time.Hour), Status: models.ShowInstanceScheduled}
	if err := db.Create(&inst).Error; err != nil {
		t.Fatalf("seed instance: %v", err)
	}
}

func TestExportToICal(t *testing.T) {
	db := newScheduleTestDB(t)
	svc := NewExportService(db, zerolog.Nop())
	db.Create(&models.Station{ID: "st1", Name: "Rock FM", Active: true})
	start := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	seedShowInstance(t, db, "st1", start)

	res, err := svc.ExportToICal(context.Background(), "st1", start.Add(-time.Hour), start.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	body := string(res.Data)
	for _, want := range []string{"BEGIN:VCALENDAR", "BEGIN:VEVENT", "SUMMARY:Morning Show", "UID:inst1@grimnir", "X-APPLE-CALENDAR-COLOR:#FF0000", "END:VCALENDAR"} {
		if !strings.Contains(body, want) {
			t.Fatalf("export missing %q in:\n%s", want, body)
		}
	}
	if res.ContentType != "text/calendar; charset=utf-8" {
		t.Fatalf("content type = %q", res.ContentType)
	}
	if !strings.HasPrefix(res.Filename, "rock-fm-schedule-2026-01-02") {
		t.Fatalf("filename = %q", res.Filename)
	}
}

func TestExportToICal_StationNotFound(t *testing.T) {
	svc := NewExportService(newScheduleTestDB(t), zerolog.Nop())
	if _, err := svc.ExportToICal(context.Background(), "missing", time.Now(), time.Now().Add(time.Hour)); err == nil {
		t.Fatal("expected error for missing station")
	}
}

func TestImportFromICal(t *testing.T) {
	db := newScheduleTestDB(t)
	svc := NewExportService(db, zerolog.Nop())
	db.Create(&models.Station{ID: "st1", Name: "Rock FM", Active: true})

	ical := "BEGIN:VEVENT\r\n" +
		"SUMMARY:Imported Show\r\n" +
		"DTSTART:20260201T090000Z\r\n" +
		"DTEND:20260201T100000Z\r\n" +
		"END:VEVENT\r\n" +
		// This one is missing DTEND and must be skipped.
		"BEGIN:VEVENT\r\n" +
		"SUMMARY:Incomplete\r\n" +
		"DTSTART:20260201T110000Z\r\n" +
		"END:VEVENT\r\n"

	res, err := svc.ImportFromICal(context.Background(), "st1", strings.NewReader(ical))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 1 {
		t.Fatalf("imported = %d, want 1", res.Imported)
	}
	if res.Skipped != 1 {
		t.Fatalf("skipped = %d, want 1", res.Skipped)
	}

	var shows int64
	db.Model(&models.Show{}).Where("station_id = ? AND name = ?", "st1", "Imported Show").Count(&shows)
	if shows != 1 {
		t.Fatalf("expected the imported show to be created, got %d", shows)
	}
}

func TestImportFromICal_ConflictSkipped(t *testing.T) {
	db := newScheduleTestDB(t)
	svc := NewExportService(db, zerolog.Nop())
	db.Create(&models.Station{ID: "st1", Name: "Rock FM", Active: true})
	start := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	seedShowInstance(t, db, "st1", start) // occupies 09:00-10:00

	// Overlapping event must be reported as a conflict, not imported.
	ical := "BEGIN:VEVENT\r\nSUMMARY:Morning Show\r\nDTSTART:20260201T093000Z\r\nDTEND:20260201T103000Z\r\nEND:VEVENT\r\n"
	res, err := svc.ImportFromICal(context.Background(), "st1", strings.NewReader(ical))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 0 || res.Skipped != 1 {
		t.Fatalf("imported=%d skipped=%d, want 0/1", res.Imported, res.Skipped)
	}
	if len(res.Errors) == 0 {
		t.Fatal("expected a conflict error to be recorded")
	}
}

func TestExportToPDF(t *testing.T) {
	db := newScheduleTestDB(t)
	svc := NewExportService(db, zerolog.Nop())
	db.Create(&models.Station{ID: "st1", Name: "Rock FM", Active: true})
	start := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	seedShowInstance(t, db, "st1", start)

	out, err := svc.ExportToPDF(context.Background(), "st1", start.Add(-time.Hour), start.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("pdf: %v", err)
	}
	html := string(out)
	if !strings.Contains(html, "Rock FM Schedule") || !strings.Contains(html, "Morning Show") {
		t.Fatalf("PDF HTML missing expected content:\n%s", html)
	}
}
