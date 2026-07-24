/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"strings"
	"testing"
)

func TestBuildHTTPOutput(t *testing.T) {
	eb := NewEncoderBuilder(EncoderConfig{OutputType: OutputTypeHTTP, OutputURL: "http://edge.example/stream"})
	got, err := eb.buildOutput()
	if err != nil {
		t.Fatalf("buildOutput: %v", err)
	}
	if got != `souphttpclientsink location="http://edge.example/stream"` {
		t.Fatalf("http output = %q", got)
	}
}

func TestBuildRTPOutput_Defaults(t *testing.T) {
	// Zero RTP fields fall back to 127.0.0.1:5004 and Opus payload type 111.
	eb := NewEncoderBuilder(EncoderConfig{OutputType: OutputTypeRTP})
	got, err := eb.buildOutput()
	if err != nil {
		t.Fatalf("buildOutput: %v", err)
	}
	want := "rtpopuspay pt=111 ! udpsink host=127.0.0.1 port=5004"
	if got != want {
		t.Fatalf("rtp default output = %q, want %q", got, want)
	}
}

func TestBuildRTPOutput_Explicit(t *testing.T) {
	eb := NewEncoderBuilder(EncoderConfig{
		OutputType:     OutputTypeRTP,
		RTPHost:        "10.0.0.9",
		RTPPort:        6000,
		RTPPayloadType: 96,
	})
	got, err := eb.buildOutput()
	if err != nil {
		t.Fatalf("buildOutput: %v", err)
	}
	for _, want := range []string{"pt=96", "host=10.0.0.9", "port=6000"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rtp output %q missing %q", got, want)
		}
	}
}

func TestBuildOutput_UnsupportedType(t *testing.T) {
	eb := NewEncoderBuilder(EncoderConfig{OutputType: OutputType("carrier-pigeon")})
	if _, err := eb.buildOutput(); err == nil {
		t.Fatal("expected error for unsupported output type")
	}
}
