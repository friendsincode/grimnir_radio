/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package landingpage

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// themes.go
// ---------------------------------------------------------------------------

func TestGetTheme(t *testing.T) {
	first := BuiltInThemes[0]
	if got := GetTheme(first.ID); got == nil || got.ID != first.ID {
		t.Fatalf("GetTheme(%q) = %v", first.ID, got)
	}
	if GetTheme("no-such-theme") != nil {
		t.Fatal("unknown theme should return nil")
	}
}

func TestGetThemeDefaults_FallsBackToFirst(t *testing.T) {
	if d := GetThemeDefaults(BuiltInThemes[0].ID); d == nil {
		t.Fatal("known theme defaults should be non-nil")
	}
	// Unknown id falls back to the first built-in theme's defaults, never nil.
	if d := GetThemeDefaults("bogus"); d == nil {
		t.Fatal("unknown theme should fall back, not return nil")
	}
}

func TestGetPlatformThemeDefaults(t *testing.T) {
	d := GetPlatformThemeDefaults()
	if d["theme"] != "daw-dark" {
		t.Fatalf("platform theme = %v", d["theme"])
	}
	if _, ok := d["hero"].(map[string]any); !ok {
		t.Fatal("platform defaults should carry a hero section")
	}
}

func TestMergeConfigAndDeepMerge(t *testing.T) {
	// nil user config returns the theme defaults unchanged.
	base := GetThemeDefaults(BuiltInThemes[0].ID)
	if got := MergeConfig(BuiltInThemes[0].ID, nil); len(got) != len(base) {
		t.Fatalf("nil user config should return defaults (len %d vs %d)", len(got), len(base))
	}

	// Deep merge: nested override replaces only the specified leaf.
	merged := deepMerge(
		map[string]any{"header": map[string]any{"showLogo": true, "tagline": "old"}, "top": 1},
		map[string]any{"header": map[string]any{"tagline": "new"}, "extra": 2},
	)
	hdr := merged["header"].(map[string]any)
	if hdr["tagline"] != "new" {
		t.Fatalf("override leaf not applied: %v", hdr["tagline"])
	}
	if hdr["showLogo"] != true {
		t.Fatal("unspecified base leaf should be preserved")
	}
	if merged["top"] != 1 || merged["extra"] != 2 {
		t.Fatalf("top-level merge wrong: %+v", merged)
	}

	// A non-map override replaces a map base wholesale.
	replaced := deepMerge(map[string]any{"k": map[string]any{"a": 1}}, map[string]any{"k": "scalar"})
	if replaced["k"] != "scalar" {
		t.Fatalf("scalar override should replace map, got %v", replaced["k"])
	}
}

// ---------------------------------------------------------------------------
// widgets.go
// ---------------------------------------------------------------------------

func TestWidgetDefinitionAndDefaults(t *testing.T) {
	if def := GetWidgetDefinition(WidgetSchedule); def == nil || def.Type != WidgetSchedule {
		t.Fatalf("schedule widget definition missing: %v", def)
	}
	if GetWidgetDefinition(WidgetType("nope")) != nil {
		t.Fatal("unknown widget type should be nil")
	}
	if d := GetWidgetDefaults(WidgetSchedule); d == nil {
		t.Fatal("known widget defaults should be non-nil")
	}
	// Unknown type returns an empty (non-nil) map.
	if d := GetWidgetDefaults(WidgetType("nope")); d == nil || len(d) != 0 {
		t.Fatalf("unknown widget defaults = %v, want empty map", d)
	}
}

func TestGetWidgetsByCategory(t *testing.T) {
	byCat := GetWidgetsByCategory()
	if len(byCat) == 0 {
		t.Fatal("expected widgets grouped into at least one category")
	}
	total := 0
	for _, defs := range byCat {
		total += len(defs)
	}
	if total != len(WidgetRegistry) {
		t.Fatalf("grouped total %d != registry %d", total, len(WidgetRegistry))
	}
}

func TestValidateWidgetConfig(t *testing.T) {
	if err := ValidateWidgetConfig(WidgetConfig{Type: WidgetText}); err != nil {
		t.Fatalf("valid widget should pass: %v", err)
	}
	if err := ValidateWidgetConfig(WidgetConfig{Type: WidgetType("bogus")}); err != ErrInvalidWidgetType {
		t.Fatalf("invalid widget err = %v, want ErrInvalidWidgetType", err)
	}
	if ErrInvalidWidgetType.Error() == "" {
		t.Fatal("error string should be non-empty")
	}
}

// ---------------------------------------------------------------------------
// renderer.go config helpers
// ---------------------------------------------------------------------------

func TestConfigHelpers(t *testing.T) {
	cfg := map[string]any{
		"b":     true,
		"iInt":  7,
		"iF":    float64(9),
		"i64":   int64(11),
		"s":     "hi",
		"other": 1,
	}
	if configVal(cfg, "missing", "def") != "def" {
		t.Fatal("configVal default")
	}
	if configVal(cfg, "s", "def") != "hi" {
		t.Fatal("configVal hit")
	}
	if !configBool(cfg, "b", false) || configBool(cfg, "missing", false) {
		t.Fatal("configBool")
	}
	if configBool(cfg, "s", true) != true {
		t.Fatal("configBool wrong-type falls back to default")
	}
	if configInt(cfg, "iInt", 0) != 7 || configInt(cfg, "iF", 0) != 9 || configInt(cfg, "i64", 0) != 11 {
		t.Fatal("configInt numeric coercion")
	}
	if configInt(cfg, "missing", 3) != 3 || configInt(cfg, "s", 3) != 3 {
		t.Fatal("configInt default")
	}
	if configString(cfg, "s", "def") != "hi" || configString(cfg, "b", "def") != "def" {
		t.Fatal("configString")
	}
}

func TestFormatAndTruncateHelpers(t *testing.T) {
	if got := formatTime(time.Date(2026, 1, 1, 15, 4, 0, 0, time.UTC)); got != "3:04 PM" {
		t.Fatalf("formatTime = %q", got)
	}
	if got := formatDuration(90 * time.Minute); got != "1h 30m" {
		t.Fatalf("formatDuration hours = %q", got)
	}
	if got := formatDuration(45 * time.Minute); got != "45m" {
		t.Fatalf("formatDuration minutes = %q", got)
	}
	if got := truncate("short", 20); got != "short" {
		t.Fatalf("truncate short = %q", got)
	}
	if got := truncate("abcdefghij", 8); got != "abcde..." {
		t.Fatalf("truncate long = %q", got)
	}
}

func TestJSONMarshalHelper(t *testing.T) {
	if got := jsonMarshal(nil); string(got) != "null" {
		t.Fatalf("nil -> %q, want null", got)
	}
	if got := jsonMarshal(map[string]int{"a": 1}); string(got) != `{"a":1}` {
		t.Fatalf("map -> %q", got)
	}
	// Unmarshalable value (a channel) falls back to "null" rather than erroring.
	if got := jsonMarshal(make(chan int)); string(got) != "null" {
		t.Fatalf("unmarshalable -> %q, want null", got)
	}
}
