/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"testing"
	"time"
)

func TestPipelineHarborSink_DecoderCmdDefault(t *testing.T) {
	s := NewPipelineHarborSink([]string{"127.0.0.1:5004"})
	if len(s.DecoderCmd) < 5 {
		t.Fatalf("DecoderCmd unexpectedly short: %v", s.DecoderCmd)
	}
	if s.DecoderCmd[0] != "gst-launch-1.0" {
		t.Errorf("DecoderCmd[0] = %q, want gst-launch-1.0", s.DecoderCmd[0])
	}
	// Must include decodebin (the whole point of the subprocess: handles
	// MP3 / AAC / Ogg / Opus / FLAC without per-format CGo glue).
	found := false
	for _, a := range s.DecoderCmd {
		if a == "decodebin" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("DecoderCmd missing decodebin: %v", s.DecoderCmd)
	}
}

func TestPipelineHarborSink_BytesBeforeBeginErrors(t *testing.T) {
	s := NewPipelineHarborSink([]string{"127.0.0.1:5004"})
	fakeSess := newSessionWithDeps("never-begun", ProtocolHarbor, time.Now())
	if err := s.Bytes(fakeSess, []byte("x")); err == nil {
		t.Error("Bytes before Begin: err = nil, want err")
	}
}

func TestPipelineHarborSink_EndWithoutBeginIsNoop(t *testing.T) {
	s := NewPipelineHarborSink([]string{"127.0.0.1:5004"})
	fakeSess := newSessionWithDeps("never-begun", ProtocolHarbor, time.Now())
	// Must not panic.
	s.End(fakeSess)
}
