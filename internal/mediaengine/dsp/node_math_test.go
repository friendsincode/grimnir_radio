/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package dsp

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// TestBuildDuckNode_RatioMath guards #84: buildDuckNode's dB->ratio conversion
// (ratio = -reduction_db/3, clamped to >=1, and 1 for non-negative reduction)
// had no test at all. A wrong divisor or a dropped clamp changes how hard a bed
// ducks under a voice.
func TestBuildDuckNode_RatioMath(t *testing.T) {
	b := NewBuilder(zerolog.Nop())
	cases := []struct {
		reduction string
		wantRatio string
	}{
		{"-12", "ratio=4.00"},
		{"-6", "ratio=2.00"},
		{"-3", "ratio=1.00"},
		{"-2", "ratio=1.00"}, // 0.67 clamped up to 1.00
		{"6", "ratio=1.00"},  // non-negative reduction => no ducking
	}
	for _, c := range cases {
		el, err := b.buildDuckNode(&pb.DSPNode{Params: map[string]string{"reduction_db": c.reduction}})
		if err != nil {
			t.Fatalf("reduction %s: %v", c.reduction, err)
		}
		if !strings.Contains(el, c.wantRatio) {
			t.Errorf("reduction_db=%s => %q, want %s", c.reduction, el, c.wantRatio)
		}
	}
}

// TestBuildLoudnessNode_PreAmpFromLUFS guards the LUFS->pre-amp offset
// (pre-amp = target_lufs + 18). Only substring "!= empty" was checked before.
func TestBuildLoudnessNode_PreAmpFromLUFS(t *testing.T) {
	b := NewBuilder(zerolog.Nop())

	// Default target -23 LUFS => pre-amp -5.00.
	def, err := b.buildLoudnessNode(&pb.DSPNode{})
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	if !strings.Contains(def, "pre-amp=-5.00") {
		t.Errorf("default loudness = %q, want pre-amp=-5.00", def)
	}

	// target -20 => pre-amp -2.00.
	set, err := b.buildLoudnessNode(&pb.DSPNode{Params: map[string]string{"target_lufs": "-20"}})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if !strings.Contains(set, "pre-amp=-2.00") {
		t.Errorf("target -20 loudness = %q, want pre-amp=-2.00", set)
	}
}
