/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package storage

import (
	"testing"
)

func TestSeverityRank(t *testing.T) {
	tests := []struct {
		severity string
		want     int
	}{
		{"", 0},
		{"unknown", 0},
		{severityWarning, 1},
		{severityCritical, 2},
		{severityEmergency, 3},
	}
	for _, tt := range tests {
		got := severityRank(tt.severity)
		if got != tt.want {
			t.Errorf("severityRank(%q) = %d, want %d", tt.severity, got, tt.want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		bytes uint64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
		{5368709120, "5.0 GB"},
	}
	for _, tt := range tests {
		got := humanBytes(tt.bytes)
		if got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestThresholdOrdering(t *testing.T) {
	// Thresholds must be ordered highest-percent first so matching picks the
	// most severe tier.
	for i := 1; i < len(defaultThresholds); i++ {
		if defaultThresholds[i].Percent >= defaultThresholds[i-1].Percent {
			t.Errorf("thresholds not ordered highest-first: index %d (%.0f%%) >= index %d (%.0f%%)",
				i, defaultThresholds[i].Percent, i-1, defaultThresholds[i-1].Percent)
		}
	}
}

func TestSeverityEscalation(t *testing.T) {
	// Verify that higher thresholds have higher severity ranks.
	for i := 0; i < len(defaultThresholds)-1; i++ {
		cur := severityRank(defaultThresholds[i].Severity)
		next := severityRank(defaultThresholds[i+1].Severity)
		if cur <= next {
			t.Errorf("threshold at %.0f%% (rank %d) should have higher severity than %.0f%% (rank %d)",
				defaultThresholds[i].Percent, cur, defaultThresholds[i+1].Percent, next)
		}
	}
}
