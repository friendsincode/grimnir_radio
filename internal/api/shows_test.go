/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"testing"
	"time"

	"github.com/teambition/rrule-go"
)

func TestRRuleParsing(t *testing.T) {
	tests := []struct {
		name      string
		rrule     string
		dtstart   time.Time
		wantCount int // expected occurrences in 1 week (Between is inclusive)
		wantErr   bool
	}{
		{
			name:      "weekly monday",
			rrule:     "FREQ=WEEKLY;BYDAY=MO",
			dtstart:   time.Date(2026, 1, 5, 19, 0, 0, 0, time.UTC), // Monday
			wantCount: 2, // Jan 5 and Jan 12 (inclusive)
			wantErr:   false,
		},
		{
			name:      "daily",
			rrule:     "FREQ=DAILY",
			dtstart:   time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
			wantCount: 8, // Jan 1-8 (inclusive)
			wantErr:   false,
		},
		{
			name:      "weekly multiple days",
			rrule:     "FREQ=WEEKLY;BYDAY=MO,WE,FR",
			dtstart:   time.Date(2026, 1, 5, 19, 0, 0, 0, time.UTC), // Monday
			wantCount: 4, // Mon 5, Wed 7, Fri 9, Mon 12 (inclusive)
			wantErr:   false,
		},
		{
			name:      "invalid rrule",
			rrule:     "INVALID;RULE",
			dtstart:   time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr, err := rrule.StrToRRule(tt.rrule)
			if (err != nil) != tt.wantErr {
				t.Errorf("StrToRRule() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			rr.DTStart(tt.dtstart)

			// Get occurrences for 1 week
			start := tt.dtstart
			end := start.Add(7 * 24 * time.Hour)
			occurrences := rr.Between(start, end, true)

			if len(occurrences) != tt.wantCount {
				t.Errorf("Between() got %d occurrences, want %d", len(occurrences), tt.wantCount)
			}
		})
	}
}

func TestRRuleWithCount(t *testing.T) {
	// Test RRULE with COUNT limit
	rr, err := rrule.StrToRRule("FREQ=WEEKLY;BYDAY=MO;COUNT=4")
	if err != nil {
		t.Fatalf("StrToRRule() error = %v", err)
	}

	dtstart := time.Date(2026, 1, 5, 19, 0, 0, 0, time.UTC) // Monday
	rr.DTStart(dtstart)

	// Get all occurrences (should be limited to 4)
	all := rr.All()
	if len(all) != 4 {
		t.Errorf("All() got %d occurrences, want 4", len(all))
	}
}

func TestRRuleWithUntil(t *testing.T) {
	// Test RRULE with UNTIL limit
	until := time.Date(2026, 1, 26, 23, 59, 59, 0, time.UTC)
	rr, err := rrule.StrToRRule("FREQ=WEEKLY;BYDAY=MO;UNTIL=20260126T235959Z")
	if err != nil {
		t.Fatalf("StrToRRule() error = %v", err)
	}

	dtstart := time.Date(2026, 1, 5, 19, 0, 0, 0, time.UTC) // Monday
	rr.DTStart(dtstart)

	// Get occurrences from start until end of January
	start := dtstart
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	occurrences := rr.Between(start, end, true)

	// Should have 4 Mondays: Jan 5, 12, 19, 26
	if len(occurrences) != 4 {
		t.Errorf("Between() got %d occurrences, want 4 (occurrences: %v, until: %v)", len(occurrences), occurrences, until)
	}
}

func TestRRuleMonthly(t *testing.T) {
	// Test monthly recurrence on first Monday
	rr, err := rrule.StrToRRule("FREQ=MONTHLY;BYDAY=1MO")
	if err != nil {
		t.Fatalf("StrToRRule() error = %v", err)
	}

	dtstart := time.Date(2026, 1, 5, 19, 0, 0, 0, time.UTC) // First Monday of Jan
	rr.DTStart(dtstart)

	// Get occurrences for 3 months
	start := dtstart
	end := start.AddDate(0, 3, 0)
	occurrences := rr.Between(start, end, true)

	// Should have 3 occurrences (first Monday of Jan, Feb, Mar)
	if len(occurrences) != 3 {
		t.Errorf("Between() got %d occurrences, want 3", len(occurrences))
	}
}

func TestRRuleTimezone(t *testing.T) {
	// Test that times are in correct timezone
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("timezone not available: %v", err)
	}

	rr, err := rrule.StrToRRule("FREQ=WEEKLY;BYDAY=MO")
	if err != nil {
		t.Fatalf("StrToRRule() error = %v", err)
	}

	// Start at 7pm Eastern
	dtstart := time.Date(2026, 1, 5, 19, 0, 0, 0, loc)
	rr.DTStart(dtstart)

	// Get first occurrence
	start := dtstart
	end := start.Add(7 * 24 * time.Hour)
	occurrences := rr.Between(start, end, true)

	if len(occurrences) < 1 {
		t.Fatal("expected at least one occurrence")
	}

	// Check that hour is preserved
	first := occurrences[0]
	if first.Hour() != 19 {
		t.Errorf("first occurrence hour = %d, want 19", first.Hour())
	}
}
