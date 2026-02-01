/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// API key constants
const (
	APIKeyPrefix      = "gr_"
	APIKeyRandomBytes = 24 // 24 bytes = 32 base64 chars ≈ 192 bits entropy
)

// Expiration options for API keys
var APIKeyExpirationOptions = []struct {
	Label string
	Days  int
}{
	{"30 days", 30},
	{"90 days", 90},
	{"180 days", 180},
	{"1 year", 365},
}

// ErrAPIKeyNotFound is returned when an API key doesn't exist.
var ErrAPIKeyNotFound = errors.New("api key not found")

// ErrAPIKeyExpired is returned when an API key has expired.
var ErrAPIKeyExpired = errors.New("api key expired")

// ErrAPIKeyRevoked is returned when an API key has been revoked.
var ErrAPIKeyRevoked = errors.New("api key revoked")

// ErrUserNotFound is returned when the user for an API key doesn't exist.
var ErrUserNotFound = errors.New("user not found")

// GenerateAPIKey creates a new API key for a user.
// Returns the plaintext key (to show to user once) and the model to store.
func GenerateAPIKey(userID, name string, expiresIn time.Duration) (string, *models.APIKey, error) {
	// Generate random bytes
	randomBytes := make([]byte, APIKeyRandomBytes)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", nil, err
	}

	// Create the key: gr_<hex encoded random bytes>
	randomHex := hex.EncodeToString(randomBytes)
	plaintextKey := APIKeyPrefix + randomHex

	// Hash the key for storage
	hash := sha256.Sum256([]byte(plaintextKey))
	keyHash := hex.EncodeToString(hash[:])

	// Create prefix for display (gr_xxxxxxxx)
	keyPrefix := plaintextKey[:11] // "gr_" + first 8 hex chars

	apiKey := &models.APIKey{
		ID:        uuid.NewString(),
		UserID:    userID,
		Name:      name,
		KeyHash:   keyHash,
		KeyPrefix: keyPrefix,
		ExpiresAt: time.Now().Add(expiresIn),
	}

	return plaintextKey, apiKey, nil
}

// ValidateAPIKey validates an API key and returns claims if valid.
// Also updates the LastUsedAt timestamp.
func ValidateAPIKey(db *gorm.DB, plaintextKey string) (*Claims, error) {
	// Hash the provided key
	hash := sha256.Sum256([]byte(plaintextKey))
	keyHash := hex.EncodeToString(hash[:])

	// Look up the key
	var apiKey models.APIKey
	result := db.Where("key_hash = ?", keyHash).First(&apiKey)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrAPIKeyNotFound
	}
	if result.Error != nil {
		return nil, result.Error
	}

	// Check if revoked
	if apiKey.IsRevoked() {
		return nil, ErrAPIKeyRevoked
	}

	// Check if expired
	if apiKey.IsExpired() {
		return nil, ErrAPIKeyExpired
	}

	// Load the user
	var user models.User
	result = db.First(&user, "id = ?", apiKey.UserID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if result.Error != nil {
		return nil, result.Error
	}

	// Check if user is suspended
	if user.Suspended {
		return nil, errors.New("user account suspended")
	}

	// Update last used timestamp (fire and forget)
	now := time.Now()
	go db.Model(&apiKey).Update("last_used_at", now)

	// Build claims
	claims := &Claims{
		UserID: user.ID,
		Roles:  []string{string(user.PlatformRole)},
	}

	return claims, nil
}

// RevokeAPIKey revokes an API key. Only the owner can revoke their own keys.
func RevokeAPIKey(db *gorm.DB, keyID, userID string) error {
	now := time.Now()
	result := db.Model(&models.APIKey{}).
		Where("id = ? AND user_id = ?", keyID, userID).
		Update("revoked_at", now)

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrAPIKeyNotFound
	}

	return nil
}

// ListAPIKeys returns all API keys for a user (without the hash).
func ListAPIKeys(db *gorm.DB, userID string) ([]models.APIKey, error) {
	var keys []models.APIKey
	err := db.Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&keys).Error

	return keys, err
}

// DeleteAPIKey permanently deletes an API key. Use RevokeAPIKey for soft delete.
func DeleteAPIKey(db *gorm.DB, keyID, userID string) error {
	result := db.Where("id = ? AND user_id = ?", keyID, userID).
		Delete(&models.APIKey{})

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrAPIKeyNotFound
	}

	return nil
}
