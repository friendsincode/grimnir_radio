/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestMiddleware_APIKeyHeader(t *testing.T) {
	db := newAuthDB(t)
	db.Create(&models.User{ID: dbtest.UUID("u1"), PlatformRole: models.PlatformRoleAdmin, Email: "u1@t.local"})
	plaintext, _ := storeKey(t, db, dbtest.UUID("u1"), "ci", time.Hour)

	var gotUser string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if claims, ok := ClaimsFromContext(r.Context()); ok {
			gotUser = claims.UserID
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := Middleware(db)(next)

	// Valid key: handler runs with claims in context.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/thing", nil)
	req.Header.Set("X-API-Key", plaintext)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid key status = %d, want 200", rec.Code)
	}
	if gotUser != dbtest.UUID("u1") {
		t.Fatalf("claims not propagated to handler: got %q", gotUser)
	}

	// Invalid key: 401, handler not reached.
	gotUser = ""
	req = httptest.NewRequest(http.MethodGet, "/api/v1/thing", nil)
	req.Header.Set("X-API-Key", "gr_not-a-real-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid key status = %d, want 401", rec.Code)
	}
	if gotUser != "" {
		t.Fatal("handler should not run for an invalid key")
	}
}
