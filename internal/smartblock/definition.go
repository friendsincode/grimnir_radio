/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package smartblock

import "time"

// Definition encodes smart block rules shipped via JSON.
type Definition struct {
	Include    []FilterRule    `json:"include"`
	Exclude    []FilterRule    `json:"exclude"`
	Weights    []WeightRule    `json:"weights"`
	Quotas     []QuotaRule     `json:"quotas"`
	Separation SeparationRules `json:"separation"`
	Sequence   SequencePolicy  `json:"sequence"`
	Duration   DurationPolicy  `json:"duration"`
	Fallbacks  []FallbackRule  `json:"fallbacks"`
}

// FilterRule applies a comparison against media metadata.
type FilterRule struct {
	Field string      `json:"field"`
	Op    string      `json:"op"`
	Value interface{} `json:"value"`
}

// WeightRule nudges a field toward selection priority.
type WeightRule struct {
	Field  string      `json:"field"`
	Value  interface{} `json:"value"`
	Weight float64     `json:"weight"`
}

// QuotaRule enforces minimum/maximum counts.
type QuotaRule struct {
	Field     string   `json:"field"`
	Values    []string `json:"values"`
	Min       int      `json:"min"`
	Max       int      `json:"max"`
	WindowSec int      `json:"window_sec"`
}

// SeparationRules describes time-based separation thresholds.
type SeparationRules struct {
	ArtistSec int `json:"artist_sec"`
	TitleSec  int `json:"title_sec"`
	AlbumSec  int `json:"album_sec"`
	LabelSec  int `json:"label_sec"`
}

// SequencePolicy configures energy and variety behaviour.
type SequencePolicy struct {
	Curve       []float64 `json:"curve"`
	Mode        string    `json:"mode"`
	AllowAdjLow bool      `json:"allow_adjacent_low"`
}

// DurationPolicy sets target length and tolerance.
type DurationPolicy struct {
	TargetMS  int64  `json:"target_ms"`
	Tolerance int64  `json:"tolerance_ms"`
	Strategy  string `json:"strategy"`
}

// FallbackRule lists alternative rule sets.
type FallbackRule struct {
	SmartBlockID string `json:"smart_block_id"`
	Limit        int    `json:"limit"`
}

// SeparationDurations converts to duration values.
func (s SeparationRules) SeparationDurations() map[string]time.Duration {
	return map[string]time.Duration{
		"artist": time.Duration(s.ArtistSec) * time.Second,
		"title":  time.Duration(s.TitleSec) * time.Second,
		"album":  time.Duration(s.AlbumSec) * time.Second,
		"label":  time.Duration(s.LabelSec) * time.Second,
	}
}
