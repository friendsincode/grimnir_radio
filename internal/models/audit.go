/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import "time"

// AuditAction defines the type of audited action.
type AuditAction string

// Audit action constants for all sensitive operations.
const (
	AuditActionUserRoleChange    AuditAction = "user.role_change"
	AuditActionUserSuspend       AuditAction = "user.suspend"
	AuditActionUserDelete        AuditAction = "user.delete"
	AuditActionAPIKeyCreate      AuditAction = "apikey.create"
	AuditActionAPIKeyRevoke      AuditAction = "apikey.revoke"
	AuditActionPriorityEmergency AuditAction = "priority.emergency"
	AuditActionPriorityOverride  AuditAction = "priority.override"
	AuditActionPriorityRelease   AuditAction = "priority.release"
	AuditActionLiveConnect       AuditAction = "live.connect"
	AuditActionLiveDisconnect    AuditAction = "live.disconnect"
	AuditActionLiveHandover      AuditAction = "live.handover"
	AuditActionScheduleRefresh   AuditAction = "schedule.refresh"
	AuditActionScheduleUpdate    AuditAction = "schedule.update"
	AuditActionStationCreate     AuditAction = "station.create"
	AuditActionStationUpdate     AuditAction = "station.update"
	AuditActionStationDelete     AuditAction = "station.delete"
	AuditActionMountCreate       AuditAction = "mount.create"
	AuditActionMountUpdate       AuditAction = "mount.update"
	AuditActionWebstreamCreate   AuditAction = "webstream.create"
	AuditActionWebstreamUpdate   AuditAction = "webstream.update"
	AuditActionWebstreamDelete   AuditAction = "webstream.delete"
	AuditActionWebstreamFailover AuditAction = "webstream.failover"
)

// AuditLog records sensitive operations for security and compliance.
type AuditLog struct {
	ID           string         `gorm:"type:uuid;primaryKey"`
	Timestamp    time.Time      `gorm:"index:idx_audit_timestamp;not null"`
	UserID       *string        `gorm:"type:uuid;index:idx_audit_user"`    // NULL for system actions
	UserEmail    string         `gorm:"type:varchar(255)"`                 // Denormalized for readability
	StationID    *string        `gorm:"type:uuid;index:idx_audit_station"` // NULL if platform-wide
	Action       AuditAction    `gorm:"type:varchar(64);index:idx_audit_action;not null"`
	ResourceType string         `gorm:"type:varchar(64)"`  // "user", "apikey", "station", etc.
	ResourceID   string         `gorm:"type:uuid"`         // ID of the affected resource
	Details      map[string]any `gorm:"type:jsonb;serializer:json"` // Action-specific details
	IPAddress    string         `gorm:"type:varchar(45)"`           // IPv4 or IPv6
	UserAgent    string         `gorm:"type:varchar(512)"`
	CreatedAt    time.Time
}

// TableName returns the table name for GORM.
func (AuditLog) TableName() string {
	return "audit_logs"
}
