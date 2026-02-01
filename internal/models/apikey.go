/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import "time"

// APIKey represents an API key for programmatic access.
type APIKey struct {
	ID         string     `gorm:"type:uuid;primaryKey" json:"id"`
	UserID     string     `gorm:"type:uuid;index;not null" json:"user_id"`
	User       User       `gorm:"foreignKey:UserID" json:"-"`
	Name       string     `gorm:"not null" json:"name"`
	KeyHash    string     `gorm:"not null" json:"-"`
	KeyPrefix  string     `gorm:"size:11" json:"key_prefix"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  time.Time  `gorm:"not null" json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// IsExpired returns true if the API key has expired.
func (k *APIKey) IsExpired() bool {
	return time.Now().After(k.ExpiresAt)
}

// IsRevoked returns true if the API key has been revoked.
func (k *APIKey) IsRevoked() bool {
	return k.RevokedAt != nil
}

// IsValid returns true if the API key is neither expired nor revoked.
func (k *APIKey) IsValid() bool {
	return !k.IsExpired() && !k.IsRevoked()
}
