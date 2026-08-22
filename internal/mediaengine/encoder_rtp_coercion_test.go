/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"strings"
	"testing"
)

// TestBuild_RTPForcesOpus48k guards #85: Build() must coerce an RTP/WebRTC output
// to 48 kHz Opus regardless of the requested rate/format, because browsers reject
// anything else. The existing RTP tests call buildOutput() directly and skip this
// coercion, so a regression would ship a 44.1 kHz or non-Opus RTP stream that no
// browser plays.
func TestBuild_RTPForcesOpus48k(t *testing.T) {
	eb := NewEncoderBuilder(EncoderConfig{
		OutputType: OutputTypeRTP,
		SampleRate: 44100,
		Format:     AudioFormatMP3,
		Channels:   2,
		Bitrate:    128,
	})
	pipeline, err := eb.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(pipeline, "rate=48000") {
		t.Errorf("RTP output must be forced to 48 kHz: %q", pipeline)
	}
	if strings.Contains(pipeline, "rate=44100") {
		t.Errorf("RTP output kept the requested 44.1 kHz: %q", pipeline)
	}
	if !strings.Contains(pipeline, "opusenc") {
		t.Errorf("RTP output must use the Opus encoder: %q", pipeline)
	}
}
