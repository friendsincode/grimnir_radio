/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package smartblock

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestToFloat(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected float64
	}{
		{"float64", float64(123.45), 123.45},
		{"float32", float32(67.89), 67.89},
		{"int", int(100), 100.0},
		{"int64", int64(200), 200.0},
		{"string", "42.5", 42.5},
		{"invalid string", "abc", 0.0},
		{"nil", nil, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toFloat(tt.input)
			// Use tolerance for float comparison
			tolerance := 0.0001
			if diff := result - tt.expected; diff > tolerance || diff < -tolerance {
				t.Errorf("toFloat(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestToBool(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected bool
	}{
		{"bool true", true, true},
		{"bool false", false, false},
		{"string true", "true", true},
		{"string TRUE", "TRUE", true},
		{"string 1", "1", true},
		{"string false", "false", false},
		{"string 0", "0", false},
		{"float64 non-zero", float64(1.5), true},
		{"float64 zero", float64(0), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toBool(tt.input)
			if result != tt.expected {
				t.Errorf("toBool(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestToString(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{"string", "hello", "hello"},
		{"float64", float64(123.45), "123.45"},
		{"int", int(42), "42"},
		{"nil", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toString(tt.input)
			if result != tt.expected {
				t.Errorf("toString(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestToFloatRange(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected [2]float64
	}{
		{"array", []interface{}{10.0, 20.0}, [2]float64{10.0, 20.0}},
		{"array single", []interface{}{5.0}, [2]float64{5.0, 0}},
		{"map", map[string]interface{}{"min": 100.0, "max": 200.0}, [2]float64{100.0, 200.0}},
		{"empty", nil, [2]float64{0, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toFloatRange(tt.input)
			if result != tt.expected {
				t.Errorf("toFloatRange(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestContains(t *testing.T) {
	values := []string{"Rock", "Pop", "Jazz"}

	tests := []struct {
		name      string
		candidate string
		expected  bool
	}{
		{"exact match", "Rock", true},
		{"case insensitive", "rock", true},
		{"not found", "Blues", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contains(values, tt.candidate)
			if result != tt.expected {
				t.Errorf("contains(%v) = %v, want %v", tt.candidate, result, tt.expected)
			}
		})
	}
}

func TestDeriveEnergy(t *testing.T) {
	tests := []struct {
		name     string
		item     models.MediaItem
		expected float64
	}{
		{"with BPM", models.MediaItem{BPM: 120.0}, 120.0},
		{"with ReplayGain", models.MediaItem{BPM: 0, ReplayGain: -3.0}, 97.0},
		{"default", models.MediaItem{BPM: 0, ReplayGain: 0}, 100.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deriveEnergy(tt.item)
			if result != tt.expected {
				t.Errorf("deriveEnergy() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCollectTags(t *testing.T) {
	item := models.MediaItem{
		Tags: []models.MediaTagLink{
			{TagID: "tag1"},
			{TagID: "tag2"},
			{TagID: "tag3"},
		},
	}

	result := collectTags(item)

	if len(result) != 3 {
		t.Errorf("collectTags() returned %d tags, want 3", len(result))
	}

	if _, ok := result["tag1"]; !ok {
		t.Error("collectTags() missing tag1")
	}
	if _, ok := result["tag2"]; !ok {
		t.Error("collectTags() missing tag2")
	}
	if _, ok := result["tag3"]; !ok {
		t.Error("collectTags() missing tag3")
	}
}

func TestQuotaState(t *testing.T) {
	rules := []QuotaRule{
		{Field: "genre", Min: 2, Max: 5, Values: []string{"Rock"}},
		{Field: "mood", Min: 1, Max: 3, Values: []string{"Happy"}},
	}

	qs := newQuotaState(rules)

	// Test initial state
	if !qs.canSelect(models.MediaItem{Genre: "Rock"}, nil) {
		t.Error("should be able to select Rock initially")
	}

	// Observe 5 Rock items
	for i := 0; i < 5; i++ {
		qs.observe(models.MediaItem{Genre: "Rock"}, nil)
	}

	// Should not be able to select more Rock
	if qs.canSelect(models.MediaItem{Genre: "Rock"}, nil) {
		t.Error("should not be able to select Rock after max reached")
	}

	// Should still be able to select non-Rock
	if !qs.canSelect(models.MediaItem{Genre: "Pop"}, nil) {
		t.Error("should be able to select Pop")
	}

	// Check warnings
	warnings := qs.warnings()
	if len(warnings) == 0 {
		t.Error("expected warnings for unmet min quotas")
	}
}

func TestBuildRecentCache(t *testing.T) {
	now := time.Now()
	plays := []models.PlayHistory{
		{Artist: "Artist1", StartedAt: now.Add(-1 * time.Hour), Metadata: map[string]interface{}{"title": "Song1"}},
		{Artist: "Artist2", StartedAt: now.Add(-2 * time.Hour), Metadata: map[string]interface{}{"title": "Song2"}},
	}

	cache := buildRecentCache(plays)

	if len(cache) == 0 {
		t.Error("buildRecentCache() returned empty cache")
	}

	if _, ok := cache["artist"]["Artist1"]; !ok {
		t.Error("buildRecentCache() missing Artist1")
	}

	if _, ok := cache["artist"]["Artist2"]; !ok {
		t.Error("buildRecentCache() missing Artist2")
	}
}

func TestViolatesSeparation(t *testing.T) {
	now := time.Now()
	recent := map[string]map[string]time.Time{
		"artist": {
			"Artist1": now.Add(-30 * time.Minute),
		},
	}

	windows := map[string]time.Duration{
		"artist": 1 * time.Hour,
	}

	item := models.MediaItem{Artist: "Artist1"}

	if !violatesSeparation(item, recent, windows) {
		t.Error("violatesSeparation() should return true for Artist1 within 1 hour")
	}

	item2 := models.MediaItem{Artist: "Artist2"}
	if violatesSeparation(item2, recent, windows) {
		t.Error("violatesSeparation() should return false for Artist2")
	}
}

func TestToStringSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected []string
		ok       bool
	}{
		{"string slice", []string{"a", "b", "c"}, []string{"a", "b", "c"}, true},
		{"interface slice", []interface{}{"x", "y", "z"}, []string{"x", "y", "z"}, true},
		{"mixed interface", []interface{}{"a", 1, "b"}, []string{"a", "1", "b"}, true},
		{"invalid type", 123, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := toStringSlice(tt.input)
			if ok != tt.ok {
				t.Errorf("toStringSlice() ok = %v, want %v", ok, tt.ok)
			}
			if ok && len(result) != len(tt.expected) {
				t.Errorf("toStringSlice() length = %v, want %v", len(result), len(tt.expected))
			}
		})
	}
}

func TestExcludeFilterSemantics(t *testing.T) {
	item := models.MediaItem{
		Artist: "Hal Anthony",
		Title:  "Behind The Woodshed",
	}

	// No exclude rules => item passes exclude checks.
	if !matchesFilters(item, nil, false) {
		t.Fatal("empty exclude rules should pass")
	}

	// Matching exclude rule => item fails exclude checks.
	excludeArtist := []FilterRule{{Field: "artist", Value: "Hal Anthony"}}
	if matchesFilters(item, excludeArtist, false) {
		t.Fatal("matching exclude rule should fail")
	}

	// Non-matching exclude rule => item passes exclude checks.
	excludeOther := []FilterRule{{Field: "artist", Value: "Someone Else"}}
	if !matchesFilters(item, excludeOther, false) {
		t.Fatal("non-matching exclude rule should pass")
	}
}

func TestMostRecentMediaID(t *testing.T) {
	now := time.Now()
	plays := []models.PlayHistory{
		{MediaID: "newest", StartedAt: now},
		{MediaID: "older", StartedAt: now.Add(-time.Minute)},
	}

	if got := mostRecentMediaID(plays); got != "newest" {
		t.Fatalf("mostRecentMediaID() = %q, want %q", got, "newest")
	}
}

func TestMostRecentMediaIDSkipsEmpty(t *testing.T) {
	plays := []models.PlayHistory{
		{MediaID: ""},
		{MediaID: "usable"},
	}

	if got := mostRecentMediaID(plays); got != "usable" {
		t.Fatalf("mostRecentMediaID() = %q, want %q", got, "usable")
	}
}
