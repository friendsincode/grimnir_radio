/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import "testing"

func TestDetectStreamType(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://example.com/live.m3u8", StreamTypeHLS},
		{"https://example.com/stream/bbc_radio.M3U8", StreamTypeHLS},
		{"https://example.com/radio/stream", StreamTypeICY},
		{"https://example.com/listen.mp3", StreamTypeICY},
		{"https://example.com/stream.m3u8?token=abc", StreamTypeHLS},
		{"not a valid url ://", StreamTypeICY},
		{"", StreamTypeICY},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := DetectStreamType(tt.url)
			if got != tt.want {
				t.Errorf("DetectStreamType(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
