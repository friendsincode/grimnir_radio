/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package smartblock

import "testing"

// findInclude returns the first Include rule for a field, or nil.
func findInclude(def Definition, field string) *FilterRule {
	for i := range def.Include {
		if def.Include[i].Field == field {
			return &def.Include[i]
		}
	}
	return nil
}

// TestApplyLegacyRuleCompat_ExplicitInversion guards #85: the dashboard's
// "exclude explicit" toggle is stored as excludeExplicit=true, but the engine
// filters on explicit=false. The compat layer must invert it, not pass true
// through, or explicit-only blocks would be generated.
func TestApplyLegacyRuleCompat_ExplicitInversion(t *testing.T) {
	def := applyLegacyRuleCompat(Definition{}, map[string]any{"excludeExplicit": true})
	r := findInclude(def, "explicit")
	if r == nil {
		t.Fatal("excludeExplicit=true should add an explicit include rule")
	}
	if r.Value != false {
		t.Errorf("explicit rule value = %v, want false (exclude explicit means explicit=false)", r.Value)
	}

	// excludeExplicit=false must not add any explicit constraint.
	def2 := applyLegacyRuleCompat(Definition{}, map[string]any{"excludeExplicit": false})
	if findInclude(def2, "explicit") != nil {
		t.Error("excludeExplicit=false must not add an explicit rule")
	}
}

// TestApplyLegacyRuleCompat_SeparationDisabledZeroed guards #85: when
// separationEnabled is explicitly false, any legacy separation values must be
// zeroed so the engine doesn't silently apply constraints the user turned off.
func TestApplyLegacyRuleCompat_SeparationDisabledZeroed(t *testing.T) {
	rules := map[string]any{
		"separationEnabled": false,
		"separation":        map[string]any{"artist": 10, "title": 5},
	}
	def := applyLegacyRuleCompat(Definition{}, rules)
	if def.Separation != (SeparationRules{}) {
		t.Errorf("disabled separation must be zeroed, got %+v", def.Separation)
	}
}

// TestApplyLegacyRuleCompat_SeparationMinutesToSeconds guards #85: legacy
// separation values are stored in minutes and the engine works in seconds, so
// each value is multiplied by 60. Absent an explicit flag but with a separation
// map present, separation stays enabled.
func TestApplyLegacyRuleCompat_SeparationMinutesToSeconds(t *testing.T) {
	rules := map[string]any{
		"separation": map[string]any{"artist": 10, "title": 5, "album": 3, "label": 2},
	}
	def := applyLegacyRuleCompat(Definition{}, rules)
	if def.Separation.ArtistSec != 600 {
		t.Errorf("artist separation = %d, want 600 (10 min)", def.Separation.ArtistSec)
	}
	if def.Separation.TitleSec != 300 {
		t.Errorf("title separation = %d, want 300 (5 min)", def.Separation.TitleSec)
	}
	if def.Separation.AlbumSec != 180 || def.Separation.LabelSec != 120 {
		t.Errorf("album/label separation wrong: %+v", def.Separation)
	}
}

// TestApplyLegacyRuleCompat_DoesNotOverrideExplicitFields guards #85: legacy
// keys only fill gaps. A Definition that already carries a genre include rule or
// a target duration must not be clobbered by the flat legacy equivalents.
func TestApplyLegacyRuleCompat_DoesNotOverrideExplicitFields(t *testing.T) {
	def := Definition{Include: []FilterRule{{Field: "genre", Value: "jazz"}}}
	def.Duration.TargetMS = 5000
	out := applyLegacyRuleCompat(def, map[string]any{"genre": "rock", "targetMinutes": 30})

	if r := findInclude(out, "genre"); r == nil || r.Value != "jazz" {
		t.Errorf("existing genre rule must win over legacy genre, got %+v", r)
	}
	if n := len(out.Include); n != 1 {
		t.Errorf("legacy genre must not add a second genre rule, Include len = %d", n)
	}
	if out.Duration.TargetMS != 5000 {
		t.Errorf("existing TargetMS must win, got %d", out.Duration.TargetMS)
	}
}
