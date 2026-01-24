/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Context key for JWT token string
const ctxKeyToken ctxKey = "token"

// AuthMiddleware checks for valid session and injects user into context.
// For web routes, we check cookies; for API we check Authorization header.
func (h *Handler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tokenStr string

		// Check cookie first (web sessions)
		if cookie, err := r.Cookie("grimnir_token"); err == nil {
			tokenStr = cookie.Value
		}

		// Fall back to Authorization header
		if tokenStr == "" {
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				tokenStr = strings.TrimPrefix(auth, "Bearer ")
			}
		}

		if tokenStr == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Parse and validate token
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
			return h.jwtSecret, nil
		})
		if err != nil || !token.Valid {
			// Clear invalid cookie
			http.SetCookie(w, &http.Cookie{
				Name:     "grimnir_token",
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
			})
			next.ServeHTTP(w, r)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		userID, _ := claims["user_id"].(string)
		if userID == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Load user from database
		var user models.User
		if err := h.db.First(&user, "id = ?", userID).Error; err != nil {
			next.ServeHTTP(w, r)
			return
		}

		// Inject user and token into context
		ctx := context.WithValue(r.Context(), ctxKeyUser, &user)
		ctx = context.WithValue(ctx, ctxKeyToken, tokenStr)

		// Check for selected station in cookie
		if stationCookie, err := r.Cookie("grimnir_station"); err == nil {
			var station models.Station
			if err := h.db.First(&station, "id = ?", stationCookie.Value).Error; err == nil {
				ctx = context.WithValue(ctx, ctxKeyStation, &station)
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuth redirects to login if not authenticated.
func (h *Handler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := h.GetUser(r)
		if user == nil {
			// For HTMX requests, return 401 with redirect header
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login?redirect="+r.URL.Path, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole checks that user has at least the specified role.
func (h *Handler) RequireRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := h.GetUser(r)
			if user == nil {
				if r.Header.Get("HX-Request") == "true" {
					w.Header().Set("HX-Redirect", "/login")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			if !roleAtLeast(user, minRole) {
				if r.Header.Get("HX-Request") == "true" {
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte("Access denied"))
					return
				}
				http.Error(w, "Access denied", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireStation ensures a station is selected.
func (h *Handler) RequireStation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		station := h.GetStation(r)
		if station == nil {
			// Redirect to station selection
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/dashboard/stations/select")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GetUser returns the authenticated user from context.
func (h *Handler) GetUser(r *http.Request) *models.User {
	if user, ok := r.Context().Value(ctxKeyUser).(*models.User); ok {
		return user
	}
	return nil
}

// GetStation returns the selected station from context.
func (h *Handler) GetStation(r *http.Request) *models.Station {
	if station, ok := r.Context().Value(ctxKeyStation).(*models.Station); ok {
		return station
	}
	return nil
}

// LoadStations loads all stations for the current user.
func (h *Handler) LoadStations(r *http.Request) []models.Station {
	user := h.GetUser(r)
	if user == nil {
		return nil
	}

	var stations []models.Station
	query := h.db.Where("active = ?", true)

	// Non-admins might have station restrictions in the future
	// For now, all authenticated users can see all active stations
	query.Find(&stations)

	return stations
}

// SetStation sets the station cookie.
func (h *Handler) SetStation(w http.ResponseWriter, stationID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "grimnir_station",
		Value:    stationID,
		Path:     "/",
		MaxAge:   86400 * 365, // 1 year
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// SetAuthToken sets the authentication cookie.
func (h *Handler) SetAuthToken(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     "grimnir_token",
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false, // Set to true in production with HTTPS
	})
}

// ClearAuthToken removes the authentication cookie.
func (h *Handler) ClearAuthToken(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "grimnir_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

// GetAuthToken returns the raw JWT token string from context.
func (h *Handler) GetAuthToken(r *http.Request) string {
	if token, ok := r.Context().Value(ctxKeyToken).(string); ok {
		return token
	}
	return ""
}

// GenerateWSToken creates a short-lived token for WebSocket connections.
// This token is safe to expose in JavaScript as it has a short TTL.
func (h *Handler) GenerateWSToken(user *models.User) string {
	if user == nil {
		return ""
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"purpose": "websocket",
		"exp":     time.Now().Add(5 * time.Minute).Unix(),
		"iat":     time.Now().Unix(),
	})

	tokenStr, err := token.SignedString(h.jwtSecret)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to sign WS token")
		return ""
	}
	return tokenStr
}
