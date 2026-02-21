/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Context key for JWT token string
const ctxKeyToken ctxKey = "token"

const csrfCookieName = "grimnir_csrf"

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
			if t.Method == nil || t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, jwt.ErrTokenSignatureInvalid
			}
			return h.jwtSecret, nil
		})
		if err != nil || !token.Valid {
			// Clear invalid cookie with same security attributes used on issue.
			h.ClearAuthToken(w)
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

// CSRFMiddleware enforces same-origin checks for state-changing dashboard requests.
// This protects cookie-authenticated endpoints without requiring per-form tokens.
func (h *Handler) CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
			next.ServeHTTP(w, r)
			return
		}

		// Only enforce for authenticated web sessions.
		if h.GetUser(r) == nil {
			next.ServeHTTP(w, r)
			return
		}

		if !isSameOriginRequest(r) {
			http.Error(w, "CSRF validation failed", http.StatusForbidden)
			return
		}
		cookie, err := r.Cookie(csrfCookieName)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			http.Error(w, "CSRF validation failed", http.StatusForbidden)
			return
		}
		supplied := strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
		if supplied == "" {
			_ = r.ParseForm()
			supplied = strings.TrimSpace(r.FormValue("csrf_token"))
			if supplied == "" {
				supplied = strings.TrimSpace(r.FormValue("_csrf"))
			}
		}
		if supplied == "" || !csrfTokensEqual(cookie.Value, supplied) {
			http.Error(w, "CSRF validation failed", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func csrfTokensEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func isSameOriginRequest(r *http.Request) bool {
	reqScheme := requestScheme(r)
	reqHost := normalizeHostForCompare(r.Host, reqScheme)

	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return normalizeHostForCompare(u.Host, u.Scheme) == reqHost
	}

	ref := strings.TrimSpace(r.Referer())
	if ref == "" {
		return false
	}
	u, err := url.Parse(ref)
	if err != nil {
		return false
	}
	return normalizeHostForCompare(u.Host, u.Scheme) == reqHost
}

func normalizeHostForCompare(host, scheme string) string {
	// Parse host with a dummy scheme to robustly split hostname and optional port.
	u, err := url.Parse("http://" + host)
	if err != nil {
		return strings.ToLower(host)
	}

	hostname := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		port = defaultPortForScheme(scheme)
	}
	return hostname + ":" + port
}

