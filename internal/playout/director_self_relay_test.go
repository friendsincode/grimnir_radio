/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import "testing"

// Cross-station syndication relays that point at one of this server's own
// /live/ mounts must pull over loopback, not the public HTTPS edge — the
// public round-trip made souphttpsrc thrash (reconnect storm hammering the
// edge, destabilizing real listeners). Genuine external relays pass through.
func TestRewriteSelfRelayURL(t *testing.T) {
	d, _ := newMockDirector(t)
	// A mount this instance broadcasts (the syndicated source).
	d.broadcast.CreateMount("vincent-easley-ii", "audio/mpeg", 128)

	cases := []struct {
		name   string
		url    string
		dest   string
		expect string
	}{
		{
			name:   "own mount rewritten to loopback",
			url:    "https://rlmradio.xyz/live/vincent-easley-ii",
			dest:   "some-other-station-mount",
			expect: "http://127.0.0.1:8080/live/vincent-easley-ii",
		},
		{
			name:   "external stream untouched",
			url:    "https://external.example.com/stream.mp3",
			dest:   "mymount",
			expect: "https://external.example.com/stream.mp3",
		},
		{
			name:   "unknown local mount untouched (real external /live path)",
			url:    "https://otherserver.net/live/not-ours",
			dest:   "mymount",
			expect: "https://otherserver.net/live/not-ours",
		},
		{
			name:   "self-feedback (dest == source mount) left alone",
			url:    "https://rlmradio.xyz/live/vincent-easley-ii",
			dest:   "vincent-easley-ii",
			expect: "https://rlmradio.xyz/live/vincent-easley-ii",
		},
		{
			name:   "unparseable url untouched",
			url:    "://bad",
			dest:   "mymount",
			expect: "://bad",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := d.rewriteSelfRelayURL(tc.url, tc.dest); got != tc.expect {
				t.Errorf("rewriteSelfRelayURL(%q, %q) = %q, want %q", tc.url, tc.dest, got, tc.expect)
			}
		})
	}
}
