/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestResolveFilePath(t *testing.T) {
	// Absolute paths pass through unchanged.
	if got := ResolveFilePath("/root", "/already/abs"); got != "/already/abs" {
		t.Fatalf("abs = %q", got)
	}
	// Relative paths are cleaned and joined under the source root.
	if got := ResolveFilePath("/root", "./sub/../file.mp3"); got != "/root/file.mp3" {
		t.Fatalf("rel = %q", got)
	}
}

func TestValidateSourceDirectory(t *testing.T) {
	// Non-existent path errors.
	if err := ValidateSourceDirectory("/no/such/dir/xyz"); err == nil {
		t.Fatal("expected error for missing directory")
	}

	// A file (not a dir) errors.
	dir := t.TempDir()
	f := filepath.Join(dir, "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := ValidateSourceDirectory(f); err == nil {
		t.Fatal("expected error when path is a file")
	}

	// An empty directory errors.
	empty := t.TempDir()
	if err := ValidateSourceDirectory(empty); err == nil {
		t.Fatal("expected error for empty directory")
	}

	// A non-empty directory validates.
	if err := ValidateSourceDirectory(dir); err != nil {
		t.Fatalf("populated dir should validate: %v", err)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		512:                    "512 bytes",
		2 * 1024:               "2.00 KB",
		3 * 1024 * 1024:        "3.00 MB",
		5 * 1024 * 1024 * 1024: "5.00 GB",
	}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Fatalf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestAzuraDaysToByDay(t *testing.T) {
	if got := azuraDaysToByDay([]int{1, 3, 5}); got != "MO,WE,FR" {
		t.Fatalf("weekdays = %q", got)
	}
	if got := azuraDaysToByDay([]int{7}); got != "SU" {
		t.Fatalf("sunday = %q", got)
	}
	// Out-of-range day codes are dropped.
	if got := azuraDaysToByDay([]int{0, 8, 2}); got != "TU" {
		t.Fatalf("invalid-dropped = %q", got)
	}
	if got := azuraDaysToByDay(nil); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

func TestBuildAzuraScheduleRecurrence(t *testing.T) {
	date := "2026-03-02"
	// 09:30 = 9*3600 + 30*60 = 34200 seconds from midnight.
	sched := AzuraCastAPISchedule{StartTime: 34200, StartDate: &date, Days: []int{1, 3}}
	dtStart, rrule, pattern := buildAzuraScheduleRecurrence(sched)
	if dtStart.Hour() != 9 || dtStart.Minute() != 30 {
		t.Fatalf("dtStart = %v, want 09:30", dtStart)
	}
	if !strings.Contains(rrule, "FREQ=WEEKLY") || !strings.Contains(rrule, "BYDAY=MO,WE") ||
		!strings.Contains(rrule, "BYHOUR=9") || !strings.Contains(rrule, "BYMINUTE=30") {
		t.Fatalf("rrule = %q", rrule)
	}
	if !strings.Contains(pattern, "Weekly") {
		t.Fatalf("pattern = %q", pattern)
	}

	// LoopOnce => one-time schedule, no RRULE.
	if _, rr, p := buildAzuraScheduleRecurrence(AzuraCastAPISchedule{StartTime: 0, LoopOnce: true, Days: []int{1}}); rr != "" || !strings.Contains(p, "One-time") {
		t.Fatalf("loop-once: rr=%q p=%q", rr, p)
	}
	// No days => also treated as one-time.
	if _, rr, _ := buildAzuraScheduleRecurrence(AzuraCastAPISchedule{StartTime: 0}); rr != "" {
		t.Fatalf("no-days rrule = %q, want empty", rr)
	}
}

func TestParseDuration(t *testing.T) {
	d, err := parseDuration("01:02:03")
	if err != nil || d != time.Hour+2*time.Minute+3*time.Second {
		t.Fatalf("HH:MM:SS = %v (err %v)", d, err)
	}
	// Milliseconds suffix is stripped.
	d, err = parseDuration("00:03:30.500")
	if err != nil || d != 3*time.Minute+30*time.Second {
		t.Fatalf("with ms = %v (err %v)", d, err)
	}
	if _, err := parseDuration("garbage"); err == nil {
		t.Fatal("expected error for malformed duration")
	}
}

func TestParseInt(t *testing.T) {
	if v, err := parseInt("42"); err != nil || v != 42 {
		t.Fatalf("parseInt(42) = %d (err %v)", v, err)
	}
	if _, err := parseInt("nope"); err == nil {
		t.Fatal("expected error for non-numeric")
	}
}

func TestDayToAbbrev(t *testing.T) {
	want := map[time.Weekday]string{
		time.Sunday: "SU", time.Monday: "MO", time.Tuesday: "TU", time.Wednesday: "WE",
		time.Thursday: "TH", time.Friday: "FR", time.Saturday: "SA",
	}
	for d, w := range want {
		if got := dayToAbbrev(d); got != w {
			t.Fatalf("dayToAbbrev(%v) = %q, want %q", d, got, w)
		}
	}
}

func TestDetectRecurrence(t *testing.T) {
	a := NewStagedAnalyzer(nil, zerolog.Nop())

	// Fewer than 3 instances => no recurrence.
	if r := a.DetectRecurrence([]ShowInstance{{}, {}}); r != nil {
		t.Fatal("expected nil for <3 instances")
	}

	// Three Mondays at 09:00, each 60 minutes => weekly-on-Monday pattern.
	base := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC) // a Monday
	var insts []ShowInstance
	for i := 0; i < 3; i++ {
		s := base.AddDate(0, 0, 7*i)
		insts = append(insts, ShowInstance{StartsAt: s, EndsAt: s.Add(time.Hour), Timezone: "UTC"})
	}
	r := a.DetectRecurrence(insts)
	if r == nil {
		t.Fatal("expected a recurrence result for a weekly pattern")
	}
	if !strings.Contains(r.RRule, "FREQ=WEEKLY") || !strings.Contains(r.RRule, "MO") {
		t.Fatalf("rrule = %q", r.RRule)
	}
	if r.DurationMinutes != 60 {
		t.Fatalf("duration = %d, want 60", r.DurationMinutes)
	}
}

func TestImportedItems_TotalCount(t *testing.T) {
	items := ImportedItems{
		MediaIDs:    []string{"a", "b"},
		PlaylistIDs: []string{"p"},
		ShowIDs:     []string{"s1", "s2", "s3"},
	}
	if got := items.TotalCount(); got != 6 {
		t.Fatalf("TotalCount = %d, want 6", got)
	}
	empty := ImportedItems{}
	if empty.TotalCount() != 0 {
		t.Fatal("empty TotalCount should be 0")
	}
}

func TestOptions_ValueScanRoundTrip(t *testing.T) {
	orig := Options{AzuraCastAPIURL: "https://azura.example", AzuraCastAPIKey: "secret"}
	v, err := orig.Value()
	if err != nil {
		t.Fatalf("value: %v", err)
	}

	var back Options
	if err := back.Scan(v); err != nil {
		t.Fatalf("scan []byte: %v", err)
	}
	if back.AzuraCastAPIURL != orig.AzuraCastAPIURL || back.AzuraCastAPIKey != orig.AzuraCastAPIKey {
		t.Fatalf("round-trip mismatch: %+v", back)
	}

	// Scan also accepts a string, and nil is a no-op.
	var fromStr Options
	if b, ok := v.([]byte); ok {
		if err := fromStr.Scan(string(b)); err != nil {
			t.Fatalf("scan string: %v", err)
		}
	}
	if err := (&Options{}).Scan(nil); err != nil {
		t.Fatalf("scan nil should be a no-op, got %v", err)
	}
	// A non-string/bytes value is rejected.
	if err := (&Options{}).Scan(12345); err == nil {
		t.Fatal("expected error scanning an int")
	}
}
