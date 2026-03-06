/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import "time"

// PlayoutQueueItem is a station/mount-scoped runtime queue entry.
// Entries are consumed in ascending position order.
type PlayoutQueueItem struct {
	ID        string  `gorm:"type:uuid;primaryKey"`
	StationID string  `gorm:"type:uuid;index:idx_playout_queue_station_mount_position,priority:1;not null"`
	MountID   string  `gorm:"type:uuid;index:idx_playout_queue_station_mount_position,priority:2;not null"`
	MediaID   string  `gorm:"type:uuid;index;not null"`
	Position  int     `gorm:"index:idx_playout_queue_station_mount_position,priority:3;not null"`
	QueuedBy  *string `gorm:"type:uuid;index"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the table name for GORM.
func (PlayoutQueueItem) TableName() string {
	return "playout_queue_items"
}
