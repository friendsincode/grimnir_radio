/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package version

import "testing"

// TestCompareVersions guards #86: UpdateAvailable = compareVersions(Version,
// latest) < 0 is the whole point of this package and had no test. Pin the
// major.minor.patch ordering and the parsing quirks (v-prefix, 2-part tags,
// pre-release suffix).
func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.40.41", "1.40.42", -1},  // older patch
		{"1.40.42", "1.40.42", 0},   // equal
		{"1.40.43", "1.40.42", 1},   // newer patch
		{"1.41.0", "1.40.99", 1},    // minor dominates patch
		{"2.0.0", "1.99.99", 1},     // major dominates
		{"v1.40.42", "1.40.42", 0},  // v-prefix ignored
		{"1.41", "1.41.0", 0},       // 2-part tag == x.y.0
		{"1.41.0-rc1", "1.41.0", 0}, // pre-release parses to x.y.z (known: not ordered before release)
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want [3]int
	}{
		{"1.40.42", [3]int{1, 40, 42}},
		{"v2.0.0", [3]int{2, 0, 0}},
		{"1.41", [3]int{1, 41, 0}},
		{"1.41.0-rc1", [3]int{1, 41, 0}},
		{"garbage", [3]int{0, 0, 0}},
	}
	for _, c := range cases {
		if got := parseVersion(c.in); got != c.want {
			t.Errorf("parseVersion(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
