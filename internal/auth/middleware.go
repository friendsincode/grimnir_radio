/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package auth

import (
	"net/http"
	"strings"
)

// Middleware validates bearer tokens and injects claims into request context.
func Middleware(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				unauthorized(w)
				return
			}

			claims, err := Parse(secret, token)
			if err != nil {
				unauthorized(w)
				return
			}

			ctx := WithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}

func extractToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header != "" {
		parts := strings.SplitN(header, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
	}

	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}

	return ""
}
