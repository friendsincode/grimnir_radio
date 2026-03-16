/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// normalizeSearchText
// ---------------------------------------------------------------------------

func TestNormalizeSearchText_LowercasesAndStrips(t *testing.T) {
	got := normalizeSearchText("Hello World")
	if got != "helloworld" {
		t.Fatalf("expected 'helloworld', got %q", got)
	}
}

func TestNormalizeSearchText_RemovesPunctuation(t *testing.T) {
	got := normalizeSearchText("don't stop me now")
	if got != "dontstopmenow" {
		t.Fatalf("expected 'dontstopmenow', got %q", got)
	}
}

func TestNormalizeSearchText_RemovesDashes(t *testing.T) {
	got := normalizeSearchText("rock-n-roll")
	if got != "rocknroll" {
		t.Fatalf("expected 'rocknroll', got %q", got)
	}
}

func TestNormalizeSearchText_TrimsWhitespace(t *testing.T) {
	got := normalizeSearchText("  hello  ")
	if got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestNormalizeSearchText_Empty(t *testing.T) {
	if got := normalizeSearchText(""); got != "" {
		t.Fatalf("expected '', got %q", got)
	}
}

func TestNormalizeSearchText_Parens(t *testing.T) {
	got := normalizeSearchText("Song (Remix) [Live]")
	if got != "songremixlive" {
		t.Fatalf("expected 'songremixlive', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// normalizedSQLExpr
// ---------------------------------------------------------------------------

func TestNormalizedSQLExpr_ContainsColumn(t *testing.T) {
	got := normalizedSQLExpr("title")
	if !strings.Contains(got, "title") {
		t.Fatalf("expected column name in SQL expr, got %q", got)
	}
}

func TestNormalizedSQLExpr_ContainsReplace(t *testing.T) {
	got := normalizedSQLExpr("artist")
	if !strings.Contains(got, "REPLACE") {
		t.Fatalf("expected REPLACE in SQL expr, got %q", got)
	}
}

func TestNormalizedSQLExpr_ContainsLower(t *testing.T) {
	got := normalizedSQLExpr("album")
	if !strings.Contains(got, "LOWER") {
		t.Fatalf("expected LOWER in SQL expr, got %q", got)
	}
}

func TestNormalizedSQLExpr_DifferentColumns(t *testing.T) {
	a := normalizedSQLExpr("title")
	b := normalizedSQLExpr("artist")
	if a == b {
		t.Fatalf("expected different SQL exprs for different columns")
	}
}
