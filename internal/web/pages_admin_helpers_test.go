/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"testing"
)

// ---------------------------------------------------------------------------
// formatBytesUint64
// ---------------------------------------------------------------------------

func TestFormatBytesUint64_Zero(t *testing.T) {
	if got := formatBytesUint64(0); got != "0 B" {
		t.Fatalf("expected '0 B', got %q", got)
	}
}

func TestFormatBytesUint64_UnderKilobyte(t *testing.T) {
	if got := formatBytesUint64(512); got != "512 B" {
		t.Fatalf("expected '512 B', got %q", got)
	}
}

func TestFormatBytesUint64_ExactKilobyte(t *testing.T) {
	if got := formatBytesUint64(1024); got != "1.0 KB" {
		t.Fatalf("expected '1.0 KB', got %q", got)
	}
}

func TestFormatBytesUint64_OneMegabyte(t *testing.T) {
	if got := formatBytesUint64(1024 * 1024); got != "1.0 MB" {
		t.Fatalf("expected '1.0 MB', got %q", got)
	}
}

func TestFormatBytesUint64_OneGigabyte(t *testing.T) {
	if got := formatBytesUint64(1024 * 1024 * 1024); got != "1.0 GB" {
		t.Fatalf("expected '1.0 GB', got %q", got)
	}
}

func TestFormatBytesUint64_HalfMegabyte(t *testing.T) {
	if got := formatBytesUint64(512 * 1024); got != "512.0 KB" {
		t.Fatalf("expected '512.0 KB', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// shortHash
// ---------------------------------------------------------------------------

func TestShortHash_ShortStringUnchanged(t *testing.T) {
	if got := shortHash("abc"); got != "abc" {
		t.Fatalf("expected 'abc', got %q", got)
	}
}

func TestShortHash_ExactlyTwelveUnchanged(t *testing.T) {
	s := "123456789012"
	if got := shortHash(s); got != s {
		t.Fatalf("expected unchanged at 12 chars, got %q", got)
	}
}

func TestShortHash_LongStringTruncated(t *testing.T) {
	s := "abcdefghijklmnopqrstuvwxyz"
	got := shortHash(s)
	if len(got) != 12 {
		t.Fatalf("expected 12 chars, got %d (%q)", len(got), got)
	}
	if got != s[:12] {
		t.Fatalf("expected first 12 chars, got %q", got)
	}
}

func TestShortHash_Empty(t *testing.T) {
	if got := shortHash(""); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}
