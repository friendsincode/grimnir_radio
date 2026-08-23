/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"math"
	"testing"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// Curve constants mirror pb.FadeCurve: 1=LINEAR, 2=LOG, 3=EXP, 4=SCURVE.

// TestCalculateFadeCurveVolume_SCurve covers the S-curve branches the existing
// tests skip: the cubic ease-in-out is defined piecewise around the midpoint and
// must be symmetric — f(p) + f(1-p) == 1 — with the midpoint at exactly 0.5.
func TestCalculateFadeCurveVolume_SCurve(t *testing.T) {
	const eps = 1e-9

	if got := calculateFadeCurveVolume(0.5, 4, true); math.Abs(got-0.5) > eps {
		t.Errorf("s-curve at 0.5 = %v, want 0.5", got)
	}

	// First-half (p<0.5) and second-half (p>=0.5) branches, checked for symmetry.
	lo := calculateFadeCurveVolume(0.25, 4, true)
	hi := calculateFadeCurveVolume(0.75, 4, true)
	if lo >= 0.5 || hi <= 0.5 {
		t.Errorf("s-curve should ease through the midpoint: f(.25)=%v f(.75)=%v", lo, hi)
	}
	if math.Abs((lo+hi)-1.0) > eps {
		t.Errorf("s-curve not symmetric: f(.25)+f(.75) = %v, want 1.0", lo+hi)
	}
}

// TestCalculateFadeCurveVolume_FadeOutInverts covers the fadeIn=false path: a
// fade-out is the fade-in curve mirrored, so volume(progress, fadeOut) must equal
// 1 - volume(progress, fadeIn) for every curve.
func TestCalculateFadeCurveVolume_FadeOutInverts(t *testing.T) {
	const eps = 1e-9
	for _, curve := range []int{1, 2, 3, 4} {
		for _, p := range []float64{0.0, 0.3, 0.5, 0.8, 1.0} {
			in := calculateFadeCurveVolume(p, pb.FadeCurve(curve), true)
			out := calculateFadeCurveVolume(p, pb.FadeCurve(curve), false)
			if math.Abs(out-(1-in)) > eps {
				t.Errorf("curve %d p=%.1f: fade-out %v != 1 - fade-in %v", curve, p, out, in)
			}
		}
	}
}

// TestCalculateFadeCurveVolume_Endpoints pins the endpoints across curves: every
// curve starts silent and ends at full volume on a fade-in, including the
// logarithmic progress==0 special case that avoids Log10(1)=0 drift.
func TestCalculateFadeCurveVolume_Endpoints(t *testing.T) {
	const eps = 1e-9
	for _, curve := range []int{1, 2, 3, 4} {
		if got := calculateFadeCurveVolume(0, pb.FadeCurve(curve), true); math.Abs(got) > eps {
			t.Errorf("curve %d at progress 0 = %v, want 0", curve, got)
		}
		if got := calculateFadeCurveVolume(1, pb.FadeCurve(curve), true); math.Abs(got-1) > eps {
			t.Errorf("curve %d at progress 1 = %v, want 1", curve, got)
		}
	}
}
