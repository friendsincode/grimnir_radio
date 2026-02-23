/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package auth

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims extends standard registered claims with role and station.
type Claims struct {
	UserID    string   `json:"uid"`
	Roles     []string `json:"roles"`
	StationID string   `json:"station_id"`
	jwt.RegisteredClaims
}

// Issue creates an HS256 JWT token string.
func Issue(secret []byte, claims Claims, ttl time.Duration) (string, error) {
	claims.RegisteredClaims = jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		Subject:   claims.UserID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

// Parse validates token string and enforces HS256 signing method.
func Parse(secret []byte, token string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if t.Method == nil || t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, jwt.ErrTokenInvalidClaims
	}
	claims.Roles = normalizeClaimRoles(claims.Roles, claims.StationID)

	return claims, nil
}

func normalizeClaimRoles(roles []string, stationID string) []string {
	if len(roles) == 0 {
		return roles
	}
	out := make([]string, 0, len(roles))
	for _, role := range roles {
		r := strings.ToLower(strings.TrimSpace(role))
		switch r {
		case "admin":
			// Legacy compatibility: unscoped admin claim means platform admin.
			if stationID == "" {
				out = append(out, "platform_admin")
			} else {
				out = append(out, "admin")
			}
		case "manager":
			// Legacy compatibility: unscoped manager claim means platform moderator.
			if stationID == "" {
				out = append(out, "platform_mod")
			} else {
				out = append(out, "manager")
			}
		case "mod", "moderator":
			out = append(out, "platform_mod")
		default:
			out = append(out, r)
		}
	}
	return out
}
