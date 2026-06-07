/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import "time"

// ListenerEvent is an anonymous telemetry event posted by the custom JS
// player (Track B-3, docs/superpowers/plans/2026-06-06-custom-js-player.md).
// Browsers POST these to /api/v1/listener-events when reconnect / degrade /
// upgrade / exhausted / play / stop transitions happen so operators can see
// aggregate stream-health trends in the dashboard (B-4 Chunk 9).
//
// No PII. The request socket IP is read for rate limiting only; it is never
// stored on the row. Operator-visible columns are event-type counts +
// station_id + stream_label; DurationMs is optional reconnect-recovery time.
type ListenerEvent struct {
	ID          string    `gorm:"type:uuid;primaryKey"`
	Timestamp   time.Time `gorm:"not null;index"`
	EventType   string    `gorm:"type:varchar(16);not null;index"` // "reconnect"|"degrade"|"upgrade"|"exhausted"|"play"|"stop"
	StationID   string    `gorm:"type:uuid;index;not null"`
	StreamLabel string    `gorm:"type:varchar(32);not null"` // "HQ", "LQ", ...
	// DurationMs is populated for "reconnect" events (how long the recovery
	// took, end-to-end, on the client). Nullable for non-reconnect events.
	DurationMs *int `gorm:""`
	CreatedAt  time.Time
}

// TableName pins the table name so the migration SQL and AutoMigrate agree
// regardless of GORM's pluralisation rules.
func (ListenerEvent) TableName() string { return "listener_events" }
