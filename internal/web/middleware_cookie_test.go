package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsSecureCookieEnv_ProductionDefaultsSecure(t *testing.T) {
	t.Setenv("GRIMNIR_COOKIE_SECURE", "")
	t.Setenv("RLM_COOKIE_SECURE", "")
	t.Setenv("GRIMNIR_ENV", "production")
	t.Setenv("RLM_ENV", "")
	if !isSecureCookieEnv() {
		t.Fatalf("expected secure cookies in production env")
	}
}

func TestIsSecureCookieEnv_DevelopmentDefaultInsecure(t *testing.T) {
	t.Setenv("GRIMNIR_COOKIE_SECURE", "")
	t.Setenv("RLM_COOKIE_SECURE", "")
	t.Setenv("GRIMNIR_ENV", "development")
	t.Setenv("RLM_ENV", "")
	if isSecureCookieEnv() {
		t.Fatalf("expected insecure cookies in development env")
	}
}

func TestIsSecureCookieEnv_ExplicitOverrideWins(t *testing.T) {
	t.Setenv("GRIMNIR_ENV", "development")
	t.Setenv("GRIMNIR_COOKIE_SECURE", "true")
	if !isSecureCookieEnv() {
		t.Fatalf("expected secure cookies with explicit true override")
	}

	t.Setenv("GRIMNIR_ENV", "production")
	t.Setenv("GRIMNIR_COOKIE_SECURE", "false")
	if isSecureCookieEnv() {
		t.Fatalf("expected insecure cookies with explicit false override")
	}
}

func TestSetAndClearAuthToken_CookieAttributesMatch(t *testing.T) {
	t.Setenv("GRIMNIR_COOKIE_SECURE", "true")
	h := &Handler{}
	rr := httptest.NewRecorder()

	h.SetAuthToken(rr, "token123", 3600)
	h.ClearAuthToken(rr)

	res := rr.Result()
	cookies := res.Cookies()
	if len(cookies) < 2 {
		t.Fatalf("expected at least 2 cookies, got %d", len(cookies))
	}

	issued := cookies[0]
	cleared := cookies[1]
	if issued.Name != "grimnir_token" || cleared.Name != "grimnir_token" {
		t.Fatalf("unexpected cookie names: %q, %q", issued.Name, cleared.Name)
	}
	if !issued.HttpOnly || !cleared.HttpOnly {
		t.Fatalf("expected HttpOnly on issued and cleared cookies")
	}
	if issued.SameSite != http.SameSiteLaxMode || cleared.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected SameSite=Lax on issued and cleared cookies")
	}
	if !issued.Secure || !cleared.Secure {
		t.Fatalf("expected Secure on issued and cleared cookies")
	}
	if cleared.MaxAge >= 0 {
		t.Fatalf("expected cleared cookie MaxAge < 0, got %d", cleared.MaxAge)
	}
}

func TestAuthMiddleware_InvalidCookieUsesSecureClearingAttributes(t *testing.T) {
	t.Setenv("GRIMNIR_COOKIE_SECURE", "true")
	h := &Handler{}
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.Header.Set("Cookie", "grimnir_token=not-a-jwt")

	h.AuthMiddleware(next).ServeHTTP(rr, req)
	if !nextCalled {
		t.Fatalf("expected next handler to be called")
	}

	setCookieHeaders := rr.Header().Values("Set-Cookie")
	if len(setCookieHeaders) == 0 {
		t.Fatalf("expected clearing Set-Cookie header")
	}
	joined := strings.Join(setCookieHeaders, "\n")
	if !strings.Contains(joined, "grimnir_token=") || !strings.Contains(joined, "Max-Age=0") {
		t.Fatalf("expected grimnir_token clearing cookie, got %q", joined)
	}
	if !strings.Contains(joined, "HttpOnly") || !strings.Contains(joined, "Secure") || !strings.Contains(joined, "SameSite=Lax") {
		t.Fatalf("expected HttpOnly+Secure+SameSite=Lax on clearing cookie, got %q", joined)
	}
}
