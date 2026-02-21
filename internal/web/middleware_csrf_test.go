package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestCSRFMiddleware_AllowsValidMutatingRequest(t *testing.T) {
	h := &Handler{}
	token := "csrf-token-123"

	req := httptest.NewRequest(http.MethodPost, "http://example.com/dashboard/test", strings.NewReader("name=value"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("X-CSRF-Token", token)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &models.User{ID: "u1"}))

	rr := httptest.NewRecorder()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	h.CSRFMiddleware(next).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("expected next handler to be called")
	}
}

func TestCSRFMiddleware_RejectsMissingToken(t *testing.T) {
	h := &Handler{}
	token := "csrf-token-123"

	req := httptest.NewRequest(http.MethodPost, "http://example.com/dashboard/test", nil)
	req.Header.Set("Origin", "http://example.com")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &models.User{ID: "u1"}))

	rr := httptest.NewRecorder()
	h.CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestCSRFMiddleware_RejectsMismatchedToken(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodDelete, "http://example.com/dashboard/test", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("X-CSRF-Token", "token-a")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "token-b"})
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &models.User{ID: "u1"}))

	rr := httptest.NewRecorder()
	h.CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestEnsureCSRFCookie_ReusesExistingToken(t *testing.T) {
	t.Setenv("GRIMNIR_COOKIE_SECURE", "true")
	req := httptest.NewRequest(http.MethodGet, "http://example.com/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "existing-token"})
	rr := httptest.NewRecorder()

	token := ensureCSRFCookie(rr, req)
	if token != "existing-token" {
		t.Fatalf("expected existing token, got %q", token)
	}
	if got := rr.Header().Get("Set-Cookie"); got != "" {
		t.Fatalf("expected no Set-Cookie when token exists, got %q", got)
	}
}
