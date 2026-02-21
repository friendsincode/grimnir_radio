/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package auth

import (
	"net/http"
	"strings"

	"gorm.io/gorm"
)

// Middleware validates API keys and injects claims into request context.
// API keys are expected in the X-API-Key header.
func Middleware(db *gorm.DB) func(http.Handler) http.Handler {
	return MiddlewareWithJWT(db, nil)
}

// MiddlewareWithJWT validates API keys or JWT Bearer tokens.
// If jwtSecret is nil, only API keys are validated.
func MiddlewareWithJWT(db *gorm.DB, jwtSecret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check for API key in X-API-Key header
			apiKey := r.Header.Get("X-API-Key")
			if apiKey != "" {
				claims, err := ValidateAPIKey(db, apiKey)
				if err != nil {
					unauthorized(w)
					return
				}
				ctx := WithClaims(r.Context(), claims)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Check for JWT Bearer token
			if jwtSecret != nil {
				token := extractToken(r)
				if token != "" {
					claims, err := Parse(jwtSecret, token)
					if err == nil && claims != nil {
						ctx := WithClaims(r.Context(), claims)
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
				}
			}

			// No valid credentials provided
			unauthorized(w)
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
	return ""
}
