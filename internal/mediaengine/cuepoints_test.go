/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"testing"

	"github.com/rs/zerolog"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// TestCalculateCuePoints guards #85: intro/outro cue math runs on every analyze
// and drives every crossfade, but was untested. IntroEnd = min(15, dur*0.1);
// OutroIn = max(dur-10, IntroEnd+5); a zero/negative duration falls back to 180s.
func TestCalculateCuePoints(t *testing.T) {
	a := NewAnalyzer(zerolog.Nop())
	cases := []struct {
		name       string
		durationMs int64
		wantIntro  float32
		wantOutro  float32
	}{
		{"3.5 min track", 210_000, 15, 200},              // intro min(15, 21)=15; outro max(200, 20)=200
		{"1 min track", 60_000, 6, 50},                   // intro min(15, 6)=6; outro max(50, 11)=50
		{"zero duration falls back to 180s", 0, 15, 170}, // intro 15; outro max(170, 20)=170
	}
	for _, c := range cases {
		resp := &pb.AnalyzeMediaResponse{DurationMs: c.durationMs}
		a.calculateCuePoints(resp)
		if resp.IntroEnd != c.wantIntro {
			t.Errorf("%s: IntroEnd = %v, want %v", c.name, resp.IntroEnd, c.wantIntro)
		}
		if resp.OutroIn != c.wantOutro {
			t.Errorf("%s: OutroIn = %v, want %v", c.name, resp.OutroIn, c.wantOutro)
		}
		if resp.OutroIn <= resp.IntroEnd {
			t.Errorf("%s: OutroIn %v must be after IntroEnd %v", c.name, resp.OutroIn, resp.IntroEnd)
		}
	}
}
