/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package version

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.40.13", "1.40.14", -1},
		{"1.40.14", "1.40.13", 1},
		{"1.40.14", "1.40.14", 0},
		{"2.0.0", "1.99.99", 1},
		{"1.41.0", "1.40.99", 1},
		{"v1.40.14", "1.40.14", 0},  // v prefix stripped
		{"2.0.0-rc.14", "2.0.0", 0}, // pre-release suffix ignored by design
		{"", "0.0.0", 0},            // unparseable = zeros
		{"garbage", "0.0.1", -1},    // ditto
	}
	for _, tc := range cases {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want [3]int
	}{
		{"1.40.14", [3]int{1, 40, 14}},
		{"v2.0.0", [3]int{2, 0, 0}},
		{"3", [3]int{3, 0, 0}},
		{"1.2.3.4", [3]int{1, 2, 3}}, // extra segments ignored
		{"", [3]int{0, 0, 0}},
	}
	for _, tc := range cases {
		if got := parseVersion(tc.in); got != tc.want {
			t.Errorf("parseVersion(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestTruncateNotes(t *testing.T) {
	if got := truncateNotes("short note", 200); got != "short note" {
		t.Errorf("short note mangled: %q", got)
	}
	if got := truncateNotes("first line\nsecond line", 200); got != "first line" {
		t.Errorf("multiline should keep first line only: %q", got)
	}
	long := strings.Repeat("x", 300)
	got := truncateNotes(long, 200)
	if len(got) != 200 || !strings.HasSuffix(got, "...") {
		t.Errorf("long note: len=%d suffix=%q", len(got), got[len(got)-3:])
	}
}

// checkerAgainst points a fresh Checker at an httptest server and runs one check.
func checkerAgainst(t *testing.T, handler http.HandlerFunc) *UpdateInfo {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	origBase := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = origBase })

	c := NewChecker(zerolog.Nop())
	c.check(context.Background())
	return c.Info()
}

func TestChecker_UpdateAvailable(t *testing.T) {
	info := checkerAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, GitHubRepo) {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Write([]byte(`{"tag_name":"v999.0.0","html_url":"https://example/rel","body":"Big release\nmore"}`))
	})
	if !info.UpdateAvailable {
		t.Error("update not flagged for a newer release")
	}
	if info.LatestVersion != "999.0.0" {
		t.Errorf("latest = %q", info.LatestVersion)
	}
	if info.ReleaseNotes != "Big release" {
		t.Errorf("notes = %q, want first line only", info.ReleaseNotes)
	}
	if info.CurrentVersion != Version {
		t.Errorf("current = %q", info.CurrentVersion)
	}
}

func TestChecker_NoUpdateWhenCurrent(t *testing.T) {
	info := checkerAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v` + Version + `"}`))
	})
	if info.UpdateAvailable {
		t.Error("update flagged for the running version")
	}
}

func TestChecker_SurvivesBadResponses(t *testing.T) {
	// Non-200: info keeps its zero-value CheckedAt (check aborted).
	info := checkerAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	if !info.CheckedAt.IsZero() {
		t.Error("non-200 response still recorded a check")
	}

	// Malformed JSON: same.
	info = checkerAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{broken"))
	})
	if !info.CheckedAt.IsZero() {
		t.Error("malformed JSON still recorded a check")
	}
}

func TestChecker_StartStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v0.0.1"}`))
	}))
	defer srv.Close()
	origBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = origBase }()

	c := NewChecker(zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && c.Info().CheckedAt.IsZero() {
		time.Sleep(10 * time.Millisecond)
	}
	if c.Info().CheckedAt.IsZero() {
		t.Error("startup check never ran")
	}
	c.Stop()
}