func requestScheme(r *http.Request) string {
	if xfProto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); xfProto != "" {
		return strings.ToLower(strings.Split(xfProto, ",")[0])
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func defaultPortForScheme(scheme string) string {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "https":
		return "443"
	default:
		return "80"
	}
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

// RequireStation ensures a station is selected and the user has access to it.
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

		// Verify user has access to this station
		user := h.GetUser(r)
		if user != nil && !h.HasStationAccess(user, station.ID) {
			h.logger.Warn().
				Str("user_id", user.ID).
				Str("station_id", station.ID).
				Msg("user attempted to access unauthorized station")

			// Clear the invalid station cookie and redirect
			h.SetStation(w, "")
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/dashboard/stations/select")
				w.WriteHeader(http.StatusForbidden)
				return
			}
			http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequirePlatformAdmin checks that user is a platform admin.
func (h *Handler) RequirePlatformAdmin(next http.Handler) http.Handler {
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

		if !user.IsPlatformAdmin() {
			if r.Header.Get("HX-Request") == "true" {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte("Platform admin access required"))
				return
			}
			http.Error(w, "Platform admin access required", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireStationPermission creates middleware that checks for a specific station permission.
func (h *Handler) RequireStationPermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !h.HasStationPermission(r, permission) {
				http.Error(w, "Permission denied", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
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

// LoadStations loads stations the current user has access to.
func (h *Handler) LoadStations(r *http.Request) []models.Station {
	user := h.GetUser(r)
	if user == nil {
		return nil
	}

	var stations []models.Station

	// Platform admins can see all active stations
	if user.IsPlatformAdmin() {
		h.db.Where("active = ?", true).Find(&stations)
		return stations
	}

	// Regular users can only see stations they're associated with
	// This includes stations they own or are members of
	h.db.Joins("JOIN station_users ON station_users.station_id = stations.id").
		Where("station_users.user_id = ? AND stations.active = ?", user.ID, true).
		Find(&stations)

	return stations
}

// HasStationAccess checks if the user has access to the specified station.
func (h *Handler) HasStationAccess(user *models.User, stationID string) bool {
	if user == nil {
		return false
	}

	// Platform admins have access to all stations
	if user.IsPlatformAdmin() {
		return true
	}

	// Check if user has a station association
	var count int64
	h.db.Model(&models.StationUser{}).
		Where("user_id = ? AND station_id = ?", user.ID, stationID).
		Count(&count)

	return count > 0
}

// GetStationRole returns the user's role in the specified station.
func (h *Handler) GetStationRole(user *models.User, stationID string) *models.StationUser {
	if user == nil {
		return nil
	}

	var stationUser models.StationUser
	if err := h.db.Where("user_id = ? AND station_id = ?", user.ID, stationID).First(&stationUser).Error; err != nil {
		return nil
	}

	return &stationUser
}

// HasStationPermission checks if the user has a specific permission in the current station.
func (h *Handler) HasStationPermission(r *http.Request, permission string) bool {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if user == nil || station == nil {
		return false
	}

	// Platform admins have all permissions
	if user.IsPlatformAdmin() {
		return true
	}

	stationUser := h.GetStationRole(user, station.ID)
	if stationUser == nil {
		return false
	}

	perms := stationUser.GetEffectivePermissions()

	switch permission {
	case "upload_media":
		return perms.CanUploadMedia
	case "delete_media":
		return perms.CanDeleteMedia
	case "edit_metadata":
		return perms.CanEditMetadata
	case "manage_playlists":
		return perms.CanManagePlaylists
	case "manage_smart_blocks":
		return perms.CanManageSmartBlocks
	case "manage_schedule":
		return perms.CanManageSchedule
	case "manage_clocks":
		return perms.CanManageClocks
	case "go_live":
		return perms.CanGoLive
	case "kick_dj":
		return perms.CanKickDJ
	case "manage_users":
		return perms.CanManageUsers
	case "manage_settings":
		return perms.CanManageSettings
	case "view_analytics":
		return perms.CanViewAnalytics
	case "manage_mounts":
		return perms.CanManageMounts
	default:
		return false
	}
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
		Secure:   isSecureCookieEnv(),
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
		Secure:   isSecureCookieEnv(),
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
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureCookieEnv(),
	})
}

func isSecureCookieEnv() bool {
	if v, ok := parseOptionalBoolEnv("GRIMNIR_COOKIE_SECURE"); ok {
		return v
	}
	if v, ok := parseOptionalBoolEnv("RLM_COOKIE_SECURE"); ok {
		return v
	}

	env := strings.ToLower(strings.TrimSpace(os.Getenv("GRIMNIR_ENV")))
	if env == "" {
		env = strings.ToLower(strings.TrimSpace(os.Getenv("RLM_ENV")))
	}
	return env == "production" || env == "prod"
}

func parseOptionalBoolEnv(key string) (bool, bool) {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch raw {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request) string {
	if r != nil {
		if c, err := r.Cookie(csrfCookieName); err == nil {
			token := strings.TrimSpace(c.Value)
			if token != "" {
				return token
			}
		}
	}

	token := generateCSRFToken()
	if token == "" {
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   86400 * 365,
		HttpOnly: false, // JS reads token to set headers for HTMX/fetch.
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureCookieEnv(),
	})
	return token
}

func generateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
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
// Uses the same Claims structure as the API auth middleware expects.
func (h *Handler) GenerateWSToken(user *models.User) string {
	if user == nil {
		return ""
	}

	// Use the same claim structure as auth.Claims for API compatibility
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid":   user.ID,
		"roles": []string{string(user.PlatformRole)},
		"exp":   time.Now().Add(5 * time.Minute).Unix(),
		"iat":   time.Now().Unix(),
		"sub":   user.ID,
	})

	tokenStr, err := token.SignedString(h.jwtSecret)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to sign WS token")
		return ""
	}
	return tokenStr
}
