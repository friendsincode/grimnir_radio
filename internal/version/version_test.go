/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package version

import "testing"

// The update banner shows truncateNotes(release.Body) as plain text. GitHub
// auto-generated bodies start with "## What's Changed", which rendered as raw
// markdown in the banner (#67). truncateNotes must return a clean plain-text
// line: no leading '#'/'*' markers, and it should skip the heading to the first
// real change line.
func TestTruncateNotes_StripsMarkdownAndSkipsHeading(t *testing.T) {
	body := "## What's Changed\n* Fix the crash by @alice in #12\n\n**Full Changelog**: https://x/y"
	got := truncateNotes(body, 200)
	if want := "Fix the crash by @alice in #12"; got != want {
		t.Fatalf("truncateNotes = %q, want %q", got, want)
	}
}

func TestTruncateNotes_PlainLineUnchanged(t *testing.T) {
	if got := truncateNotes("Just a plain summary.", 200); got != "Just a plain summary." {
		t.Fatalf("truncateNotes = %q, want plain line unchanged", got)
	}
}

func TestTruncateNotes_HeadingOnlyStripsMarkers(t *testing.T) {
	if got := truncateNotes("## Only a heading here", 200); got != "Only a heading here" {
		t.Fatalf("truncateNotes = %q, want heading markers stripped", got)
	}
}

func TestTruncateNotes_Truncates(t *testing.T) {
	long := "abcdefghijklmnopqrstuvwxyz"
	got := truncateNotes(long, 10)
	if len(got) != 10 || got[len(got)-3:] != "..." {
		t.Fatalf("truncateNotes = %q, want 10 chars ending with ...", got)
	}
}
