/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import "time"

// NotificationType defines the type of notification.
type NotificationType string

const (
	NotificationTypeShowReminder      NotificationType = "show_reminder"      // Reminder before show starts
	NotificationTypeScheduleChange    NotificationType = "schedule_change"    // Schedule was modified
	NotificationTypeRequestStatus     NotificationType = "request_status"     // Request approved/rejected
	NotificationTypeNewAssignment     NotificationType = "new_assignment"     // Assigned to cover a show
	NotificationTypeSchedulePublished NotificationType = "schedule_published" // New schedule available
	NotificationTypeShowCancelled     NotificationType = "show_cancelled"     // Show was cancelled
)

// NotificationChannel defines the delivery channel.
type NotificationChannel string

const (
	NotificationChannelEmail NotificationChannel = "email"
	NotificationChannelSMS   NotificationChannel = "sms"
	NotificationChannelPush  NotificationChannel = "push"
	NotificationChannelInApp NotificationChannel = "in_app"
)

// NotificationStatus defines the delivery status.
type NotificationStatus string

const (
	NotificationStatusPending NotificationStatus = "pending"
	NotificationStatusSent    NotificationStatus = "sent"
	NotificationStatusFailed  NotificationStatus = "failed"
	NotificationStatusRead    NotificationStatus = "read" // For in-app notifications
)

// NotificationPreference stores user notification settings.
type NotificationPreference struct {
	ID               string              `gorm:"type:uuid;primaryKey" json:"id"`
	UserID           string              `gorm:"type:uuid;index:idx_notification_prefs_user;not null" json:"user_id"`
	NotificationType NotificationType    `gorm:"type:varchar(64);not null" json:"notification_type"`
	Channel          NotificationChannel `gorm:"type:varchar(32);not null" json:"channel"`
	Enabled          bool                `gorm:"not null;default:true" json:"enabled"`

	// Channel-specific config (e.g., reminder_minutes for show_reminder)
	Config map[string]any `gorm:"type:jsonb;serializer:json" json:"config,omitempty"`

	// Relationships
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for GORM.
func (NotificationPreference) TableName() string {
	return "notification_preferences"
}

// Notification stores a notification log entry.
type Notification struct {
	ID               string              `gorm:"type:uuid;primaryKey" json:"id"`
	UserID           string              `gorm:"type:uuid;index:idx_notifications_user;not null" json:"user_id"`
	NotificationType NotificationType    `gorm:"type:varchar(64);index:idx_notifications_type;not null" json:"notification_type"`
	Channel          NotificationChannel `gorm:"type:varchar(32);not null" json:"channel"`
	Subject          string              `gorm:"type:varchar(255)" json:"subject,omitempty"`
	Body             string              `gorm:"type:text;not null" json:"body"`
	Status           NotificationStatus  `gorm:"type:varchar(32);not null;default:'pending';index:idx_notifications_status" json:"status"`
	SentAt           *time.Time          `json:"sent_at,omitempty"`
	ReadAt           *time.Time          `json:"read_at,omitempty"`
	Error            string              `gorm:"type:text" json:"error,omitempty"`

	// Reference to related entity (show instance, request, etc.)
	ReferenceType string `gorm:"type:varchar(64)" json:"reference_type,omitempty"`
	ReferenceID   string `gorm:"type:uuid" json:"reference_id,omitempty"`

	// Additional metadata
	Metadata map[string]any `gorm:"type:jsonb;serializer:json" json:"metadata,omitempty"`

	// Relationships
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// TableName returns the table name for GORM.
func (Notification) TableName() string {
	return "notifications"
}

// DefaultNotificationPreferences returns the default preferences for a new user.
func DefaultNotificationPreferences(userID string) []NotificationPreference {
	return []NotificationPreference{
		{
			UserID:           userID,
			NotificationType: NotificationTypeShowReminder,
			Channel:          NotificationChannelEmail,
			Enabled:          true,
			Config:           map[string]any{"reminder_minutes": 30},
		},
		{
			UserID:           userID,
			NotificationType: NotificationTypeScheduleChange,
			Channel:          NotificationChannelEmail,
			Enabled:          true,
		},
		{
			UserID:           userID,
			NotificationType: NotificationTypeRequestStatus,
			Channel:          NotificationChannelEmail,
			Enabled:          true,
		},
		{
			UserID:           userID,
			NotificationType: NotificationTypeNewAssignment,
			Channel:          NotificationChannelEmail,
			Enabled:          true,
		},
		{
			UserID:           userID,
			NotificationType: NotificationTypeShowReminder,
			Channel:          NotificationChannelInApp,
			Enabled:          true,
			Config:           map[string]any{"reminder_minutes": 15},
		},
		{
			UserID:           userID,
			NotificationType: NotificationTypeScheduleChange,
			Channel:          NotificationChannelInApp,
			Enabled:          true,
		},
		{
			UserID:           userID,
			NotificationType: NotificationTypeRequestStatus,
			Channel:          NotificationChannelInApp,
			Enabled:          true,
		},
	}
}
