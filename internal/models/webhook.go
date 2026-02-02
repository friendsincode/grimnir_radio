/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"time"

	"github.com/google/uuid"
)

// WebhookEventType defines types of webhook events.
type WebhookEventType string

const (
	WebhookEventShowStart WebhookEventType = "show_start"
	WebhookEventShowEnd   WebhookEventType = "show_end"
)

// WebhookTarget stores webhook configuration for a station.
type WebhookTarget struct {
	ID        string `gorm:"type:uuid;primaryKey" json:"id"`
	StationID string `gorm:"type:uuid;index;not null" json:"station_id"`
	URL       string `gorm:"type:varchar(512);not null" json:"url"`
	Events    string `gorm:"type:varchar(255)" json:"events"` // comma-separated: show_start,show_end
	Secret    string `gorm:"type:varchar(255)" json:"-"`      // for HMAC signing
	Active    bool   `gorm:"not null;default:true" json:"active"`

	// Relationships
	Station *Station `gorm:"foreignKey:StationID" json:"station,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for GORM.
func (WebhookTarget) TableName() string {
	return "webhook_targets"
}

// NewWebhookTarget creates a new webhook target with a random secret.
func NewWebhookTarget(stationID, url, events string) *WebhookTarget {
	return &WebhookTarget{
		ID:        uuid.NewString(),
		StationID: stationID,
		URL:       url,
		Events:    events,
		Secret:    uuid.NewString(),
		Active:    true,
	}
}

// WebhookLog records webhook delivery attempts.
type WebhookLog struct {
	ID         string    `gorm:"type:uuid;primaryKey" json:"id"`
	TargetID   string    `gorm:"type:uuid;index;not null" json:"target_id"`
	Event      string    `gorm:"type:varchar(64);not null" json:"event"`
	Payload    string    `gorm:"type:text;not null" json:"payload"`
	StatusCode int       `json:"status_code"`
	Response   string    `gorm:"type:text" json:"response,omitempty"`
	Error      string    `gorm:"type:text" json:"error,omitempty"`
	Duration   int       `json:"duration_ms"` // Response time in milliseconds
	CreatedAt  time.Time `json:"created_at"`
}

// TableName returns the table name for GORM.
func (WebhookLog) TableName() string {
	return "webhook_logs"
}
