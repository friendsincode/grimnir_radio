/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newAuthDB(t *testing.T) *gorm.DB {
	t.Helper()
	return dbtest.Open(t, &models.User{}, &models.APIKey{})
}

// storeKey generates a key for a user and persists both.
func storeKey(t *testing.T, db *gorm.DB, userID, name string, expiresIn time.Duration) (string, *models.APIKey) {
	t.Helper()
	plaintext, key, err := GenerateAPIKey(userID, name, expiresIn)
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if err := db.Create(key).Error; err != nil {
		t.Fatalf("store key: %v", err)
	}
	return plaintext, key
}

func TestGenerateAPIKey_ShapeAndHash(t *testing.T) {
	plaintext, key, err := GenerateAPIKey(dbtest.UUID("u1"), "ci", 30*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if !strings.HasPrefix(plaintext, APIKeyPrefix) {
		t.Fatalf("plaintext %q missing prefix %q", plaintext, APIKeyPrefix)
	}
	if key.KeyPrefix != plaintext[:11] {
		t.Fatalf("KeyPrefix = %q, want %q", key.KeyPrefix, plaintext[:11])
	}
	if key.KeyHash == "" || key.KeyHash == plaintext {
		t.Fatal("KeyHash must be a non-plaintext hash")
	}
	if key.ExpiresAt.Before(time.Now()) {
		t.Fatal("ExpiresAt should be in the future")
	}
}

func TestValidateAPIKey_HappyPathUpdatesLastUsed(t *testing.T) {
	db := newAuthDB(t)
	db.Create(&models.User{ID: dbtest.UUID("u1"), PlatformRole: models.PlatformRoleAdmin, Email: "u1@t.local"})
	plaintext, key := storeKey(t, db, dbtest.UUID("u1"), "ci", time.Hour)

	claims, err := ValidateAPIKey(db, plaintext)
	if err != nil {
		t.Fatalf("ValidateAPIKey: %v", err)
	}
	if claims.UserID != dbtest.UUID("u1") || len(claims.Roles) != 1 || claims.Roles[0] != string(models.PlatformRoleAdmin) {
		t.Fatalf("unexpected claims: %+v", claims)
	}

	// The fire-and-forget goroutine should stamp last_used_at.
	deadline := time.Now().Add(2 * time.Second)
	for {
		var reloaded models.APIKey
		db.First(&reloaded, "id = ?", key.ID)
		if reloaded.LastUsedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("last_used_at was never stamped")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestValidateAPIKey_NotFound(t *testing.T) {
	db := newAuthDB(t)
	if _, err := ValidateAPIKey(db, "gr_deadbeef"); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("err = %v, want ErrAPIKeyNotFound", err)
	}
}

func TestValidateAPIKey_Revoked(t *testing.T) {
	db := newAuthDB(t)
	db.Create(&models.User{ID: dbtest.UUID("u1"), PlatformRole: models.PlatformRoleAdmin, Email: "u1@t.local"})
	plaintext, key := storeKey(t, db, dbtest.UUID("u1"), "ci", time.Hour)
	now := time.Now()
	db.Model(&models.APIKey{}).Where("id = ?", key.ID).Update("revoked_at", &now)

	if _, err := ValidateAPIKey(db, plaintext); !errors.Is(err, ErrAPIKeyRevoked) {
		t.Fatalf("err = %v, want ErrAPIKeyRevoked", err)
	}
}

func TestValidateAPIKey_Expired(t *testing.T) {
	db := newAuthDB(t)
	db.Create(&models.User{ID: dbtest.UUID("u1"), PlatformRole: models.PlatformRoleAdmin, Email: "u1@t.local"})
	plaintext, _ := storeKey(t, db, dbtest.UUID("u1"), "ci", -time.Hour) // already expired

	if _, err := ValidateAPIKey(db, plaintext); !errors.Is(err, ErrAPIKeyExpired) {
		t.Fatalf("err = %v, want ErrAPIKeyExpired", err)
	}
}

func TestValidateAPIKey_SuspendedUser(t *testing.T) {
	// Note: the api_keys -> users foreign key makes an orphan key (the
	// ErrUserNotFound path) impossible in Postgres, so that branch is defensive
	// only and not exercised here — sqlite let it through by ignoring the FK.
	db := newAuthDB(t)
	db.Create(&models.User{ID: dbtest.UUID("u2"), PlatformRole: models.PlatformRoleAdmin, Suspended: true, Email: "u2@t.local"})
	plaintextSusp, _ := storeKey(t, db, dbtest.UUID("u2"), "ci", time.Hour)
	if _, err := ValidateAPIKey(db, plaintextSusp); err == nil || !strings.Contains(err.Error(), "suspended") {
		t.Fatalf("suspended user err = %v, want suspended", err)
	}
}

func TestRevokeAPIKey(t *testing.T) {
	db := newAuthDB(t)
	db.Create(&models.User{ID: dbtest.UUID("u1"), PlatformRole: models.PlatformRoleAdmin, Email: "u1@t.local"})
	_, key := storeKey(t, db, dbtest.UUID("u1"), "ci", time.Hour)

	if err := RevokeAPIKey(db, key.ID, dbtest.UUID("u1")); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}
	var reloaded models.APIKey
	db.First(&reloaded, "id = ?", key.ID)
	if reloaded.RevokedAt == nil {
		t.Fatal("revoked_at not set")
	}

	// Wrong owner and unknown id both report not-found.
	if err := RevokeAPIKey(db, key.ID, dbtest.UUID("someone-else")); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("revoke wrong owner err = %v, want ErrAPIKeyNotFound", err)
	}
	if err := RevokeAPIKey(db, dbtest.UUID("no-such-id"), dbtest.UUID("u1")); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("revoke unknown err = %v, want ErrAPIKeyNotFound", err)
	}
}

func TestListAndDeleteAPIKeys(t *testing.T) {
	db := newAuthDB(t)
	db.Create(&models.User{ID: dbtest.UUID("u1"), PlatformRole: models.PlatformRoleAdmin, Email: "u1@t.local"})
	db.Create(&models.User{ID: dbtest.UUID("other"), PlatformRole: models.PlatformRoleAdmin, Email: "other@t.local"})
	_, k1 := storeKey(t, db, dbtest.UUID("u1"), "first", time.Hour)
	storeKey(t, db, dbtest.UUID("u1"), "second", time.Hour)
	storeKey(t, db, dbtest.UUID("other"), "theirs", time.Hour)

	keys, err := ListAPIKeys(db, dbtest.UUID("u1"))
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("ListAPIKeys returned %d, want 2 (only u1's)", len(keys))
	}

	if err := DeleteAPIKey(db, k1.ID, dbtest.UUID("u1")); err != nil {
		t.Fatalf("DeleteAPIKey: %v", err)
	}
	if err := DeleteAPIKey(db, k1.ID, dbtest.UUID("u1")); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("second delete err = %v, want ErrAPIKeyNotFound", err)
	}
	keys, _ = ListAPIKeys(db, dbtest.UUID("u1"))
	if len(keys) != 1 {
		t.Fatalf("after delete, u1 has %d keys, want 1", len(keys))
	}
}
