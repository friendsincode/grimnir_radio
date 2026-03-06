package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCoverageProfile(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "coverage.out")
	if err := os.WriteFile(profile, []byte("mode: atomic\nfile1.go:1.1,2.2 2 1\nfile2.go:1.1,2.2 3 0\nfile3.go:1.1,2.2 5 4\n"), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	got, err := parseCoverageProfile(profile)
	if err != nil {
		t.Fatalf("parse profile: %v", err)
	}
	if got != 70 {
		t.Fatalf("expected 70, got %.1f", got)
	}
}

func TestCoverageBadgeHandlers(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "coverage.out")
	if err := os.WriteFile(profile, []byte("mode: atomic\nfile1.go:1.1,2.2 4 1\nfile2.go:1.1,2.2 6 0\n"), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	t.Setenv("GRIMNIR_COVERAGE_PROFILE", profile)
	t.Setenv("GRIMNIR_COVERAGE_TARGET", "80")

	a := &API{}

	t.Run("badge returns shields payload", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/public/coverage/badge", nil)
		a.handleCoverageBadge(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		body := rr.Body.String()
		for _, want := range []string{`"label":"coverage"`, `"message":"40.0%"`, `"color":"c0392b"`} {
			if !strings.Contains(body, want) {
				t.Fatalf("expected badge body to contain %q, got %s", want, body)
			}
		}
	})

	t.Run("summary returns machine-readable coverage", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/public/coverage", nil)
		a.handleCoverageSummary(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		body := rr.Body.String()
		for _, want := range []string{`"percent":40`, `"target":80`, `"available":true`} {
			if !strings.Contains(body, want) {
				t.Fatalf("expected summary body to contain %q, got %s", want, body)
			}
		}
	})

	t.Run("target badge returns static target payload", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/public/coverage/target-badge", nil)
		a.handleCoverageTargetBadge(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		body := rr.Body.String()
		for _, want := range []string{`"label":"coverage target"`, `"message":"80%"`, `"color":"0a7f5a"`} {
			if !strings.Contains(body, want) {
				t.Fatalf("expected target badge body to contain %q, got %s", want, body)
			}
		}
	})
}

func TestCoverageBadgeUnavailableWithoutProfile(t *testing.T) {
	t.Setenv("GRIMNIR_COVERAGE_PROFILE", filepath.Join(t.TempDir(), "missing.out"))
	a := &API{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/coverage/badge", nil)
	a.handleCoverageBadge(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"message":"unavailable"`) {
		t.Fatalf("unexpected unavailable body: %s", rr.Body.String())
	}
}
