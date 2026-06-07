/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import "time"

// StationStream is a listener-facing audio stream endpoint for a station.
// One station has N streams ordered by Priority (lower = higher preference).
// The custom JS player (Track B-3 / docs/superpowers/plans/2026-06-06-custom-js-player.md)
// fetches these via GET /api/v1/stations/<id>/streams and walks the list
// HQ -> LQ on persistent failure, walking back up on recovery.
//
// This is intentionally distinct from Mount: Mount describes the encoder
// pipeline output (format, bitrate, channels, sample rate, encoder preset)
// the media engine produces. StationStream describes the public URL a
// listener's browser dials, plus the human label ("HQ" / "LQ") shown in
// the player UI. A future deployment may want to advertise an HLS URL or
// a CDN-fronted URL without changing the mount that produced the bytes.
type StationStream struct {
	ID          string `gorm:"type:uuid;primaryKey"`
	StationID   string `gorm:"type:uuid;index;not null"`
	URL         string `gorm:"type:varchar(512);not null"`
	Format      string `gorm:"type:varchar(16);not null"` // "mp3", "aac", "opus"
	BitrateKbps int    `gorm:"not null;default:0"`
	Label       string `gorm:"type:varchar(32);not null"` // "HQ", "LQ", etc.
	// Priority: lower is preferred. HQ=1, LQ=2, etc. The player tries
	// the lowest-priority stream first and walks up on failure.
	Priority  int `gorm:"not null;default:0;index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
