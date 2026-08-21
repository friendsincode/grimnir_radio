/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package scheduling

import (
	"strings"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestCheckGaps_MessageNamesLimit guards #83: the gap-violation message must
// name the configured limit as a number. It was building the text with
// string(rune(maxGapMinutes)), which renders a control character (U+001E for 30)
// instead of "30", so operators saw "Schedule gap exceeds [ctrl] minutes".
func TestCheckGaps_MessageNamesLimit(t *testing.T) {
	v := &Validator{}
	rule := models.ScheduleRule{
		RuleType: models.RuleTypeGap,
		Severity: models.RuleSeverityWarning,
		Config:   map[string]any{"max_gap_minutes": float64(30)},
	}
	got := v.checkGaps(rule, []ScheduleItem{item("a", 9, 10), item("b", 11, 12)}, at(9, 0), at(12, 0))
	if len(got) != 1 {
		t.Fatalf("expected 1 gap violation, got %d", len(got))
	}
	if !strings.Contains(got[0].Message, "30") {
		t.Errorf("gap message should name the 30-minute limit, got %q", got[0].Message)
	}
}
