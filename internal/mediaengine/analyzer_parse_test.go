/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"bytes"
	"math"
	"testing"

	"github.com/rs/zerolog"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

func approxEq(a, b, tol float32) bool {
	return float32(math.Abs(float64(a-b))) <= tol
}

func TestDBToLinear(t *testing.T) {
	if got := dbToLinear(0); !approxEq(got, 1, 1e-6) {
		t.Fatalf("0 dB -> %f, want 1", got)
	}
	if got := dbToLinear(-60); got != 0 {
		t.Fatalf("-60 dB floor -> %f, want 0", got)
	}
	if got := dbToLinear(-100); got != 0 {
		t.Fatalf("below floor -> %f, want 0", got)
	}
	// -20 dB is 0.1 linear.
	if got := dbToLinear(-20); !approxEq(got, 0.1, 1e-4) {
		t.Fatalf("-20 dB -> %f, want ~0.1", got)
	}
	// Positive dB clamps to 1.
	if got := dbToLinear(6); got != 1 {
		t.Fatalf("+6 dB should clamp to 1, got %f", got)
	}
}

func TestParseLoudnessOutput(t *testing.T) {
	a := NewAnalyzer(zerolog.Nop())
	resp := &pb.AnalyzeMediaResponse{}
	out := "integrated loudness: -14.5 LUFS\nloudness range: 7.2 LU\ntrue peak: -1.3 dBTP"
	a.parseLoudnessOutput(out, resp)

	if !approxEq(resp.LoudnessLufs, -14.5, 1e-3) {
		t.Fatalf("LoudnessLufs = %f, want -14.5", resp.LoudnessLufs)
	}
	// ReplayGain targets -18 LUFS.
	if !approxEq(resp.ReplayGain, -3.5, 1e-3) {
		t.Fatalf("ReplayGain = %f, want -3.5", resp.ReplayGain)
	}
	if !approxEq(resp.LoudnessRange, 7.2, 1e-3) {
		t.Fatalf("LoudnessRange = %f, want 7.2", resp.LoudnessRange)
	}
	if !approxEq(resp.TruePeak, -1.3, 1e-3) {
		t.Fatalf("TruePeak = %f, want -1.3", resp.TruePeak)
	}
}

func TestParseLoudnessOutput_NoMatches(t *testing.T) {
	a := NewAnalyzer(zerolog.Nop())
	resp := &pb.AnalyzeMediaResponse{}
	a.parseLoudnessOutput("nothing useful here", resp)
	if resp.LoudnessLufs != 0 || resp.TruePeak != 0 {
		t.Fatalf("unmatched output should leave fields zero: %+v", resp)
	}
}

func TestParseWaveformOutputFromBuffer(t *testing.T) {
	a := NewAnalyzer(zerolog.Nop())
	// Two frames of peak+rms; values are in dB and get converted to 0-1 linear.
	buf := bytes.NewBufferString(
		"peak=(double){ 0.0, -6.0 } rms=(double){ -20.0, -20.0 }\n" +
			"peak=(double){ -6.0, 0.0 } rms=(double){ -60.0, -20.0 }\n",
	)
	resp := &pb.GenerateWaveformResponse{}
	a.parseWaveformOutputFromBuffer(buf, resp, pb.WaveformType_WAVEFORM_TYPE_BOTH)

	if len(resp.PeakLeft) != 2 || len(resp.RmsLeft) != 2 {
		t.Fatalf("expected 2 frames each, got peak=%d rms=%d", len(resp.PeakLeft), len(resp.RmsLeft))
	}
	// 0 dB -> 1.0 linear.
	if !approxEq(resp.PeakLeft[0], 1, 1e-4) {
		t.Fatalf("PeakLeft[0] = %f, want 1", resp.PeakLeft[0])
	}
	// -60 dB floor -> 0.
	if resp.RmsLeft[1] != 0 {
		t.Fatalf("RmsLeft[1] = %f, want 0 (floor)", resp.RmsLeft[1])
	}
}

func TestParseWaveformOutputFromBuffer_PeakOnly(t *testing.T) {
	a := NewAnalyzer(zerolog.Nop())
	buf := bytes.NewBufferString("peak=(double){ 0.0, 0.0 } rms=(double){ -20.0, -20.0 }\n")
	resp := &pb.GenerateWaveformResponse{}
	a.parseWaveformOutputFromBuffer(buf, resp, pb.WaveformType_WAVEFORM_TYPE_PEAK)

	if len(resp.PeakLeft) != 1 {
		t.Fatalf("expected 1 peak frame, got %d", len(resp.PeakLeft))
	}
	if len(resp.RmsLeft) != 0 {
		t.Fatalf("peak-only should skip rms, got %d rms frames", len(resp.RmsLeft))
	}
}
