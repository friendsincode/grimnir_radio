/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoverageColor_AllBranches(t *testing.T) {
	cases := []struct {
		percent float64
		target  float64
		want    string
	}{
		{80.0, 80.0, "0a7f5a"}, // exactly at target → green
		{90.0, 80.0, "0a7f5a"}, // above target → green
		{65.0, 80.0, "dfb317"}, // 65/80 = 81.25% → yellow (>= 75%)
		{60.0, 80.0, "dfb317"}, // 60/80 = 75% → yellow (exactly 75%)
		{50.0, 80.0, "c0392b"}, // below 75% → red
	}

	for _, c := range cases {
		got := coverageColor(c.percent, c.target)
		if got != c.want {
			t.Errorf("coverageColor(%.1f, %.1f) = %q, want %q", c.percent, c.target, got, c.want)
		}
	}
}

func TestCoverageTarget_Branches(t *testing.T) {
	// Empty env → 80
	t.Setenv("GRIMNIR_COVERAGE_TARGET", "")
	if got := coverageTarget(); got != 80 {
		t.Errorf("empty env: got %.1f, want 80", got)
	}

	// Zero value → 80
	t.Setenv("GRIMNIR_COVERAGE_TARGET", "0")
	if got := coverageTarget(); got != 80 {
		t.Errorf("zero: got %.1f, want 80", got)
	}

	// Negative → 80
	t.Setenv("GRIMNIR_COVERAGE_TARGET", "-10")
	if got := coverageTarget(); got != 80 {
		t.Errorf("negative: got %.1f, want 80", got)
	}

	// Valid → use it
	t.Setenv("GRIMNIR_COVERAGE_TARGET", "75")
	if got := coverageTarget(); got != 75 {
		t.Errorf("valid 75: got %.1f, want 75", got)
	}
}

func TestCoverageProfilePath_Branches(t *testing.T) {
	// Custom path
	t.Setenv("GRIMNIR_COVERAGE_PROFILE", "/custom/path/cov.out")
	got := coverageProfilePath()
	if got != "/custom/path/cov.out" {
		t.Errorf("custom path: got %q", got)
	}

	// Whitespace-trimmed custom path
	t.Setenv("GRIMNIR_COVERAGE_PROFILE", "  /trimmed/cov.out  ")
	got = coverageProfilePath()
	if got != "/trimmed/cov.out" {
		t.Errorf("trimmed path: got %q", got)
	}

	// Empty (whitespace only) → default
	t.Setenv("GRIMNIR_COVERAGE_PROFILE", "   ")
	got = coverageProfilePath()
	if strings.Contains(got, "GRIMNIR") {
		t.Errorf("expected default, got %q", got)
	}
}

func TestParseCoverageProfile_ErrorCases(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		_, err := parseCoverageProfile("/nonexistent/path.out")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid header", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "bad.out")
		os.WriteFile(f, []byte("NOTAHEADER\nfile.go:1.1,2.2 5 3\n"), 0644) //nolint:errcheck
		_, err := parseCoverageProfile(f)
		if err == nil {
			t.Fatal("expected error for invalid header")
		}
	})

	t.Run("no statements", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "empty.out")
		os.WriteFile(f, []byte("mode: count\n"), 0644) //nolint:errcheck
		_, err := parseCoverageProfile(f)
		if err == nil {
			t.Fatal("expected error for zero statements")
		}
	})
}

func TestHandleCoverageSummary_NoProfile(t *testing.T) {
	a := &API{}
	t.Setenv("GRIMNIR_COVERAGE_PROFILE", filepath.Join(t.TempDir(), "missing.out"))

	req := httptest.NewRequest(http.MethodGet, "/coverage/summary", nil)
	rr := httptest.NewRecorder()
	a.handleCoverageSummary(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"available":false`) {
		t.Fatalf("expected available=false, got: %s", body)
	}
}

func TestHandleCoverageSummary_WithProfile(t *testing.T) {
	a := &API{}
	content := "mode: count\nfile.go:1.1,2.2 10 10\n"
	f := filepath.Join(t.TempDir(), "cov.out")
	os.WriteFile(f, []byte(content), 0644) //nolint:errcheck
	t.Setenv("GRIMNIR_COVERAGE_PROFILE", f)

	req := httptest.NewRequest(http.MethodGet, "/coverage/summary", nil)
	rr := httptest.NewRecorder()
	a.handleCoverageSummary(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"available":true`) {
		t.Fatalf("expected available=true, got: %s", body)
	}
}
