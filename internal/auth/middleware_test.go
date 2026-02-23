package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMiddlewareWithJWT_AcceptsBearerToken(t *testing.T) {
	secret := []byte("test-secret")
	token, err := Issue(secret, Claims{
		UserID:    "u1",
		Roles:     []string{"admin"},
		StationID: "s1",
	}, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok || claims == nil {
			t.Fatalf("expected claims in context")
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stations", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	MiddlewareWithJWT(nil, secret)(next).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMiddlewareWithJWT_RejectsQueryToken(t *testing.T) {
	secret := []byte("test-secret")
	token, err := Issue(secret, Claims{
		UserID:    "u1",
		Roles:     []string{"admin"},
		StationID: "s1",
	}, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stations?token="+token, nil)
	rr := httptest.NewRecorder()

	MiddlewareWithJWT(nil, secret)(next).ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for query token auth, got %d", rr.Code)
	}
}

func TestMiddlewareWithJWT_AcceptsQueryTokenForEventsWebSocketUpgrade(t *testing.T) {
	secret := []byte("test-secret")
	token, err := Issue(secret, Claims{
		UserID: "u1",
		Roles:  []string{"platform_admin"},
	}, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok || claims == nil {
			t.Fatalf("expected claims in context")
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?types=now_playing&token="+token, nil)
	req.Header.Set("Upgrade", "websocket")
	rr := httptest.NewRecorder()

	MiddlewareWithJWT(nil, secret)(next).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for websocket query token auth, got %d body=%s", rr.Code, rr.Body.String())
	}
}
