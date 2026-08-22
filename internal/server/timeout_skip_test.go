/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package server

import (
	"net/http/httptest"
	"testing"
)

// TestSkipRequestTimeout guards #86: the request-timeout middleware must bypass
// the 60s deadline for long-lived and large-body requests — WebSocket upgrades,
// /live/ broadcast streams, and the two upload paths — while enforcing it on
// everything else. A regression here either strangles a stream at 60s or leaves
// ordinary routes without a timeout.
func TestSkipRequestTimeout(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		upgrade string
		want    bool
	}{
		{"websocket upgrade", "/dashboard/live", "websocket", true},
		{"live stream prefix", "/live/main.mp3", "", true},
		{"live exact no trailing slash", "/live", "", false},
		{"media upload", "/dashboard/media/upload", "", true},
		{"migrations import", "/dashboard/settings/migrations/import", "", true},
		{"ordinary dashboard route", "/dashboard/shows", "", false},
		{"root", "/", "", false},
		{"healthz", "/healthz", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tc.path, nil)
			if tc.upgrade != "" {
				r.Header.Set("Upgrade", tc.upgrade)
			}
			if got := skipRequestTimeout(r); got != tc.want {
				t.Errorf("skipRequestTimeout(%q, upgrade=%q) = %v, want %v", tc.path, tc.upgrade, got, tc.want)
			}
		})
	}
}
