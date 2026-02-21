package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestParse_ValidHS256(t *testing.T) {
	secret := []byte("test-secret")
	token, err := Issue(secret, Claims{
		UserID:    "u1",
		Roles:     []string{"admin"},
		StationID: "s1",
	}, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	claims, err := Parse(secret, token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.UserID != "u1" {
		t.Fatalf("expected user id u1, got %q", claims.UserID)
	}
}

func TestParse_RejectsUnexpectedAlgorithm(t *testing.T) {
	secret := []byte("test-secret")
	now := time.Now()
	claims := Claims{
		UserID:    "u1",
		Roles:     []string{"admin"},
		StationID: "s1",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Subject:   "u1",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS384, claims)
	tokenStr, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}

	if _, err := Parse(secret, tokenStr); err == nil {
		t.Fatalf("expected parse to reject non-HS256 token")
	}
}
