/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import "time"

// PlayoutQueueDecision is the transient "leader popped this queue item" row
// that lockstep followers read so both control planes play the same
// user-queued track instead of one of them silently falling back to a random
// pick. The leader writes one row per pop; followers look up the most-recent
// row for the (station, mount) within ExpiresAt and return the same MediaID.
//
// See issue #240. Rows are swept by a background goroutine; ExpiresAt is set
// short enough (15s) that abandoned rows never accumulate but long enough
// that a follower running on a slightly slow tick still sees the decision.
type PlayoutQueueDecision struct {
	ID                string    `gorm:"type:uuid;primaryKey"`
	StationID         string    `gorm:"type:uuid;index:idx_playout_queue_decisions_station_decided,priority:1;not null"`
	MountID           string    `gorm:"type:uuid;index:idx_playout_queue_decisions_station_decided,priority:2;not null"`
	MediaID           string    `gorm:"type:uuid;not null"`
	SourceQueueItemID string    `gorm:"type:uuid;not null"`
	DecidedAt         time.Time `gorm:"index:idx_playout_queue_decisions_station_decided,priority:3,sort:desc;not null"`
	ExpiresAt         time.Time `gorm:"index;not null"`
}

// TableName returns the table name for GORM.
func (PlayoutQueueDecision) TableName() string {
	return "playout_queue_decisions"
}
