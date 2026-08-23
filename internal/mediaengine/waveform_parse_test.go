/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"math"
	"strings"
	"testing"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

func approx32(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

// TestDbToLinear pins the dB->linear amplitude conversion: silence floors at
// -60dB, 0dB is unity, and it never exceeds 1.
func TestDbToLinear(t *testing.T) {
	cases := []struct {
		db   float32
		want float32
	}{
		{-60, 0},  // floor
		{-120, 0}, // below floor still 0
		{0, 1},    // unity
		{6, 1},    // positive dB clamps to 1
		{-6.0206, 0.5},
		{-20, 0.1},
	}
	for _, c := range cases {
		if got := dbToLinear(c.db); !approx32(got, c.want) {
			t.Errorf("dbToLinear(%v) = %v, want %v", c.db, got, c.want)
		}
	}
}

// TestParseWaveformOutput_TypeFiltering feeds two GStreamer level lines and
// checks the waveform-type gate: PEAK collects only peak samples, RMS only rms,
// and each dB pair is converted through dbToLinear in order.
func TestParseWaveformOutput_TypeFiltering(t *testing.T) {
	a := &Analyzer{}
	input := "level, peak=(double){ 0.0, -60.0 }, rms=(double){ -6.0206, -20.0 }\n" +
		"level, peak=(double){ -20.0, 0.0 }, rms=(double){ -60.0, -6.0206 }\n"

	t.Run("peak only", func(t *testing.T) {
		resp := &pb.GenerateWaveformResponse{}
		a.parseWaveformOutput(strings.NewReader(input), resp, pb.WaveformType_WAVEFORM_TYPE_PEAK)
		if len(resp.PeakLeft) != 2 || len(resp.PeakRight) != 2 {
			t.Fatalf("peak samples = %d/%d, want 2/2", len(resp.PeakLeft), len(resp.PeakRight))
		}
		if len(resp.RmsLeft) != 0 {
			t.Errorf("rms should be empty for PEAK type, got %d", len(resp.RmsLeft))
		}
		// First line: peak 0dB -> 1.0 left, -60dB -> 0 right.
		if !approx32(resp.PeakLeft[0], 1.0) || !approx32(resp.PeakRight[0], 0) {
			t.Errorf("first peak = %v/%v, want 1.0/0", resp.PeakLeft[0], resp.PeakRight[0])
		}
	})

	t.Run("both", func(t *testing.T) {
		resp := &pb.GenerateWaveformResponse{}
		a.parseWaveformOutput(strings.NewReader(input), resp, pb.WaveformType_WAVEFORM_TYPE_BOTH)
		if len(resp.PeakLeft) != 2 || len(resp.RmsLeft) != 2 {
			t.Fatalf("both should collect peak and rms, got peak %d rms %d", len(resp.PeakLeft), len(resp.RmsLeft))
		}
		// First line rms: -6.0206dB -> 0.5 left, -20dB -> 0.1 right.
		if !approx32(resp.RmsLeft[0], 0.5) || !approx32(resp.RmsRight[0], 0.1) {
			t.Errorf("first rms = %v/%v, want 0.5/0.1", resp.RmsLeft[0], resp.RmsRight[0])
		}
	})
}
