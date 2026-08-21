/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package schedule

import (
	"testing"
	"time"
)

// TestParseICalEvents_TZIDParameter guards #83: a DTSTART carrying a TZID
// parameter (which Google and Apple Calendar always emit) must still be parsed.
// The old code matched only "DTSTART:", so those events kept a zero Start and
// were dropped at import ("Imported: 0").
func TestParseICalEvents_TZIDParameter(t *testing.T) {
	content := "BEGIN:VCALENDAR\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:tz@grimnir\r\n" +
		"SUMMARY:Google Export\r\n" +
		"DTSTART;TZID=America/New_York:20260316T100000\r\n" +
		"DTEND;TZID=America/New_York:20260316T110000\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n"

	events := parseICalEvents(content)
	if len(events) != 1 {
		t.Fatalf("parsed %d events, want 1", len(events))
	}
	if events[0].Start.IsZero() {
		t.Fatal("DTSTART with a TZID parameter was not parsed; Start is zero (event would be dropped at import)")
	}
	if !events[0].Start.Equal(time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("Start = %v, want the value after the TZID colon", events[0].Start)
	}
	if events[0].End.IsZero() {
		t.Fatal("DTEND with a TZID parameter was not parsed; End is zero")
	}
}
