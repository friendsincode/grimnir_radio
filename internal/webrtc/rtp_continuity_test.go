/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webrtc

import "testing"

// TestRewriteContinuity guards #85: the broadcaster rewrites incoming RTP so all
// downstream peers see one continuous stream across pipeline restarts. Outgoing
// sequence numbers must increment by exactly one per packet regardless of the
// input, and when a restart is detected (a large or small-backward sequence
// jump) the timestamp offset must shift so the outgoing timestamp stays
// monotonic with a ~20ms (960-sample) gap rather than jumping backward.
func TestRewriteContinuity(t *testing.T) {
	b := &Broadcaster{ssrc: 42}

	// First packet initializes tracking. Offset is zero, so output == input ts.
	outSeq, outTS := b.rewriteContinuity(1000, 5000)
	if outSeq != 1 {
		t.Fatalf("first outSeq = %d, want 1", outSeq)
	}
	if outTS != 5000 {
		t.Fatalf("first outTS = %d, want 5000 (no offset yet)", outTS)
	}

	// A continuous packet: seq +1, ts advanced. No discontinuity, offset stays 0.
	outSeq, outTS = b.rewriteContinuity(1001, 5960)
	if outSeq != 2 {
		t.Fatalf("second outSeq = %d, want 2", outSeq)
	}
	if outTS != 5960 {
		t.Fatalf("second outTS = %d, want 5960", outTS)
	}

	// A pipeline restart: a huge forward sequence jump (>30000) with the input
	// timestamp reset near zero. The offset must lift the outgoing timestamp to
	// exactly lastOutTS + 960, keeping the stream monotonic.
	prevOutTS := outTS
	outSeq, outTS = b.rewriteContinuity(40000, 100)
	if outSeq != 3 {
		t.Fatalf("post-restart outSeq = %d, want 3 (still +1)", outSeq)
	}
	if outTS != prevOutTS+960 {
		t.Errorf("post-restart outTS = %d, want %d (lastOutTS + 960 gap)", outTS, prevOutTS+960)
	}
	if outTS <= prevOutTS {
		t.Errorf("outgoing timestamp went backward: %d <= %d", outTS, prevOutTS)
	}
}
