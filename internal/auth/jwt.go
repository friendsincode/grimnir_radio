/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package auth

import (
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

// Issue creates JWT token string.
func Issue(secret []byte, claims Claims, ttl time.Duration) (string, error) {
	claims.RegisteredClaims = jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		Subject:   claims.UserID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

// Parse validates token string.
func Parse(secret []byte, token string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		return secret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, jwt.ErrTokenInvalidClaims
	}

	return claims, nil
}
