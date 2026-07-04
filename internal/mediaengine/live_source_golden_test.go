/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import "testing"

// Golden strings for the live-input source fragment (issue #254). The scheme
// switch decides which GStreamer element a live DJ lands on; a wrong default
// port or dropped caps filter only fails at runtime on the engine.
func TestGolden_BuildLiveSource(t *testing.T) {
	p := &Pipeline{}

	cases := []struct {
		name string
		url  string
		want string
	}{
		{"empty defaults to tcp server on 8001", "", "tcpserversrc port=8001 ! decodebin"},
		{"tcp with port", "tcp://0.0.0.0:9000", "tcpserversrc port=9000 ! decodebin"},
		{"tcp without port defaults 8001", "tcp://0.0.0.0", "tcpserversrc port=8001 ! decodebin"},
		{"udp with port gets rtp caps", "udp://0.0.0.0:5008", "udpsrc port=5008 ! application/x-rtp ! decodebin"},
		{"rtp without port defaults 5004", "rtp://0.0.0.0", "udpsrc port=5004 ! application/x-rtp ! decodebin"},
		{"http falls through to souphttpsrc", "http://icecast.example/mount", `souphttpsrc location="http://icecast.example/mount" ! decodebin`},
		{"unparseable url falls back to souphttpsrc", "://not-a-url", `souphttpsrc location="://not-a-url" ! decodebin`},
		{"whitespace-only is trimmed to the default", "   ", "tcpserversrc port=8001 ! decodebin"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.buildLiveSource(tc.url); got != tc.want {
				t.Errorf("buildLiveSource(%q)\n got: %s\nwant: %s", tc.url, got, tc.want)
			}
		})
	}
}
