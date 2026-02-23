package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersMiddleware_BaselineHeaders(t *testing.T) {
	h := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/public/schedule", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options=%q, want nosniff", got)
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options=%q, want DENY", got)
	}
	if got := rr.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("Referrer-Policy=%q, want strict-origin-when-cross-origin", got)
	}
	if got := rr.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatalf("expected Content-Security-Policy header")
	}
	if got := rr.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("expected no HSTS on non-HTTPS request, got %q", got)
	}
}

func TestSecurityHeadersMiddleware_SetsHSTSOnHTTPS(t *testing.T) {
	h := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Strict-Transport-Security"); got != "max-age=31536000; includeSubDomains" {
		t.Fatalf("Strict-Transport-Security=%q, want max-age=31536000; includeSubDomains", got)
	}
}

func TestSecurityHeadersMiddleware_AllowsLandingPagePreviewIframe(t *testing.T) {
	h := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []string{
		"/dashboard/station/landing-page/preview",
		"/dashboard/admin/landing-page/preview",
	}

	for _, path := range tests {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if got := rr.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
			t.Fatalf("%s X-Frame-Options=%q, want SAMEORIGIN", path, got)
		}
		if got := rr.Header().Get("Content-Security-Policy"); got == "" || !strings.Contains(got, "frame-ancestors 'self'") {
			t.Fatalf("%s Content-Security-Policy=%q, want frame-ancestors 'self'", path, got)
		}
	}
}
